"use client"

import * as React from "react"
import Link from "next/link"
import { EllipsisVerticalIcon, QrCodeIcon } from "lucide-react"
import { toast } from "sonner"

import { ClientStatus } from "@/components/clients/client-status"
import { useConfirm } from "@/components/confirm"
import { CopyButton } from "@/components/copy-button"
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useIsMobile } from "@/hooks/use-mobile"
import { api, errorText, type Client, type ClientAccess } from "@/lib/api"
import { daysUntil, formatBytes, formatDate, fromDateInput, GiB, toDateInput } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { protocols } from "@/lib/protocols"
import { useCanEdit } from "@/lib/session"
import { cn } from "@/lib/utils"

/** A scannable QR code: always dark on white, whatever the theme. */
export function QR({ svg, label, className }: { svg: string; label: string; className?: string }) {
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={`data:image/svg+xml;base64,${btoa(svg)}`}
      alt={label}
      className={cn("aspect-square w-full max-w-56 rounded-lg border bg-white p-2 text-black", className)}
    />
  )
}

function Usage({ client }: { client: Client }) {
  const { t, tp, locale } = useI18n()
  const used = client.upload + client.download
  const pct = client.trafficLimit > 0 ? Math.min(100, (used / client.trafficLimit) * 100) : 0
  const days = client.expiresAt ? daysUntil(client.expiresAt) : null
  return (
    <div className="grid gap-3">
      <div className="flex items-baseline justify-between">
        <span className="font-medium">{t("clients.drawer.traffic")}</span>
        <span className="tabular-nums">
          {formatBytes(used, locale)}
          <span className="text-muted-foreground">
            {" / "}
            {client.trafficLimit > 0 ? formatBytes(client.trafficLimit, locale) : t("clients.drawer.unlimited")}
          </span>
        </span>
      </div>
      {client.trafficLimit > 0 && <Progress value={pct} />}
      <div className="flex justify-between gap-2 text-muted-foreground tabular-nums">
        <span>
          ↑ {formatBytes(client.upload, locale)} · ↓ {formatBytes(client.download, locale)}
        </span>
        <span>
          {client.expiresAt
            ? days! > 0
              ? tp("clients.drawer.daysLeft", days!)
              : t("clients.drawer.expiredOn", { date: formatDate(client.expiresAt, locale) })
            : t("clients.drawer.noExpiry")}
        </span>
      </div>
    </div>
  )
}

function Access({ clientId, version }: { clientId: number; version: number }) {
  const { t } = useI18n()
  const [access, setAccess] = React.useState<ClientAccess | null>(null)
  const [qr, setQr] = React.useState<{ svg: string; label: string } | null>(null)

  React.useEffect(() => {
    let alive = true
    api.clientAccess(clientId).then(
      (a) => {
        if (!alive) return
        setAccess(a)
        const first = a.subscriptionQr
          ? { svg: a.subscriptionQr, label: t("clients.drawer.subscription") }
          : a.links[0]
            ? { svg: a.links[0].qr, label: a.links[0].label }
            : null
        setQr(first)
      },
      (err) => alive && toast.error(errorText(err, t))
    )
    return () => {
      alive = false
    }
  }, [clientId, version, t])

  if (!access) return <p className="text-muted-foreground">{t("common.loading")}</p>
  if (!access.publicHost) {
    return (
      <p className="text-muted-foreground">
        <Link href="/settings/" className="text-foreground underline underline-offset-4">
          {t("clients.drawer.noHost")}
        </Link>
      </p>
    )
  }
  if (access.links.length === 0) {
    return (
      <p className="text-muted-foreground">
        <Link href="/inbounds/" className="text-foreground underline underline-offset-4">
          {t("clients.drawer.noInbounds")}
        </Link>
      </p>
    )
  }

  const rows = [
    ...(access.subscriptionUrl
      ? [
          {
            key: "sub",
            label: t("clients.drawer.subscription"),
            sub: t("clients.drawer.subscriptionHint"),
            url: access.subscriptionUrl,
            qr: access.subscriptionQr!,
          },
        ]
      : []),
    ...access.links.map((l) => ({ key: `l${l.inboundId}`, label: l.label, sub: protocols[l.protocol].label, url: l.url, qr: l.qr })),
  ]

  return (
    <div className="grid gap-4">
      {qr && (
        <div className="flex flex-col items-center gap-2">
          <QR svg={qr.svg} label={qr.label} />
          <span className="text-muted-foreground">{qr.label}</span>
        </div>
      )}
      <div className="divide-y overflow-hidden rounded-lg border">
        {rows.map((r) => (
          <div key={r.key} className={cn("flex items-center gap-2 px-3 py-2", qr?.label === r.label && "bg-muted/50")}>
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{r.label}</div>
              <div className="truncate text-xs text-muted-foreground">{r.sub}</div>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground"
              onClick={() => setQr({ svg: r.qr, label: r.label })}
            >
              <QrCodeIcon />
              <span className="sr-only">{t("clients.drawer.showQr")}</span>
            </Button>
            <CopyButton value={r.url} />
          </div>
        ))}
      </div>
      {!access.subscriptionUrl && <p className="text-muted-foreground">{t("clients.drawer.subscriptionsOff")}</p>}
    </div>
  )
}

