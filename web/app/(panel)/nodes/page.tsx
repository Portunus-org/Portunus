"use client"

import * as React from "react"
import Link from "next/link"
import { ArrowRightIcon, EllipsisVerticalIcon, PencilIcon, PlusIcon, PowerIcon, RefreshCwIcon, ServerIcon, SplitIcon, TerminalIcon, Trash2Icon } from "lucide-react"
import { toast } from "@/lib/toast"

import { ChainColorPicker } from "@/components/chains/chain-color-picker"
import { ChainEdgeChips } from "@/components/chains/chain-edge-chips"
import { useConfirm } from "@/components/confirm"
import { columnHelper, DataTable } from "@/components/data-table"
import { NodeDrawer } from "@/components/nodes/node-drawer"
import { NodeStatus } from "@/components/nodes/node-status"
import { NodesMapCard, type ChainDraft } from "@/components/nodes/nodes-map-card"
import { RuleDrawer, type DraftChainRule } from "@/components/routing/rule-drawer"
import { useTargetLabel } from "@/components/routing/target-select"
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
import { api, errorText, type Chain, type Node, type RouteRule } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useSession } from "@/lib/session"
import { useResource } from "@/lib/use-resource"
import { cn } from "@/lib/utils"

const col = columnHelper<Node>()
const chainCol = columnHelper<Chain>()

