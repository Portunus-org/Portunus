"use client"

import * as React from "react"
import { EllipsisVerticalIcon, PencilIcon, PlusIcon, PowerIcon, RotateCcwIcon, SearchIcon, Trash2Icon } from "lucide-react"
import { toast } from "@/lib/toast"

import { ClientDrawer, NEW_CLIENT_EVENT } from "@/components/clients/client-drawer"
import { ClientStatus } from "@/components/clients/client-status"
import { TrafficCell } from "@/components/clients/traffic-cell"
import { clientViews, filterClients } from "@/components/clients/views"
import { useConfirm } from "@/components/confirm"
import { columnHelper, DataTable } from "@/components/data-table"
import { Badge } from "@/components/ui/badge"
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
import { daysUntil, formatDate } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { useCanEdit } from "@/lib/session"
import { useResource } from "@/lib/use-resource"

const col = columnHelper<Client>()
const used = (c: Client) => c.upload + c.download

export default function ClientsPage() {
  const { t, tp, locale } = useI18n()
  const canEdit = useCanEdit()
  const { data, reload } = useResource(api.clients, 10_000)
  const [view, setView] = React.useState("all")
  const [query, setQuery] = React.useState("")
  const [selected, setSelected] = React.useState<Client | null>(null)
  const [drawerOpen, setDrawerOpen] = React.useState(false)
  const [confirm, confirmDialog] = useConfirm()

  // Bumped on every open so the drawer starts from a clean state: after a
  // client is created the drawer shows it, and the next "new" must be empty.
  const [drawerKey, setDrawerKey] = React.useState(0)

  const open = React.useCallback((c: Client | null) => {
    // Rendered in the background and shown when ready: building the drawer
    // must not hold up the frame of the click.
    React.startTransition(() => {
      setSelected(c)
      setDrawerKey((k) => k + 1)
      setDrawerOpen(true)
    })
  }, [])

  // A client named on the overview comes here as #client-<id>: the table
  // turns to its row and lights it up.
  const [found, setFound] = React.useState<string | null>(null)

  // The sidebar's "New client" button links here with #new.
  React.useEffect(() => {
    const onHash = () => {
      const hash = window.location.hash
      if (hash === "#new" && canEdit) {
        open(null)
        history.replaceState(null, "", window.location.pathname)
      }
      if (hash.startsWith("#client-")) {
        setFound(hash.slice("#client-".length))
        history.replaceState(null, "", window.location.pathname)
      }
    }
    onHash()
    // Client-side navigation to the same page changes the hash without a
    // hashchange event, so the sidebar button also announces itself.
    const onNew = () => canEdit && open(null)
    window.addEventListener("hashchange", onHash)
    window.addEventListener(NEW_CLIENT_EVENT, onNew)
    return () => {
      window.removeEventListener("hashchange", onHash)
      window.removeEventListener(NEW_CLIENT_EVENT, onNew)
    }
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
          cell: ({ row }) => <ClientStatus client={row.original} />,
        }),
        col.accessor(used, {
          id: "traffic",
          header: t("clients.col.traffic"),
          cell: ({ row }) => <TrafficCell client={row.original} />,
        }),
        col.accessor((c) => (c.expiresAt ? new Date(c.expiresAt).getTime() : Infinity), {
          id: "expires",
          header: t("clients.col.expires"),
          cell: ({ row }) => {
            const e = row.original.expiresAt
            if (!e) return <span className="text-muted-foreground">{t("common.never")}</span>
            const d = daysUntil(e)
            return (
              <span className="flex items-center gap-2 tabular-nums">
                {formatDate(e, locale)}
                {d > 0 && d <= 7 && (
                  <Badge variant="outline" className="px-1.5 text-muted-foreground">
                    {tp("clients.drawer.daysLeft", d)}
                  </Badge>
                )}
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
            return (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="flex size-8 text-muted-foreground data-[state=open]:bg-muted" size="icon">
                    <EllipsisVerticalIcon />
                    <span className="sr-only">{t("common.actions")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-44">
                  <DropdownMenuItem onSelect={() => open(c)}>
                    <PencilIcon />
                    {t("common.edit")}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onSelect={() =>
                      act(
                        () =>
                          api.updateClient(c.id, {
                            name: c.name,
                            trafficLimit: c.trafficLimit,
                            expiresAt: c.expiresAt,
                            note: c.note,
                            enabled: !c.enabled,
                          }),
                        t(c.enabled ? "clients.toast.disabled" : "clients.toast.enabled")
                      )
                    }
                  >
                    <PowerIcon />
                    {t(c.enabled ? "common.disable" : "common.enable")}
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => act(() => api.resetClientTraffic(c.id), t("clients.toast.reset"))}>
                    <RotateCcwIcon />
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
                    <Trash2Icon />
                    {t("common.delete")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )
          },
        }),
      ]),
    [act, canEdit, confirm, open, t, tp, locale]
  )

  // The table is built only when what it shows changes: opening a drawer
  // re-renders this page, and twenty rows of cells should not be redone for
  // that in the very frame the drawer starts to move.
  const table = React.useMemo(() => {
    const clients = data ?? []
    const rows = filterClients(clients, view, query)
    return (
      <DataTable
        data={rows}
        columns={columns}
        getRowId={(r) => String(r.id)}
        found={found}
        views={clientViews(clients, t)}
        view={view}
        onViewChange={setView}
        initialHidden={["created"]}
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
                <span className="sr-only lg:not-sr-only">{t("clients.add")}</span>
              </Button>
            )}
          </>
        }
        loading={!data}
        empty={clients.length === 0 ? t("clients.empty") : t("common.nothingMatches")}
      />
    )
  }, [data, view, query, columns, t, canEdit, open, found])

  return (
    <>
      {table}

      <ClientDrawer key={drawerKey} open={drawerOpen} onOpenChange={setDrawerOpen} client={selected} onChanged={reload} />
      {confirmDialog}
    </>
  )
}
