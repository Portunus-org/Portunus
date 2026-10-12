"use client"

import * as React from "react"
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts"

import { SteadyWidth } from "@/components/steady-width"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import type { TrafficPoint } from "@/lib/api"
import { formatBytes } from "@/lib/format"
import { useI18n } from "@/lib/i18n"

// The periods to look at: how many steps back and how long a step is.
const ranges = {
  day: { steps: 24, hours: 1 },
  week: { steps: 7, hours: 24 },
  month: { steps: 30, hours: 24 },
} as const
type Range = keyof typeof ranges

const HOUR = 3_600_000
const ANIMATION_MS = 600

/**
 * Traffic of all clients together over time: dashboard-01's interactive
 * area chart. The server keeps it by the hour; here it is put into the
 * steps of the chosen period, in the viewer's own time zone, with the
 * silent hours and days filled in.
 */
export const TrafficHistory = React.memo(function TrafficHistory({ points }: { points: TrafficPoint[] }) {
  const { t, locale } = useI18n()
  const [range, setRange] = React.useState<Range>("week")
  const config = React.useMemo(
    () =>
      ({
        download: { label: t("overview.download"), color: "var(--primary)" },
        upload: { label: t("overview.upload"), color: "var(--muted-foreground)" },
      }) satisfies ChartConfig,
    [t]
  )

  const data = React.useMemo(() => {
    const { steps, hours } = ranges[range]
    // The current step is the last one: this hour, or today.
    const end = new Date()
    if (hours === 24) end.setHours(0, 0, 0, 0)
    else end.setMinutes(0, 0, 0)
    const rows = Array.from({ length: steps }, (_, i) => {
      const at = new Date(end)
      if (hours === 24) at.setDate(at.getDate() - (steps - 1 - i))
      else at.setTime(at.getTime() - (steps - 1 - i) * HOUR)
      return { at: at.getTime(), download: 0, upload: 0 }
    })
    for (const p of points) {
      const time = new Date(p.at).getTime()
      // Days differ in length where clocks change, so a step is found by
      // its start and not by dividing.
      let i = rows.length - 1
      while (i >= 0 && rows[i].at > time) i--
      if (i < 0) continue
      rows[i].download += p.download
      rows[i].upload += p.upload
    }
    return rows
  }, [points, range])
  const total = data.reduce((n, r) => n + r.download + r.upload, 0)

  // The chart draws itself in when the page opens and when the period is
  // changed, and at no other time: fresh numbers just take their place.
  const [animate, setAnimate] = React.useState(true)
  const [shown, setShown] = React.useState(range)
  if (shown !== range) {
    setShown(range)
    setAnimate(true)
  }
  React.useEffect(() => {
    const timer = setTimeout(() => setAnimate(false), ANIMATION_MS + 200)
    return () => clearTimeout(timer)
  }, [shown])

  const label = (at: number, long = false) =>
    range === "day"
      ? new Intl.DateTimeFormat(
          locale,
          long ? { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" } : { hour: "2-digit", minute: "2-digit" }
        ).format(at)
      : new Intl.DateTimeFormat(locale, { day: "numeric", month: "short" }).format(at)

  return (
    <Card className="@container/card">
      <CardHeader>
        <CardTitle>{t("overview.history.title")}</CardTitle>
        <CardDescription>{t(`overview.history.total.${range}`, { n: formatBytes(total, locale) })}</CardDescription>
        <CardAction>
          <ToggleGroup
            type="single"
            value={range}
            onValueChange={(v) => v && setRange(v as Range)}
            variant="outline"
            className="hidden *:data-[slot=toggle-group-item]:px-4! @[767px]/card:flex"
          >
            {(Object.keys(ranges) as Range[]).map((r) => (
              <ToggleGroupItem key={r} value={r}>
                {t(`overview.history.${r}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <Select value={range} onValueChange={(v) => setRange(v as Range)}>
            <SelectTrigger className="flex w-36 @[767px]/card:hidden" size="sm" aria-label={t("overview.history.period")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {(Object.keys(ranges) as Range[]).map((r) => (
                  <SelectItem key={r} value={r}>
                    {t(`overview.history.${r}`)}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </CardAction>
      </CardHeader>
      <CardContent className="px-2 pt-4 sm:px-6 sm:pt-6">
        <SteadyWidth>
          <ChartContainer config={config} className="aspect-auto h-[250px] w-full justify-start">
            <AreaChart accessibilityLayer data={data} margin={{ left: 12, right: 12 }}>
              <defs>
                <linearGradient id="fillDownload" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="var(--color-download)" stopOpacity={0.9} />
                  <stop offset="95%" stopColor="var(--color-download)" stopOpacity={0.1} />
                </linearGradient>
                <linearGradient id="fillUpload" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="var(--color-upload)" stopOpacity={0.8} />
                  <stop offset="95%" stopColor="var(--color-upload)" stopOpacity={0.1} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="at"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                minTickGap={32}
                tickFormatter={(at: number) => label(at)}
              />
              <ChartTooltip
                cursor={false}
                content={
                  <ChartTooltipContent
                    labelFormatter={(_, payload) => label(Number(payload?.[0]?.payload?.at), true)}
                    formatter={(value, name) => (
                      <>
                        <div
                          className="size-2.5 shrink-0 rounded-[2px] bg-(--color-bg)"
                          style={{ "--color-bg": `var(--color-${name})` } as React.CSSProperties}
                        />
                        <span className="text-muted-foreground">{config[name as keyof typeof config]?.label ?? name}</span>
                        <span className="ml-auto pl-4 font-medium text-foreground tabular-nums">{formatBytes(Number(value), locale)}</span>
                      </>
                    )}
                  />
                }
              />
              <Area
                dataKey="upload"
                type="monotone"
                fill="url(#fillUpload)"
                stroke="var(--color-upload)"
                stackId="traffic"
                isAnimationActive={animate}
                animationDuration={ANIMATION_MS}
              />
              <Area
                dataKey="download"
                type="monotone"
                fill="url(#fillDownload)"
                stroke="var(--color-download)"
                stackId="traffic"
                isAnimationActive={animate}
                animationDuration={ANIMATION_MS}
              />
            </AreaChart>
          </ChartContainer>
        </SteadyWidth>
      </CardContent>
    </Card>
  )
})
