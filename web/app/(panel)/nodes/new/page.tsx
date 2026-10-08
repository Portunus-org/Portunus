"use client"

import * as React from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { ArrowLeftIcon, ArrowRightIcon, LoaderIcon } from "lucide-react"

import { JoinCommand } from "@/components/nodes/join-command"
import { RecoveryKey } from "@/components/nodes/recovery-key"
import { ChoiceCard, StepFrame, Task } from "@/components/wizard"
import { FormField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup } from "@/components/ui/radio-group"
import { Textarea } from "@/components/ui/textarea"
import { api, errorText, type Node, type NodeKind } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useSession } from "@/lib/session"

type Step = "type" | "details"

// ------------------------------------------------------------------ done

/** After creation: the command to run, and a live wait for the server. */
function AgentDone({ node: created }: { node: Node }) {
  const { t } = useI18n()
  const [node, setNode] = React.useState(created)

  React.useEffect(() => {
    let alive = true
    let timer: ReturnType<typeof setTimeout>
    // One check at a time: an unreachable server takes a while to give up on.
    const poll = async () => {
      const nodes = await api.checkNodes().catch(() => null)
      if (!alive) return
      const fresh = nodes?.find((n) => n.id === created.id)
      if (fresh) setNode(fresh)
      if (!(fresh?.joined && fresh.status.state === "online")) timer = setTimeout(poll, 4000)
    }
    timer = setTimeout(poll, 4000)
    return () => {
      alive = false
      clearTimeout(timer)
    }
  }, [created.id])

  const online = node.joined && node.status.state === "online"
  const rejected = node.status.state === "rejected"

  return (
    <div className="flex w-full max-w-xl flex-col gap-6 duration-500 animate-in fade-in">
      <Card>
        <CardHeader>
          <CardTitle className="text-xl">{t("nodeWizard.run.title")}</CardTitle>
          <CardDescription>{t("nodeWizard.run.lead", { address: node.address })}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6">
          <JoinCommand node={node} />
          {node.standby && <RecoveryKey node={node} />}
          <ul className="grid gap-4 border-t pt-6">
            <Task
              state={online ? "done" : rejected ? "failed" : "running"}
              label={t(online ? "nodeWizard.run.joined" : "nodeWizard.run.waiting")}
              note={
                rejected
                  ? t("nodeWizard.run.problem", {
                      message: node.status.error ?? "",
                    })
                  : undefined
              }
            />
          </ul>
        </CardContent>
      </Card>
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="outline" asChild>
          <Link href="/nodes/">{t("common.done")}</Link>
        </Button>
        {node.isExit && (
          <Button asChild>
            <Link href="/routing/">
              {t("nodeWizard.toRouting")}
              <ArrowRightIcon />
            </Link>
          </Button>
        )}
      </div>
    </div>
  )
}

/** After importing a link: measure the proxy so the result is visible. */
function LinkDone({ node: created }: { node: Node }) {
  const { t } = useI18n()
  const [node, setNode] = React.useState(created)
  const [tries, setTries] = React.useState(0)
  const measured = node.status.latencyMs !== undefined
  const gaveUp = !measured && tries >= 5

  React.useEffect(() => {
    if (measured || gaveUp) return
    let alive = true
    // The first check comes a little later: sing-box is loading the new outbound.
    const timer = setTimeout(async () => {
      const nodes = await api.checkNodes().catch(() => null)
      const fresh = nodes?.find((n) => n.id === created.id)
      if (!alive) return
      if (fresh) setNode(fresh)
      setTries((n) => n + 1)
    }, 3000)
    return () => {
      alive = false
      clearTimeout(timer)
    }
  }, [created.id, measured, gaveUp, tries])

  return (
    <div className="flex w-full max-w-xl flex-col gap-6 duration-500 animate-in fade-in">
      <Card>
        <CardHeader>
          <CardTitle className="text-xl">{t("nodeWizard.linkDone.title", { name: node.name })}</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="grid gap-4">
            <Task
              state={measured ? "done" : gaveUp ? "failed" : "running"}
              label={
                measured
                  ? t("nodeWizard.linkDone.ok", {
                      ms: node.status.latencyMs ?? 0,
                    })
                  : t(gaveUp ? "nodes.state.offline" : "nodeWizard.linkDone.measuring")
              }
              note={gaveUp ? t("nodeWizard.linkDone.fail") : undefined}
            />
          </ul>
        </CardContent>
      </Card>
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="outline" asChild>
          <Link href="/nodes/">{t("common.done")}</Link>
        </Button>
        <Button asChild>
          <Link href="/routing/">
            {t("nodeWizard.toRouting")}
            <ArrowRightIcon />
          </Link>
        </Button>
      </div>
    </div>
  )
}

// ------------------------------------------------------------------ flow

