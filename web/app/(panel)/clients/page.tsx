"use client"

import * as React from "react"
import { EllipsisVerticalIcon, PlusIcon, SearchIcon } from "lucide-react"
import { toast } from "sonner"

import { ClientDrawer } from "@/components/clients/client-drawer"
import { ClientStatus } from "@/components/clients/client-status"
import { useConfirm } from "@/components/confirm"
import { columnHelper, DataTable } from "@/components/data-table"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { api, errorText, type Client } from "@/lib/api"
import { daysUntil, formatBytes, formatDate } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { useCanEdit } from "@/lib/session"
import { useResource } from "@/lib/use-resource"

const col = columnHelper<Client>()
const used = (c: Client) => c.upload + c.download

export default function ClientsPage() {
  const { t, locale } = useI18n()
  const canEdit = useCanEdit()
  const { data, reload } = useResource(api.clients, 30_000)
  const [view, setView] = React.useState("all")
  const [query, setQuery] = React.useState("")
  const [selected, setSelected] = React.useState<Client | null>(null)
  const [drawerOpen, setDrawerOpen] = React.useState(false)
  const [confirm, confirmDialog] = useConfirm()

  const open = React.useCallback((c: Client | null) => {
    setSelected(c)
    setDrawerOpen(true)
  }, [])

  // The sidebar's "New client" button links here with #new.
  React.useEffect(() => {
    const onHash = () => {
      if (window.location.hash === "#new" && canEdit) {
        open(null)
        history.replaceState(null, "", window.location.pathname)
      }
    }
    onHash()
    window.addEventListener("hashchange", onHash)
    return () => window.removeEventListener("hashchange", onHash)
  }, [canEdit, open])

  const act = React.useCallback(
    async (fn: () => Promise<unknown>, done: string) => {
      try {
        await fn()
        toast.success(done)
        reload()
      } catch (err) {
        toast.error(errorText(err, t))
      }
    },
    [reload, t]
  )

  const columns = React.useMemo(
    () =>
      col.columns([
        col.accessor("name", {
          header: t("clients.col.name"),
          enableHiding: false,
          cell: ({ row }) => (
            <Button variant="link" className="w-fit px-0 text-left text-foreground" onClick={() => open(row.original)}>
              {row.original.name}
            </Button>
          ),
        }),
        col.accessor("status", {
          header: t("clients.col.status"),
          cell: ({ getValue }) => <ClientStatus status={getValue()} />,
        }),
        col.accessor(used, {
          id: "traffic",
          header: () => <div className="w-full text-right">{t("clients.col.traffic")}</div>,
          cell: ({ row }) => (
            <div className="text-right tabular-nums">
              {formatBytes(used(row.original), locale)}
              {row.original.trafficLimit > 0 && (
                <span className="text-muted-foreground"> / {formatBytes(row.original.trafficLimit, locale)}</span>
              )}
            </div>
          ),
        }),
        col.accessor((c) => (c.expiresAt ? new Date(c.expiresAt).getTime() : Infinity), {
          id: "expires",
          header: t("clients.col.expires"),
          cell: ({ row }) => {
            const e = row.original.expiresAt
            if (!e) return <span className="text-muted-foreground">{t("common.never")}</span>
            const d = daysUntil(e)
            return (
              <span className="tabular-nums">
                {formatDate(e, locale)}
                {d > 0 && d <= 7 && <span className="text-muted-foreground"> · {d}</span>}
              </span>
            )
          },
        }),
        col.accessor("note", {
          header: t("clients.col.note"),
          enableSorting: false,
          cell: ({ getValue }) => <span className="line-clamp-1 max-w-64 text-muted-foreground">{getValue()}</span>,
        }),
        col.accessor((c) => new Date(c.createdAt).getTime(), {
          id: "created",
          header: t("clients.col.created"),
          cell: ({ row }) => <span className="text-muted-foreground tabular-nums">{formatDate(row.original.createdAt, locale)}</span>,
        }),
        col.display({
          id: "actions",
          cell: ({ row }) => {
            const c = row.original
            if (!canEdit) return null
            const input = { name: c.name, trafficLimit: c.trafficLimit, expiresAt: c.expiresAt, note: c.note }
            return (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="flex size-8 text-muted-foreground data-[state=open]:bg-muted" size="icon">
                    <EllipsisVerticalIcon />
                    <span className="sr-only">{t("common.actions")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-44">
                  <DropdownMenuItem onSelect={() => open(c)}>{t("common.open")}</DropdownMenuItem>
                  <DropdownMenuItem
                    onSelect={() =>
                      act(
                        () => api.updateClient(c.id, { ...input, enabled: !c.enabled }),
                        t(c.enabled ? "clients.toast.disabled" : "clients.toast.enabled")
                      )
                    }
                  >
                    {t(c.enabled ? "common.disable" : "common.enable")}
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => act(() => api.resetClientTraffic(c.id), t("clients.toast.reset"))}>
                    {t("clients.resetTraffic")}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() =>
                      confirm({
                        title: t("clients.confirm.delete", { name: c.name }),
                        description: t("clients.confirm.deleteText"),
                        action: t("common.delete"),
                        destructive: true,
                        onConfirm: () => act(() => api.deleteClient(c.id), t("clients.toast.deleted")),
                      })
                    }
                  >
                    {t("common.delete")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )
          },
        }),
      ]),
    [act, canEdit, confirm, open, t, locale]
  )

  const clients = data ?? []
  const needsAttention = (c: Client) => c.status === "expired" || c.status === "limited"
  const byView: Record<string, (c: Client) => boolean> = {
    all: () => true,
    active: (c) => c.status === "active",
    attention: needsAttention,
    disabled: (c) => c.status === "disabled",
  }
  const count = (f: (c: Client) => boolean) => clients.filter(f).length
  const q = query.trim().toLowerCase()
  const rows = clients.filter((c) => byView[view](c) && (!q || c.name.toLowerCase().includes(q) || c.note.toLowerCase().includes(q)))

  return (
    <>
      <DataTable
        data={rows}
        columns={columns}
        getRowId={(r) => String(r.id)}
        views={[
          { value: "all", label: t("clients.view.all") },
          { value: "active", label: t("clients.view.active"), count: count(byView.active) },
          { value: "attention", label: t("clients.view.attention"), count: count(needsAttention) },
          { value: "disabled", label: t("clients.view.disabled"), count: count(byView.disabled) },
        ]}
        view={view}
        onViewChange={setView}
        columnLabels={{
          status: t("clients.col.status"),
          traffic: t("clients.col.traffic"),
          expires: t("clients.col.expires"),
          note: t("clients.col.note"),
          created: t("clients.col.created"),
        }}
        toolbar={
          <>
            <div className="relative">
              <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t("common.search")}
                aria-label={t("common.search")}
                className="h-7 w-32 pl-8 text-sm lg:w-48"
              />
            </div>
            {canEdit && (
              <Button variant="outline" size="sm" onClick={() => open(null)}>
                <PlusIcon />
                <span className="hidden lg:inline">{t("clients.add")}</span>
              </Button>
            )}
          </>
        }
        empty={!data ? t("common.loading") : clients.length === 0 ? t("clients.empty") : t("common.nothingMatches")}
      />

      <ClientDrawer key={selected?.id ?? "new"} open={drawerOpen} onOpenChange={setDrawerOpen} client={selected} onChanged={reload} />
      {confirmDialog}
    </>
  )
}
