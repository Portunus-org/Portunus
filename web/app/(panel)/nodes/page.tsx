"use client"

import * as React from "react"
import Link from "next/link"
import { EllipsisVerticalIcon, PencilIcon, PlusIcon, PowerIcon, RefreshCwIcon, ServerIcon, TerminalIcon, Trash2Icon } from "lucide-react"
import { toast } from "@/lib/toast"

import { useConfirm } from "@/components/confirm"
import { columnHelper, DataTable } from "@/components/data-table"
import { NodeDrawer } from "@/components/nodes/node-drawer"
import { NodeStatus } from "@/components/nodes/node-status"
import { NodesMapCard } from "@/components/nodes/nodes-map-card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { api, errorText, type Node } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useSession } from "@/lib/session"
import { useResource } from "@/lib/use-resource"

const col = columnHelper<Node>()

export default function NodesPage() {
  const { t } = useI18n()
  const isAdmin = useSession().user.role === "admin"
  const { data, reload } = useResource(api.nodes, 10_000)
  const { data: topology, reload: reloadTopology } = useResource(api.topology, 30_000)
  const [selected, setSelected] = React.useState<{
    node: Node
    join: boolean
  } | null>(null)
  const [drawerOpen, setDrawerOpen] = React.useState(false)
  const [drawerKey, setDrawerKey] = React.useState(0)
  const [checking, setChecking] = React.useState(false)
  const [confirm, confirmDialog] = useConfirm()

  const open = React.useCallback((node: Node, join = false) => {
    // Rendered in the background and shown when ready: building the drawer
    // must not hold up the frame of the click.
    React.startTransition(() => {
      setSelected({ node, join })
      setDrawerKey((k) => k + 1)
      setDrawerOpen(true)
    })
  }, [])

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

  async function check() {
    setChecking(true)
    try {
      await api.checkNodes()
      await reload()
    } catch (err) {
      toast.error(errorText(err, t))
    } finally {
      setChecking(false)
    }
  }

  const columns = React.useMemo(
    () =>
      col.columns([
        col.accessor("name", {
          header: t("nodes.col.name"),
          enableHiding: false,
          cell: ({ row }) => (
            <Button variant="link" className="w-fit px-0 text-left text-foreground" onClick={() => open(row.original)}>
              {row.original.name}
            </Button>
          ),
        }),
        col.accessor("address", {
          header: t("nodes.col.address"),
          cell: ({ getValue }) => <span className="font-mono text-muted-foreground">{getValue()}</span>,
        }),
        col.display({
          id: "roles",
          header: t("nodes.col.roles"),
          cell: ({ row }) => (
            <div className="flex flex-wrap gap-1.5">
              {row.original.isExit && (
                <Badge variant="outline" className="px-1.5 text-muted-foreground">
                  {t("nodes.role.exit")}
                </Badge>
              )}
              {row.original.isEntry && (
                <Badge variant="outline" className="px-1.5 text-muted-foreground">
                  {t("nodes.role.entry")}
                </Badge>
              )}
              {row.original.standby && (
                <Badge variant="outline" className="px-1.5 text-muted-foreground">
                  {t("nodes.role.standby")}
                </Badge>
              )}
              {row.original.kind === "link" && (
                <Badge variant="outline" className="px-1.5 text-muted-foreground">
                  {t("nodes.kind.link")}
                </Badge>
              )}
            </div>
          ),
        }),
        col.accessor((n) => n.status.state, {
          id: "status",
          header: t("nodes.col.status"),
          cell: ({ row }) => <NodeStatus node={row.original} />,
        }),
        col.accessor((n) => n.clients, {
          id: "clients",
          // Under the first letter of its heading, like every other column:
          // a number set against the far edge of a wide column reads as
          // belonging to the next one.
          header: t("nodes.col.clients"),
          cell: ({ row }) => (
            <div className="tabular-nums" title={t("nodes.clientsHint")}>
              {row.original.isExit && row.original.clients}
            </div>
          ),
        }),
        col.accessor((n) => n.status.latencyMs ?? Infinity, {
          id: "latency",
          header: t("nodes.col.latency"),
          cell: ({ row }) => (
            <div className="tabular-nums" title={t("nodes.latencyHint")}>
              {row.original.status.latencyMs !== undefined && t("nodes.latency", { ms: row.original.status.latencyMs })}
            </div>
          ),
        }),
        col.display({
          id: "actions",
          cell: ({ row }) => {
            const n = row.original
            if (!isAdmin) return null
            return (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="flex size-8 text-muted-foreground data-[state=open]:bg-muted" size="icon">
                    <EllipsisVerticalIcon />
                    <span className="sr-only">{t("common.actions")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-52">
                  <DropdownMenuItem onSelect={() => open(n)}>
                    <PencilIcon />
                    {t("common.edit")}
                  </DropdownMenuItem>
                  {n.kind === "agent" && (
                    <DropdownMenuItem onSelect={() => open(n, true)}>
                      <TerminalIcon />
                      {t("nodes.join")}
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem
                    onSelect={() =>
                      act(
                        () =>
                          api.updateNode(n.id, {
                            name: n.name,
                            isExit: n.isExit,
                            isEntry: n.isEntry,
                            enabled: !n.enabled,
                            hopPort: n.hopPort,
                            agentPort: n.agentPort,
                          }),
                        t(n.enabled ? "nodes.toast.disabled" : "nodes.toast.enabled")
                      )
                    }
                  >
                    <PowerIcon />
                    {t(n.enabled ? "common.disable" : "common.enable")}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() =>
                      confirm({
                        title: t("nodes.confirm.delete", { name: n.name }),
                        description: t(n.kind === "agent" ? "nodes.confirm.deleteAgent" : "nodes.confirm.deleteLink"),
                        action: t("common.delete"),
                        destructive: true,
                        onConfirm: () => act(() => api.deleteNode(n.id), t("nodes.toast.deleted")),
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
    [act, confirm, isAdmin, open, t]
  )

  const nodes = data ?? []
  const add = isAdmin && (
    <Button variant="outline" size="sm" asChild>
      <Link href="/nodes/new/">
        <PlusIcon />
        <span className="sr-only lg:not-sr-only">{t("nodes.add")}</span>
      </Link>
    </Button>
  )

  return (
    <>
      {data && nodes.length === 0 ? (
        <div className="px-4 lg:px-6">
          <Empty className="border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ServerIcon />
              </EmptyMedia>
              <EmptyTitle>{t("nodes.empty.title")}</EmptyTitle>
              <EmptyDescription>{t("nodes.empty.text")}</EmptyDescription>
            </EmptyHeader>
            {isAdmin && (
              <EmptyContent>
                <Button asChild>
                  <Link href="/nodes/new/">
                    <PlusIcon />
                    {t("nodes.add")}
                  </Link>
                </Button>
              </EmptyContent>
            )}
          </Empty>
        </div>
      ) : (
        <>
          {!data && (
            <div className="px-4 lg:px-6">
              <Skeleton className="h-96 w-full rounded-xl" />
            </div>
          )}
          {data && (
            <div className="px-4 lg:px-6">
              <NodesMapCard
                main={topology?.main}
                tiles={topology?.tiles ?? false}
                mapStyle={topology?.map}
                nodes={nodes}
                canEdit={isAdmin}
                onChanged={() => {
                  reload()
                  reloadTopology()
                }}
              />
            </div>
          )}
          <DataTable
            data={nodes}
            columns={columns}
            getRowId={(n) => String(n.id)}
            onRowClick={(n) => open(n)}
            columnLabels={{
              address: t("nodes.col.address"),
              roles: t("nodes.col.roles"),
              status: t("nodes.col.status"),
              latency: t("nodes.col.latency"),
            }}
            toolbar={
              <>
                <Button variant="outline" size="sm" onClick={check} disabled={checking}>
                  <RefreshCwIcon className={checking ? "animate-spin" : undefined} />
                  <span className="sr-only lg:not-sr-only">{t(checking ? "nodes.checking" : "nodes.check")}</span>
                </Button>
                {add}
              </>
            }
            loading={!data}
          />
        </>
      )}

      {selected && (
        <NodeDrawer
          key={drawerKey}
          open={drawerOpen}
          onOpenChange={setDrawerOpen}
          node={selected.node}
          showJoin={selected.join}
          main={topology?.main}
          tiles={topology?.tiles ?? false}
          mapStyle={topology?.map}
          nodes={nodes}
          onSaved={reload}
        />
      )}
      {confirmDialog}
    </>
  )
}
