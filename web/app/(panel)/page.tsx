"use client"

import * as React from "react"
import Link from "next/link"
import { ArrowDownIcon, ArrowRightIcon, ArrowUpIcon, CircleAlertIcon, CircleCheckIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { ClientsOverview } from "@/components/clients/clients-overview"
import { TrafficHistory } from "@/components/traffic-history"
import { Skeleton } from "@/components/ui/skeleton"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { formatBytes, formatDuration } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { useSettingsWindow } from "@/lib/settings-dialog"
import { protocols } from "@/lib/protocols"
import { useCanEdit } from "@/lib/session"
import { useResource } from "@/lib/use-resource"
import { cn } from "@/lib/utils"

const REFRESH_MS = 10_000

function StatCard({
  label,
  value,
  badge,
  footer,
  sub,
}: {
  label: string
  value: React.ReactNode
  badge?: React.ReactNode
  footer: React.ReactNode
  sub?: React.ReactNode
}) {
  return (
    <Card className="@container/card">
      <CardHeader>
        <CardDescription>{label}</CardDescription>
        <CardTitle className="font-sans text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
          {value === undefined || value === null || value === false ? <Skeleton className="h-8 w-20" /> : value}
        </CardTitle>
        {badge && <CardAction>{badge}</CardAction>}
      </CardHeader>
      {/* The cards of a row are equally tall; the footer takes what is left,
          so its tint reaches the bottom of every one of them. */}
      <CardFooter className="flex-1 flex-col items-start justify-start gap-1.5 text-sm">
        <div className="flex gap-2 font-medium">{footer || <Skeleton className="h-4 w-32" />}</div>
        {sub && <div className="text-muted-foreground">{sub}</div>}
      </CardFooter>
    </Card>
  )
}

/**
 * Traffic of the busiest clients as thin bars, each split into what was
 * downloaded and what was sent, on one scale: the busiest client's bar is
 * the full width.
 */
/** Downloaded and uploaded, short: an arrow each, which says what it is when pointed at. */
function TrafficArrows({ download, upload }: { download: number; upload: number }) {
  const { t, locale } = useI18n()
  const part = (Icon: typeof ArrowDownIcon, label: string, bytes: number) => (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          tabIndex={0}
          className="inline-flex items-center gap-1 rounded-sm whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Icon className="size-4 text-muted-foreground" aria-hidden />
          <span className="sr-only">{label}</span>
          {formatBytes(bytes, locale)}
        </span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
  return (
    <span className="inline-flex flex-wrap gap-x-4 gap-y-0.5 tabular-nums">
      {part(ArrowDownIcon, t("overview.download"), download)}
      {part(ArrowUpIcon, t("overview.upload"), upload)}
    </span>
  )
}

/**
 * What to do on an empty panel, in order: nothing works until there is a
 * connection, and nobody can use it until there is a client. Shown until
 * both exist.
 */
function FirstSteps({ hasInbound, hasClient }: { hasInbound: boolean; hasClient: boolean }) {
  const { t } = useI18n()
  const canEdit = useCanEdit()
  const steps = [
    {
      done: hasInbound,
      title: t("overview.steps.inbound"),
      text: t("overview.steps.inboundText"),
      href: "/inbounds/new/",
      action: t("overview.steps.inboundAction"),
    },
    {
      done: hasClient,
      title: t("overview.steps.client"),
      text: t("overview.steps.clientText"),
      href: "/clients/#new",
      action: t("overview.steps.clientAction"),
    },
    { done: false, title: t("overview.steps.share"), text: t("overview.steps.shareText") },
  ]
  const current = steps.findIndex((s) => !s.done)
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("overview.steps.title")}</CardTitle>
        <CardDescription>{t("overview.steps.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <ol className="grid gap-5">
          {steps.map((step, i) => (
            <li key={i} className="flex items-start gap-3">
              {step.done ? (
                <CircleCheckIcon className="mt-0.5 size-5 shrink-0 fill-green-500 text-background dark:fill-green-400" />
              ) : (
                <span
                  className={cn(
                    "mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full border text-xs tabular-nums",
                    i === current ? "border-foreground font-medium" : "text-muted-foreground"
                  )}
                >
                  {i + 1}
                </span>
              )}
              <div className="grid flex-1 gap-1">
                <span className={cn("font-medium", i !== current && "text-muted-foreground")}>{step.title}</span>
                <span className="text-muted-foreground">{step.text}</span>
                {i === current && step.href && canEdit && (
                  <div className="pt-1.5">
                    <Button size="sm" asChild>
                      <Link href={step.href}>
                        {step.action}
                        <ArrowRightIcon />
                      </Link>
                    </Button>
                  </div>
                )}
              </div>
            </li>
          ))}
        </ol>
      </CardContent>
    </Card>
  )
}

export default function OverviewPage() {
  const { t, tp, locale } = useI18n()
  const { openSettings } = useSettingsWindow()
  const { data: system } = useResource(api.system, REFRESH_MS)
  const { data: sb } = useResource(api.singbox, REFRESH_MS)
  const { data: clients } = useResource(api.clients, REFRESH_MS)
  const { data: inbounds } = useResource(api.inbounds, REFRESH_MS)
  const { data: settings } = useResource(api.settings)

  const cs = React.useMemo(() => clients ?? [], [clients])
  const { data: hours } = useResource(api.traffic, 60_000)
  const history = React.useMemo(() => hours ?? [], [hours])
  const active = cs.filter((c) => c.status === "active").length
  const attention = cs.filter((c) => c.status === "expired" || c.status === "limited").length
  const traffic = cs.reduce((n, c) => n + c.upload + c.download, 0)
  const enabled = (inbounds ?? []).filter((i) => i.enabled)
  const state = sb?.status.state
  const problem = sb?.applyError || (state === "failed" ? sb?.status.lastError : "")

  return (
    <>
      {(settings && !settings.publicHost) || (system && !system.listenLocal) ? (
        <div className="grid gap-4 px-4 lg:px-6">
          {settings && !settings.publicHost && (
            <Alert>
              <CircleAlertIcon />
              <AlertTitle>{t("overview.alert.noHost")}</AlertTitle>
              <AlertDescription>
                <Button variant="link" className="h-auto p-0" onClick={() => openSettings("server")}>
                  {t("nav.settings")} <ArrowRightIcon />
                </Button>
              </AlertDescription>
            </Alert>
          )}
          {system && !system.listenLocal && (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>{t("overview.alert.exposed", { addr: system.listen })}</AlertTitle>
            </Alert>
          )}
        </div>
      ) : null}

      {clients && inbounds && (inbounds.length === 0 || cs.length === 0) && (
        <div className="px-4 lg:px-6">
          <FirstSteps hasInbound={inbounds.length > 0} hasClient={cs.length > 0} />
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 px-4 *:data-[slot=card]:bg-gradient-to-t *:data-[slot=card]:from-primary/5 *:data-[slot=card]:to-card *:data-[slot=card]:shadow-xs lg:px-6 @xl/main:grid-cols-2 @5xl/main:grid-cols-4 dark:*:data-[slot=card]:bg-card">
        <StatCard
          label={t("overview.activeClients")}
          value={clients && active}
          footer={attention > 0 ? tp("overview.needAttention", attention) : t("overview.allWithinLimits")}
          sub={clients && tp("overview.clientsTotal", cs.length)}
        />
        <StatCard
          label={t("overview.traffic")}
          value={clients && formatBytes(traffic, locale)}
          footer={
            clients && <TrafficArrows download={cs.reduce((n, c) => n + c.download, 0)} upload={cs.reduce((n, c) => n + c.upload, 0)} />
          }
          sub={t("overview.chart.description")}
        />
        <StatCard
          label={t("overview.inbounds")}
          value={inbounds && enabled.length}
          footer={
            inbounds &&
            (enabled.length ? [...new Set(enabled.map((i) => protocols[i.protocol].label))].join(", ") : t("overview.noneEnabled"))
          }
          sub={inbounds && tp("overview.inboundsTotal", inbounds.length)}
        />
        <StatCard
          label={t("overview.singbox")}
          value={state && t(`state.${state}`)}
          badge={sb?.status.version && <Badge variant="outline">{sb.status.version}</Badge>}
          footer={
            problem
              ? t("overview.configNotApplied")
              : state === "running" && sb
                ? t("overview.up", {
                    d: formatDuration(sb.uptimeSeconds, locale),
                  })
                : t("overview.startsWithInbound")
          }
          sub={sb && sb.status.restarts > 0 ? tp("overview.restarts", sb.status.restarts) : settings?.publicHost}
        />
      </div>

      <div className="px-4 lg:px-6">
        <TrafficHistory points={history} />
      </div>

      <ClientsOverview clients={cs} loading={!clients} />
    </>
  )
}
