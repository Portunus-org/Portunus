"use client"

import * as React from "react"
import Link from "next/link"
import { ArrowDownIcon, ArrowUpIcon, EllipsisVerticalIcon, PencilIcon, PlusIcon, RefreshCwIcon, SplitIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { useConfirm } from "@/components/confirm"
import { RuleDrawer } from "@/components/routing/rule-drawer"
import { TargetSelect, targetValue, useTargetLabel } from "@/components/routing/target-select"
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
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { api, errorText, type Node, type RouteList, type RouteRule, type RouteTarget } from "@/lib/api"
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
function targetBroken(target: RouteTarget, nodes: Node[]): boolean {
  const exits = nodes.filter((n) => n.isExit && n.enabled)
  if (target.action === "auto") return exits.length === 0
  if (target.action === "node") return !exits.some((n) => n.id === target.nodeId)
  return false
}

export default function RoutingPage() {
  const { t, tp, locale } = useI18n()
  const canEdit = useCanEdit()
  const { data: routing, reload } = useResource(api.routing)
  const { data: nodeData } = useResource(api.nodes, 30_000)
  const nodes = React.useMemo(() => nodeData ?? [], [nodeData])
  const targetLabel = useTargetLabel(nodes)
  const summary = useListSummary()
  const [confirm, confirmDialog] = useConfirm()

  const [ruleDrawer, setRuleDrawer] = React.useState<{
    key: number
    rule: RouteRule | null
  } | null>(null)
  const [ruleOpen, setRuleOpen] = React.useState(false)
  const keys = React.useRef(0)
  // Where all other traffic goes is chosen first and saved after: picking
  // an item in a list must not reroute everybody at once.
  const [pending, setPending] = React.useState<RouteTarget | null>(null)

  const openRule = (rule: RouteRule | null) => {
    setRuleDrawer({ key: ++keys.current, rule })
    setRuleOpen(true)
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

  const rules = routing.rules
  const listById = new Map(routing.lists.map((l) => [l.id, l]))
  const move = (index: number, by: number) => {
    const ids = rules.map((r) => r.id)
    const [id] = ids.splice(index, 1)
    ids.splice(index + by, 0, id)
    act(() => api.orderRouteRules(ids))
  }
  const hasExits = nodes.some((n) => n.isExit)
  const chosen = pending ?? routing.default
  const changed = targetValue(chosen) !== targetValue(routing.default)

  return (
    <div className="grid gap-4 px-4 lg:px-6">
      <Card>
        <CardHeader>
          <CardTitle>{t("routing.default.title")}</CardTitle>
          <CardDescription>{t("routing.default.description")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 text-sm">
          <div className="flex flex-wrap items-center gap-3">
            <TargetSelect value={chosen} onChange={setPending} nodes={nodes} disabled={!canEdit} className="w-full sm:w-96" />
            {changed ? (
              <>
                <Button
                  size="sm"
                  onClick={async () => {
                    await act(() => api.setDefaultRoute(chosen), t("routing.toast.defaultSaved"))
                    setPending(null)
                  }}
                >
                  {t("common.save")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => setPending(null)}>
                  {t("common.cancel")}
                </Button>
              </>
            ) : (
              targetBroken(routing.default, nodes) && <StatusBadge tone="error">{t("routing.target.missing")}</StatusBadge>
            )}
          </div>
          {nodes.some((n) => n.isEntry && n.enabled) && chosen.action === "direct" && (
            <p className="text-muted-foreground">{t("routing.directNote")}</p>
          )}
          {nodeData && !hasExits && (
            <p className="text-muted-foreground">
              {t("routing.noExits")}{" "}
              <Link href="/nodes/new/" className="font-medium text-foreground underline underline-offset-4">
                {t("routing.addNode")}
              </Link>
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("routing.rules.title")}</CardTitle>
          <CardDescription>{t("routing.rules.description")}</CardDescription>
          {canEdit && rules.length > 0 && (
            <CardAction>
              <Button variant="outline" size="sm" onClick={() => openRule(null)}>
                <PlusIcon />
                <span className="sr-only sm:not-sr-only">{t("routing.rules.add")}</span>
              </Button>
            </CardAction>
          )}
        </CardHeader>
        <CardContent>
          {rules.length === 0 ? (
            <Empty className="border">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SplitIcon />
                </EmptyMedia>
                <EmptyTitle>{t("routing.rules.empty")}</EmptyTitle>
                <EmptyDescription>{t("routing.rules.emptyText")}</EmptyDescription>
              </EmptyHeader>
              {canEdit && (
                <EmptyContent>
                  <Button onClick={() => openRule(null)}>
                    <PlusIcon />
                    {t("routing.rules.add")}
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          ) : (
            <div className="overflow-hidden rounded-lg border">
              <Table>
                <TableHeader className="bg-muted">
                  <TableRow>
                    <TableHead className="w-10" />
                    <TableHead>{t("routing.rules.col.what")}</TableHead>
                    <TableHead>{t("routing.rules.col.target")}</TableHead>
                    <TableHead className="w-16">{t("routing.rules.col.enabled")}</TableHead>
                    <TableHead className="w-24" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rules.map((rule, i) => {
                    const list = listById.get(rule.listId)
                    return (
                      <TableRow key={rule.id} className={rule.enabled ? undefined : "text-muted-foreground"}>
                        <TableCell className="text-muted-foreground tabular-nums">{i + 1}</TableCell>
                        <TableCell>
                          <div className="grid">
                            {canEdit ? (
                              <Button
                                variant="link"
                                className="h-auto w-fit px-0 text-left font-medium text-foreground"
                                onClick={() => openRule(rule)}
                              >
                                {list?.name}
                              </Button>
                            ) : (
                              <span className="font-medium">{list?.name}</span>
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
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap items-center gap-2">
                            {targetLabel(rule)}
                            {rule.enabled && targetBroken(rule, nodes) && (
                              <StatusBadge tone="error">{t("routing.target.missing")}</StatusBadge>
                            )}
                          </div>
                        </TableCell>
                        <TableCell>
                          <Switch
                            checked={rule.enabled}
                            disabled={!canEdit}
                            aria-label={t("routing.rules.col.enabled")}
                            onCheckedChange={(enabled) =>
                              act(() =>
                                api.updateRouteRule(rule.id, {
                                  action: rule.action,
                                  nodeId: rule.nodeId,
                                  listId: rule.listId,
                                  enabled,
                                })
                              )
                            }
                          />
                        </TableCell>
                        <TableCell>
                          {canEdit && (
                            <div className="flex justify-end">
                              <Button
                                variant="ghost"
                                size="icon"
                                className="size-8 text-muted-foreground"
                                disabled={i === 0}
                                onClick={() => move(i, -1)}
                              >
                                <ArrowUpIcon />
                                <span className="sr-only">{t("routing.rules.moveUp")}</span>
                              </Button>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="size-8 text-muted-foreground"
                                disabled={i === rules.length - 1}
                                onClick={() => move(i, 1)}
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
                                    <DropdownMenuItem
                                      onSelect={() => act(() => api.refreshRouteList(list.id), t("routing.toast.listRefreshed"))}
                                    >
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
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      {ruleDrawer && (
        <RuleDrawer
          key={ruleDrawer.key}
          open={ruleOpen}
          onOpenChange={setRuleOpen}
          rule={ruleDrawer.rule}
          list={ruleDrawer.rule ? (listById.get(ruleDrawer.rule.listId) ?? null) : null}
          nodes={nodes}
          onSaved={reload}
        />
      )}
      {confirmDialog}
    </div>
  )
}