function ClientForm({
  client,
  formId,
  onSubmitted,
  setPending,
}: {
  client: Client | null
  formId: string
  onSubmitted: (c: Client) => void
  setPending: (p: boolean) => void
}) {
  const { t } = useI18n()
  const canEdit = useCanEdit()
  const [enabled, setEnabled] = React.useState(client?.enabled ?? true)
  const [error, setError] = React.useState<string | null>(null)

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const gb = Number(f.get("limit") || 0)
    if (!Number.isFinite(gb) || gb < 0) {
      setError(t("clients.form.limitInvalid"))
      return
    }
    const input = {
      name: String(f.get("name") ?? ""),
      enabled,
      trafficLimit: Math.round(gb * GiB),
      expiresAt: fromDateInput(String(f.get("expires") ?? "")),
      note: String(f.get("note") ?? ""),
    }
    setPending(true)
    setError(null)
    try {
      const saved = client ? await api.updateClient(client.id, input) : await api.createClient(input)
      toast.success(t(client ? "clients.toast.saved" : "clients.toast.created"))
      onSubmitted(saved)
    } catch (err) {
      setError(errorText(err, t))
    } finally {
      setPending(false)
    }
  }

  return (
    <form id={formId} onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <fieldset disabled={!canEdit} className="contents">
        <div className="flex flex-col gap-3">
          <Label htmlFor="name">{t("clients.form.name")}</Label>
          <Input id="name" name="name" defaultValue={client?.name} required maxLength={64} autoFocus={!client} />
        </div>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-3">
            <Label htmlFor="limit">{t("clients.form.limit")}</Label>
            <Input
              id="limit"
              name="limit"
              type="number"
              min={0}
              step="any"
              placeholder={t("clients.form.limitPlaceholder")}
              className="tabular-nums"
              defaultValue={client && client.trafficLimit > 0 ? +(client.trafficLimit / GiB).toFixed(2) : ""}
            />
          </div>
          <div className="flex flex-col gap-3">
            <Label htmlFor="expires">{t("clients.form.expires")}</Label>
            <Input id="expires" name="expires" type="date" defaultValue={toDateInput(client?.expiresAt ?? null)} />
          </div>
        </div>
        <div className="flex flex-col gap-3">
          <Label htmlFor="note">{t("clients.form.note")}</Label>
          <Textarea id="note" name="note" rows={2} maxLength={500} defaultValue={client?.note} />
        </div>
        <div className="flex items-center justify-between gap-3">
          <Label htmlFor="client-enabled">{t("clients.form.enabled")}</Label>
          <Switch id="client-enabled" checked={enabled} onCheckedChange={setEnabled} />
        </div>
      </fieldset>
      {error && <p className="text-sm text-destructive">{error}</p>}
    </form>
  )
}

