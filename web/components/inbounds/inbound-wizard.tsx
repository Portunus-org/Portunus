"use client"

import * as React from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { ArrowLeftIcon, ArrowRightIcon, LoaderIcon } from "lucide-react"

import { CAInstall } from "@/components/ca-install"
import { CopyButton } from "@/components/copy-button"
import { DomainStatus, useDomainCheck } from "@/components/domain-check"
import { ChoiceCard, Note, StepFrame, Task, type TaskState } from "@/components/wizard"
import { FormField, SwitchField, FormError } from "@/components/form-field"
import { NumberInput } from "@/components/number-input"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { RadioGroup } from "@/components/ui/radio-group"
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
import { FormErrorsContext, useFormErrors } from "@/lib/form-errors"
import type { MessageKey } from "@/lib/messages"
import { protocolOrder, protocols } from "@/lib/protocols"
import { useCanEdit, useSession } from "@/lib/session"

type Step = "protocol" | "camouflage" | "certificate" | "protect" | "review"

const usesTLS = (p: Protocol) => p === "hysteria2" || p === "tuic" || p === "trojan"

const protocolTags: Record<Protocol, MessageKey[]> = {
  vless: [],
  hysteria2: ["wizard.tag.recommended", "wizard.tag.cert"],
  tuic: ["wizard.tag.cert"],
  trojan: ["wizard.tag.cert"],
  shadowsocks: [],
}

