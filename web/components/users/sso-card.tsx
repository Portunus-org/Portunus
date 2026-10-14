"use client"

import * as React from "react"

import { useConfirm } from "@/components/confirm"
import { StatusBadge } from "@/components/status"
import { SSOSetup } from "@/components/users/sso-setup"
import { Button } from "@/components/ui/button"
import { Pane, PaneContent, PaneDescription, PaneFooter, PaneHeader, PaneTitle } from "@/components/settings-pane"
import { api, errorText, ssoInput, type SSOSettings } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { toast } from "@/lib/toast"
import { useResource } from "@/lib/use-resource"

/**
 * Sign-in through an OIDC provider, as it stands: not set up, on, or off.
 * Setting it up and changing it is a wizard that takes the section's place.
 */
export function SSOCard() {
  const { t } = useI18n()
  const { data, reload } = useResource(api.sso)
  const [confirm, confirmDialog] = useConfirm()
  const s = data?.settings
  const configured = !!s && s.issuer !== "" && s.clientId !== ""
  const [setup, setSetup] = React.useState(false)

  const turn = async (s: SSOSettings, enabled: boolean) => {
    try {
      await api.saveSSO({ ...ssoInput(s), enabled })
      toast.success(t(enabled ? "sso.toast.on" : "sso.toast.off"))
      reload()
    } catch (err) {
      toast.error(errorText(err, t))
    }
  }

  const groups = s
    ? (["admin", "operator", "viewer"] as const).filter((r) => s[`${r}Group`]).map((r) => `${s[`${r}Group`]}: ${t(`role.${r}`)}`)
    : []

  if (setup && data)
    return (
      <SSOSetup
        settings={data.settings}
        callback={data.callback}
        onCancel={() => setSetup(false)}
        onDone={() => {
          setSetup(false)
          reload()
        }}
      />
    )

  return (
    <Pane>
      {confirmDialog}
      <PaneHeader>
        <PaneTitle>{t("sso.title")}</PaneTitle>
        <PaneDescription>{t("sso.description")}</PaneDescription>
      </PaneHeader>
      {s && configured && (
        <PaneContent>
          <dl className="grid grid-cols-[10rem_1fr] items-baseline gap-y-3 text-sm" data-setting="sso-summary">
            <dt className="text-muted-foreground">{t("sso.row.state")}</dt>
            <dd>
              <StatusBadge tone={s.enabled ? "ok" : "off"}>{t(s.enabled ? "sso.state.on" : "sso.state.off")}</StatusBadge>
            </dd>
            <dt className="text-muted-foreground">{t("sso.row.provider")}</dt>
            <dd className="min-w-0 break-all">
              {s.name ? `${s.name}, ` : ""}
              {s.issuer}
            </dd>
            <dt className="text-muted-foreground">{t("sso.row.flow")}</dt>
            <dd>{t("sso.row.flowValue")}</dd>
            <dt className="text-muted-foreground">{t("sso.row.access")}</dt>
            <dd className="grid gap-1">
              {groups.map((g) => (
                <span key={g}>{g}</span>
              ))}
              <span className={groups.length ? "text-muted-foreground" : ""}>
                {s.defaultRole ? t("sso.row.everyone", { role: t(`role.${s.defaultRole}`) }) : t("sso.row.nobodyElse")}
              </span>
            </dd>
          </dl>
        </PaneContent>
      )}
      {s && (
        <PaneFooter className="gap-2 border-t">
          <Button size="sm" variant={configured ? "outline" : "default"} data-setting="sso-setup" onClick={() => setSetup(true)}>
            {t(configured ? "sso.change" : "sso.setUp")}
          </Button>
          {configured &&
            (s.enabled ? (
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  confirm({
                    title: t("sso.off.title"),
                    description: t("sso.off.description"),
                    action: t("sso.turnOff"),
                    onConfirm: () => turn(s, false),
                  })
                }
              >
                {t("sso.turnOff")}
              </Button>
            ) : (
              <Button size="sm" variant="outline" onClick={() => turn(s, true)}>
                {t("sso.turnOn")}
              </Button>
            ))}
        </PaneFooter>
      )}
    </Pane>
  )
}