export default function NewNodePage() {
  const { t } = useI18n()
  const router = useRouter()
  const isAdmin = useSession().user.role === "admin"

  const [step, setStep] = React.useState<Step>("type")
  const [kind, setKind] = React.useState<NodeKind>("agent")
  const [name, setName] = React.useState("")
  const [address, setAddress] = React.useState("")
  const [isExit, setIsExit] = React.useState(true)
  const [isEntry, setIsEntry] = React.useState(false)
  const [standby, setStandby] = React.useState(false)
  const [link, setLink] = React.useState("")
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [created, setCreated] = React.useState<Node | null>(null)

  React.useEffect(() => {
    if (!isAdmin) router.replace("/nodes/")
  }, [isAdmin, router])

  const ready = step === "type" || (kind === "agent" ? !!name.trim() && !!address.trim() && (isExit || isEntry || standby) : !!link.trim())

  async function create() {
    setPending(true)
    setError(null)
    try {
      setCreated(
        await api.createNode(
          kind === "agent"
            ? {
                kind,
                name: name.trim(),
                address: address.trim(),
                isExit,
                isEntry,
                standby,
              }
            : { kind, name: name.trim(), link: link.trim() }
        )
      )
    } catch (err) {
      setError(errorText(err, t))
    } finally {
      setPending(false)
    }
  }

  if (!isAdmin) return null

  if (created) {
    return (
      <div className="flex justify-center px-4 lg:px-6">
        {created.kind === "agent" ? <AgentDone node={created} /> : <LinkDone node={created} />}
      </div>
    )
  }

  return (
    <div className="flex justify-center px-4 lg:px-6">
      <StepFrame
        step={step === "type" ? 1 : 2}
        total={2}
        title={t(step === "type" ? "nodeWizard.type.title" : kind === "agent" ? "nodeWizard.server.title" : "nodeWizard.link.title")}
        lead={t(step === "type" ? "nodeWizard.type.lead" : kind === "agent" ? "nodeWizard.server.lead" : "nodeWizard.link.lead")}
        footer={
          <>
            {step === "type" ? (
              <Button variant="ghost" asChild>
                <Link href="/nodes/">{t("common.cancel")}</Link>
              </Button>
            ) : (
              <Button
                variant="ghost"
                onClick={() => {
                  setError(null)
                  setStep("type")
                }}
              >
                <ArrowLeftIcon />
                {t("common.back")}
              </Button>
            )}
            {step === "type" ? (
              <Button onClick={() => setStep("details")}>
                {t("common.next")}
                <ArrowRightIcon />
              </Button>
            ) : (
              <Button onClick={create} disabled={pending || !ready}>
                {pending && <LoaderIcon className="animate-spin" />}
                {t("nodeWizard.create")}
              </Button>
            )}
          </>
        }
      >
        {step === "type" && (
          <RadioGroup value={kind} onValueChange={(v) => setKind(v as NodeKind)} className="gap-3">
            <ChoiceCard value="agent" id="k-agent" title={t("nodeWizard.type.agent")} description={t("nodeWizard.type.agentDesc")} />
            <ChoiceCard value="link" id="k-link" title={t("nodeWizard.type.link")} description={t("nodeWizard.type.linkDesc")} />
          </RadioGroup>
        )}

        {step === "details" && kind === "agent" && (
          <>
            <FormField id="name" label={t("nodes.form.name")}>
              <Input
                id="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("nodes.form.namePlaceholder")}
                maxLength={64}
                autoFocus
              />
            </FormField>
            <FormField id="address" label={t("nodes.form.address")} hint={t("nodes.form.addressHint")}>
              <Input
                id="address"
                value={address}
                onChange={(e) => setAddress(e.target.value)}
                placeholder="203.0.113.20"
                autoCapitalize="none"
                spellCheck={false}
              />
            </FormField>
            <div className="grid gap-3">
              <Label>{t("nodes.form.roles")}</Label>
              <FieldGroup className="gap-3">
                <Field orientation="horizontal">
                  <Checkbox id="isExit" checked={isExit} onCheckedChange={(v) => setIsExit(!!v)} />
                  <FieldContent>
                    <FieldLabel htmlFor="isExit">{t("nodes.form.exit")}</FieldLabel>
                    <FieldDescription>{t("nodes.form.exitHint")}</FieldDescription>
                  </FieldContent>
                </Field>
                <Field orientation="horizontal">
                  <Checkbox id="isEntry" checked={isEntry} onCheckedChange={(v) => setIsEntry(!!v)} />
                  <FieldContent>
                    <FieldLabel htmlFor="isEntry">{t("nodes.form.entry")}</FieldLabel>
                    <FieldDescription>{t("nodes.form.entryHint")}</FieldDescription>
                  </FieldContent>
                </Field>
                <Field orientation="horizontal">
                  <Checkbox id="standby" checked={standby} onCheckedChange={(v) => setStandby(!!v)} />
                  <FieldContent>
                    <FieldLabel htmlFor="standby">{t("nodes.form.standby")}</FieldLabel>
                    <FieldDescription>{t("nodes.form.standbyHint")}</FieldDescription>
                  </FieldContent>
                </Field>
              </FieldGroup>
            </div>
          </>
        )}

        {step === "details" && kind === "link" && (
          <>
            <FormField id="link" label={t("nodeWizard.link.link")}>
              <Textarea
                id="link"
                value={link}
                onChange={(e) => setLink(e.target.value)}
                rows={4}
                placeholder="vless://…"
                className="font-mono text-xs break-all"
                autoCapitalize="none"
                spellCheck={false}
                autoFocus
              />
            </FormField>
            <FormField id="name" label={t("nodes.form.name")} hint={t("nodeWizard.link.nameHint")}>
              <Input id="name" value={name} onChange={(e) => setName(e.target.value)} maxLength={64} />
            </FormField>
          </>
        )}

        <FormError>{error}</FormError>
      </StepFrame>
    </div>
  )
}
