"use client"

import * as React from "react"
import { Trash2Icon } from "lucide-react"

import { useConfirm } from "@/components/confirm"
import { DomainStatus, useDomainCheck } from "@/components/domain-check"
import { FormError, FormField } from "@/components/form-field"
import { Pane, PaneContent, PaneDescription, PaneFooter, PaneGroup, PaneHeader, PaneTitle } from "@/components/settings-pane"
import { StatusBadge } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Note } from "@/components/wizard"
import { api, errorText, type DomainCertificate, type DomainInfo } from "@/lib/api"
import { formatDate } from "@/lib/format"
import { useFormErrors } from "@/lib/form-errors"
import { useI18n } from "@/lib/i18n"
import { useSession } from "@/lib/session"
import { toast } from "@/lib/toast"
import { useResource } from "@/lib/use-resource"

/**
 * The server's domain with the certificate it is served with, the way to
 * move to another domain, and the certificates of earlier ones. A move
 * takes everything that goes by the domain along at once (the panel, the
 * subscriptions, the links, the connections with a Let's Encrypt
 * certificate); when the panel is opened by its domain, the new domain's
 * certificate is obtained first and the move waits for it.
 */
/** The session key a domain to move to is handed over under. */
export const DOMAIN_PREFILL = "portunus.domainPrefill"

export function DomainCard() {
  const { t, locale } = useI18n()
  const { user } = useSession()
  const isAdmin = user.role === "admin"
  // Looked at again while a move waits for its certificate.
  const [waiting, setWaiting] = React.useState(false)
  const { data, reload } = useResource(api.domain, waiting ? 5000 : 30000)
  const [confirm, confirmDialog] = useConfirm()
  // A domain handed over from the server's settings ("move to the name the
  // links use"), taken once.
  const [next, setNext] = React.useState(() => {
    try {
      const v = sessionStorage.getItem(DOMAIN_PREFILL) ?? ""
      sessionStorage.removeItem(DOMAIN_PREFILL)
      return v
    } catch {
      return ""
    }
  })
  const [pending, setPending] = React.useState(false)
  const { errors, wrong, clear, fail } = useFormErrors()
  const check = useDomainCheck(next)
  // This server's address, for the words when a domain leads elsewhere.
  const { data: address } = useResource(api.setupAddress)
  const detectedIp = address?.detected.ipv4 || address?.detected.ipv6 || ""

  // A move that was waiting is done: the page opened by the old domain
  // goes over to the new one, where the panel now is.
  const was = React.useRef<DomainInfo | undefined>(undefined)
  React.useEffect(() => {
    const before = was.current
    was.current = data
    if (data && !!data.next !== waiting) setTimeout(() => setWaiting(!!data.next))
    if (!before?.next || !data || data.next || data.domain !== before.next) return
    toast.success(t("domain.toast.moved", { domain: data.domain }))
    if (data.panelUrl && window.location.hostname === before.domain) window.location.assign(data.panelUrl)
  }, [data, waiting, t])

  if (!data) return null

  const certificate = (c: DomainCertificate | null) =>
    c ? t("domain.certificate.until", { issuer: c.issuer, date: formatDate(c.notAfter, locale) }) : t("domain.certificate.none")

  function move() {
    const domain = next.trim().toLowerCase()
    if (!domain) return wrong("domain", t("error.required"))
    confirm({
      title: t("domain.move.title", { domain }),
      description: data!.domain ? t("domain.move.description", { old: data!.domain, domain }) : t("domain.move.first", { domain }),
      action: t("domain.move.action"),
      onConfirm: async () => {
        setPending(true)
        clear()
        try {
          const after = await api.moveDomain(domain)
          setNext("")
          toast.success(after.next ? t("domain.toast.waiting", { domain }) : t("domain.toast.moved", { domain }))
          reload()
        } catch (err) {
          fail(err, ["domain"])
        } finally {
          setPending(false)
        }
      },
    })
  }

  const cancel = async () => {
    try {
      await api.cancelDomainMove()
      reload()
    } catch (err) {
      toast.error(errorText(err, t))
    }
  }

  const remove = (c: DomainCertificate) =>
    confirm({
      title: t("domain.remove.title", { domain: c.domain }),
      description: t("domain.remove.description"),
      action: t("common.delete"),
      destructive: true,
      onConfirm: async () => {
        try {
          await api.removeCertificate(c.domain)
          toast.success(t("domain.toast.removed", { domain: c.domain }))
          reload()
        } catch (err) {
          toast.error(errorText(err, t))
        }
      },
    })

  const pointsHere = check.state === "done" && check.check.pointsHere

  return (
    <Pane>
      {confirmDialog}
      <PaneHeader>
        <PaneTitle>{t("domain.title")}</PaneTitle>
        <PaneDescription>{t("domain.description")}</PaneDescription>
      </PaneHeader>
      <PaneContent>
        <FormField id="domain-current" label={t("domain.current")} hint={t("domain.currentHint")}>
          <div id="domain-current" data-setting="domain-current" className="grid gap-1.5">
            {data.domain ? (
              <>
                <span className="font-mono text-sm break-all">{data.domain}</span>
                <StatusBadge tone={data.certificate ? "ok" : "pending"} className="w-fit">
                  {certificate(data.certificate)}
                </StatusBadge>
              </>
            ) : (
              <span className="text-sm text-muted-foreground">{t("domain.none")}</span>
            )}
          </div>
        </FormField>

        {data.next ? (
          <div className="grid gap-3" data-setting="domain-next">
            <Note tone="pending">{t("domain.waiting", { domain: data.next })}</Note>
            {data.moveError && <FormError>{data.moveError}</FormError>}
            {isAdmin && (
              <div>
                <Button size="sm" variant="outline" onClick={cancel}>
                  {t("domain.cancel")}
                </Button>
              </div>
            )}
          </div>
        ) : data.own ? (
          <Note tone="info">{t("domain.own")}</Note>
        ) : (
          isAdmin && (
            <FormField
              id="domain"
              label={t(data.domain ? "domain.new" : "domain.add")}
              hint={t(data.domain ? "domain.newHint" : "domain.addHint")}
              error={errors.domain}
            >
              <div className="grid gap-3">
                <Input
                  id="domain"
                  value={next}
                  onChange={(e) => (setNext(e.target.value), clear())}
                  placeholder="vpn.example.com"
                  autoCapitalize="none"
                  spellCheck={false}
                  aria-invalid={!!errors.domain}
                />
                {next.trim() && <DomainStatus domain={next} status={check} ip={detectedIp} />}
              </div>
            </FormField>
          )
        )}

        {data.others.length > 0 && (
          <PaneGroup title={t("domain.others")}>
            <ul className="grid gap-2" data-setting="domain-others">
              {data.others.map((c) => (
                <li key={c.domain} className="flex items-center justify-between gap-4 rounded-lg border px-3 py-2">
                  <div className="grid min-w-0 gap-0.5">
                    <span className="truncate font-mono text-sm">{c.domain}</span>
                    <span className="text-xs text-muted-foreground">{certificate(c)}</span>
                  </div>
                  {isAdmin && (
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={t("domain.remove.title", { domain: c.domain })}
                      onClick={() => remove(c)}
                    >
                      <Trash2Icon />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </PaneGroup>
        )}
        <FormError>{errors.form}</FormError>
      </PaneContent>
      {isAdmin && !data.next && !data.own && (
        <PaneFooter className="border-t">
          <Button size="sm" onClick={move} disabled={pending || !pointsHere}>
            {t(data.domain ? "domain.move.button" : "domain.add.button")}
          </Button>
        </PaneFooter>
      )}
    </Pane>
  )
}
