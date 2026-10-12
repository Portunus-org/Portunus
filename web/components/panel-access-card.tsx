"use client"

import * as React from "react"
import { RefreshCwIcon } from "lucide-react"
import { toast } from "sonner"

import { useConfirm } from "@/components/confirm"
import { CopyButton } from "@/components/copy-button"
import { FormError, FormField, SwitchField } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { RadioGroup } from "@/components/ui/radio-group"
import { Textarea } from "@/components/ui/textarea"
import { ChoiceCard, Note } from "@/components/wizard"
import { api, errorText, type PanelAccess, type PanelAccessCheck } from "@/lib/api"
import { formatDate } from "@/lib/format"
import { useI18n } from "@/lib/i18n"

/** The secret address the panel is opened by, with a way to replace it. */
export function PanelAddress({ access, onChanged }: { access: PanelAccess; onChanged?: (a: PanelAccess) => void }) {
  const { t } = useI18n()
  const [confirm, confirmDialog] = useConfirm()
  return (
    <div className="grid gap-2 text-sm">
      <span className="font-medium">{t("access.address")}</span>
      <div className="relative">
        <pre className="rounded-lg bg-muted p-4 pr-12 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">{access.url}</pre>
        <CopyButton value={access.url} className="absolute top-2 right-2 text-muted-foreground" />
      </div>
      <span className="text-muted-foreground">{t("access.addressHint", { origin: access.url.slice(0, access.url.lastIndexOf("/")) })}</span>
      {onChanged && (
        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() =>
              confirm({
                title: t("access.rotate.title"),
                description: t("access.rotate.text"),
                action: t("access.rotate"),
                onConfirm: async () => {
                  try {
                    onChanged(await api.rotatePanelGate())
                    toast.success(t("access.rotated"))
                  } catch (err) {
                    toast.error(errorText(err, t))
                  }
                },
              })
            }
          >
            <RefreshCwIcon />
            {t("access.rotate")}
          </Button>
        </div>
      )}
      {confirmDialog}
    </div>
  )
}

// How long the certificate is waited for before the wait is called a failure.
const CERTIFICATE_WAIT_MS = 3 * 60_000

/**
 * Checks, one after the other, that the panel has a certificate and that
 * this browser reaches it by the domain. The first is asked of the server
 * (and with Let's Encrypt is what obtains the certificate); the second is
 * tried from here, again and again: a new DNS record takes a while to
 * reach everybody.
 */
export function usePanelAccessChecks(enabled: boolean) {
  const [check, setCheck] = React.useState<PanelAccessCheck | null>(null)
  const [reached, setReached] = React.useState(false)
  const [round, setRound] = React.useState(0)
  // While the certificate is waited for: what the server last heard from
  // Let's Encrypt, if anything.
  const [waiting, setWaiting] = React.useState<{ said?: string } | null>(null)

  React.useEffect(() => {
    if (!enabled) return
    let alive = true
    let timer: ReturnType<typeof setTimeout>
    // A certificate takes from seconds to a couple of minutes. The server
    // answers every few seconds how things stand, and until it has the
    // certificate that is shown as waiting, with whatever Let's Encrypt
    // has said. It becomes an error when the server
    // says that waiting will not help, or when it has lasted too long.
    const started = Date.now()
    const attempt = () =>
      api
        .checkPanelAccess()
        .catch((err): PanelAccessCheck => ({ ok: false, error: err instanceof Error ? err.message : String(err) }))
        .then((c) => {
          if (!alive) return
          if (c.ok || c.final || Date.now() - started > CERTIFICATE_WAIT_MS) {
            setWaiting(null)
            setCheck(c)
          } else {
            setWaiting({ said: c.error || undefined })
            timer = setTimeout(attempt, c.waiting ? 500 : 3000)
          }
        })
    attempt()
    return () => {
      alive = false
      clearTimeout(timer)
    }
  }, [enabled, round])

  const certified = !!check?.ok
  React.useEffect(() => {
    if (!enabled || !certified || reached) return
    let alive = true
    let timer: ReturnType<typeof setTimeout>
    const attempt = () =>
      api.browserReachesPanel().then(
        (ok) => {
          if (!alive) return
          if (ok) setReached(true)
          else timer = setTimeout(attempt, 4000)
        },
        () => alive && (timer = setTimeout(attempt, 4000))
      )
    attempt()
    return () => {
      alive = false
      clearTimeout(timer)
    }
  }, [enabled, certified, reached])

  const retry = React.useCallback(() => {
    setCheck(null)
    setWaiting(null)
    setReached(false)
    setRound((r) => r + 1)
  }, [])
  return { check, reached, retry, waiting }
}

/** The two checks as status lines. */
export function PanelAccessStatus({
  check,
  reached,
  domain,
  waiting,
}: {
  check: PanelAccessCheck | null
  reached: boolean
  domain: string
  waiting?: { said?: string } | null
}) {
  const { t, locale } = useI18n()
  if (!check) {
    return (
      <div className="grid gap-1">
        <Note tone="pending">{t("access.check.certificate")}</Note>
        {waiting?.said && <p className="pl-6 text-sm text-muted-foreground">{waiting.said}</p>}
      </div>
    )
  }
  if (!check.ok)
    return (
      <Note tone="error">
        {check.final || !check.waiting
          ? t("access.check.failed", { error: check.error ?? "" })
          : t("access.check.slow", { error: check.error ?? "" })}
      </Note>
    )
  return (
    <div className="grid gap-2">
      <Note tone={check.trusted ? "ok" : "warn"}>
        {t(check.trusted ? "access.check.certified" : "access.check.untrusted", {
          issuer: check.issuer ?? "",
          date: formatDate(check.notAfter, locale),
        })}
      </Note>
      {reached ? (
        <Note tone="ok">{t("access.check.reached", { domain })}</Note>
      ) : (
        <Note tone="pending">{t("access.check.reaching", { domain })}</Note>
      )}
    </div>
  )
}