export default function NodesPage() {
  const { t } = useI18n()
  const isAdmin = useSession().user.role === "admin"
  const { data, reload } = useResource(api.nodes, 10_000)
  const { data: topology, reload: reloadTopology } = useResource(api.topology, 30_000)
  const { data: chains, reload: reloadChains } = useResource(api.chains, 10_000)
  const { data: routingData, reload: reloadRouting } = useResource(api.routing, 30_000)

  const [tab, setTab] = React.useState<"nodes" | "chains">("nodes")
  const [selected, setSelected] = React.useState<{
    node: Node
    join: boolean
  } | null>(null)
  const [drawerOpen, setDrawerOpen] = React.useState(false)
  const [drawerKey, setDrawerKey] = React.useState(0)
  const [chainDraft, setChainDraft] = React.useState<ChainDraft | null>(null)
  const [ruleDrawer, setRuleDrawer] = React.useState<{
    key: number
    rule: RouteRule | null
    chainId: number
    draftRule?: DraftChainRule | null
  } | null>(null)
  const [ruleOpen, setRuleOpen] = React.useState(false)
  const [checking, setChecking] = React.useState(false)
  const [confirm, confirmDialog] = useConfirm()

  const nodes = React.useMemo(() => data ?? [], [data])
  const chainList = React.useMemo(() => chains ?? [], [chains])
  const targetLabel = useTargetLabel(nodes)
  const listById = React.useMemo(
    () => new Map((routingData?.lists ?? []).map((l) => [l.id, l])),
    [routingData?.lists]
  )
  const rulesByChain = React.useMemo(() => {
    const m = new Map<number, RouteRule[]>()
    for (const r of routingData?.rules ?? []) {
      if (r.chainId != null) {
        m.set(r.chainId, [...(m.get(r.chainId) ?? []), r])
      }
    }
    return m
  }, [routingData?.rules])

  const open = React.useCallback((node: Node, join = false) => {
    React.startTransition(() => {
      setSelected({ node, join })
      setDrawerKey((k) => k + 1)
      setDrawerOpen(true)
    })
  }, [])

  const openChainEditor = React.useCallback((chain: Chain | null) => {
    setDrawerOpen(false)
    setTab("chains")
    if (chain) {
      setChainDraft({
        id: chain.id,
        name: chain.name,
        color: chain.color ?? "",
        edges: chain.edges.map(({ from, to }) => ({ from, to })),
        backups: [...chain.backups],
      })
    } else {
      setChainDraft({
        id: null,
        name: "",
        color: "",
        edges: [],
        backups: [],
      })
    }
  }, [])

  const openRuleForChain = React.useCallback((rule: RouteRule | null, chainId: number) => {
    setRuleDrawer((d) => ({ key: (d?.key ?? 0) + 1, rule, chainId, draftRule: undefined }))
    setRuleOpen(true)
  }, [])

  const openDraftRule = React.useCallback((draftRule: DraftChainRule | null) => {
    setRuleDrawer((d) => ({ key: (d?.key ?? 0) + 1, rule: null, chainId: -1, draftRule }))
    setRuleOpen(true)
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

  const actChain = React.useCallback(
    async (fn: () => Promise<unknown>, done: string) => {
      try {
        await fn()
        toast.success(done)
        reloadChains()
      } catch (err) {
        toast.error(errorText(err, t))
      }
    },
    [reloadChains, t]
  )

  const confirmDeleteChain = React.useCallback(
    (c: Chain) => {
      confirm({
        title: t("chains.confirm.delete", { name: c.name }),
        description: t("chains.confirm.deleteText"),
        action: t("common.delete"),
        destructive: true,
        onConfirm: () =>
          actChain(async () => {
            await api.deleteChain(c.id)
            setChainDraft((cur) => (cur?.id === c.id ? null : cur))
          }, t("chains.toast.deleted")),
      })
    },
    [actChain, confirm, t]
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
        col.accessor(
          (n) => [n.isExit && "exit", n.isEntry && "entry", n.standby && "standby", n.kind === "link" && "link"].filter(Boolean).join(","),
          {
            id: "roles",
            header: t("nodes.col.roles"),
            enableSorting: false,
            cell: ({ row }) => (
              <div className="flex flex-wrap gap-1.5 in-[dd]:justify-end">
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
          }
        ),
        col.accessor((n) => n.status.state, {
          id: "status",
          header: t("nodes.col.status"),
          cell: ({ row }) => <NodeStatus node={row.original} />,
        }),
        col.accessor((n) => (n.isExit ? n.clients : undefined), {
          id: "clients",
          header: t("nodes.col.clients"),
          cell: ({ row }) => (
            <div className="tabular-nums" title={t("nodes.clientsHint")}>
              {row.original.isExit && row.original.clients}
            </div>
          ),
        }),
        col.accessor((n) => n.status.latencyMs, {
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

  const chainColumns = React.useMemo(
    () =>
      chainCol.columns([
        chainCol.accessor("name", {
          header: t("nodes.col.name"),
          enableHiding: false,
          cell: ({ row }) => {
            const c = row.original
            return (
              <div className="flex items-center gap-1.5">
                {isAdmin ? (
                  <ChainColorPicker
                    value={c.color}
                    label={t("chains.color")}
                    onPick={async (color) => {
                      try {
                        await api.updateChain(c.id, {
                          name: c.name,
                          color,
                          edges: c.edges.map(({ from, to }) => ({ from, to })),
                          backups: [...c.backups],
                        })
                        reloadChains()
                      } catch (err) {
                        toast.error(errorText(err, t))
                      }
                    }}
                  />
                ) : (
                  <span
                    className="size-2.5 shrink-0 rounded-full bg-foreground"
                    style={c.color ? { backgroundColor: c.color } : undefined}
                  />
                )}
                <Button variant="link" className="w-fit px-0 text-left text-foreground" onClick={() => openChainEditor(c)}>
                  {c.name}
                </Button>
              </div>
            )
          },
        }),
        chainCol.accessor((c) => c.edges.length, {
          id: "route",
          header: t("clients.col.route"),
          enableSorting: false,
          cell: ({ row }) => (
            <ChainEdgeChips chain={row.original} nodes={nodes} mainLabel={t("chains.main")} backupLabel={t("chains.backup")} />
          ),
        }),
        chainCol.accessor((c) => (rulesByChain.get(c.id) ?? []).length, {
          id: "filters",
          header: t("chains.col.filters"),
          cell: ({ row }) => {
            const c = row.original
            const cRules = rulesByChain.get(c.id) ?? []
            return (
              <div className="flex flex-wrap items-center gap-1.5 in-[dd]:justify-end" onClick={(e) => e.stopPropagation()}>
                {cRules.length > 0 ? (
                  cRules.map((rule) => {
                    const list = listById.get(rule.listId)
                    return (
                      <Badge
                        key={rule.id}
                        variant="secondary"
                        className={cn("max-w-full cursor-pointer gap-1 overflow-hidden", !rule.enabled && "opacity-50")}
                        onClick={() => isAdmin && openRuleForChain(rule, c.id)}
                      >
                        <span className="truncate">{list?.name ?? "?"}</span>
                        <ArrowRightIcon className="size-3 shrink-0 text-muted-foreground" />
                        <span className="truncate">{targetLabel(rule)}</span>
                      </Badge>
                    )
                  })
                ) : (
                  <span className="text-xs text-muted-foreground">{t("chains.stage.noFilters")}</span>
                )}
                {isAdmin && (
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    onClick={() => openRuleForChain(null, c.id)}
                  >
                    <PlusIcon />
                    {t("chains.stage.addFilter")}
                  </Button>
                )}
              </div>
            )
          },
        }),
        chainCol.accessor("clients", {
          header: t("nodes.col.clients"),
          cell: ({ row }) => <div className="tabular-nums">{row.original.clients}</div>,
        }),
        chainCol.display({
          id: "actions",
          cell: ({ row }) => {
            const c = row.original
            if (!isAdmin) return null
            return (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="flex size-8 text-muted-foreground data-[state=open]:bg-muted" size="icon">
                    <EllipsisVerticalIcon />
                    <span className="sr-only">{t("common.actions")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-48">
                  <DropdownMenuItem onSelect={() => openChainEditor(c)}>
                    <PencilIcon />
                    {t("common.edit")}
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => openRuleForChain(null, c.id)}>
                    <SplitIcon />
                    {t("chains.stage.addFilter")}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onSelect={() => confirmDeleteChain(c)}>
                    <Trash2Icon />
                    {t("common.delete")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )
          },
        }),
      ]),
    [confirmDeleteChain, isAdmin, listById, nodes, openChainEditor, openRuleForChain, reloadChains, rulesByChain, t, targetLabel]
  )

  const addNodeBtn = isAdmin && (
    <Button variant="outline" size="sm" asChild>
      <Link href="/nodes/new/">
        <PlusIcon />
        <span className="sr-only lg:not-sr-only">{t("nodes.add")}</span>
      </Link>
    </Button>
  )

  const tableViews = React.useMemo(
    () => [
      { value: "nodes", label: t("nodes.title"), count: nodes.length },
      { value: "chains", label: t("chains.title"), count: chainList.length },
    ],
    [chainList.length, nodes.length, t]
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
                chains={chainList}
                draft={chainDraft}
                onDraftChange={(next) => {
                  setChainDraft(next)
                  if (next) setTab("chains")
                }}
                onSavedChain={() => {
                  reloadChains()
                  reloadRouting()
                }}
                onDeleteChain={confirmDeleteChain}
                rules={routingData?.rules ?? []}
                lists={routingData?.lists ?? []}
                onOpenRule={isAdmin ? openRuleForChain : undefined}
                onOpenDraftRule={isAdmin ? openDraftRule : undefined}
              />
            </div>
          )}
          {tab === "nodes" ? (
            <DataTable
              views={tableViews}
              view={tab}
              onViewChange={(v) => setTab(v as "nodes" | "chains")}
              data={nodes}
              columns={columns}
              getRowId={(n) => String(n.id)}
              onRowClick={(n) => open(n)}
              columnLabels={{
                address: t("nodes.col.address"),
                roles: t("nodes.col.roles"),
                status: t("nodes.col.status"),
                clients: t("nodes.col.clients"),
                latency: t("nodes.col.latency"),
              }}
              toolbar={
                <>
                  <Button variant="outline" size="sm" onClick={check} disabled={checking}>
                    <RefreshCwIcon className={checking ? "animate-spin" : undefined} />
                    <span className="sr-only lg:not-sr-only">{t(checking ? "nodes.checking" : "nodes.check")}</span>
                  </Button>
                  {addNodeBtn}
                </>
              }
              loading={!data}
            />
          ) : (
            <DataTable
              views={tableViews}
              view={tab}
              onViewChange={(v) => setTab(v as "nodes" | "chains")}
              data={chainList}
              columns={chainColumns}
              getRowId={(c) => String(c.id)}
              onRowClick={(c) => openChainEditor(c)}
              columnLabels={{
                route: t("clients.col.route"),
                filters: t("chains.col.filters"),
                clients: t("nodes.col.clients"),
              }}
              toolbar={
                isAdmin &&
                nodes.length > 0 && (
                  <Button variant="outline" size="sm" onClick={() => openChainEditor(null)}>
                    <PlusIcon />
                    <span className="sr-only lg:not-sr-only">{t("chains.new")}</span>
                  </Button>
                )
              }
              loading={!chains}
              empty={t("chains.empty")}
            />
          )}
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
      {ruleDrawer && (
        <RuleDrawer
          key={ruleDrawer.key}
          open={ruleOpen}
          onOpenChange={setRuleOpen}
          rule={ruleDrawer.rule}
          list={ruleDrawer.rule ? (listById.get(ruleDrawer.rule.listId) ?? null) : null}
          nodes={nodes}
          chains={chainList}
          initialScope={ruleDrawer.chainId >= 0 ? ruleDrawer.chainId : undefined}
          onSaved={reloadRouting}
          draftRule={ruleDrawer.draftRule}
          draftChainName={chainDraft?.name}
          onSaveDraft={
            ruleDrawer.chainId < 0
              ? (savedDraft) => {
                  setChainDraft((cur) => {
                    if (!cur) return cur
                    const existing = cur.pendingRules ?? []
                    const idx = existing.findIndex((r) => r.key === savedDraft.key)
                    const nextRules =
                      idx >= 0 ? existing.map((r, i) => (i === idx ? savedDraft : r)) : [...existing, savedDraft]
                    return { ...cur, pendingRules: nextRules }
                  })
                }
              : undefined
          }
          onDeleteDraft={
            ruleDrawer.chainId < 0 && ruleDrawer.draftRule
              ? () => {
                  const delKey = ruleDrawer.draftRule?.key
                  if (!delKey) return
                  setChainDraft((cur) =>
                    cur ? { ...cur, pendingRules: (cur.pendingRules ?? []).filter((r) => r.key !== delKey) } : cur
                  )
                }
              : undefined
          }
        />
      )}
      {confirmDialog}
    </>
  )
}
