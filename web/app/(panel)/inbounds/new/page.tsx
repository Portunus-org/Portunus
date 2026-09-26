"use client"

import * as React from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { ArrowLeftIcon, ArrowRightIcon, LoaderIcon } from "lucide-react"

import { CAInstall } from "@/components/ca-install"
import { CopyButton } from "@/components/copy-button"
import { DomainStatus, useDomainCheck } from "@/components/domain-check"
import { ChoiceCard, Note, StepFrame, Task, type TaskState } from "@/components/wizard"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup } from "@/components/ui/radio-group"
import { Switch } from "@/components/ui/switch"
import {
  api,
  errorText,
  type ACMEChallenge,
  type Inbound,
  type InboundInput,
  type InboundSuggestion,
  type Protocol,
  type Settings,
  type TLSMode,
} from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"
import { protocolOrder, protocols } from "@/lib/protocols"
import { useCanEdit, useSession } from "@/lib/session"

type Step = "protocol" | "camouflage" | "certificate" | "protect" | "review"

const usesTLS = (p: Protocol) => p === "hysteria2" || p === "tuic" || p === "trojan"

const protocolTags: Record<Protocol, MessageKey[]> = {
  vless: ["wizard.tag.recommended", "wizard.tag.fast"],
  hysteria2: ["wizard.tag.resilient", "wizard.tag.cert"],
  tuic: ["wizard.tag.cert"],
  trojan: ["wizard.tag.cert"],
  shadowsocks: ["wizard.tag.plain"],
}

function stepsFor(p: Protocol): Step[] {
  const s: Step[] = ["protocol"]
  if (p === "vless") s.push("camouflage")
  if (usesTLS(p)) s.push("certificate")
  if (p === "hysteria2") s.push("protect")
  s.push("review")
  return s
}

function nginxSnippet(port: number) {
  return `location /.well-known/acme-challenge/ {
    proxy_pass http://127.0.0.1:${port};
    proxy_set_header Host $host;
}`
}

/** Everything the wizard collects; turned into an InboundInput at the end. */
interface Draft {
  protocol: Protocol
  name: string
  port: number
  reality: string
  realityCustom: string
  tlsMode: TLSMode
  domain: string
  challenge: ACMEChallenge
  cloudflareToken: string
  certificatePath: string
  keyPath: string
  serverName: string
  strictTls: boolean
  masquerade: string
  obfs: boolean
}

const emptyDraft: Draft = {
  protocol: "vless",
  name: "",
  port: 443,
  reality: "",
  realityCustom: "",
  tlsMode: "self-signed",
  domain: "",
  challenge: "http",
  cloudflareToken: "",
  certificatePath: "",
  keyPath: "",
  serverName: "",
  strictTls: false,
  masquerade: "",
  obfs: false,
}

/** Fills the draft with what the server suggested for the protocol. */
function applySuggestion(d: Draft, s: InboundSuggestion): Draft {
  const best = s.reality?.find((r) => r.ok)?.host ?? ""
  return {
    ...d,
    port: s.port || protocols[d.protocol].defaultPort,
    reality: best || "custom",
    realityCustom: best ? "" : "www.microsoft.com",
    tlsMode: (s.certificate?.recommended as TLSMode) ?? "self-signed",
    domain: s.domain,
    challenge: s.certificate?.challenge ?? "http",
    serverName: s.domain || s.publicHost,
    masquerade: s.masquerade ?? "",
  }
}

// ----------------------------------------------------------------- steps

function ProtocolStep({ value, onChange }: { value: Protocol; onChange: (p: Protocol) => void }) {
  const { t } = useI18n()
  return (
    <RadioGroup value={value} onValueChange={(v) => onChange(v as Protocol)} className="gap-3">
      {protocolOrder.map((p) => (
        <ChoiceCard
          key={p}
          value={p}
          id={`p-${p}`}
          title={protocols[p].label}
          description={t(`wizard.protocol.${p}` as MessageKey)}
          badges={
            <>
              <Badge variant="outline" className="px-1.5 text-muted-foreground">
                {protocols[p].transport}
              </Badge>
              {protocolTags[p].map((tag) => (
                <Badge key={tag} variant="outline" className="px-1.5 text-muted-foreground">
                  {t(tag)}
                </Badge>
              ))}
            </>
          }
        />
      ))}
    </RadioGroup>
  )
}

