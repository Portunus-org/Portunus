"use client"

import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { Node, RouteTarget } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

/** A route target as one select value: "direct", "auto", "block" or "node:<id>". */
export function targetValue(t: RouteTarget): string {
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

/** Where traffic goes: directly, through an exit, or nowhere. */
export function TargetSelect({
  id,
  value,
  onChange,
  nodes,
  allowBlock = false,
  disabled,
  className,
}: {
  id?: string
  value: RouteTarget
  onChange: (t: RouteTarget) => void
  nodes: Node[]
  allowBlock?: boolean
  disabled?: boolean
  className?: string
}) {
  const { t } = useI18n()
  const exits = nodes.filter((n) => n.isExit)
  return (
    <Select value={targetValue(value)} onValueChange={(v) => onChange(parseTarget(v))} disabled={disabled}>
      <SelectTrigger id={id} className={className ?? "w-full"}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectItem value="direct">{t("routing.target.direct")}</SelectItem>
          {exits.length > 0 && <SelectItem value="auto">{t("routing.target.auto")}</SelectItem>}
          {exits.map((n) => (
            <SelectItem key={n.id} value={`node:${n.id}`}>
              {t("routing.target.node", { name: n.name })}
            </SelectItem>
          ))}
          {allowBlock && <SelectItem value="block">{t("routing.target.block")}</SelectItem>}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
