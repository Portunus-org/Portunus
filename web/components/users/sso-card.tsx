"use client"

import * as React from "react"

import { CopyField } from "@/components/copy-button"
import { FormError, FormField, SwitchField } from "@/components/form-field"
import { Pane, PaneContent, PaneDescription, PaneFooter, PaneGroup, PaneHeader, PaneTitle } from "@/components/settings-pane"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Note } from "@/components/wizard"
import { api, ssoInput, type Role, type SSOSettings } from "@/lib/api"
import { useFormErrors } from "@/lib/form-errors"
import { useI18n } from "@/lib/i18n"
import { useSession } from "@/lib/session"
import { toast } from "@/lib/toast"
import { useResource } from "@/lib/use-resource"

const fallbacks = ["", "viewer", "operator", "admin"] as const
const roles = ["admin", "operator", "viewer"] as const

/**
 * Sign-in through an OIDC provider: the provider, the application made for
 * the panel there, and who gets in, as rows like every other section of
 * the settings, saved together by its button. Turning it on asks the
 * provider first, so a wrong address is said at the address.
 */
export function SSOCard() {
  const { data, reload } = useResource(api.sso)
  if (!data) return null
  // Saved, the form starts again from what the server keeps.
  return <SSOForm key={JSON.stringify(data.settings)} settings={data.settings} callback={data.callback} onSaved={reload} />
}

function SSOForm({ settings, callback, onSaved }: { settings: SSOSettings; callback: string; onSaved: () => void }) {
  const { t } = useI18n()
  const { user } = useSession()
  const isAdmin = user.role === "admin"
  const [v, setV] = React.useState({ ...settings, clientSecret: "" })
  const [pending, setPending] = React.useState(false)
  const { errors, wrong, clear, fail } = useFormErrors()
  const set = (patch: Partial<typeof v>) => {
    setV((old) => ({ ...old, ...patch }))
    clear()
  }

  async function save(e: React.FormEvent) {
    e.preventDefault()
    clear()
    const issuer = v.issuer.trim()
    if (v.enabled || issuer) {
      if (!issuer) return wrong("issuer", t("sso.error.noIssuer"))
      if (!/^https?:\/\/[^\s/]+/i.test(issuer)) return wrong("issuer", t("sso.error.notUrl"))
    }
    if (v.enabled && !v.clientId.trim()) return wrong("clientId", t("sso.error.noClient"))
    setPending(true)
    try {
      // Nothing is turned on for a provider that is not there.
      if (v.enabled && (issuer !== settings.issuer || !settings.enabled)) {
        const found = await api.testSSO(issuer).catch(() => ({ ok: false }))
        if (!found.ok) return wrong("issuer", t("sso.checkFailed"))
      }
      // An empty secret means "as it was": the stored one is never shown, so it cannot be retyped.
      await api.saveSSO({ ...ssoInput({ ...v, issuer }), ...(v.clientSecret ? { clientSecret: v.clientSecret } : {}) })
      toast.success(v.enabled === settings.enabled ? t("settings.saved") : t(v.enabled ? "sso.toast.on" : "sso.toast.off"))
      onSaved()
    } catch (err) {
      fail(err, ["issuer", "ssoName", "clientId", "clientSecret", "defaultRole", "groupsClaim", "scopes"], { name: "ssoName" })
    } finally {
      setPending(false)
    }
  }

  const text = (id: string, key: keyof typeof v, extra: React.ComponentProps<typeof Input> = {}) => (
    <Input
      id={id}
      value={String(v[key])}
      onChange={(e) => set({ [key]: e.target.value })}
      spellCheck={false}
      autoComplete="off"
      {...extra}
    />
  )

  return (
    <Pane>
      <form className="contents" noValidate onSubmit={save}>
        <PaneHeader>
          <PaneTitle>{t("sso.title")}</PaneTitle>
          <PaneDescription>{t("sso.description")}</PaneDescription>
        </PaneHeader>
        <PaneContent>
          <fieldset disabled={!isAdmin} className="grid gap-10">
            <SwitchField
              id="sso-enabled"
              label={t("sso.enabled")}
              hint={t("sso.enabledHint")}
              checked={v.enabled}
              onCheckedChange={(enabled) => set({ enabled })}
            />

            <PaneGroup title={t("sso.group.provider")}>
              <FormField id="issuer" label={t("sso.issuer")} hint={t("sso.issuerHint")} error={errors.issuer}>
                {text("issuer", "issuer", { type: "url", placeholder: "https://id.example.com" })}
              </FormField>
              <FormField id="ssoName" label={t("sso.name")} hint={t("sso.nameHint")} error={errors.ssoName}>
                {text("ssoName", "name", { maxLength: 40, placeholder: "SSO", spellCheck: undefined })}
              </FormField>
            </PaneGroup>

            <PaneGroup title={t("sso.group.client")}>
              <Note tone="info">{t("sso.pkce")}</Note>
              <FormField id="callback" label={t("sso.callback")} hint={t("sso.callbackHint")}>
                <CopyField id="callback" value={callback} />
              </FormField>
              <FormField id="clientId" label={t("sso.clientId")} error={errors.clientId}>
                {text("clientId", "clientId")}
              </FormField>
              <FormField
                id="clientSecret"
                label={t("sso.clientSecret")}
                hint={t(settings.hasSecret ? "sso.clientSecretKept" : "sso.clientSecretHint")}
                error={errors.clientSecret}
              >
                {text("clientSecret", "clientSecret", {
                  type: "password",
                  autoComplete: "new-password",
                  placeholder: settings.hasSecret ? "••••••••••••" : "",
                })}
              </FormField>
            </PaneGroup>

            <PaneGroup title={t("sso.group.access")}>
              <p className="-mt-3 max-w-2xl text-sm text-muted-foreground">{t("sso.accessLead")}</p>
              {roles.map((role) => (
                <FormField key={role} id={`${role}Group`} label={t("sso.groupFor", { role: t(`role.${role}`) })}>
                  {text(`${role}Group`, `${role}Group`, { placeholder: t("sso.groupPlaceholder") })}
                </FormField>
              ))}
              <FormField
                id="defaultRole"
                label={t("sso.fallback")}
                hint={t(v.defaultRole ? "sso.fallbackRoleHint" : "sso.fallbackNoneHint")}
                error={errors.defaultRole}
              >
                <Select value={v.defaultRole || "none"} onValueChange={(r) => set({ defaultRole: r === "none" ? "" : (r as Role) })}>
                  <SelectTrigger id="defaultRole" className="w-full">
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
              <FormField id="groupsClaim" label={t("sso.groupsClaim")} hint={t("sso.groupsClaimHint")} error={errors.groupsClaim}>
                {text("groupsClaim", "groupsClaim", { className: "font-mono" })}
              </FormField>
              <FormField id="scopes" label={t("sso.scopes")} hint={t("sso.scopesHint")} error={errors.scopes}>
                {text("scopes", "scopes", { className: "font-mono" })}
              </FormField>
            </PaneGroup>
          </fieldset>
          <FormError>{errors.form}</FormError>
        </PaneContent>
        {isAdmin && (
          <PaneFooter className="border-t">
            <Button type="submit" size="sm" disabled={pending}>
              {pending ? t("common.saving") : t("common.save")}
            </Button>
          </PaneFooter>
        )}
      </form>
    </Pane>
  )
}