function CamouflageStep({
  draft,
  set,
  sug,
}: {
  draft: Draft
  set: (p: Partial<Draft>) => void
  sug: InboundSuggestion | null
}) {
  const { t } = useI18n()
  if (!sug) return <Note tone="pending">{t("wizard.reality.measuring")}</Note>
  return (
    <RadioGroup value={draft.reality} onValueChange={(v) => set({ reality: v })} className="gap-3">
      {(sug.reality ?? []).slice(0, 5).map((r) => (
        <ChoiceCard
          key={r.host}
          value={r.host}
          id={`r-${r.host}`}
          title={<span className="font-mono">{r.host}</span>}
          badges={
            <Badge variant="outline" className="px-1.5 text-muted-foreground">
              {r.ok ? t("wizard.reality.ms", { ms: r.rttMs ?? 0 }) : t("wizard.reality.unsuitable")}
            </Badge>
          }
          description={!r.ok ? r.reason : undefined}
        />
      ))}
      <ChoiceCard value="custom" id="r-custom" title={t("wizard.reality.custom")} description={t("wizard.reality.customHint")}>
        {draft.reality === "custom" && (
          <Input
            className="mt-1"
            value={draft.realityCustom}
            onChange={(e) => set({ realityCustom: e.target.value })}
            placeholder="www.example.com"
            autoCapitalize="none"
            spellCheck={false}
            autoFocus
          />
        )}
      </ChoiceCard>
    </RadioGroup>
  )
}

function ChallengeForm({
  draft,
  set,
  sug,
  isAdmin,
}: {
  draft: Draft
  set: (p: Partial<Draft>) => void
  sug: InboundSuggestion
  isAdmin: boolean
}) {
  const { t } = useI18n()
  const plan = sug.certificate!
  const [verify, setVerify] = React.useState<{ state: "idle" | "pending" | "ok" | "fail"; message?: string }>({ state: "idle" })
  const domain = draft.domain.trim() || "vpn.example.com"
  const port = plan.proxyPort || 8088
  // Back from Cloudflare: straight HTTP when port 80 is free, else through
  // the reverse proxy that holds it.
  const httpChallenge: ACMEChallenge = plan.challenge !== "cloudflare" ? plan.challenge : plan.httpServer80 ? "proxy" : "http"

  async function check() {
    setVerify({ state: "pending" })
    const r = await api.verifyProxy(draft.domain.trim(), port).catch((e) => ({ ok: false, message: errorText(e, t) }))
    setVerify(r.ok ? { state: "ok" } : { state: "fail", message: r.message })
  }

  return (
    <div className="grid gap-3 rounded-lg border p-4 text-sm">
      <span className="font-medium">{t("wizard.cert.via")}</span>
      {draft.challenge === "http" && <p className="text-muted-foreground">{t("wizard.cert.via.http")}</p>}
      {draft.challenge === "proxy" && (
        <>
          <p className="text-muted-foreground">{t("wizard.cert.via.proxy", { server: plan.httpServer80 || "nginx", domain })}</p>
          <div className="relative">
            <pre className="overflow-x-auto rounded-lg bg-muted p-4 pr-12 font-mono text-xs leading-relaxed">{nginxSnippet(port)}</pre>
            <CopyButton value={nginxSnippet(port)} className="absolute top-2 right-2 text-muted-foreground" />
          </div>
          <p className="text-muted-foreground">{t("wizard.cert.via.npm", { domain })}</p>
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="outline" size="sm" onClick={check} disabled={verify.state === "pending" || !draft.domain.trim()}>
              {verify.state === "pending" ? t("wizard.cert.verifying") : t("wizard.cert.verify")}
            </Button>
            {verify.state === "ok" && <Note tone="ok">{t("wizard.cert.verifyOk")}</Note>}
            {verify.state === "fail" && <Note tone="error">{t("wizard.cert.verifyFail", { message: verify.message ?? "" })}</Note>}
          </div>
        </>
      )}
      {draft.challenge === "cloudflare" && (
        <>
          <p className="text-muted-foreground">{t("wizard.cert.via.cloudflare")}</p>
          {isAdmin && (
            <div className="grid gap-2">
              <Label htmlFor="cfToken">{t("wizard.cert.cloudflareToken")}</Label>
              <Input
                id="cfToken"
                type="password"
                autoComplete="off"
                value={draft.cloudflareToken}
                onChange={(e) => set({ cloudflareToken: e.target.value })}
                placeholder={plan.cloudflareTokenSet ? "••••••••" : undefined}
              />
              <p className="text-muted-foreground">
                {plan.cloudflareTokenSet ? t("wizard.cert.cloudflareTokenSet") : t("wizard.cert.cloudflareTokenHint")}
              </p>
            </div>
          )}
        </>
      )}
      {isAdmin ? (
        <Button
          variant="link"
          size="sm"
          className="h-auto w-fit px-0 text-muted-foreground"
          onClick={() => set({ challenge: draft.challenge === "cloudflare" ? httpChallenge : "cloudflare" })}
        >
          {t(draft.challenge === "cloudflare" ? "wizard.cert.via.useHttp" : "wizard.cert.via.useCloudflare")}
        </Button>
      ) : (
        !plan.challengeSaved && <p className="text-muted-foreground">{t("wizard.cert.via.adminOnly")}</p>
      )}
    </div>
  )
}

