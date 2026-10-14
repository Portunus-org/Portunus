"use client"

import * as React from "react"
import { ArrowLeftIcon, ArrowRightIcon } from "lucide-react"

import { CopyButton } from "@/components/copy-button"
import { FormError, FormField } from "@/components/form-field"
import { Pane, PaneContent, PaneDescription, PaneFooter, PaneHeader, PaneTitle } from "@/components/settings-pane"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Note } from "@/components/wizard"
import { api, ApiError, errorText, ssoInput, type Role, type SSOSettings } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { toast } from "@/lib/toast"

const steps = ["provider", "client", "access"] as const
type Step = (typeof steps)[number]
const fallbacks = ["", "viewer", "operator", "admin"] as const

/**
 * Setting up sign-in through a provider, a step at a time, in the order it
 * is done: where the provider is, the application made for the panel there,
 * and who gets in. It takes the place of the SSO section in the settings
 * window while it lasts; finishing turns sign-in on and returns there.
 */
export function SSOSetup({
  settings,
  callback,
  onDone,
  onCancel,
}: {
  settings: SSOSettings
  callback: string
  onDone: () => void
  onCancel: () => void
}) {
  const { t } = useI18n()
  const [step, setStep] = React.useState<Step>("provider")
  const [v, setV] = React.useState({ ...settings, clientSecret: "" })
  const [advanced, setAdvanced] = React.useState(settings.groupsClaim !== "groups" || settings.scopes !== "openid profile email groups")
  const [pending, setPending] = React.useState(false)
  // What is wrong, said at the field it is about; "form" for what belongs to none.
  const [errors, setErrors] = React.useState<Record<string, string>>({})
  const set = (patch: Partial<typeof v>) => {
    setV({ ...v, ...patch })
    setErrors({})
  }
  const wrong = (field: string, message: string) => setErrors({ [field]: message })
  const index = steps.indexOf(step)

  async function next() {
    setErrors({})
    if (step === "provider") {
      const issuer = v.issuer.trim()
      if (!issuer) return wrong("issuer", t("sso.error.noIssuer"))
      if (!/^https?:\/\/[^\s/]+/i.test(issuer)) return wrong("issuer", t("sso.error.notUrl"))
      // Nothing is asked about a provider that is not there.
      setPending(true)
      const found = await api.testSSO(issuer).catch(() => ({ ok: false }))
      setPending(false)
      if (!found.ok) return wrong("issuer", t("sso.checkFailed"))
      return setStep("client")
    }
    if (step === "client") {
      if (!v.clientId.trim()) return wrong("clientId", t("sso.error.noClient"))
      return setStep("access")
    }
    setPending(true)
    try {
      // An empty secret means "as it was": the stored one is never shown, so it cannot be retyped.
      await api.saveSSO({ ...ssoInput(v), enabled: true, ...(v.clientSecret ? { clientSecret: v.clientSecret } : {}) })
      toast.success(t("sso.toast.on"))
      onDone()
    } catch (err) {
      // The server names the field; one from an earlier step is shown there.
      const field = err instanceof ApiError && err.field ? err.field : "form"
      if (field === "issuer") setStep("provider")
      if (field === "clientId") setStep("client")
      wrong(field, errorText(err, t))
      setPending(false)
    }
  }

  return (
    <Pane>
      <PaneHeader>
        <div className="col-start-1 mb-3 grid gap-2">
          <span className="text-sm text-muted-foreground">{t("welcome.step", { n: index + 1, total: steps.length })}</span>
          <Progress
            value={((index + 1) / steps.length) * 100}
            className="h-1 *:data-[slot=progress-indicator]:duration-500 *:data-[slot=progress-indicator]:ease-out"
          />
        </div>
        <PaneTitle>{t(`sso.step.${step}.title`)}</PaneTitle>
        <PaneDescription>{t(`sso.step.${step}.lead`)}</PaneDescription>
      </PaneHeader>
      <PaneContent key={step} className="duration-300 animate-in fade-in">
        {step === "provider" && (
          <>
            <FormField id="issuer" label={t("sso.issuer")} hint={t("sso.issuerHint")} error={errors.issuer}>
              <Input
                id="issuer"
                aria-invalid={!!errors.issuer}
                type="url"
                value={v.issuer}
                onChange={(e) => set({ issuer: e.target.value })}
                placeholder="https://id.example.com"
                spellCheck={false}
                autoFocus
              />
            </FormField>
            <FormField id="name" label={t("sso.name")} hint={t("sso.nameHint")}>
              <Input id="name" value={v.name} onChange={(e) => set({ name: e.target.value })} maxLength={40} placeholder="SSO" />
            </FormField>
            {pending && <Note tone="pending">{t("sso.checking")}</Note>}
          </>
        )}

        {step === "client" && (
          <>
            <Note tone="info">{t("sso.pkce")}</Note>
            <FormField id="callback" label={t("sso.callback")} hint={t("sso.callbackHint")}>
              <div className="relative">
                <Input id="callback" readOnly value={callback} className="pr-10 font-mono text-xs" />
                <CopyButton value={callback} className="absolute top-1/2 right-1 -translate-y-1/2 text-muted-foreground" />
              </div>
            </FormField>
            <FormField id="clientId" label={t("sso.clientId")} error={errors.clientId}>
              <Input
                id="clientId"
                aria-invalid={!!errors.clientId}
                value={v.clientId}
                onChange={(e) => set({ clientId: e.target.value })}
                spellCheck={false}
                autoComplete="off"
              />
            </FormField>
            <FormField
              id="clientSecret"
              label={t("sso.clientSecret")}
              hint={t(settings.hasSecret ? "sso.clientSecretKept" : "sso.clientSecretHint")}
            >
              <Input
                id="clientSecret"
                type="password"
                value={v.clientSecret}
                onChange={(e) => set({ clientSecret: e.target.value })}
                placeholder={settings.hasSecret ? "••••••••••••" : ""}
                autoComplete="new-password"
              />
            </FormField>
          </>
        )}

        {step === "access" && (
          <>
            {(["admin", "operator", "viewer"] as const).map((role) => (
              <FormField key={role} id={`${role}Group`} label={t(`role.${role}`)}>
                <Input
                  id={`${role}Group`}
                  value={v[`${role}Group`]}
                  onChange={(e) => set({ [`${role}Group`]: e.target.value })}
                  placeholder={t("sso.groupPlaceholder")}
                  spellCheck={false}
                  autoComplete="off"
                />
              </FormField>
            ))}
            <FormField
              id="defaultRole"
              label={t("sso.fallback")}
              hint={t(v.defaultRole ? "sso.fallbackRoleHint" : "sso.fallbackNoneHint")}
              error={errors.defaultRole}
            >
              <Select value={v.defaultRole || "none"} onValueChange={(r) => set({ defaultRole: r === "none" ? "" : (r as Role) })}>
                <SelectTrigger id="defaultRole" className="w-full" aria-invalid={!!errors.defaultRole}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {fallbacks.map((r) => (
                      <SelectItem key={r || "none"} value={r || "none"}>
                        {r ? t(`role.${r}`) : t("sso.fallbackNone")}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </FormField>
            {advanced ? (
              <>
                <FormField id="groupsClaim" label={t("sso.groupsClaim")} hint={t("sso.groupsClaimHint")}>
                  <Input
                    id="groupsClaim"
                    value={v.groupsClaim}
                    onChange={(e) => set({ groupsClaim: e.target.value })}
                    spellCheck={false}
                    className="font-mono"
                  />
                </FormField>
                <FormField id="scopes" label={t("sso.scopes")} hint={t("sso.scopesHint")} error={errors.scopes}>
                  <Input
                    id="scopes"
                    aria-invalid={!!errors.scopes}
                    value={v.scopes}
                    onChange={(e) => set({ scopes: e.target.value })}
                    spellCheck={false}
                    className="font-mono"
                  />
                </FormField>
              </>
            ) : (
              <div>
                <Button variant="link" className="h-auto p-0 text-muted-foreground" onClick={() => setAdvanced(true)}>
                  {t("sso.advanced")}
                </Button>
              </div>
            )}
          </>
        )}
        <FormError>{errors.form ?? (step === "access" && !advanced ? errors.scopes : undefined)}</FormError>
      </PaneContent>
      <PaneFooter>
        {index === 0 ? (
          <Button variant="ghost" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
        ) : (
          <Button variant="ghost" onClick={() => (setErrors({}), setStep(steps[index - 1]))}>
            <ArrowLeftIcon />
            {t("common.back")}
          </Button>
        )}
        <Button className="ml-auto" onClick={next} disabled={pending}>
          {index === steps.length - 1 ? t("sso.finish") : t("common.next")}
          {index < steps.length - 1 && <ArrowRightIcon />}
        </Button>
      </PaneFooter>
    </Pane>
  )
}