// The step each field of the wizard is asked on, by its id.
const stepOfField: Record<string, Step> = {
  realityCustom: "camouflage",
  domain: "certificate",
  fileDomain: "certificate",
  serverName: "certificate",
  certPath: "certificate",
  keyPath: "certificate",
  cfToken: "certificate",
  masquerade: "protect",
  name: "review",
  port: "review",
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
  protocol: "hysteria2",
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

function CamouflageStep({ draft, set, sug }: { draft: Draft; set: (p: Partial<Draft>) => void; sug: InboundSuggestion | null }) {
  const { t } = useI18n()
  const errors = React.useContext(FormErrorsContext)
  if (!sug) return <Note tone="pending">{t("wizard.reality.measuring")}</Note>
  // With the panel opened by its own domain, that domain comes first and
  // is the one to take: the connection then shares the panel's port, and a
  // stranger gets the panel's site with its real certificate. The measured
  // sites stay below for whoever wants one of them; such a connection goes
  // on another port, because the panel's port answers by the panel's name.
  const own = sug.sharedWith
  const choose = (v: string) => {
    if (!own) return set({ reality: v })
    const port = v === own ? sug.port : draft.port === sug.port ? (sug.otherPort ?? draft.port) : draft.port
    set({ reality: v, port })
  }
  return (
    <RadioGroup value={draft.reality} onValueChange={choose} className="gap-3">
      {(sug.reality ?? []).slice(0, own ? 6 : 5).map((r) => (
        <ChoiceCard
          key={r.host}
          value={r.host}
          id={`r-${r.host}`}
          title={<span className="font-mono">{r.host}</span>}
          badges={
            <Badge variant="outline" className="px-1.5 text-muted-foreground">
              {r.host === own
                ? t("wizard.reality.recommended")
                : r.ok
                  ? t("wizard.reality.ms", { ms: r.rttMs ?? 0 })
                  : t("wizard.reality.unsuitable")}
            </Badge>
          }
          description={r.host === own ? t("wizard.reality.shared", { port: sug.port }) : !r.ok ? r.reason : undefined}
        />
      ))}
      <ChoiceCard value="custom" id="r-custom" title={t("wizard.reality.custom")} description={t("wizard.reality.customHint")}>
        {draft.reality === "custom" && (
          <Input
            id="realityCustom"
            aria-invalid={!!errors.realityCustom}
            className="mt-1"
            value={draft.realityCustom}
            onChange={(e) => set({ realityCustom: e.target.value })}
            placeholder="www.example.com"
            autoCapitalize="none"
            spellCheck={false}
            autoFocus
          />
        )}
        {draft.reality === "custom" && <FormError>{errors.realityCustom}</FormError>}
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
  const [verify, setVerify] = React.useState<{
    state: "idle" | "pending" | "ok" | "fail"
    message?: string
  }>({ state: "idle" })
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
          <p className="text-muted-foreground">
            {t("wizard.cert.via.proxy", {
              server: plan.httpServer80 || "nginx",
              domain,
            })}
          </p>
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
            <FormField
              id="cfToken"
              label={t("wizard.cert.cloudflareToken")}
              hint={plan.cloudflareTokenSet ? t("wizard.cert.cloudflareTokenSet") : t("wizard.cert.cloudflareTokenHint")}
            >
              <Input
                id="cfToken"
                type="password"
                autoComplete="off"
                value={draft.cloudflareToken}
                onChange={(e) => set({ cloudflareToken: e.target.value })}
                placeholder={plan.cloudflareTokenSet ? "••••••••" : undefined}
              />
            </FormField>
          )}
        </>
      )}
      {isAdmin ? (
        <Button
          variant="link"
          size="sm"
          className="h-auto w-fit px-0 text-muted-foreground"
          onClick={() =>
            set({
              challenge: draft.challenge === "cloudflare" ? httpChallenge : "cloudflare",
            })
          }
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
  if (!sug)
    return (
      <div className="grid gap-3">
        <Skeleton className="h-16 w-full rounded-lg" />
        <Skeleton className="h-16 w-full rounded-lg" />
      </div>
    )
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
          <FormField id="domain" label={t("wizard.cert.domain")}>
            <Input
              id="domain"
              value={draft.domain}
              onChange={(e) => set({ domain: e.target.value })}
              placeholder="vpn.example.com"
              autoCapitalize="none"
              spellCheck={false}
            />
            <DomainStatus domain={draft.domain} status={dns} ip={sug.publicHost} />
          </FormField>
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
          <FormField id="serverName" label={t("wizard.cert.serverName")} hint={t("wizard.cert.serverNameHint")}>
            <Input
              id="serverName"
              value={draft.serverName}
              onChange={(e) => set({ serverName: e.target.value })}
              autoCapitalize="none"
              spellCheck={false}
            />
          </FormField>
          <SwitchField
            id="strict"
            label={t("wizard.cert.strict")}
            hint={t("wizard.cert.strictHint")}
            checked={draft.strictTls}
            onCheckedChange={(v) => set({ strictTls: v })}
          />
        </div>
      )}

      <ChoiceCard value="file" id="c-file" title={t("wizard.cert.file")} description={t("wizard.cert.fileDesc")} />
      {draft.tlsMode === "file" && (
        <div className="grid gap-4 duration-300 animate-in fade-in">
          <FormField id="fileDomain" label={t("wizard.cert.domain")}>
            <Input
              id="fileDomain"
              value={draft.domain}
              onChange={(e) => set({ domain: e.target.value })}
              placeholder="vpn.example.com"
              autoCapitalize="none"
              spellCheck={false}
            />
          </FormField>
          <FormField id="certPath" label={t("wizard.cert.certPath")}>
            <Input
              id="certPath"
              value={draft.certificatePath}
              onChange={(e) => set({ certificatePath: e.target.value })}
              placeholder={`/certs/live/${draft.domain.trim() || "vpn.example.com"}/fullchain.pem`}
              spellCheck={false}
            />
          </FormField>
          <FormField id="keyPath" label={t("wizard.cert.keyPath")} hint={t("wizard.cert.pathsHint")}>
            <Input
              id="keyPath"
              value={draft.keyPath}
              onChange={(e) => set({ keyPath: e.target.value })}
              placeholder={`/certs/live/${draft.domain.trim() || "vpn.example.com"}/privkey.pem`}
              spellCheck={false}
            />
          </FormField>
        </div>
      )}
    </RadioGroup>
  )
}

