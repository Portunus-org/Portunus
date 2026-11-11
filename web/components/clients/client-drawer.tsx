"use client"

import * as React from "react"
import Link from "next/link"
import { ArrowRightIcon, EllipsisVerticalIcon, KeyRoundIcon, QrCodeIcon, RotateCcwIcon, Trash2Icon } from "lucide-react"
import { toast } from "@/lib/toast"

import { ChainEdgeChips } from "@/components/chains/chain-edge-chips"
import { ClientStatus } from "@/components/clients/client-status"
import { useConfirm } from "@/components/confirm"
import { CopyButton } from "@/components/copy-button"
import { DateField } from "@/components/date-field"
import { FormField, SwitchField, FormError } from "@/components/form-field"
import { NumberInput } from "@/components/number-input"
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
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectSeparator, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { drawerScroll } from "@/lib/utils"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"
import { useIsMobile } from "@/hooks/use-mobile"
import { useFormErrors } from "@/lib/form-errors"
import { api, errorText, type Chain, type Client, type ClientAccess, type ClientInput, type Node } from "@/lib/api"
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

function ConnectionLabel({ label }: { label: string }) {
  const { t } = useI18n()
  const arrowMatch = label.includes(" → ") ? " → " : label.includes(" -> ") ? " -> " : null
  if (!arrowMatch) {
    return <span>{label === "Main" ? t("chains.main") : label}</span>
  }
  const parts = label.split(arrowMatch)
  return (
    <span className="inline-flex items-center gap-1.5">
      {parts.map((p, idx) => {
        const text = p === "Main" ? t("chains.main") : p
        return (
          <React.Fragment key={idx}>
            {idx > 0 && <ArrowRightIcon className="size-3.5 shrink-0 text-muted-foreground" />}
            <span>{text}</span>
          </React.Fragment>
        )
      })}
    </span>
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
                label: a.links[0].label || a.links[0].name || protocols[a.links[0].protocol].label,
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
      label: l.label || l.name || protocols[l.protocol].label,
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
          <span className="text-muted-foreground"><ConnectionLabel label={qr.label} /></span>
        </div>
      )}
      <div className="divide-y overflow-hidden rounded-lg border">
        {rows.map((r) => (
          <div key={r.key} className={cn("flex items-center gap-2 px-3 py-2", qr?.label === r.label && "bg-muted/50")}>
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium"><ConnectionLabel label={r.label} /></div>
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
      {access.links.length > 1 && (
        <p className="text-xs text-muted-foreground">{t("clients.drawer.entriesHint")}</p>
      )}
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
  const { errors, wrong, clear, fail, numbers } = useFormErrors()
  // The client's route is either one chain or an exit. One select carries
  // both: chain items are "c<id>", exits "xe<id>", the automatic one "x0".
  const [chainVal, setChainVal] = React.useState(String(client?.chainId ?? 0))
  const [exitVal, setExitVal] = React.useState(String(client?.exitId ?? 0))
  // The one select shows whichever of the two is in effect.
  const routeVal = chainVal !== "0" ? `c${chainVal}` : exitVal === "0" ? "x0" : `xe${exitVal}`

  function onRouteChange(v: string) {
    if (v.startsWith("c")) setChainVal(v.slice(1))
    else if (v === "x0") {
      setChainVal("0")
      setExitVal("0")
    } else {
      setChainVal("0")
      setExitVal(v.slice(2))
    }
  }
  const [allNodes, setAllNodes] = React.useState<Node[]>([])
  const [chains, setChains] = React.useState<Chain[]>([])
  const exits = allNodes.filter((n) => n.isExit && n.enabled)

  React.useEffect(() => {
    api.nodes().then(setAllNodes, () => {})
    api.chains().then(setChains, () => {})
  }, [])


  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!numbers(e.currentTarget)) return
    const f = new FormData(e.currentTarget)
    const gb = Number(f.get("limit") || 0)
    if (!Number.isFinite(gb) || gb < 0) {
      wrong("limit", t("clients.form.limitInvalid"))
      return
    }
    // Only the route fields that actually changed go to the server. Both
    // may change at once: switching from a chain to an exit in one save.
    const prevChain = client?.chainId ?? 0
    const prevExit = client?.exitId ?? 0
    const chainId = Number(chainVal)
    const exitId = Number(exitVal)
    const changes: Partial<ClientInput> = {}
    if (chainId !== prevChain) {
      // Picking or unpicking a chain takes over the route.
      changes.chainId = chainId
    }
    if (chainId === 0 && exitId !== prevExit) {
      changes.exitId = exitId
    }
    const input = {
      name: String(f.get("name") ?? ""),
      enabled,
      trafficLimit: Math.round(gb * GiB),
      expiresAt: fromDateInput(String(f.get("expires") ?? "")),
      note: String(f.get("note") ?? ""),
      ...changes,
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
      fail(err, ["name", "limit", "expires", "route", "note"], {
        trafficLimit: "limit",
        expiresAt: "expires",
        exitId: "route",
        chainId: "route",
      })
    } finally {
      setPending(false)
    }
  }

  return (
    <form id={formId} onSubmit={onSubmit} onInput={clear} className="flex flex-col gap-4" noValidate>
      <fieldset disabled={!canEdit} className="contents">
        {(exits.length > 0 || chains.length > 0) && (
          <FormField
            id="route"
            label={t("clients.form.route")}
            hint={t("clients.form.routeHint")}
            error={errors.route}
          >
            <Select value={routeVal} onValueChange={onRouteChange}>
              <SelectTrigger id="route" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {chains.length > 0 && (
                  <>
                    <SelectGroup>
                      <SelectLabel>{t("clients.form.routeChains")}</SelectLabel>
                      {chains.map((c) => (
                        <SelectItem key={c.id} value={`c${c.id}`}>
                          <span className="flex items-center gap-2">
                            <span
                              className="size-2 shrink-0 rounded-full bg-foreground"
                              style={c.color ? { backgroundColor: c.color } : undefined}
                            />
                            <span>{c.name}</span>
                          </span>
                        </SelectItem>
                      ))}
                    </SelectGroup>
                    <SelectSeparator />
                  </>
                )}
                <SelectGroup>
                  <SelectLabel>{t("clients.form.routeExits")}</SelectLabel>
                  <SelectItem value="x0">{t(client?.exitId || client?.chainId ? "clients.form.exitAnew" : "clients.form.exitAuto")}</SelectItem>
                  {exits.map((n) => (
                    <SelectItem key={n.id} value={`xe${n.id}`}>
                      {n.status.latencyMs === undefined ? n.name : t("clients.form.exitSpeed", { name: n.name, ms: n.status.latencyMs })}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {chainVal !== "0" &&
              (() => {
                const c = chains.find((x) => x.id === Number(chainVal))
                if (!c) return null
                return (
                  <div className="mt-2">
                    <ChainEdgeChips chain={c} nodes={allNodes} mainLabel={t("chains.main")} backupLabel={t("chains.backup")} />
                  </div>
                )
              })()}
          </FormField>
        )}
        <FormField id="name" label={t("clients.form.name")} error={errors.name}>
          <Input id="name" name="name" defaultValue={client?.name} required maxLength={64} autoFocus={!client} />
        </FormField>
        <div className="grid items-start gap-4 sm:grid-cols-2">
          <FormField id="limit" label={t("clients.form.limit")} error={errors.limit}>
            <NumberInput
              id="limit"
              name="limit"
              min={0}
              max={1048576}
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

  // The page itself shrinks above the keyboard (interactive-widget=resizes-content);
  // vaul resizing the drawer as well left it short once the keyboard was gone.
  return (
    <Drawer open={open} onOpenChange={onOpenChange} direction={isMobile ? "bottom" : "right"} repositionInputs={false}>
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
              key={client ? `${client.id}:${client.exitId ?? 0}:${client.chainId ?? 0}` : "new"}
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