function CertificateStep({
  draft,
  set,
  sug,
  isAdmin,
}: {
  draft: Draft
  set: (p: Partial<Draft>) => void
  sug: InboundSuggestion | null
  isAdmin: boolean
}) {
  const { t } = useI18n()
  const dns = useDomainCheck(draft.tlsMode === "acme" ? draft.domain : "", sug?.domainCheck)
  if (!sug) return <Note tone="pending">{t("common.loading")}</Note>
  const recommended = (
    <Badge variant="secondary" className="px-1.5">
      {t("wizard.cert.recommended")}
    </Badge>
  )
  const rec = sug.certificate?.recommended

  return (
    <RadioGroup value={draft.tlsMode} onValueChange={(v) => set({ tlsMode: v as TLSMode })} className="gap-3">
      <ChoiceCard
        value="acme"
        id="c-acme"
        title={t("wizard.cert.acme")}
        description={t("wizard.cert.acmeDesc")}
        badges={rec === "acme" && recommended}
      />
      {draft.tlsMode === "acme" && (
        <div className="grid gap-4 duration-300 animate-in fade-in">
          <div className="grid gap-2">
            <Label htmlFor="domain">{t("wizard.cert.domain")}</Label>
            <Input
              id="domain"
              value={draft.domain}
              onChange={(e) => set({ domain: e.target.value })}
              placeholder="vpn.example.com"
              autoCapitalize="none"
              spellCheck={false}
            />
            <DomainStatus domain={draft.domain} status={dns} ip={sug.publicHost} />
          </div>
          <ChallengeForm draft={draft} set={set} sug={sug} isAdmin={isAdmin} />
        </div>
      )}

      <ChoiceCard
        value="self-signed"
        id="c-private"
        title={t("wizard.cert.private")}
        description={t("wizard.cert.privateDesc")}
        badges={rec !== "acme" && recommended}
      />
      {draft.tlsMode === "self-signed" && (
        <div className="grid gap-4 duration-300 animate-in fade-in">
          <div className="grid gap-2">
            <Label htmlFor="serverName">{t("wizard.cert.serverName")}</Label>
            <Input
              id="serverName"
              value={draft.serverName}
              onChange={(e) => set({ serverName: e.target.value })}
              autoCapitalize="none"
              spellCheck={false}
            />
            <p className="text-sm text-muted-foreground">{t("wizard.cert.serverNameHint")}</p>
          </div>
          <div className="flex items-start justify-between gap-4">
            <div className="grid gap-1.5">
              <Label htmlFor="strict">{t("wizard.cert.strict")}</Label>
              <p className="text-sm text-muted-foreground">{t("wizard.cert.strictHint")}</p>
            </div>
            <Switch id="strict" checked={draft.strictTls} onCheckedChange={(v) => set({ strictTls: v })} />
          </div>
        </div>
      )}

      <ChoiceCard value="file" id="c-file" title={t("wizard.cert.file")} description={t("wizard.cert.fileDesc")} />
      {draft.tlsMode === "file" && (
        <div className="grid gap-4 duration-300 animate-in fade-in">
          <div className="grid gap-2">
            <Label htmlFor="fileDomain">{t("wizard.cert.domain")}</Label>
            <Input
              id="fileDomain"
              value={draft.domain}
              onChange={(e) => set({ domain: e.target.value })}
              placeholder="vpn.example.com"
              autoCapitalize="none"
              spellCheck={false}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="certPath">{t("wizard.cert.certPath")}</Label>
            <Input
              id="certPath"
              value={draft.certificatePath}
              onChange={(e) => set({ certificatePath: e.target.value })}
              placeholder={`/certs/live/${draft.domain.trim() || "vpn.example.com"}/fullchain.pem`}
              spellCheck={false}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="keyPath">{t("wizard.cert.keyPath")}</Label>
            <Input
              id="keyPath"
              value={draft.keyPath}
              onChange={(e) => set({ keyPath: e.target.value })}
              placeholder={`/certs/live/${draft.domain.trim() || "vpn.example.com"}/privkey.pem`}
              spellCheck={false}
            />
            <p className="text-sm text-muted-foreground">{t("wizard.cert.pathsHint")}</p>
          </div>
        </div>
      )}
    </RadioGroup>
  )
}

