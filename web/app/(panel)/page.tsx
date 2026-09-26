"use client"

import * as React from "react"
import Link from "next/link"
import { ArrowRightIcon, CircleAlertIcon } from "lucide-react"
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts"

import { StatusBadge } from "@/components/status"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { api, type Client } from "@/lib/api"
import { formatBytes, formatDuration } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { protocols } from "@/lib/protocols"
import { useResource } from "@/lib/use-resource"

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
        <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">{value}</CardTitle>
        {badge && <CardAction>{badge}</CardAction>}
      </CardHeader>
      <CardFooter className="flex-col items-start gap-1.5 text-sm">
        <div className="line-clamp-1 flex gap-2 font-medium">{footer}</div>
        {sub && <div className="text-muted-foreground">{sub}</div>}
      </CardFooter>
    </Card>
  )
}

function TrafficChart({ clients }: { clients: Client[] }) {
  const { t, locale } = useI18n()
  const config = {
    download: { label: t("overview.download"), color: "var(--primary)" },
    upload: { label: t("overview.upload"), color: "var(--muted-foreground)" },
  } satisfies ChartConfig
  const top = [...clients]
    .map((c) => ({ name: c.name, download: c.download, upload: c.upload, total: c.download + c.upload }))
    .filter((c) => c.total > 0)
    .sort((a, b) => b.total - a.total)
    .slice(0, 10)

  return (
    <Card className="@container/card">
      <CardHeader>
        <CardTitle>{t("overview.chart.title")}</CardTitle>
        <CardDescription>{t("overview.chart.description")}</CardDescription>
      </CardHeader>
      <CardContent className="px-2 pt-4 sm:px-6 sm:pt-6">
        {top.length === 0 ? (
          <div className="flex h-[250px] flex-col items-center justify-center gap-1 text-center">
            <p className="font-medium">{t("overview.chart.empty")}</p>
            <p className="text-sm text-muted-foreground">{t("overview.chart.emptyHint")}</p>
          </div>
        ) : (
          <ChartContainer config={config} className="aspect-auto w-full" style={{ height: Math.max(250, 60 + top.length * 36) }}>
            <BarChart data={top} layout="vertical" margin={{ left: 0, right: 12 }}>
              <CartesianGrid horizontal={false} />
              <YAxis dataKey="name" type="category" tickLine={false} axisLine={false} width={110} tickMargin={8} />
              <XAxis
                type="number"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                tickFormatter={(v: number) => formatBytes(v, locale)}
              />
              <ChartTooltip
                cursor={false}
                content={
                  <ChartTooltipContent
                    indicator="dot"
                    formatter={(v, name) => (
                      <div className="flex w-full justify-between gap-4">
                        <span className="text-muted-foreground">{config[name as keyof typeof config]?.label ?? name}</span>
                        <span className="font-mono font-medium tabular-nums">{formatBytes(Number(v), locale)}</span>
                      </div>
                    )}
                  />
                }
              />
              <ChartLegend content={<ChartLegendContent />} />
              <Bar dataKey="download" stackId="t" fill="var(--color-download)" radius={[4, 0, 0, 4]} />
              <Bar dataKey="upload" stackId="t" fill="var(--color-upload)" radius={[0, 4, 4, 0]} />
            </BarChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  )
}

export default function OverviewPage() {
  const { t, tp, locale } = useI18n()
  const { data: system } = useResource(api.system, REFRESH_MS)
  const { data: sb } = useResource(api.singbox, REFRESH_MS)
  const { data: clients } = useResource(api.clients, REFRESH_MS)
  const { data: inbounds } = useResource(api.inbounds, REFRESH_MS)
  const { data: settings } = useResource(api.settings)

  const cs = clients ?? []
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
                <Button asChild variant="link" className="h-auto p-0">
                  <Link href="/settings/">
                    {t("nav.settings")} <ArrowRightIcon />
                  </Link>
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

      <div className="grid grid-cols-1 gap-4 px-4 *:data-[slot=card]:bg-gradient-to-t *:data-[slot=card]:from-primary/5 *:data-[slot=card]:to-card *:data-[slot=card]:shadow-xs lg:px-6 @xl/main:grid-cols-2 @5xl/main:grid-cols-4 dark:*:data-[slot=card]:bg-card">
        <StatCard
          label={t("overview.activeClients")}
          value={clients ? active : "—"}
          badge={clients && <Badge variant="outline">{t("overview.ofTotal", { n: cs.length })}</Badge>}
          footer={attention > 0 ? tp("overview.needAttention", attention) : t("overview.allWithinLimits")}
          sub={
            <Link href="/clients/" className="hover:text-foreground hover:underline">
              {t("overview.manageClients")}
            </Link>
          }
        />
        <StatCard
          label={t("overview.traffic")}
          value={clients ? formatBytes(traffic, locale) : "—"}
          footer={t("overview.trafficFooter")}
          sub={t("overview.trafficSub")}
        />
        <StatCard
          label={t("overview.inbounds")}
          value={inbounds ? enabled.length : "—"}
          badge={inbounds && <Badge variant="outline">{t("overview.ofTotal", { n: inbounds.length })}</Badge>}
          footer={
            enabled.length ? [...new Set(enabled.map((i) => protocols[i.protocol].label))].join(", ") : t("overview.noneEnabled")
          }
          sub={
            <Link href="/inbounds/" className="hover:text-foreground hover:underline">
              {t("overview.manageInbounds")}
            </Link>
          }
        />
        <StatCard
          label={t("overview.singbox")}
          value={state ? t(`state.${state}`) : "—"}
          badge={sb?.status.version && <Badge variant="outline">{sb.status.version}</Badge>}
          footer={
            problem
              ? t("overview.configNotApplied")
              : state === "running" && sb
                ? t("overview.up", { d: formatDuration(sb.uptimeSeconds, locale) })
                : t("overview.startsWithInbound")
          }
          sub={tp("overview.restarts", sb?.status.restarts ?? 0)}
        />
      </div>

      <div className="px-4 lg:px-6">
        <TrafficChart clients={cs} />
      </div>

      <div className="px-4 lg:px-6">
        <div className="overflow-hidden rounded-lg border">
          <Table>
            <TableHeader className="bg-muted">
              <TableRow>
                <TableHead>{t("overview.table.node")}</TableHead>
                <TableHead>{t("overview.table.role")}</TableHead>
                <TableHead className="hidden md:table-cell">{t("settings.server.publicHost")}</TableHead>
                <TableHead className="hidden sm:table-cell">{t("overview.table.memory")}</TableHead>
                <TableHead>sing-box</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow>
                <TableCell className="font-medium">main</TableCell>
                <TableCell>
                  <Badge variant="outline" className="px-1.5 text-muted-foreground">
                    {t("overview.table.entry")}
                  </Badge>
                </TableCell>
                <TableCell className="hidden font-mono text-muted-foreground md:table-cell">{settings?.publicHost || "—"}</TableCell>
                <TableCell className="hidden tabular-nums sm:table-cell">
                  {system ? formatBytes(system.memory.rssBytes, locale) : "—"}
                </TableCell>
                <TableCell>
                  {state && (
                    <StatusBadge tone={state === "running" ? "ok" : state === "failed" ? "error" : state === "starting" ? "pending" : "off"}>
                      {t(`state.${state}`)}
                    </StatusBadge>
                  )}
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </div>
    </>
  )
}
