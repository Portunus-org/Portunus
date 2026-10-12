"use client"

import { ArrowDownIcon, ArrowUpIcon } from "lucide-react"

import { StatusBadge, type Tone } from "@/components/status"
import { Badge } from "@/components/ui/badge"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { Client, ClientStatus as Status } from "@/lib/api"
import { formatBytes, formatDateTime } from "@/lib/format"
import { useI18n } from "@/lib/i18n"

const tone: Record<Status, Tone> = {
  active: "ok",
  disabled: "off",
  expired: "error",
  limited: "error",
}

/**
 * The client's state. One that is connected and passing traffic right now
 * says so in the same badge, with a live dot in place of the tick, and its
 * speed a pointer away. One that may connect but is silent tells when it
 * was last seen.
 */
export function ClientStatus({ client }: { client: Pick<Client, "status" | "online" | "downRate" | "upRate" | "lastSeenAt"> }) {
  const { t, locale } = useI18n()
  if (client.status !== "active") return <StatusBadge tone={tone[client.status]}>{t(`clients.status.${client.status}`)}</StatusBadge>
  const rate = (bytes: number) => t("clients.rate", { n: formatBytes(bytes, locale) })
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} className="inline-flex rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring">
          {client.online ? (
            <Badge variant="outline" className="px-1.5 text-foreground">
              <span className="relative mx-0.5 flex size-2">
                <span className="absolute inline-flex size-full animate-ping rounded-full bg-green-500 opacity-60 motion-reduce:hidden dark:bg-green-400" />
                <span className="relative inline-flex size-2 rounded-full bg-green-500 dark:bg-green-400" />
              </span>
              {t("clients.status.online")}
            </Badge>
          ) : (
            <StatusBadge tone="ok">{t("clients.status.active")}</StatusBadge>
          )}
        </span>
      </TooltipTrigger>
      <TooltipContent className="tabular-nums">
        {client.online ? (
          <span className="inline-flex items-center gap-3">
            <span className="inline-flex items-center gap-1">
              <ArrowDownIcon className="size-3.5" aria-label={t("overview.download")} />
              {rate(client.downRate)}
            </span>
            <span className="inline-flex items-center gap-1">
              <ArrowUpIcon className="size-3.5" aria-label={t("overview.upload")} />
              {rate(client.upRate)}
            </span>
          </span>
        ) : client.lastSeenAt ? (
          t("clients.lastSeen", { date: formatDateTime(client.lastSeenAt, locale) })
        ) : (
          t("clients.neverSeen")
        )}
      </TooltipContent>
    </Tooltip>
  )
}
