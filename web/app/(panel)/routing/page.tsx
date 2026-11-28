"use client"

import * as React from "react"
import Link from "next/link"
import { ArrowDownIcon, ArrowUpIcon, EllipsisVerticalIcon, PencilIcon, PlusIcon, RefreshCwIcon, Trash2Icon } from "lucide-react"
import { toast } from "@/lib/toast"

import { useConfirm } from "@/components/confirm"
import { DefaultRouteDrawer } from "@/components/routing/default-route-drawer"
import { RuleDrawer } from "@/components/routing/rule-drawer"
import { useTargetLabel } from "@/components/routing/target-select"
import { StatusBadge } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useIsMobile } from "@/hooks/use-mobile"
import { cardTap } from "@/lib/card-tap"
import { cn } from "@/lib/utils"
import { api, errorText, type Chain, type Node, type RouteList, type RouteRule, type RouteTarget } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { useCanEdit } from "@/lib/session"
import { useResource } from "@/lib/use-resource"

/** What a list holds, in words: "120 sites, 3 addresses". */
function useListSummary() {
  const { tp } = useI18n()
  return (l: RouteList) =>
    [l.domains > 0 && tp("routing.list.sites", l.domains), l.addresses > 0 && tp("routing.list.addresses", l.addresses)]
      .filter(Boolean)
      .join(", ")
}

/** A target that cannot carry traffic right now: its exit is gone or off. */
function targetBroken(target: RouteTarget, nodes: Node[], chains: Chain[]): boolean {
  const exits = nodes.filter((n) => n.isExit && n.enabled)
  if (target.action === "auto") return exits.length === 0 && chains.length === 0
  if (target.action === "node") return !exits.some((n) => n.id === target.nodeId)
  return false
}