function ProtectStep({ draft, set }: { draft: Draft; set: (p: Partial<Draft>) => void }) {
  const { t } = useI18n()
  return (
    <>
      <div className="grid gap-2">
        <Label htmlFor="masquerade">{t("wizard.masquerade")}</Label>
        <Input
          id="masquerade"
          value={draft.masquerade}
          onChange={(e) => set({ masquerade: e.target.value })}
          placeholder="https://www.example.com"
          autoCapitalize="none"
          spellCheck={false}
        />
        <p className="text-sm text-muted-foreground">{t("wizard.masqueradeHint")}</p>
      </div>
      <div className="flex items-start justify-between gap-4">
        <div className="grid gap-1.5">
          <Label htmlFor="obfs">{t("wizard.obfs")}</Label>
          <p className="text-sm text-muted-foreground">{t("wizard.obfsHint")}</p>
        </div>
        <Switch id="obfs" checked={draft.obfs} onCheckedChange={(v) => set({ obfs: v })} />
      </div>
    </>
  )
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 py-2.5">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate text-right">{children}</span>
    </div>
  )
}

function certLabel(d: Draft, t: ReturnType<typeof useI18n>["t"]) {
  if (d.tlsMode === "acme") return `${t("wizard.cert.acme")} · ${d.domain.trim()}`
  if (d.tlsMode === "file") return `${t("wizard.cert.file")} · ${d.domain.trim()}`
  return `${t("wizard.cert.private")} · ${d.serverName.trim()}`
}

function ReviewStep({ draft, set, sug }: { draft: Draft; set: (p: Partial<Draft>) => void; sug: InboundSuggestion | null }) {
  const { t } = useI18n()
  const p = draft.protocol
  return (
    <>
      <div className="grid grid-cols-[1fr_7rem] gap-4">
        <div className="grid gap-2">
          <Label htmlFor="name">{t("wizard.review.name")}</Label>
          <Input
            id="name"
            value={draft.name}
            onChange={(e) => set({ name: e.target.value })}
            placeholder={protocols[p].label}
            maxLength={64}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="port">{t("wizard.review.port")}</Label>
          <Input
            id="port"
            type="number"
            min={1}
            max={65535}
            className="tabular-nums"
            value={draft.port || ""}
            onChange={(e) => set({ port: Number(e.target.value) })}
          />
        </div>
      </div>
      {sug?.preferredPortBusy && draft.port === sug.port && (
        <p className="-mt-3 text-sm text-muted-foreground">{t("wizard.portBusy", { port: sug.port })}</p>
      )}
      <div className="divide-y rounded-lg border px-4 text-sm">
        <Row label={t("wizard.review.protocol")}>{protocols[p].label}</Row>
        <Row label={t("wizard.review.transport")}>{protocols[p].transport}</Row>
        {p === "vless" && (
          <Row label={t("wizard.review.camouflage")}>
            <span className="font-mono">{draft.reality === "custom" ? draft.realityCustom : draft.reality}</span>
          </Row>
        )}
        {usesTLS(p) && <Row label={t("wizard.review.certificate")}>{certLabel(draft, t)}</Row>}
        {p === "hysteria2" && (
          <>
            <Row label={t("wizard.review.masquerade")}>{draft.masquerade || "—"}</Row>
            <Row label={t("wizard.review.obfs")}>{t(draft.obfs ? "wizard.review.on" : "wizard.review.off")}</Row>
          </>
        )}
      </div>
    </>
  )
}

// ------------------------------------------------------------------ done

