import { ArrowRightIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import type { Chain, Node } from "@/lib/api"
import { cn } from "@/lib/utils"

/**
 * A chain as a row of small chips, one per link: its two ends in one pill,
 * left to right. Links that leave from a backup entry are drawn dashed and
 * muted, so the reserve path reads at a glance without taking the primary
 * one's place.
 */
export function ChainEdgeChips({
  chain,
  nodes,
  mainLabel,
  backupLabel,
}: {
  chain: Chain
  nodes: Node[]
  mainLabel: string
  backupLabel: string
}) {
  const nameOf = (id: number) => (id === 0 ? mainLabel : nodes.find((n) => n.id === id)?.name ?? "?")
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5 in-[dd]:justify-end">
      {chain.edges.map((e) => (
        <Badge
          key={e.id}
          variant="outline"
          className={cn("gap-1 px-2 py-0.5", chain.backups.includes(e.from) && "border-dashed text-muted-foreground")}
        >
          {nameOf(e.from)}
          <ArrowRightIcon className="size-3 shrink-0 text-muted-foreground" />
          {nameOf(e.to)}
          {chain.backups.includes(e.from) && (
            <span className="text-[11px] text-muted-foreground">{backupLabel}</span>
          )}
        </Badge>
      ))}
    </div>
  )
}
