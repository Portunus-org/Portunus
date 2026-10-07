"use client"

import * as React from "react"
import { toast } from "sonner"

import { CopyButton } from "@/components/copy-button"
import { StatusBadge } from "@/components/status"
import { Button } from "@/components/ui/button"
import {
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { useIsMobile } from "@/hooks/use-mobile"
import { api, errorText, type Inbound, type TLSMode } from "@/lib/api"
import { formatDate } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"
import { protocols } from "@/lib/protocols"

function Field({ id, label, hint, children }: { id: string; label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-3">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint && <p className="text-muted-foreground">{hint}</p>}
    </div>
  )
}

function Value({ label, value }: { label: string; value?: string | number }) {
  if (value === undefined || value === "") return null
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className="flex min-w-0 items-center gap-1">
        <span className="truncate font-mono text-xs">{value}</span>
        <CopyButton value={String(value)} />
      </span>
    </div>
  )
}

const tlsHint: Record<TLSMode, MessageKey> = {
  acme: "inbounds.form.cert.acmeHint",
  file: "inbounds.form.cert.fileHint",
  "self-signed": "inbounds.form.cert.selfSignedHint",
}

/**
 * Edit an inbound. New ones are created by the wizard (/inbounds/new/);
 * the protocol is fixed once created and key material comes from the server.
 */
