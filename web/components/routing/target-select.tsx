"use client"

import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { Chain, Node, RouteTarget } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

/** A route target as one select value: "direct", "auto", "block", "node:<id>", or "inherit" for null. */
export function targetValue(t: RouteTarget | null): string {
  if (!t) return "inherit"
  return t.action === "node" ? `node:${t.nodeId}` : t.action
}

export function parseTarget(v: string): RouteTarget {
  if (v.startsWith("node:")) return { action: "node", nodeId: Number(v.slice(5)) }
  return { action: v as RouteTarget["action"] }
}

/** The words for a target, e.g. "Through Amsterdam". */
export function useTargetLabel(nodes: Node[]) {
  const { t } = useI18n()
  return (target: RouteTarget) => {
    if (target.action !== "node") return t(`routing.target.${target.action}`)
    const node = nodes.find((n) => n.id === target.nodeId)
    return node ? t("routing.target.node", { name: node.name }) : t("routing.target.missing")
  }
}

function ExitItems({ exits }: { exits: Node[] }) {
  const { t } = useI18n()
  return (
    <>
      {exits.map((n) => (
        <SelectItem key={n.id} value={`node:${n.id}`}>
          {t("routing.target.node", { name: n.name })}
        </SelectItem>
      ))}
    </>
  )
}

/** Where traffic goes: directly, through an exit, or nowhere. */
export function TargetSelect({
  id,
  value,
  onChange,
  nodes,
  chains = [],
  allowBlock = false,
  disabled,
  className,
}: {
  id?: string
  value: RouteTarget
  onChange: (t: RouteTarget) => void
  nodes: Node[]
  chains?: Chain[]
  allowBlock?: boolean
  disabled?: boolean
  className?: string
}) {
  const { t } = useI18n()
  const exits = nodes.filter((n) => n.isExit)
  const hasAuto = exits.length > 0 || chains.length > 0
  return (
    <Select value={targetValue(value)} onValueChange={(v) => onChange(parseTarget(v))} disabled={disabled}>
      <SelectTrigger id={id} className={className ?? "w-full"}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectItem value="direct">{t("routing.target.direct")}</SelectItem>
          {hasAuto && <SelectItem value="auto">{t("routing.target.auto")}</SelectItem>}
          <ExitItems exits={exits} />
          {allowBlock && <SelectItem value="block">{t("routing.target.block")}</SelectItem>}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

/** Where a chain's unmatched traffic goes: inherit global, directly, through the chain, or through an exit. */
export function ChainDefaultSelect({
  id,
  value,
  globalTarget,
  onChange,
  nodes,
  disabled,
  className,
}: {
  id?: string
  value: RouteTarget | null
  globalTarget?: RouteTarget
  onChange: (t: RouteTarget | null) => void
  nodes: Node[]
  disabled?: boolean
  className?: string
}) {
  const { t } = useI18n()
  const targetLabel = useTargetLabel(nodes)
  const exits = nodes.filter((n) => n.isExit)
  const inheritLabel = globalTarget
    ? t("routing.default.inheritOf", { target: targetLabel(globalTarget) })
    : t("routing.default.inherit")
  return (
    <Select
      value={targetValue(value)}
      onValueChange={(v) => onChange(v === "inherit" ? null : parseTarget(v))}
      disabled={disabled}
    >
      <SelectTrigger id={id} className={className ?? "w-full"}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectItem value="inherit">{inheritLabel}</SelectItem>
          <SelectItem value="direct">{t("routing.target.direct")}</SelectItem>
          <SelectItem value="auto">{t("routing.target.auto")}</SelectItem>
          <ExitItems exits={exits} />
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