function DoneStep({ inbound, domain }: { inbound: Inbound; domain: string }) {
  const { t } = useI18n()
  const needsCert = inbound.tlsMode === "acme"
  const [singbox, setSingbox] = React.useState<TaskState>("running")
  const [cert, setCert] = React.useState<TaskState>(needsCert ? "pending" : "done")

  React.useEffect(() => {
    let alive = true
    const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
    ;(async () => {
      for (let i = 0; i < 30 && alive; i++) {
        const s = await api.singbox().catch(() => null)
        if (s?.status.state === "running" && !s.applyError) break
        await sleep(700)
      }
      if (!alive) return
      setSingbox("done")
      if (!needsCert) return
      setCert("running")
      const began = Date.now()
      while (alive) {
        const ins = await api.inbounds().catch(() => [])
        if (ins.find((i) => i.id === inbound.id)?.certificate) {
          setCert("done")
          return
        }
        if (Date.now() - began > 10_000) setCert("slow")
        if (Date.now() - began > 180_000) return
        await sleep(2000)
      }
    })()
    return () => {
      alive = false
    }
  }, [inbound.id, needsCert])

  const name = inbound.name || protocols[inbound.protocol].label

  return (
    <div className="flex w-full max-w-xl flex-col gap-6 duration-500 animate-in fade-in">
      <Card>
        <CardHeader>
          <CardTitle className="text-xl">{t("wizard.done.title", { name })}</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="grid gap-4">
            <Task state="done" label={t("wizard.done.created")} />
            <Task state={singbox} label={t("wizard.done.running")} />
            {needsCert && (
              <Task
                state={cert}
                label={t(cert === "done" ? "wizard.done.issued" : "wizard.done.issuing", { domain })}
                note={cert === "slow" ? t("wizard.done.issuingSlow") : undefined}
              />
            )}
          </ul>
        </CardContent>
      </Card>

      {inbound.tlsMode === "self-signed" && (
        <Card>
          <CardHeader>
            <CardTitle>{t("ca.title")}</CardTitle>
            <CardDescription>{t("ca.lead")}</CardDescription>
          </CardHeader>
          <CardContent>
            <CAInstall compact />
          </CardContent>
        </Card>
      )}

      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="outline" asChild>
          <Link href="/inbounds/">{t("wizard.done.finish")}</Link>
        </Button>
        <Button asChild>
          <Link href="/clients/">
            {t("wizard.done.clients")}
            <ArrowRightIcon />
          </Link>
        </Button>
      </div>
    </div>
  )
}

// ------------------------------------------------------------------ flow