export function InboundDrawer({
  open,
  onOpenChange,
  inbound,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  inbound: Inbound
  onSaved: () => void
}) {
  const { t, locale } = useI18n()
  const isMobile = useIsMobile()
  const protocol = inbound.protocol
  const [enabled, setEnabled] = React.useState(inbound.enabled)
  const [obfs, setObfs] = React.useState(inbound.obfs ?? false)
  const [strictTls, setStrictTls] = React.useState(inbound.strictTls ?? false)
  const [tlsMode, setTlsMode] = React.useState<TLSMode>(inbound.tlsMode ?? "self-signed")
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)

  const info = protocols[protocol]
  const usesTLS = protocol === "hysteria2" || protocol === "tuic" || protocol === "trojan"

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const str = (k: string) => (f.has(k) ? String(f.get(k)) : undefined)
    const input = {
      protocol,
      name: String(f.get("name") ?? ""),
      port: Number(f.get("port")),
      enabled,
      realityServer: str("realityServer"),
      realityServerPort: f.has("realityServerPort") ? Number(f.get("realityServerPort")) : undefined,
      serverName: str("serverName"),
      tlsMode: usesTLS ? tlsMode : undefined,
      certificatePath: str("certificatePath"),
      keyPath: str("keyPath"),
      masqueradeUrl: str("masqueradeUrl"),
      obfs: protocol === "hysteria2" ? obfs : undefined,
      strictTls: usesTLS && tlsMode === "self-signed" ? strictTls : undefined,
    }
    setPending(true)
    setError(null)
    try {
      await api.updateInbound(inbound.id, input)
      toast.success(t("inbounds.toast.saved"))
      onSaved()
      onOpenChange(false)
    } catch (err) {
      setError(errorText(err, t))
    } finally {
      setPending(false)
    }
  }

  return (
    <Drawer open={open} onOpenChange={onOpenChange} direction={isMobile ? "bottom" : "right"}>
      <DrawerContent>
        <DrawerHeader className="gap-1">
          <DrawerTitle>{inbound.name || protocols[protocol].label}</DrawerTitle>
          <DrawerDescription>{t("inbounds.drawer.subtitle", { protocol: protocols[protocol].label, port: inbound.port })}</DrawerDescription>
        </DrawerHeader>

        <form id="inbound-form" onSubmit={onSubmit} className="flex flex-col gap-4 overflow-y-auto px-4 text-sm" noValidate>
          <div className="grid grid-cols-[1fr_7rem] gap-4">
            <Field id="name" label={t("inbounds.form.name")}>
              <Input id="name" name="name" defaultValue={inbound.name} placeholder={protocols[protocol].label} maxLength={64} />
            </Field>
            <Field id="port" label={t("inbounds.form.port", { transport: info.transport })}>
              <Input
                id="port"
                name="port"
                type="number"
                min={1}
                max={65535}
                required
                className="tabular-nums"
                defaultValue={inbound.port ?? info.defaultPort}
                              />
            </Field>
          </div>

          {protocol === "vless" && (
            <div className="grid grid-cols-[1fr_7rem] gap-4">
              <Field id="realityServer" label={t("inbounds.form.realityTarget")} hint={t("inbounds.form.realityTargetHint")}>
                <Input
                  id="realityServer"
                  name="realityServer"
                  defaultValue={inbound.realityServer ?? "www.microsoft.com"}
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </Field>
              <Field id="realityServerPort" label={t("inbounds.form.targetPort")}>
                <Input
                  id="realityServerPort"
                  name="realityServerPort"
                  type="number"
                  min={1}
                  max={65535}
                  className="tabular-nums"
                  defaultValue={inbound.realityServerPort ?? 443}
                />
              </Field>
            </div>
          )}

          {usesTLS && (
            <>
              <Field id="tlsMode" label={t("inbounds.form.certificate")} hint={t(tlsHint[tlsMode])}>
                <Select value={tlsMode} onValueChange={(v) => setTlsMode(v as TLSMode)}>
                  <SelectTrigger id="tlsMode" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="acme">{t("inbounds.form.cert.acme")}</SelectItem>
                      <SelectItem value="file">{t("inbounds.form.cert.file")}</SelectItem>
                      <SelectItem value="self-signed">{t("inbounds.form.cert.selfSigned")}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field
                id="serverName"
                label={t(tlsMode === "self-signed" ? "inbounds.form.serverName" : "inbounds.form.domain")}
                hint={t(tlsMode === "self-signed" ? "inbounds.form.serverNameHint" : "inbounds.form.domainHint")}
              >
                <Input
                  id="serverName"
                  name="serverName"
                  placeholder={tlsMode === "self-signed" ? undefined : "vpn.example.com"}
                  defaultValue={inbound.serverName}
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </Field>
              {tlsMode === "self-signed" && (
                <div className="flex items-start justify-between gap-4">
                  <div className="grid gap-1.5">
                    <Label htmlFor="strictTls">{t("wizard.cert.strict")}</Label>
                    <p className="text-muted-foreground">{t("wizard.cert.strictHint")}</p>
                    <a href={api.caCertificateUrl("pem")} download className="w-fit underline underline-offset-4">
                      {t("ca.download")}
                    </a>
                  </div>
                  <Switch id="strictTls" checked={strictTls} onCheckedChange={setStrictTls} />
                </div>
              )}
              {tlsMode === "file" && (
                <>
                  <Field id="certificatePath" label={t("inbounds.form.certPath")}>
                    <Input
                      id="certificatePath"
                      name="certificatePath"
                      placeholder="/certs/live/vpn.example.com/fullchain.pem"
                      defaultValue={inbound.certificatePath}
                      spellCheck={false}
                    />
                  </Field>
                  <Field id="keyPath" label={t("inbounds.form.keyPath")} hint={t("inbounds.form.pathsHint")}>
                    <Input
                      id="keyPath"
                      name="keyPath"
                      placeholder="/certs/live/vpn.example.com/privkey.pem"
                      defaultValue={inbound.keyPath}
                      spellCheck={false}
                    />
                  </Field>
                </>
              )}
            </>
          )}

          {protocol === "hysteria2" && (
            <>
              <Field id="masqueradeUrl" label={t("inbounds.form.masquerade")} hint={t("inbounds.form.masqueradeHint")}>
                <Input
                  id="masqueradeUrl"
                  name="masqueradeUrl"
                  placeholder="https://www.example.com"
                  defaultValue={inbound.masqueradeUrl}
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </Field>
              <div className="flex items-start justify-between gap-4">
                <div className="grid gap-1.5">
                  <Label htmlFor="obfs">{t("inbounds.form.obfs")}</Label>
                  <p className="text-muted-foreground">{t("inbounds.form.obfsHint")}</p>
                </div>
                <Switch id="obfs" checked={obfs} onCheckedChange={setObfs} />
              </div>
            </>
          )}

          <div className="flex items-center justify-between gap-4">
            <Label htmlFor="enabled">{t("inbounds.form.enabled")}</Label>
            <Switch id="enabled" checked={enabled} onCheckedChange={setEnabled} />
          </div>

          <Separator />
          <div className="flex flex-col gap-2">
            <Value label={t("inbounds.info.publicKey")} value={inbound.realityPublicKey} />
            <Value label={t("inbounds.info.shortId")} value={inbound.realityShortId} />
            <Value label={t("inbounds.info.certSha256")} value={inbound.certSha256} />
            <Value label={t("inbounds.info.method")} value={inbound.method} />
            {inbound.tlsMode && inbound.tlsMode !== "self-signed" && (
              <div className="flex items-center justify-between gap-2">
                <span className="text-muted-foreground">{t("inbounds.info.certificate")}</span>
                {inbound.certificate ? (
                  <span className="text-right">
                    {inbound.certificate.issuer}
                    <span className="text-muted-foreground">
                      {", "}
                      {t("inbounds.cert.until", { date: formatDate(inbound.certificate.notAfter, locale) })}
                    </span>
                  </span>
                ) : (
                  <StatusBadge tone="pending">{t("inbounds.info.notIssued")}</StatusBadge>
                )}
              </div>
            )}
            {inbound.privateCertExpires && (
              <div className="flex items-center justify-between gap-2">
                <span className="text-muted-foreground">{t("inbounds.info.certificate")}</span>
                <span className="text-right">
                  Portunus CA
                  <span className="text-muted-foreground">
                    {", "}
                    {t("inbounds.cert.until", { date: formatDate(inbound.privateCertExpires, locale) })}
                  </span>
                </span>
              </div>
            )}
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </form>

        <DrawerFooter>
          <Button type="submit" form="inbound-form" disabled={pending}>
            {pending ? t("common.saving") : t("common.save")}
          </Button>
          <DrawerClose asChild>
            <Button variant="outline">{t("common.cancel")}</Button>
          </DrawerClose>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  )
}
