"use client"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { Client } from "@/lib/api"
import { formatBytes } from "@/lib/format"
import { useI18n } from "@/lib/i18n"

/**
 * A client's traffic in a table: what is used and, in a quieter tone, the
 * limit. How it splits and how much is left are a pointer away.
 */
export function TrafficCell({ client }: { client: Pick<Client, "upload" | "download" | "trafficLimit"> }) {
  const { t, locale } = useI18n()
  const used = client.upload + client.download
  const limit = client.trafficLimit
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} className="rounded-sm whitespace-nowrap tabular-nums outline-none focus-visible:ring-2 focus-visible:ring-ring">
          {formatBytes(used, locale)}
          {limit > 0 && <span className="text-muted-foreground"> {t("clients.ofLimit", { limit: formatBytes(limit, locale) })}</span>}
        </span>
      </TooltipTrigger>
      <TooltipContent className="grid grid-cols-[auto_auto] gap-x-4 gap-y-0.5 tabular-nums">
        <span>{t("overview.download")}</span>
        <span className="text-right">{formatBytes(client.download, locale)}</span>
        <span>{t("overview.upload")}</span>
        <span className="text-right">{formatBytes(client.upload, locale)}</span>
        {limit > 0 && (
          <>
            <span>{t("clients.remaining")}</span>
            <span className="text-right">{formatBytes(Math.max(0, limit - used), locale)}</span>
          </>
        )}
      </TooltipContent>
    </Tooltip>
  )
}