function ProtectStep({ draft, set }: { draft: Draft; set: (p: Partial<Draft>) => void }) {
  const { t } = useI18n()
  return (
    <>
      <FormField id="masquerade" label={t("wizard.masquerade")} hint={t("wizard.masqueradeHint")}>
        <Input
          id="masquerade"
          value={draft.masquerade}
          onChange={(e) => set({ masquerade: e.target.value })}
          placeholder="https://www.example.com"
          autoCapitalize="none"
          spellCheck={false}
        />
      </FormField>
      <SwitchField
        id="obfs"
        label={t("wizard.obfs")}
        hint={t("wizard.obfsHint")}
        checked={draft.obfs}
        onCheckedChange={(v) => set({ obfs: v })}
      />
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
  const name = d.tlsMode === "self-signed" ? d.serverName.trim() : d.domain.trim()
  const source = t(d.tlsMode === "acme" ? "wizard.cert.acme" : d.tlsMode === "file" ? "wizard.cert.file" : "wizard.cert.private")
  return (
    <span className="grid">
      <span>{source}</span>
      <span className="text-muted-foreground">{name}</span>
    </span>
  )
}

function ReviewStep({ draft, set, sug }: { draft: Draft; set: (p: Partial<Draft>) => void; sug: InboundSuggestion | null }) {
  const { t } = useI18n()
  const p = draft.protocol
  return (
    <>
      <div className="grid grid-cols-[1fr_7rem] gap-4">
        <FormField id="name" label={t("wizard.review.name")}>
          <Input
            id="name"
            value={draft.name}
            onChange={(e) => set({ name: e.target.value })}
            placeholder={protocols[p].label}
            maxLength={64}
          />
        </FormField>
        <FormField id="port" label={t("wizard.review.port")}>
          <NumberInput
            id="port"
            min={1}
            max={65535}
            className="tabular-nums"
            value={draft.port || ""}
            onChange={(e) => set({ port: Number(e.target.value) })}
          />
        </FormField>
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
            <Row label={t("wizard.review.masquerade")}>{draft.masquerade || t("wizard.review.off")}</Row>
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

/**
 * What changes when the wizard is a part of the first setup and not a page
 * of its own: other steps follow it, it can be skipped, and what it made is
 * handed on instead of being shown.
 */
export interface WizardGuide {
  /** Steps of the setup that come after the wizard's own. */
  more: number
  /** steps: how many the wizard itself took for this protocol. */
  onCreated: (inbound: Inbound, steps: number) => void
  onSkip: () => void
}

/** The wizard for a new connection: steps that depend on the protocol. */
export function InboundWizard({ guide }: { guide?: WizardGuide }) {
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
  const { errors, clear, fail } = useFormErrors()
  const [created, setCreated] = React.useState<Inbound | null>(null)

  const set = React.useCallback((p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p })), [])
  const sug = suggestions[draft.protocol] ?? null

  React.useEffect(() => {
    if (!canEdit && !guide) router.replace("/inbounds/")
  }, [canEdit, guide, router])

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
      (err) => alive && fail(err)
    )
    return () => {
      alive = false
    }
  }, [draft.protocol, suggestions, fail])

  const steps = stepsFor(draft.protocol)
  const index = steps.indexOf(step)

  function chooseProtocol(p: Protocol) {
    const s = suggestions[p]
    setDraft({
      ...emptyDraft,
      protocol: p,
      port: protocols[p].defaultPort,
      ...(s ? applySuggestion({ ...emptyDraft, protocol: p }, s) : {}),
    })
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
    clear()
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
      const inbound = await api.createInbound(input)
      if (guide) guide.onCreated(inbound, steps.length)
      else setCreated(inbound)
    } catch (err) {
      // Everything is sent from the last step; what the server refuses is
      // shown at its field, on the step that asked for it.
      const field = fail(err, Object.keys(stepOfField), {
        realityServer: d.reality === "custom" ? "realityCustom" : "-",
        serverName: d.tlsMode === "self-signed" ? "serverName" : d.tlsMode === "file" ? "fileDomain" : "domain",
        certificatePath: "certPath",
        certificate: "certPath",
        masqueradeUrl: "masquerade",
        cloudflareToken: "cfToken",
      })
      if (field in stepOfField) setStep(stepOfField[field])
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
          total={steps.length + (guide?.more ?? 0)}
          title={t(titles[step][0])}
          lead={titles[step][1]}
          aside={
            guide && (
              <button type="button" onClick={guide.onSkip} className="hover:text-foreground hover:underline">
                {t("common.skip")}
              </button>
            )
          }
          footer={
            <>
              {index === 0 ? (
                guide ? (
                  <span />
                ) : (
                  <Button variant="ghost" asChild>
                    <Link href="/inbounds/">{t("common.cancel")}</Link>
                  </Button>
                )
              ) : (
                <Button
                  variant="ghost"
                  onClick={() => {
                    clear()
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
          <FormErrorsContext.Provider value={errors}>
            <div className="contents" onInput={clear}>
              {step === "protocol" && <ProtocolStep value={draft.protocol} onChange={chooseProtocol} />}
              {step === "camouflage" && <CamouflageStep draft={draft} set={set} sug={sug} />}
              {step === "certificate" && <CertificateStep draft={draft} set={set} sug={sug} isAdmin={isAdmin} />}
              {step === "protect" && <ProtectStep draft={draft} set={set} />}
              {step === "review" && <ReviewStep draft={draft} set={set} sug={sug} />}
            </div>
          </FormErrorsContext.Provider>
          <FormError>{errors.form}</FormError>
        </StepFrame>
      )}
    </div>
  )
}
