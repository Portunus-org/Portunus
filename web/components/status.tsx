import { CircleCheckIcon, CircleSlashIcon, CircleXIcon, LoaderIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export type Tone = "ok" | "pending" | "off" | "error"

/** Status badge in the style of dashboard-01's "Done" / "In Process". */
export function StatusBadge({ tone, children, className }: { tone: Tone; children: React.ReactNode; className?: string }) {
  return (
    <Badge variant="outline" className={cn("px-1.5 text-muted-foreground", className)}>
      {tone === "ok" && <CircleCheckIcon className="fill-green-500 text-background dark:fill-green-400" />}
      {tone === "pending" && <LoaderIcon className="animate-spin [animation-duration:2s]" />}
      {tone === "off" && <CircleSlashIcon />}
      {tone === "error" && <CircleXIcon className="fill-red-500 text-background dark:fill-red-400" />}
      {children}
    </Badge>
  )
}