/**
 * Details and editing for one client, or the creation form when client is
 * null. After creation the drawer switches to the new client so its QR code
 * is on screen right away.
 */
export function ClientDrawer({
  open,
  onOpenChange,
  client: initial,
  onChanged,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  client: Client | null
  onChanged: () => void
}) {
  const { t, locale } = useI18n()
  const isMobile = useIsMobile()
  const canEdit = useCanEdit()
  const [client, setClient] = React.useState(initial)
  const [pending, setPending] = React.useState(false)
  const [accessVersion, setAccessVersion] = React.useState(0)
  const [confirm, confirmDialog] = useConfirm()

  const act = async (fn: () => Promise<Client | void>, done: string, close = false) => {
    try {
      const c = await fn()
      toast.success(done)
      onChanged()
      if (close) onOpenChange(false)
      else if (c) {
        setClient(c)
        setAccessVersion((v) => v + 1)
      }
    } catch (err) {
      toast.error(errorText(err, t))
    }
  }

  return (
    <Drawer open={open} onOpenChange={onOpenChange} direction={isMobile ? "bottom" : "right"}>
      <DrawerContent>
        <DrawerHeader className="gap-1">
          <div className="flex items-center justify-between gap-2">
            <DrawerTitle className="truncate">{client ? client.name : t("clients.drawer.new")}</DrawerTitle>
            {client && <ClientStatus status={client.status} />}
          </div>
          <DrawerDescription>
            {client ? t("clients.drawer.created", { date: formatDate(client.createdAt, locale) }) : t("clients.drawer.newHint")}
          </DrawerDescription>
        </DrawerHeader>

        <div className="flex flex-col gap-4 overflow-y-auto px-4 text-sm">
          {client && (
            <>
              <Usage client={client} />
              <Separator />
              {canEdit && (
                <>
                  <Access clientId={client.id} version={accessVersion} />
                  <Separator />
                </>
              )}
            </>
          )}
          <ClientForm
            key={client?.id ?? "new"}
            client={client}
            formId="client-form"
            setPending={setPending}
            onSubmitted={(c) => {
              onChanged()
              setClient(c)
              setAccessVersion((v) => v + 1)
            }}
          />
        </div>

        <DrawerFooter>
          {canEdit && (
            <Button type="submit" form="client-form" disabled={pending}>
              {pending ? t("common.saving") : client ? t("common.save") : t("clients.form.create")}
            </Button>
          )}
          <div className="flex gap-2">
            <DrawerClose asChild>
              <Button variant="outline" className="flex-1">
                {t("common.close")}
              </Button>
            </DrawerClose>
            {client && canEdit && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="icon">
                    <EllipsisVerticalIcon />
                    <span className="sr-only">{t("clients.more")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" side="top" className="w-48">
                  <DropdownMenuItem onSelect={() => act(() => api.resetClientTraffic(client.id), t("clients.toast.reset"))}>
                    {t("clients.resetTraffic")}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onSelect={() =>
                      confirm({
                        title: t("clients.confirm.rotate"),
                        description: t("clients.confirm.rotateText"),
                        action: t("clients.rotate"),
                        onConfirm: () => act(() => api.rotateClient(client.id), t("clients.toast.rotated")),
                      })
                    }
                  >
                    {t("clients.rotate")}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() =>
                      confirm({
                        title: t("clients.confirm.delete", { name: client.name }),
                        description: t("clients.confirm.deleteText"),
                        action: t("common.delete"),
                        destructive: true,
                        onConfirm: () => act(() => api.deleteClient(client.id), t("clients.toast.deleted"), true),
                      })
                    }
                  >
                    {t("common.delete")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        </DrawerFooter>
        {confirmDialog}
      </DrawerContent>
    </Drawer>
  )
}