export default function NewInboundPage() {
  const { t } = useI18n()
  const router = useRouter()
  const canEdit = useCanEdit()
  const { user } = useSession()
  const isAdmin = user.role === "admin"

  const [draft, setDraft] = React.useState<Draft>(emptyDraft)
  const [step, setStep] = React.useState<Step>("protocol")
  const [suggestions, setSuggestions] = React.useState<Partial<Record<Protocol, InboundSuggestion>>>({})
  const [settings, setSettings] = React.useState<Settings | null>(null)
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [created, setCreated] = React.useState<Inbound | null>(null)

  const set = React.useCallback((p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p })), [])
  const sug = suggestions[draft.protocol] ?? null

  React.useEffect(() => {
    if (!canEdit) router.replace("/inbounds/")
  }, [canEdit, router])

  React.useEffect(() => {
    api.settings().then(setSettings, () => {})
  }, [])

  // Ask the server for the chosen protocol's suggestion (ports, probes,
  // certificate plan) as soon as it is picked; the answer fills the draft.
  React.useEffect(() => {
    const p = draft.protocol
    if (suggestions[p]) return
    let alive = true
    api.suggestInbound(p).then(
      (s) => {
        if (!alive) return
        setSuggestions((all) => ({ ...all, [p]: s }))
        setDraft((d) => (d.protocol === p ? applySuggestion(d, s) : d))
      },
      (err) => alive && setError(errorText(err, t))
    )
    return () => {
      alive = false
    }
  }, [draft.protocol, suggestions, t])

  const steps = stepsFor(draft.protocol)
  const index = steps.indexOf(step)

  function chooseProtocol(p: Protocol) {
    const s = suggestions[p]
    setDraft({ ...emptyDraft, protocol: p, port: protocols[p].defaultPort, ...(s ? applySuggestion({ ...emptyDraft, protocol: p }, s) : {}) })
  }

  // Whether the current step has what it needs to go on.
  const ready = (() => {
    switch (step) {
      case "protocol":
        return true
      case "camouflage":
        return !!sug && (draft.reality !== "custom" || draft.realityCustom.trim().includes("."))
      case "certificate":
        if (!sug) return false
        if (draft.tlsMode === "acme") {
          if (!draft.domain.trim()) return false
          return draft.challenge !== "cloudflare" || !isAdmin || !!sug.certificate?.cloudflareTokenSet || !!draft.cloudflareToken.trim()
        }
        if (draft.tlsMode === "file") return !!(draft.domain.trim() && draft.certificatePath.trim() && draft.keyPath.trim())
        return !!draft.serverName.trim()
      case "protect":
        return true
      case "review":
        return draft.port > 0 && draft.port <= 65535
    }
  })()

  async function create() {
    setPending(true)
    setError(null)
    const d = draft
    const tls = usesTLS(d.protocol)
    try {
      // The certificate check method is a server-wide setting: store it
      // when the admin chose or confirmed one here.
      if (tls && d.tlsMode === "acme" && isAdmin && settings) {
        const plan = sug?.certificate
        if (!plan?.challengeSaved || plan.challenge !== d.challenge || d.cloudflareToken.trim() || !settings.domain) {
          setSettings(
            await api.updateSettings({
              publicHost: settings.publicHost,
              subscriptionBaseUrl: settings.subscriptionBaseUrl,
              acmeEmail: settings.acmeEmail,
              acmeChallenge: d.challenge,
              acmeHttpPort: settings.acmeHttpPort,
              cloudflareToken: d.cloudflareToken.trim() || undefined,
              domain: settings.domain || d.domain.trim(),
            })
          )
        }
      }
      const input: InboundInput = {
        protocol: d.protocol,
        name: d.name.trim(),
        port: d.port,
        enabled: true,
      }
      if (d.protocol === "vless") {
        input.realityServer = d.reality === "custom" ? d.realityCustom.trim() : d.reality
        input.realityServerPort = 443
      }
      if (tls) {
        input.tlsMode = d.tlsMode
        input.serverName = d.tlsMode === "self-signed" ? d.serverName.trim() : d.domain.trim()
        if (d.tlsMode === "file") {
          input.certificatePath = d.certificatePath.trim()
          input.keyPath = d.keyPath.trim()
        }
        if (d.tlsMode === "self-signed") input.strictTls = d.strictTls
      }
      if (d.protocol === "hysteria2") {
        input.masqueradeUrl = d.masquerade.trim()
        input.obfs = d.obfs
      }
      setCreated(await api.createInbound(input))
    } catch (err) {
      setError(errorText(err, t))
    } finally {
      setPending(false)
    }
  }

  if (!canEdit) return null

  const titles: Record<Step, [MessageKey, string]> = {
    protocol: ["wizard.protocol.title", t("wizard.protocol.lead")],
    camouflage: ["wizard.reality.title", t("wizard.reality.lead")],
    certificate: ["wizard.cert.title", t("wizard.cert.lead", { protocol: protocols[draft.protocol].label })],
    protect: ["wizard.protect.title", t("wizard.protect.lead")],
    review: ["wizard.review.title", t("wizard.review.lead")],
  }

  return (
    <div className="flex justify-center px-4 lg:px-6">
      {created ? (
        <DoneStep inbound={created} domain={draft.domain.trim()} />
      ) : (
        <StepFrame
          step={index + 1}
          total={steps.length}
          title={t(titles[step][0])}
          lead={titles[step][1]}
          footer={
            <>
              {index === 0 ? (
                <Button variant="ghost" asChild>
                  <Link href="/inbounds/">{t("common.cancel")}</Link>
                </Button>
              ) : (
                <Button
                  variant="ghost"
                  onClick={() => {
                    setError(null)
                    setStep(steps[index - 1])
                  }}
                >
                  <ArrowLeftIcon />
                  {t("common.back")}
                </Button>
              )}
              {step === "review" ? (
                <Button onClick={create} disabled={pending || !ready}>
                  {pending ? (
                    <>
                      <LoaderIcon className="animate-spin" />
                      {t("wizard.creating")}
                    </>
                  ) : (
                    t("wizard.create")
                  )}
                </Button>
              ) : (
                <Button onClick={() => setStep(steps[index + 1])} disabled={!ready}>
                  {t("common.next")}
                  <ArrowRightIcon />
                </Button>
              )}
            </>
          }
        >
          {step === "protocol" && <ProtocolStep value={draft.protocol} onChange={chooseProtocol} />}
          {step === "camouflage" && <CamouflageStep draft={draft} set={set} sug={sug} />}
          {step === "certificate" && <CertificateStep draft={draft} set={set} sug={sug} isAdmin={isAdmin} />}
          {step === "protect" && <ProtectStep draft={draft} set={set} />}
          {step === "review" && <ReviewStep draft={draft} set={set} sug={sug} />}
          {error && <p className="text-sm text-destructive">{error}</p>}
        </StepFrame>
      )}
    </div>
  )
}
