"use client"

import * as React from "react"
import { ArrowRightIcon, GitBranchIcon, MapPinIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react"
import { toast } from "@/lib/toast"

import { ChainColorSelect } from "@/components/chains/chain-color-picker"
import { NodeMap, NodeMapLegend, nodeMapState, type MapPath, type MapPoint, type MapState } from "@/components/nodes/node-map"
import type { DraftChainRule } from "@/components/routing/rule-drawer"
import { sourceToList } from "@/components/routing/source-fields"
import { useTargetLabel } from "@/components/routing/target-select"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { api, errorText, type Chain, type GeoLocation, type MapStyle, type Node, type RouteList, type RouteRule } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"

// A real arrow for hover labels: Leaflet tooltips render HTML, so a text
// glyph would look like typography and a dot reads as punctuation.
export const ARROW_HTML =
  '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-1px" aria-hidden="true"><path d="M5 12h14m-6-6 6 6-6 6"/></svg>'

/** One link of a chain: 0 is the main server. */
export interface ChainEdgeDraft {
  from: number
  to: number
}

/** State of a chain being created or edited on the big map. */
export interface ChainDraft {
  id: number | null
  name: string
  color: string
  edges: ChainEdgeDraft[]
  backups: number[]
  pendingRules?: DraftChainRule[]
}

function place(l: GeoLocation): string {
  return [l.city, l.country].filter(Boolean).join(", ")
}

/**
 * The links that can carry traffic now: both of their servers answer, the
 * one they lead to still reaches an exit, and they hang off a working entry
 * point. Everything else is drawn faint so the route traffic takes now,
 * including the one that took over when a server went down, stands out.
 */
function liveOf(edges: ChainEdgeDraft[], usable: (id: number) => boolean): Set<string> {
  const out = new Map<number, ChainEdgeDraft[]>()
  const incoming = new Set<number>()
  for (const e of edges) {
    out.set(e.from, [...(out.get(e.from) ?? []), e])
    incoming.add(e.to)
  }
  const reachesExit = new Map<number, boolean>()
  function canReachExit(id: number): boolean {
    const known = reachesExit.get(id)
    if (known != null) return known
    reachesExit.set(id, false)
    const leaves = out.get(id) ?? []
    const ok = leaves.length === 0 ? incoming.has(id) : leaves.some((e) => usable(e.to) && canReachExit(e.to))
    reachesExit.set(id, ok)
    return ok
  }
  const reachable = new Set<number>()
  const stack: number[] = []
  for (const from of out.keys()) {
    if (from === 0 || !incoming.has(from)) {
      if (usable(from) && canReachExit(from)) {
        reachable.add(from)
        stack.push(from)
      }
    }
  }
  while (stack.length > 0) {
    const id = stack.pop()!
    for (const e of out.get(id) ?? []) {
      if (usable(e.to) && canReachExit(e.to) && !reachable.has(e.to)) {
        reachable.add(e.to)
        stack.push(e.to)
      }
    }
  }
  const live = new Set<string>()
  for (const e of edges) {
    if (reachable.has(e.from) && usable(e.from) && usable(e.to) && canReachExit(e.to)) {
      live.add(`${e.from}|${e.to}`)
    }
  }
  return live
}

/** The main server and one node as points for the map. */
export function mapPoints(
  main: { host: string; location: GeoLocation | null } | undefined,
  nodes: Node[],
  t: ReturnType<typeof useI18n>["t"]
): MapPoint[] {
  const points: MapPoint[] = []
  if (main?.location) {
    points.push({
      id: "main",
      name: t("nodes.map.state.main"),
      detail: [main.host, place(main.location)].filter(Boolean).join(", "),
      state: "main",
      location: main.location,
    })
  }
  for (const n of nodes) {
    if (!n.location) continue
    const detail = [n.address, place(n.location), n.status.latencyMs !== undefined && t("nodes.latency", { ms: n.status.latencyMs })]
    points.push({
      id: `n${n.id}`,
      name: n.name,
      detail: detail.filter(Boolean).join(", "),
      state: nodeMapState(n),
      location: n.location,
    })
  }
  return points
}

/** The points traffic can enter the chain at: outgoing links and no incoming ones; 0 is not a source of its own. */
export function sourcesOf(edges: ChainEdgeDraft[]): number[] {
  const out = new Set<number>()
  const inn = new Set<number>()
  for (const e of edges) {
    if (e.from !== 0) out.add(e.from)
    inn.add(e.to)
  }
  return [...out].filter((id) => !inn.has(id)).sort((a, b) => a - b)
}

/** Sinks: servers with incoming links and no outgoing ones. */
function sinksOf(edges: ChainEdgeDraft[]): number[] {
  const out = new Set<number>()
  const inn = new Set<number>()
  for (const e of edges) {
    out.add(e.from)
    if (e.to !== 0) inn.add(e.to)
  }
  return [...inn].filter((id) => !out.has(id)).sort((a, b) => a - b)
}

/** Intermediate transit hops: servers with both incoming and outgoing links. */
function relaysOf(edges: ChainEdgeDraft[]): number[] {
  const out = new Set<number>()
  const inn = new Set<number>()
  for (const e of edges) {
    if (e.from !== 0) out.add(e.from)
    if (e.to !== 0) inn.add(e.to)
  }
  return [...out].filter((id) => inn.has(id)).sort((a, b) => a - b)
}

function pointToNodeId(id: string): number | null {
  if (id === "main") return 0
  if (id.startsWith("n")) {
    const n = Number(id.slice(1))
    return Number.isNaN(n) ? null : n
  }
  return null
}

/**
 * The map of servers with the chains drawn over it, plus the node-editor
 * mode for connecting servers with Main or Backup lines on the big map.
 */
export function NodesMapCard({
  main,
  tiles,
  mapStyle,
  nodes,
  canEdit,
  onChanged,
  chains,
  draft,
  onDraftChange,
  onSavedChain,
  onDeleteChain,
  rules = [],
  lists = [],
  onOpenRule,
  onOpenDraftRule,
}: {
  /** Detailed map tiles are available through the panel. */
  tiles: boolean
  mapStyle?: MapStyle
  main: { host: string; location: GeoLocation | null } | undefined
  nodes: Node[]
  canEdit: boolean
  onChanged: () => void
  chains: Chain[]
  draft: ChainDraft | null
  onDraftChange: (draft: ChainDraft | null) => void
  onSavedChain: () => void
  onDeleteChain?: (chain: Chain) => void
  rules?: RouteRule[]
  lists?: RouteList[]
  onOpenRule?: (rule: RouteRule | null, chainId: number) => void
  onOpenDraftRule?: (rule: DraftChainRule | null) => void
}) {
  const { t } = useI18n()
  const targetLabel = useTargetLabel(nodes)
  const cardRef = React.useRef<HTMLDivElement>(null)

  // Moving the main server: a click only shows where it would go, and it
  // goes there when that is saved. A stray click must not move a server.
  const [moving, setMoving] = React.useState(false)
  const [pick, setPick] = React.useState<{ lat: number; lon: number } | null>(null)
  const stopMoving = () => {
    setMoving(false)
    setPick(null)
  }

  // Editor state when draft !== null
  const [lineMode, setLineMode] = React.useState<"main" | "backup">("main")
  const [from, setFrom] = React.useState<number | null>(null)
  const [error, setError] = React.useState<string | null>(null)
  const [saving, setSaving] = React.useState(false)
  const [prevDraftId, setPrevDraftId] = React.useState<number | null | undefined>(undefined)

  const currentDraftId = draft ? draft.id : undefined
  if (prevDraftId !== currentDraftId) {
    setPrevDraftId(currentDraftId)
    setFrom(null)
    setError(null)
    setLineMode("main")
  }

  React.useEffect(() => {
    if (currentDraftId !== undefined) {
      cardRef.current?.scrollIntoView({ behavior: "smooth", block: "start" })
    }
  }, [currentDraftId])

  React.useEffect(() => {
    if (!draft) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && from !== null) {
        e.stopPropagation()
        setFrom(null)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [draft, from])

  const points = mapPoints(main && pick ? { ...main, location: { ...pick, manual: true } } : main, nodes, t)
  const states = [...new Set(points.map((p) => p.state))] as MapState[]
  const byId = React.useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes])
  const listById = React.useMemo(() => new Map(lists.map((l) => [l.id, l])), [lists])
  const unplaced = nodes.filter((n) => !n.location)

  async function moveMain(location: { lat: number; lon: number }) {
    stopMoving()
    try {
      await api.setServerLocation(location)
      toast.success(t("nodes.location.saved"))
      onChanged()
    } catch (err) {
      toast.error(errorText(err, t))
    }
  }

  const nameOf = (id: number) => (id === 0 ? t("chains.main") : byId.get(id)?.name ?? "?")

  const locOf = (id: number): [number, number] | null => {
    if (id === 0) return main?.location ? [main.location.lat, main.location.lon] : null
    const l = byId.get(id)?.location
    return l ? [l.lat, l.lon] : null
  }

  // Connect two nodes in editor mode (called either by drag-drop or 2 clicks).
  const connectNodes = (a: number, b: number) => {
    if (!draft) return
    setError(null)
    if (a === b) {
      setFrom(null)
      return
    }
    if (b === 0) {
      setError(t("chains.error.toMain"))
      setFrom(null)
      return
    }
    if (a > 0 && byId.get(a)?.kind === "link") {
      setError(t("chains.linkSource"))
      setFrom(null)
      return
    }
    if (lineMode === "backup" && a === 0) {
      setError(t("chains.error.mainNotBackup"))
      setFrom(null)
      return
    }
    const exists = draft.edges.some((e) => e.from === a && e.to === b)
    const nextEdges = exists ? draft.edges : [...draft.edges, { from: a, to: b }]
    const nextSources = new Set(sourcesOf(nextEdges))
    let nextBackups = draft.backups.filter((id) => nextSources.has(id) && byId.get(id)?.isEntry)
    if (lineMode === "backup" && a > 0) {
      if (!byId.get(a)?.isEntry) {
        setError(t("chains.sourceNotEntry", { names: nameOf(a) }))
        setFrom(null)
        return
      }
      if (!nextBackups.includes(a)) nextBackups = [...nextBackups, a]
    } else if (lineMode === "main" && a > 0) {
      nextBackups = nextBackups.filter((id) => id !== a)
    }
    onDraftChange({
      ...draft,
      edges: nextEdges,
      backups: nextBackups,
    })
    setFrom(null)
  }

  const pickNode = (id: number) => {
    if (!draft) return
    setError(null)
    if (from === null) {
      if (id > 0 && byId.get(id)?.kind === "link") {
        setError(t("chains.linkSource"))
        return
      }
      if (lineMode === "backup" && id === 0) {
        setError(t("chains.error.mainNotBackup"))
        return
      }
      setFrom(id)
      return
    }
    if (from === id) {
      setFrom(null)
      return
    }
    connectNodes(from, id)
  }

  const removeEdge = (edge: ChainEdgeDraft) => {
    if (!draft) return
    setError(null)
    const nextEdges = draft.edges.filter((e) => !(e.from === edge.from && e.to === edge.to))
    const nextSources = new Set(sourcesOf(nextEdges))
    const nextBackups = draft.backups.filter((id) => nextSources.has(id) && byId.get(id)?.isEntry)
    onDraftChange({
      ...draft,
      edges: nextEdges,
      backups: nextBackups,
    })
    setFrom(null)
  }

  const sources = React.useMemo(() => (draft ? sourcesOf(draft.edges) : []), [draft])
  const badSources = React.useMemo(() => sources.filter((id) => !byId.get(id)?.isEntry), [sources, byId])
  const canSave = !!draft && draft.name.trim().length > 0 && draft.edges.length > 0 && badSources.length === 0 && !saving

  async function saveChain() {
    if (!draft || !canSave) return
    setSaving(true)
    setError(null)
    const payload = {
      name: draft.name.trim(),
      color: draft.color,
      edges: draft.edges,
      backups: draft.backups,
    }
    try {
      if (draft.id != null) {
        await api.updateChain(draft.id, payload)
        toast.success(t("chains.toast.saved"))
        onSavedChain()
        onDraftChange(null)
      } else {
        const created = await api.createChain(payload)
        for (const r of draft.pendingRules ?? []) {
          const createdList = await api.createRouteList(sourceToList(r.source))
          try {
            await api.createRouteRule({ ...r.target, listId: createdList.id, chainId: created.id })
          } catch (err) {
            await api.deleteRouteList(createdList.id).catch(() => {})
            throw err
          }
        }
        toast.success(t("chains.toast.created"))
        onSavedChain()
        onDraftChange(null)
      }
    } catch (err) {
      setError(errorText(err, t))
    } finally {
      setSaving(false)
    }
  }

  // Build paths for NodeMap: when editing a chain, show ONLY that chain's edges;
  // otherwise show all chains.
  let anyBlocked = false
  let anyDashed = false
  const paths: MapPath[] = []
  if (draft) {
    for (const e of draft.edges) {
      const a = locOf(e.from)
      const b = locOf(e.to)
      if (!a || !b || (a[0] === b[0] && a[1] === b[1])) continue
      const dashed = draft.backups.includes(e.from)
      anyDashed = anyDashed || dashed
      paths.push({
        points: [a, b],
        label: `${nameOf(e.from)} ${ARROW_HTML} ${nameOf(e.to)}`,
        dashed,
        color: draft.color || undefined,
        onClick: () => removeEdge(e),
      })
    }
  } else {
    for (const c of chains) {
      const live = liveOf(c.edges, (id) => id === 0 || byId.get(id)?.status.state === "online")
      const srcs = sourcesOf(c.edges)
      if (c.edges.some((e) => e.from === 0)) srcs.push(0)
      const primaryLive = srcs.some(
        (s) => !c.backups.includes(s) && c.edges.some((e) => e.from === s && live.has(`${e.from}|${e.to}`))
      )
      for (const e of c.edges) {
        if (primaryLive && c.backups.includes(e.from)) continue
        const a = locOf(e.from)
        const b = locOf(e.to)
        if (!a || !b || (a[0] === b[0] && a[1] === b[1])) continue
        const blocked = !live.has(`${e.from}|${e.to}`)
        const dashed = c.backups.includes(e.from)
        anyBlocked = anyBlocked || blocked
        anyDashed = anyDashed || dashed
        paths.push({
          points: [a, b],
          label: `${c.name}: ${nameOf(e.from)} ${ARROW_HTML} ${nameOf(e.to)}`,
          dashed,
          blocked,
          color: c.color || undefined,
        })
      }
    }
  }

  // Stage pipeline calculation when editing a chain
  const entryIds = React.useMemo(() => {
    if (!draft) return []
    const res: number[] = []
    if (draft.edges.some((e) => e.from === 0)) res.push(0)
    for (const s of sources) res.push(s)
    return res
  }, [draft, sources])
  const relayIds = React.useMemo(() => (draft ? relaysOf(draft.edges) : []), [draft])
  const exitIds = React.useMemo(() => (draft ? sinksOf(draft.edges) : []), [draft])
  const chainRules = React.useMemo(
    () => (draft?.id != null ? rules.filter((r) => r.chainId === draft.id) : []),
    [draft, rules]
  )
  const editingChainObj = React.useMemo(
    () => (draft?.id != null ? chains.find((c) => c.id === draft.id) ?? null : null),
    [chains, draft]
  )

  return (
    <Card ref={cardRef}>
      <CardHeader>
        <CardTitle>{draft ? (draft.id == null ? t("chains.new") : t("chains.editTitle")) : t("nodes.map.title")}</CardTitle>
        <CardDescription>
          {draft ? t("chains.drawHint") : moving ? t("nodes.map.pickHint") : t("nodes.map.description")}
        </CardDescription>
        {canEdit && (
          <CardAction className="flex items-center gap-2">
            {draft ? (
              <>
                {editingChainObj && onDeleteChain && (
                  <Button
                    variant="outline"
                    size="icon-sm"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => onDeleteChain(editingChainObj)}
                    title={t("common.delete")}
                  >
                    <Trash2Icon />
                    <span className="sr-only">{t("common.delete")}</span>
                  </Button>
                )}
                <Button variant="outline" size="sm" onClick={() => onDraftChange(null)}>
                  {t("common.cancel")}
                </Button>
                <Button size="sm" disabled={!canSave} onClick={() => saveChain()}>
                  {t(draft.id == null ? "chains.create" : "common.save")}
                </Button>
              </>
            ) : moving ? (
              <>
                <Button size="sm" disabled={!pick} onClick={() => pick && moveMain(pick)}>
                  {t("nodes.location.save")}
                </Button>
                <Button variant="outline" size="sm" onClick={stopMoving}>
                  {t("common.cancel")}
                </Button>
              </>
            ) : (
              <>
                {nodes.length > 0 && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      onDraftChange({
                        id: null,
                        name: "",
                        color: "",
                        edges: [],
                        backups: [],
                      })
                    }
                  >
                    <GitBranchIcon />
                    <span className="sr-only lg:not-sr-only">{t("chains.new")}</span>
                  </Button>
                )}
                <Button variant="outline" size="sm" onClick={() => setMoving(true)}>
                  <MapPinIcon />
                  <span className="sr-only lg:not-sr-only">{t("nodes.map.moveMain")}</span>
                </Button>
              </>
            )}
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="grid gap-3">
        {draft && (
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={draft.name}
              onChange={(e) => onDraftChange({ ...draft, name: e.target.value })}
              placeholder={t("chains.namePlaceholder")}
              maxLength={64}
              aria-label={t("chains.form.name")}
              className="h-8 w-full sm:w-56"
            />
            <ChainColorSelect
              value={draft.color}
              onPick={(color) => onDraftChange({ ...draft, color })}
            />
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              spacing={0}
              className="w-full sm:w-auto"
              value={lineMode}
              onValueChange={(v) => {
                if (v === "main" || v === "backup") {
                  setLineMode(v)
                  setFrom(null)
                  setError(null)
                }
              }}
            >
              <ToggleGroupItem value="main" className="flex-1 gap-2 px-3 sm:flex-initial">
                <span className="h-0.5 w-3.5 shrink-0 rounded-full bg-current" />
                <span>{t("chains.line.main")}</span>
              </ToggleGroupItem>
              <ToggleGroupItem value="backup" className="flex-1 gap-2 px-3 sm:flex-initial">
                <span className="w-3.5 shrink-0 border-t-2 border-dashed border-current" />
                <span>{t("chains.line.backup")}</span>
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
        )}

        <NodeMap
          points={points}
          tiles={tiles}
          mapStyle={mapStyle}
          onPick={moving && !draft ? setPick : undefined}
          cursor={draft && from !== null ? (from === 0 ? "main" : `n${from}`) : undefined}
          cursorDashed={lineMode === "backup"}
          cursorColor={draft?.color || undefined}
          onPointClick={
            draft
              ? (id) => {
                  const n = pointToNodeId(id)
                  if (n !== null) pickNode(n)
                }
              : undefined
          }
          onConnect={
            draft
              ? (fromId, toId) => {
                  const a = pointToNodeId(fromId)
                  const b = pointToNodeId(toId)
                  if (a !== null && b !== null) connectNodes(a, b)
                }
              : undefined
          }
          onCancelConnect={draft ? () => setFrom(null) : undefined}
          paths={paths}
        />

        {draft ? (
          <div className="grid gap-3">
            {unplaced.length > 0 && (
              <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                <span>{t("chains.unplaced")}</span>
                {!main?.location && (
                  <Button
                    type="button"
                    variant={from === 0 ? "default" : "outline"}
                    size="xs"
                    onClick={() => pickNode(0)}
                  >
                    {t("chains.main")}
                  </Button>
                )}
                {unplaced.map((n) => (
                  <Button
                    key={n.id}
                    type="button"
                    variant={from === n.id ? "default" : "outline"}
                    size="xs"
                    onClick={() => pickNode(n.id)}
                  >
                    {n.name}
                  </Button>
                ))}
              </div>
            )}

            {draft.edges.length > 0 && (
              <div className="flex flex-wrap items-center gap-1.5">
                {draft.edges.map((e) => {
                  const backup = draft.backups.includes(e.from)
                  return (
                    <Badge
                      key={`${e.from}-${e.to}`}
                      variant="outline"
                      className={cn("gap-1 pr-1", backup && "border-dashed text-muted-foreground")}
                    >
                      <span>{nameOf(e.from)}</span>
                      <ArrowRightIcon className="size-3 text-muted-foreground" />
                      <span>{nameOf(e.to)}</span>
                      {backup && <span className="text-muted-foreground">{t("chains.backup")}</span>}
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        className="size-4 rounded-full text-muted-foreground hover:text-foreground"
                        aria-label={t("chains.edgeRemove")}
                        onClick={() => removeEdge(e)}
                      >
                        <XIcon className="size-3" />
                      </Button>
                    </Badge>
                  )
                })}
              </div>
            )}

            {badSources.length > 0 && (
              <p className="text-xs text-destructive">
                {t("chains.sourceNotEntry", { names: badSources.map(nameOf).join(", ") })}
              </p>
            )}
            {error && <p className="text-xs text-destructive">{error}</p>}

            <div className="grid gap-2.5 rounded-lg border bg-muted/30 p-3">
              <span className="text-xs font-medium text-muted-foreground">{t("chains.stage.title")}</span>
              <div
                className={cn(
                  "grid gap-2",
                  relayIds.length > 0
                    ? "lg:grid-cols-[1fr_auto_1.3fr_auto_1fr_auto_1fr] lg:items-stretch"
                    : "lg:grid-cols-[1fr_auto_1.4fr_auto_1fr] lg:items-stretch"
                )}
              >
                {/* Stage 1: Entry */}
                <div className="grid content-start gap-1.5 rounded-md border bg-card p-2.5">
                  <span className="text-xs font-medium">{t("chains.stage.entry")}</span>
                  <div className="flex flex-wrap gap-1">
                    {entryIds.length > 0 ? (
                      entryIds.map((id) => {
                        const isBackup = draft.backups.includes(id)
                        return (
                          <Badge
                            key={id}
                            variant="outline"
                            className={cn(isBackup && "border-dashed text-muted-foreground")}
                          >
                            <span>{nameOf(id)}</span>
                            {isBackup && <span className="text-muted-foreground">{t("chains.backup")}</span>}
                          </Badge>
                        )
                      })
                    ) : (
                      <span className="text-xs text-muted-foreground">{t("chains.stage.emptyEntry")}</span>
                    )}
                  </div>
                  <span className="text-[11px] text-muted-foreground">{t("chains.stage.entryHint")}</span>
                </div>

                <div className="hidden items-center justify-center lg:flex">
                  <ArrowRightIcon className="size-4 text-muted-foreground" />
                </div>

                {/* Stage 2: Traffic Filtering at Entry */}
                <div className="grid content-start gap-1.5 rounded-md border bg-card p-2.5">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="text-xs font-medium">{t("chains.stage.filter")}</span>
                    {(onOpenRule || onOpenDraftRule) && (
                      <Button
                        type="button"
                        variant="outline"
                        size="xs"
                        onClick={() => {
                          if (draft.id != null) {
                            onOpenRule?.(null, draft.id)
                          } else {
                            onOpenDraftRule?.(null)
                          }
                        }}
                      >
                        <PlusIcon />
                        {t("chains.stage.addFilter")}
                      </Button>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-1">
                    {draft.id != null ? (
                      chainRules.length > 0 ? (
                        chainRules.map((rule) => {
                          const list = listById.get(rule.listId)
                          return (
                            <Badge
                              key={rule.id}
                              variant="secondary"
                              className={cn("max-w-full cursor-pointer gap-1 overflow-hidden", !rule.enabled && "opacity-50")}
                              onClick={() => draft.id != null && onOpenRule?.(rule, draft.id)}
                            >
                              <span className="truncate">{list?.name ?? "?"}</span>
                              <ArrowRightIcon className="size-3 shrink-0 text-muted-foreground" />
                              <span className="truncate">{targetLabel(rule)}</span>
                            </Badge>
                          )
                        })
                      ) : (
                        <span className="text-xs text-muted-foreground">{t("chains.stage.noFilters")}</span>
                      )
                    ) : (draft.pendingRules ?? []).length > 0 ? (
                      (draft.pendingRules ?? []).map((dr) => (
                        <Badge
                          key={dr.key}
                          variant="secondary"
                          className="max-w-full cursor-pointer gap-1 overflow-hidden pr-1"
                          onClick={() => onOpenDraftRule?.(dr)}
                        >
                          <span className="truncate">{dr.source.name || "?"}</span>
                          <ArrowRightIcon className="size-3 shrink-0 text-muted-foreground" />
                          <span className="truncate">{targetLabel(dr.target)}</span>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-xs"
                            className="size-4 rounded-full text-muted-foreground hover:text-foreground"
                            aria-label={t("common.delete")}
                            onClick={(e) => {
                              e.stopPropagation()
                              onDraftChange({
                                ...draft,
                                pendingRules: (draft.pendingRules ?? []).filter((x) => x.key !== dr.key),
                              })
                            }}
                          >
                            <XIcon className="size-3" />
                          </Button>
                        </Badge>
                      ))
                    ) : (
                      <span className="text-xs text-muted-foreground">{t("chains.stage.noFilters")}</span>
                    )}
                  </div>
                  <span className="text-[11px] text-muted-foreground">{t("chains.stage.filterHint")}</span>
                </div>

                {relayIds.length > 0 && (
                  <>
                    <div className="hidden items-center justify-center lg:flex">
                      <ArrowRightIcon className="size-4 text-muted-foreground" />
                    </div>
                    <div className="grid content-start gap-1.5 rounded-md border bg-card p-2.5">
                      <span className="text-xs font-medium">{t("chains.stage.transit")}</span>
                      <div className="flex flex-wrap gap-1">
                        {relayIds.map((id) => (
                          <Badge key={id} variant="outline">
                            {nameOf(id)}
                          </Badge>
                        ))}
                      </div>
                    </div>
                  </>
                )}

                <div className="hidden items-center justify-center lg:flex">
                  <ArrowRightIcon className="size-4 text-muted-foreground" />
                </div>

                {/* Stage 3: Exit */}
                <div className="grid content-start gap-1.5 rounded-md border bg-card p-2.5">
                  <span className="text-xs font-medium">{t("chains.stage.exit")}</span>
                  <div className="flex flex-wrap gap-1">
                    {exitIds.length > 0 ? (
                      exitIds.map((id) => (
                        <Badge key={id} variant="outline">
                          {nameOf(id)}
                        </Badge>
                      ))
                    ) : (
                      <span className="text-xs text-muted-foreground">{t("chains.stage.emptyExit")}</span>
                    )}
                  </div>
                  <span className="text-[11px] text-muted-foreground">{t("chains.stage.exitHint")}</span>
                </div>
              </div>
            </div>
          </div>
        ) : (
          <>
            <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
              <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-4 gap-y-1">
                {paths.length > 0 && (
                  <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span className="h-0.5 w-4 rounded-full bg-foreground/60" />
                    {t("chains.legend")}
                  </span>
                )}
                {anyDashed && (
                  <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span className="w-4 border-t-2 border-dashed border-foreground/60" />
                    {t("chains.legendBackup")}
                  </span>
                )}
                {anyBlocked && (
                  <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span className="h-0.5 w-4 rounded-full bg-foreground/20" />
                    {t("chains.legendBlocked")}
                  </span>
                )}
                <NodeMapLegend states={states} />
              </div>
              <span className="text-xs text-muted-foreground">{t("nodes.map.autoHint")}</span>
            </div>
            {unplaced.length > 0 && (
              <p className="text-xs text-muted-foreground">
                {t("nodes.map.unplaced", { names: unplaced.map((n) => n.name).join(", ") })}
              </p>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}
