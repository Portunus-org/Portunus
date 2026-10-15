"use client"

import * as React from "react"
import Link from "next/link"
import { EllipsisVerticalIcon, KeyRoundIcon, QrCodeIcon, RotateCcwIcon, Trash2Icon } from "lucide-react"
import { toast } from "@/lib/toast"

import { ClientStatus } from "@/components/clients/client-status"
import { useConfirm } from "@/components/confirm"
import { CopyButton } from "@/components/copy-button"
import { DateField } from "@/components/date-field"
import { FormField, SwitchField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerFooter, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { drawerScroll } from "@/lib/utils"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"
import { useIsMobile } from "@/hooks/use-mobile"
import { useFormErrors } from "@/lib/form-errors"
import { api, errorText, type Client, type ClientAccess, type Node } from "@/lib/api"
import { daysUntil, formatBytes, formatDate, fromDateInput, GiB, toDateInput } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { useSettingsWindow } from "@/lib/settings-dialog"
import { protocols } from "@/lib/protocols"
import { useCanEdit } from "@/lib/session"
import { cn } from "@/lib/utils"

/** A scannable QR code: always dark on white, whatever the theme. */
export function QR({ svg, label, className }: { svg: string; label: string; className?: string }) {
  return (
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
            {" "}
            {client.trafficLimit > 0
              ? t("clients.ofLimit", {
                  limit: formatBytes(client.trafficLimit, locale),
                })
              : t("clients.drawer.unlimited")}
          </span>
        </span>
      </div>
      {client.trafficLimit > 0 && <Progress value={pct} />}
      <div className="flex justify-between gap-2 text-muted-foreground tabular-nums">
        <span>
          {t("overview.trafficSplit", {
            down: formatBytes(client.download, locale),
            up: formatBytes(client.upload, locale),
          })}
        </span>
        <span>
          {client.expiresAt
            ? days! > 0
              ? tp("clients.drawer.daysLeft", days!)
              : t("clients.drawer.expiredOn", {
                  date: formatDate(client.expiresAt, locale),
                })
            : t("clients.drawer.noExpiry")}
        </span>
      </div>
    </div>
  )
}

/**
 * How a client connects: the QR code, and the subscription and the links of
 * every connection to copy or to show as a code. Also used by the first
 * setup, right after its client is made.
 */
export function Access({ clientId, version = 0 }: { clientId: number; version?: number }) {
  const { openSettings } = useSettingsWindow()
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
            ? {
                svg: a.links[0].qr,
                label: a.links[0].name || protocols[a.links[0].protocol].label,
              }
            : null
        setQr(first)
      },
      (err) => alive && toast.error(errorText(err, t))
    )
    return () => {
      alive = false
    }
  }, [clientId, version, t])

  // In the shape of what is coming: the code, then a line per connection.
  if (!access)
    return (
      <div className="grid justify-items-center gap-4">
        <Skeleton className="size-44 rounded-xl" />
        <Skeleton className="h-14 w-full rounded-lg" />
      </div>
    )
  if (!access.publicHost) {
    return (
      <p className="text-muted-foreground">
        <button type="button" onClick={() => openSettings("server")} className="text-foreground underline underline-offset-4">
          {t("clients.drawer.noHost")}
        </button>
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
    ...access.links.map((l, i) => ({
      key: `l${i}`,
      // Links to a backup entry are named after it by the server.
      label: l.host === access.publicHost ? l.name || protocols[l.protocol].label : l.label,
      sub: t("clients.drawer.linkSub", {
        transport: protocols[l.protocol].transport,
        port: l.port,
      }),
      url: l.url,
      qr: l.qr,
    })),
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
  const { errors, wrong, clear, fail } = useFormErrors()
  // "0" has the panel choose an exit.
  const bound = String(client?.exitId ?? 0)
  const [exit, setExit] = React.useState(bound)
  const [exits, setExits] = React.useState<Node[]>([])

  React.useEffect(() => {
    api.nodes().then(
      (nodes) => setExits(nodes.filter((n) => n.isExit && n.enabled)),
      () => {}
    )
  }, [])

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const gb = Number(f.get("limit") || 0)
    if (!Number.isFinite(gb) || gb < 0) {
      wrong("limit", t("clients.form.limitInvalid"))
      return
    }
    const input = {
      name: String(f.get("name") ?? ""),
      enabled,
      trafficLimit: Math.round(gb * GiB),
      expiresAt: fromDateInput(String(f.get("expires") ?? "")),
      note: String(f.get("note") ?? ""),
      ...(exit !== bound ? { exitId: Number(exit) } : {}),
    }
    if (!input.name.trim()) {
      wrong("name", t("error.required"))
      return
    }
    setPending(true)
    clear()
    try {
      const saved = client ? await api.updateClient(client.id, input) : await api.createClient(input)
      toast.success(t(client ? "clients.toast.saved" : "clients.toast.created"))
      onSubmitted(saved)
    } catch (err) {
      fail(err, ["name", "limit", "expires", "exit", "note"], { trafficLimit: "limit", expiresAt: "expires", exitId: "exit" })
    } finally {
      setPending(false)
    }
  }

  return (
    <form id={formId} onSubmit={onSubmit} onInput={clear} className="flex flex-col gap-4" noValidate>
      <fieldset disabled={!canEdit} className="contents">
        <FormField id="name" label={t("clients.form.name")} error={errors.name}>
          <Input id="name" name="name" defaultValue={client?.name} required maxLength={64} autoFocus={!client} />
        </FormField>
        <div className="grid items-start gap-4 sm:grid-cols-2">
          <FormField id="limit" label={t("clients.form.limit")} error={errors.limit}>
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
          </FormField>
          <FormField id="expires" label={t("clients.form.expires")} error={errors.expires}>
            <DateField
              id="expires"
              name="expires"
              defaultValue={toDateInput(client?.expiresAt ?? null)}
              placeholder={t("common.never")}
              clearLabel={t("clients.form.noExpiry")}
            />
          </FormField>
        </div>
        {exits.length > 0 && (
          <FormField id="exit" label={t("clients.form.exit")} hint={t("clients.form.exitHint")} error={errors.exit}>
            <Select value={exit} onValueChange={setExit}>
              <SelectTrigger id="exit" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="0">{t(client?.exitId ? "clients.form.exitAnew" : "clients.form.exitAuto")}</SelectItem>
                  {exits.map((n) => (
                    <SelectItem key={n.id} value={String(n.id)}>
                      {n.status.latencyMs === undefined ? n.name : t("clients.form.exitSpeed", { name: n.name, ms: n.status.latencyMs })}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </FormField>
        )}
        <FormField id="note" label={t("clients.form.note")} error={errors.note}>
          <Textarea id="note" name="note" rows={2} maxLength={500} defaultValue={client?.note} />
        </FormField>
        <SwitchField id="client-enabled" label={t("clients.form.enabled")} checked={enabled} onCheckedChange={setEnabled} />
      </fieldset>
      <FormError>{errors.form}</FormError>
    </form>
  )
}

/** Fired on window by the sidebar's "New client" button. */
export const NEW_CLIENT_EVENT = "portunus:new-client"

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
        <DrawerHeader className="gap-1 text-left!">
          <div className="flex items-center justify-between gap-2">
            <DrawerTitle className="truncate">{client ? client.name : t("clients.drawer.new")}</DrawerTitle>
            {client && <ClientStatus client={client} />}
          </div>
          <DrawerDescription>
            {client
              ? t("clients.drawer.created", {
                  date: formatDate(client.createdAt, locale),
                })
              : t("clients.drawer.newHint")}
          </DrawerDescription>
        </DrawerHeader>

        <ScrollArea className={drawerScroll}>
          <div className="flex flex-col gap-4 px-4 text-sm">
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
              key={client ? `${client.id}:${client.exitId}` : "new"}
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
        </ScrollArea>

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
                    <RotateCcwIcon />
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
                    <KeyRoundIcon />
                    {t("clients.rotate")}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() =>
                      confirm({
                        title: t("clients.confirm.delete", {
                          name: client.name,
                        }),
                        description: t("clients.confirm.deleteText"),
                        action: t("common.delete"),
                        destructive: true,
                        onConfirm: () => act(() => api.deleteClient(client.id), t("clients.toast.deleted"), true),
                      })
                    }
                  >
                    <Trash2Icon />
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
