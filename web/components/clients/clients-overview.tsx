"use client"

import * as React from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { SearchIcon } from "lucide-react"

import { ClientStatus } from "@/components/clients/client-status"
import { TrafficCell } from "@/components/clients/traffic-cell"
import { clientViews, filterClients } from "@/components/clients/views"
import { columnHelper, DataTable } from "@/components/data-table"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import type { Client } from "@/lib/api"
import { daysUntil, formatDate } from "@/lib/format"
import { useI18n } from "@/lib/i18n"

const col = columnHelper<Client>()

/**
 * The clients at a glance, for the overview: the same table as on the
 * clients page, tabs and page controls included, to look at and not to
 * edit. Whoever used the most comes first; a name leads to its row on the clients page, where things are changed.
 */
export const ClientsOverview = React.memo(function ClientsOverview({ clients, loading = false }: { clients: Client[]; loading?: boolean }) {
  const { t, tp, locale } = useI18n()
  const router = useRouter()
  const [query, setQuery] = React.useState("")
  const [view, setView] = React.useState("all")

  const columns = React.useMemo(
    () =>
      col.columns([
        col.accessor("name", {
          header: t("clients.col.name"),
          enableHiding: false,
          cell: ({ row }) => (
            <Link href={`/clients/#client-${row.original.id}`} className="font-medium underline-offset-4 hover:underline">
              {row.original.name}
            </Link>
          ),
        }),
        col.accessor("status", {
          header: t("clients.col.status"),
          cell: ({ row }) => <ClientStatus client={row.original} />,
        }),
        col.accessor((c) => c.upload + c.download, {
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
      ]),
    [t, tp, locale]
  )

  const rows = React.useMemo(() => filterClients(clients, view, query), [clients, view, query])
  const views = React.useMemo(() => clientViews(clients, t), [clients, t])

  return (
    <DataTable
      views={views}
      view={view}
      onViewChange={setView}
      pager="always"
      data={rows}
      loading={loading}
      columns={columns}
      getRowId={(r) => String(r.id)}
      onRowClick={(c) => router.push(`/clients/#client-${c.id}`)}
      initialSorting={[{ id: "traffic", desc: true }]}
      initialHidden={["note", "created"]}
      pageSize={10}
      columnLabels={{
        status: t("clients.col.status"),
        traffic: t("clients.col.traffic"),
        expires: t("clients.col.expires"),
        note: t("clients.col.note"),
        created: t("clients.col.created"),
      }}
      toolbar={
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
      }
      empty={clients.length === 0 ? t("overview.clients.empty") : undefined}
    />
  )
})