export default function RoutingPage() {
  const isMobile = useIsMobile()
  const { t, tp, locale } = useI18n()
  const canEdit = useCanEdit()
  const { data: routing, reload } = useResource(api.routing)
  const { data: nodeData } = useResource(api.nodes, 30_000)
  const { data: chainData, reload: reloadChains } = useResource(api.chains, 30_000)
  const nodes = React.useMemo(() => nodeData ?? [], [nodeData])
  const chains = React.useMemo(() => chainData ?? [], [chainData])
  const targetLabel = useTargetLabel(nodes)
  const summary = useListSummary()
  const [confirm, confirmDialog] = useConfirm()

  const [ruleDrawer, setRuleDrawer] = React.useState<{
    key: number
    rule: RouteRule | null
  } | null>(null)
  const [ruleOpen, setRuleOpen] = React.useState(false)
  // Which chain's rules are on screen; null shows every rule. A chain card
  // links here as #chain-<id> so its filters open straight away.
  const [scopeFilter, setScopeFilter] = React.useState<number | null>(null)

  React.useEffect(() => {
    const onHash = () => {
      const m = /^#chain-(\d+)$/.exec(window.location.hash)
      if (m) setScopeFilter(Number(m[1]))
      history.replaceState(null, "", window.location.pathname)
    }
    onHash()
    window.addEventListener("hashchange", onHash)
    return () => window.removeEventListener("hashchange", onHash)
  }, [])
  const [defaultDrawer, setDefaultDrawer] = React.useState<{ key: number; scope: number } | null>(null)
  const [defaultOpen, setDefaultOpen] = React.useState(false)

  const openRule = (rule: RouteRule | null) => {
    setRuleDrawer((d) => ({ key: (d?.key ?? 0) + 1, rule }))
    setRuleOpen(true)
  }

  const openDefault = (scope: number) => {
    setDefaultDrawer((d) => ({ key: (d?.key ?? 0) + 1, scope }))
    setDefaultOpen(true)
  }

  const act = async (fn: () => Promise<unknown>, done?: string) => {
    try {
      await fn()
      if (done) toast.success(done)
    } catch (err) {
      toast.error(errorText(err, t))
    } finally {
      reload()
    }
  }

  if (!routing) {
    return (
      <div className="grid gap-4 px-4 lg:px-6">
        <Skeleton className="h-32 w-full rounded-xl" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    )
  }

  const activeChain = scopeFilter != null ? chains.find((c) => c.id === scopeFilter) : undefined
  const effectiveScope = activeChain ? activeChain.id : null
  const rules = routing.rules
  const visibleRules = effectiveScope == null ? rules : rules.filter((r) => r.chainId === effectiveScope)
  const listById = new Map(routing.lists.map((l) => [l.id, l]))
  // Ordering is global, so a filtered view still moves the rule among all of them.
  const move = (ruleId: number, by: number) => {
    const ids = rules.map((r) => r.id)
    const i = ids.indexOf(ruleId)
    if (i < 0 || i + by < 0 || i + by >= ids.length) return
    const [id] = ids.splice(i, 1)
    ids.splice(i + by, 0, id)
    act(() => api.orderRouteRules(ids))
  }

  const hasExits = nodes.some((n) => n.isExit)
  const showScope = chains.length > 0 || rules.some((r) => r.chainId != null)
  const defaultIsDirect = activeChain ? (activeChain.default ?? routing.default).action === "direct" : routing.default.action === "direct"
  const defaultBroken = activeChain
    ? !!activeChain.default && activeChain.default.action === "node" && targetBroken(activeChain.default, nodes, chains)
    : targetBroken(routing.default, nodes, chains)
  const defaultScopeId = activeChain?.id ?? 0
  const defaultTargetText = activeChain
    ? activeChain.default
      ? targetLabel(activeChain.default)
      : t("routing.default.inheritOf", { target: targetLabel(routing.default) })
    : targetLabel(routing.default)
  const defaultScopeBadge = activeChain ? (
    <Badge variant="outline" className="gap-1.5 px-1.5 text-muted-foreground">
      <span
        className="size-2 shrink-0 rounded-full bg-foreground"
        style={activeChain.color ? { backgroundColor: activeChain.color } : undefined}
      />
      <span>{activeChain.name}</span>
    </Badge>
  ) : (
    <span className="text-muted-foreground">{t("routing.rule.scope.global")}</span>
  )
  const defaultWhat = (
    <div className="grid min-w-0">
      {canEdit ? (
        <Button
          variant="link"
          className="h-auto w-fit max-w-full px-0 text-left font-medium break-words whitespace-normal text-foreground"
          onClick={() => openDefault(defaultScopeId)}
        >
          {t("routing.default.title")}
        </Button>
      ) : (
        <span className="font-medium break-words">{t("routing.default.title")}</span>
      )}
      <span className="text-xs text-muted-foreground">
        {visibleRules.length > 0 ? t("routing.default.afterRules") : t("routing.default.description")}
      </span>
    </div>
  )
  const defaultTargetCell = (
    <div className="flex flex-wrap items-center gap-2">
      {defaultTargetText}
      {defaultBroken && <StatusBadge tone="error">{t("routing.target.missing")}</StatusBadge>}
    </div>
  )
  const defaultControls = canEdit ? (
    <div className="flex justify-end">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="size-8 text-muted-foreground data-[state=open]:bg-muted">
            <EllipsisVerticalIcon />
            <span className="sr-only">{t("common.actions")}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-44">
          <DropdownMenuItem onSelect={() => openDefault(defaultScopeId)}>
            <PencilIcon />
            {t("common.edit")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  ) : null

  // The parts of a rule, laid out as a table row on a wide screen and as
  // a card on a phone.
  const ruleParts = (rule: RouteRule) => {
    const i = rules.indexOf(rule)
    const list = listById.get(rule.listId)
    const chain = rule.chainId != null ? chains.find((c) => c.id === rule.chainId) : undefined
    return {
      what: (
        <div className="grid min-w-0">
          {canEdit ? (
            <Button
              variant="link"
              className="h-auto w-fit max-w-full px-0 text-left font-medium break-words whitespace-normal text-foreground"
              onClick={() => openRule(rule)}
            >
              {list?.name}
            </Button>
          ) : (
            <span className="font-medium break-words">{list?.name}</span>
          )}
          {list && (
            <span className="text-xs text-muted-foreground">
              {[summary(list), list.skipped > 0 && tp("routing.list.skipped", list.skipped)].filter(Boolean).join(", ")}
            </span>
          )}
          {list?.kind === "url" &&
            (list.fetchError ? (
              <span className="text-xs text-destructive">{t("routing.list.updateFailed")}</span>
            ) : (
              list.fetchedAt && (
                <span className="text-xs text-muted-foreground">
                  {t("routing.list.updated", {
                    date: formatDateTime(list.fetchedAt, locale),
                  })}
                </span>
              )
            ))}
        </div>
      ),
      scope:
        rule.chainId != null ? (
          chain ? (
            <Badge variant="outline" className="gap-1.5 px-1.5 text-muted-foreground">
              <span
                className="size-2 shrink-0 rounded-full bg-foreground"
                style={chain.color ? { backgroundColor: chain.color } : undefined}
              />
              <span>{chain.name}</span>
            </Badge>
          ) : (
            <StatusBadge tone="error">{t("routing.rule.scope.missing")}</StatusBadge>
          )
        ) : (
          <span className="text-muted-foreground">{t("routing.rule.scope.global")}</span>
        ),
      target: (
        <div className="flex flex-wrap items-center gap-2">
          {targetLabel(rule)}
          {rule.enabled && targetBroken(rule, nodes, chains) && <StatusBadge tone="error">{t("routing.target.missing")}</StatusBadge>}
        </div>
      ),
      toggle: (
        <Switch
          checked={rule.enabled}
          disabled={!canEdit}
          aria-label={t("routing.rules.col.enabled")}
          onCheckedChange={(enabled) =>
            act(() =>
              api.updateRouteRule(rule.id, {
                action: rule.action,
                nodeId: rule.nodeId,
                chainId: rule.chainId,
                listId: rule.listId,
                enabled,
              })
            )
          }
        />
      ),
      controls: (
        <>
          {canEdit && (
            <div className="flex justify-end">
              <Button variant="ghost" size="icon" className="size-8 text-muted-foreground" disabled={i === 0} onClick={() => move(rule.id, -1)}>
                <ArrowUpIcon />
                <span className="sr-only">{t("routing.rules.moveUp")}</span>
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="size-8 text-muted-foreground"
                disabled={i === rules.length - 1}
                onClick={() => move(rule.id, 1)}
              >
                <ArrowDownIcon />
                <span className="sr-only">{t("routing.rules.moveDown")}</span>
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon" className="size-8 text-muted-foreground data-[state=open]:bg-muted">
                    <EllipsisVerticalIcon />
                    <span className="sr-only">{t("common.actions")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-44">
                  <DropdownMenuItem onSelect={() => openRule(rule)}>
                    <PencilIcon />
                    {t("common.edit")}
                  </DropdownMenuItem>
                  {list?.kind === "url" && (
                    <DropdownMenuItem onSelect={() => act(() => api.refreshRouteList(list.id), t("routing.toast.listRefreshed"))}>
                      <RefreshCwIcon />
                      {t("routing.list.refresh")}
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() =>
                      confirm({
                        title: t("routing.confirm.deleteRule"),
                        description: t("routing.confirm.deleteRuleText"),
                        action: t("common.delete"),
                        destructive: true,
                        onConfirm: () => act(() => api.deleteRouteRule(rule.id), t("routing.toast.ruleDeleted")),
                      })
                    }
                  >
                    <Trash2Icon />
                    {t("common.delete")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          )}
        </>
      ),
    }
  }

  return (
    <div className="grid gap-4 px-4 lg:px-6">
      <Card>
        <CardHeader>
          <CardTitle>{t("routing.rules.title")}</CardTitle>
          <CardDescription>{t("routing.rules.description")}</CardDescription>
          {canEdit && (
            <CardAction>
              <Button variant="outline" size="sm" onClick={() => openRule(null)}>
                <PlusIcon />
                <span className="sr-only sm:not-sr-only">{t("routing.rules.add")}</span>
              </Button>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-3 text-sm">
          {chains.length > 0 && (
            <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-1.5">
              <Tabs
                value={effectiveScope == null ? "all" : String(effectiveScope)}
                onValueChange={(v) => setScopeFilter(v === "all" ? null : Number(v))}
              >
                <TabsList className="h-auto! max-w-full flex-wrap justify-start gap-1 p-1">
                  <TabsTrigger value="all" className="h-auto flex-none px-2.5 py-1">
                    {t("routing.rules.filter.all")}
                  </TabsTrigger>
                  {chains.map((c) => (
                    <TabsTrigger key={c.id} value={String(c.id)} className="h-auto flex-none px-2.5 py-1">
                      <span
                        className="size-2 shrink-0 rounded-full bg-foreground"
                        style={c.color ? { backgroundColor: c.color } : undefined}
                      />
                      <span>{c.name}</span>
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
              {effectiveScope != null && (
                <p className="text-xs text-muted-foreground">{t("chains.stage.filterHint")}</p>
              )}
            </div>
          )}
          {isMobile ? (
            <div className="grid grid-cols-[minmax(0,1fr)] gap-3">
              {visibleRules.map((rule) => {
                const part = ruleParts(rule)
                return (
                  <div
                    key={rule.id}
                    {...cardTap(canEdit ? () => openRule(rule) : undefined)}
                    className={cn(
                      "grid min-w-0 grid-cols-[minmax(0,1fr)] gap-3 rounded-lg border p-4 text-sm",
                      canEdit && cardTap(() => {}).className,
                      !rule.enabled && "text-muted-foreground"
                    )}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex min-w-0 gap-3">
                        <span className="pt-0.5 text-muted-foreground tabular-nums">{rules.indexOf(rule) + 1}</span>
                        {part.what}
                      </div>
                      <div className="-mt-1 -mr-2 shrink-0">{part.controls}</div>
                    </div>
                    {showScope ? (
                      <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-4 gap-y-2 pl-6">
                        <dt className="text-muted-foreground">{t("routing.rule.scope")}</dt>
                        <dd className="flex min-w-0 max-w-full justify-end text-right [&>*]:min-w-0 [&>*]:max-w-full">
                          {part.scope}
                        </dd>
                        <dt className="text-muted-foreground">{t("routing.rules.col.target")}</dt>
                        <dd className="flex min-w-0 max-w-full justify-end text-right [&>*]:min-w-0 [&>*]:max-w-full">
                          {part.target}
                        </dd>
                        <dt className="text-muted-foreground">{t("routing.rules.col.enabled")}</dt>
                        <dd className="flex min-w-0 max-w-full justify-end text-right [&>*]:min-w-0 [&>*]:max-w-full">
                          {part.toggle}
                        </dd>
                      </dl>
                    ) : (
                      <div className="flex items-center justify-between gap-3 pl-6">
                        {part.target}
                        {part.toggle}
                      </div>
                    )}
                  </div>
                )
              })}
              <div
                {...cardTap(canEdit ? () => openDefault(defaultScopeId) : undefined)}
                className={cn(
                  "grid min-w-0 grid-cols-[minmax(0,1fr)] gap-3 rounded-lg border bg-muted/30 p-4 text-sm",
                  canEdit && cardTap(() => {}).className
                )}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex min-w-0 gap-3">
                    <span className="pt-0.5 text-muted-foreground">*</span>
                    {defaultWhat}
                  </div>
                  <div className="-mt-1 -mr-2 shrink-0">{defaultControls}</div>
                </div>
                {showScope ? (
                  <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-4 gap-y-2 pl-6">
                    <dt className="text-muted-foreground">{t("routing.rule.scope")}</dt>
                    <dd className="flex min-w-0 max-w-full justify-end text-right [&>*]:min-w-0 [&>*]:max-w-full">
                      {defaultScopeBadge}
                    </dd>
                    <dt className="text-muted-foreground">{t("routing.rules.col.target")}</dt>
                    <dd className="flex min-w-0 max-w-full justify-end text-right [&>*]:min-w-0 [&>*]:max-w-full">
                      {defaultTargetCell}
                    </dd>
                  </dl>
                ) : (
                  <div className="pl-6">{defaultTargetCell}</div>
                )}
              </div>
            </div>
          ) : (
            <div className="overflow-hidden rounded-lg border">
              <Table>
                <TableHeader className="bg-muted">
                  <TableRow>
                    <TableHead className="w-10" />
                    <TableHead>{t("routing.rules.col.what")}</TableHead>
                    {showScope && <TableHead>{t("routing.rule.scope")}</TableHead>}
                    <TableHead>{t("routing.rules.col.target")}</TableHead>
                    <TableHead className="w-16">{t("routing.rules.col.enabled")}</TableHead>
                    <TableHead className="w-24" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visibleRules.map((rule) => {
                    const part = ruleParts(rule)
                    return (
                      <TableRow key={rule.id} className={rule.enabled ? undefined : "text-muted-foreground"}>
                        <TableCell className="text-muted-foreground tabular-nums">{rules.indexOf(rule) + 1}</TableCell>
                        <TableCell>{part.what}</TableCell>
                        {showScope && <TableCell>{part.scope}</TableCell>}
                        <TableCell>{part.target}</TableCell>
                        <TableCell>{part.toggle}</TableCell>
                        <TableCell>{part.controls}</TableCell>
                      </TableRow>
                    )
                  })}
                  <TableRow className="bg-muted/30 hover:bg-muted/40">
                    <TableCell className="text-muted-foreground">*</TableCell>
                    <TableCell>{defaultWhat}</TableCell>
                    {showScope && <TableCell>{defaultScopeBadge}</TableCell>}
                    <TableCell>{defaultTargetCell}</TableCell>
                    <TableCell />
                    <TableCell>{defaultControls}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          )}
          {visibleRules.length === 0 && (
            <p className="text-xs text-muted-foreground">
              {effectiveScope != null ? t("routing.rules.noneForScope") : t("routing.rules.empty")}
            </p>
          )}
          {nodes.some((n) => n.isEntry && n.enabled) && defaultIsDirect && (
            <p className="text-xs text-muted-foreground">{t("routing.directNote")}</p>
          )}
          {nodeData && !hasExits && (
            <p className="text-xs text-muted-foreground">
              {t("routing.noExits")}{" "}
              <Link href="/nodes/new/" className="font-medium text-foreground underline underline-offset-4">
                {t("routing.addNode")}
              </Link>
            </p>
          )}
        </CardContent>
      </Card>

      {defaultDrawer && (
        <DefaultRouteDrawer
          key={defaultDrawer.key}
          open={defaultOpen}
          onOpenChange={setDefaultOpen}
          initialScope={defaultDrawer.scope}
          routingDefault={routing.default}
          nodes={nodes}
          chains={chains}
          onSaved={async () => {
            reload()
            await reloadChains()
          }}
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
          chains={chains}
          initialScope={effectiveScope ?? 0}
          onSaved={reload}
        />
      )}
      {confirmDialog}
    </div>
  )
}