/** Settings: opening the panel by its domain. */
export function PanelAccessCard({ canEdit }: { canEdit: boolean }) {
  const { t } = useI18n()
  const [access, setAccess] = React.useState<PanelAccess | null>(null)
  const [enabled, setEnabled] = React.useState(false)
  const [mode, setMode] = React.useState<"acme" | "own">("acme")
  const [port, setPort] = React.useState(443)
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const { check, reached, retry, waiting } = usePanelAccessChecks(!!access?.enabled)

  const show = React.useCallback((a: PanelAccess) => {
    setAccess(a)
    setEnabled(a.enabled)
    setMode(a.mode)
    setPort(a.port)
  }, [])
  React.useEffect(() => {
    api.panelAccess().then(show, () => {})
  }, [show])

  if (!access) return null

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    const f = new FormData(form)
    const certificate = String(f.get("certificate") ?? "").trim()
    const key = String(f.get("key") ?? "").trim()
    setPending(true)
    setError(null)
    try {
      show(
        await api.setPanelAccess({
          enabled,
          mode,
          port: mode === "acme" ? 443 : port,
          ...(mode === "own" && (certificate || key) ? { certificate, key } : {}),
          decoy: String(f.get("decoy") ?? ""),
        })
      )
      // The key has been stored; it should not stay on screen.
      form.reset()
      retry()
      toast.success(t("settings.saved"))
    } catch (err) {
      setError(errorText(err, t))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <form className="contents" noValidate onSubmit={onSubmit}>
        <CardHeader>
          <CardTitle>{t("access.title")}</CardTitle>
          <CardDescription>{t("access.description")}</CardDescription>
        </CardHeader>
        <CardContent>
          <fieldset disabled={!canEdit} className="grid max-w-xl gap-6">
            {!access.domain ? (
              <Note tone="warn">{t("access.needsDomain")}</Note>
            ) : (
              <>
                <SwitchField
                  id="panel-access"
                  label={t("access.enable", { domain: access.domain })}
                  hint={t("access.enableHint")}
                  checked={enabled}
                  onCheckedChange={setEnabled}
                />
                {enabled && (
                  <>
                    <RadioGroup value={mode} onValueChange={(v) => setMode(v as "acme" | "own")} className="gap-3">
                      <ChoiceCard value="acme" id="access-acme" title={t("access.acme")} description={t("access.acmeHint")} />
                      <ChoiceCard value="own" id="access-own" title={t("access.own")} description={t("access.ownHint")} />
                    </RadioGroup>
                    {mode === "own" && (
                      <>
                        <FormField id="access-port" label={t("access.port")} hint={t("access.portHint")}>
                          <Input
                            id="access-port"
                            type="number"
                            min={1}
                            max={65535}
                            className="max-w-32 tabular-nums"
                            value={port}
                            onChange={(e) => setPort(Number(e.target.value))}
                          />
                        </FormField>
                        <FormField
                          id="certificate"
                          label={t("access.certificate")}
                          hint={t(access.hasCertificate ? "access.certificateKeep" : "access.certificateHint")}
                        >
                          <Textarea
                            id="certificate"
                            name="certificate"
                            rows={4}
                            spellCheck={false}
                            className="font-mono text-xs"
                            placeholder="-----BEGIN CERTIFICATE-----"
                          />
                        </FormField>
                        <FormField id="key" label={t("access.key")}>
                          <Textarea
                            id="key"
                            name="key"
                            rows={4}
                            spellCheck={false}
                            className="font-mono text-xs"
                            placeholder="-----BEGIN PRIVATE KEY-----"
                          />
                        </FormField>
                      </>
                    )}
                  </>
                )}
                {enabled && (
                  <FormField id="decoy" label={t("access.decoy")} hint={t("access.decoyHint")}>
                    <Input id="decoy" name="decoy" defaultValue={access.decoy} placeholder={t("access.decoyAuto")} spellCheck={false} />
                  </FormField>
                )}
                {access.enabled && (
                  <>
                    {access.shared && <Note tone="ok">{t("access.shared", { port: access.port })}</Note>}
                    {access.error && !check ? (
                      <Note tone="error">{access.error}</Note>
                    ) : (
                      <PanelAccessStatus check={check} reached={reached} domain={access.domain} waiting={waiting} />
                    )}
                    <PanelAddress access={access} onChanged={canEdit ? show : undefined} />
                  </>
                )}
              </>
            )}
          </fieldset>
          <FormError>{error}</FormError>
        </CardContent>
        {canEdit && access.domain && (
          <CardFooter className="border-t">
            <Button type="submit" size="sm" disabled={pending}>
              {pending ? t("common.saving") : t("common.save")}
            </Button>
          </CardFooter>
        )}
      </form>
    </Card>
  )
}
