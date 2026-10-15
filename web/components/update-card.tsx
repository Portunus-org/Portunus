"use client"

import * as React from "react"
import { CalendarClockIcon, DownloadIcon, LoaderCircleIcon, RefreshCwIcon } from "lucide-react"
import { enUS, ru } from "react-day-picker/locale"
import { toast } from "@/lib/toast"

import { useConfirm } from "@/components/confirm"
import { CopyButton } from "@/components/copy-button"
import { SwitchField } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Pane, PaneAction, PaneContent, PaneDescription, PaneFooter, PaneHeader, PaneTitle } from "@/components/settings-pane"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { api, errorText, type UpdateInfo } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { useI18n } from "@/lib/i18n"

/** Sent on the window when a check for a new version was just made. */
export const updateChecked = "portunus:update-checked"

/** What brings the main server to the newest release by hand, run next to its docker-compose.yml. */
export const updateCommand = "docker compose pull && docker compose up -d"

/** Run once on a server set up before updates could be installed from the panel: it brings the updater. */
export const updaterCommand =
  "curl -fsSLO https://raw.githubusercontent.com/Portunus-org/Portunus/main/docker-compose.yml && docker compose pull && docker compose up -d"

const hourLabels = Array.from({ length: 24 }, (_, h) => `${String(h).padStart(2, "0")}:00`)

function Command({ value }: { value: string }) {
  return (
    <div className="relative">
      <pre className="rounded-lg bg-muted p-4 pr-12 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">{value}</pre>
      <CopyButton value={value} className="absolute top-2 right-2 text-muted-foreground" />
    </div>
  )
}

function HourSelect({ id, value, onChange }: { id?: string; value: number; onChange: (hour: number) => void }) {
  return (
    <Select value={String(value)} onValueChange={(v) => onChange(Number(v))}>
      <SelectTrigger id={id} className="w-24 tabular-nums">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {hourLabels.map((label, h) => (
            <SelectItem key={h} value={String(h)} className="tabular-nums">
              {label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

// The schedule is kept in UTC and chosen in the viewer's own time: a day of
// the week (-1: any) and an hour, here and there.
function toLocal(weekday: number, minute: number) {
  const at = new Date()
  at.setUTCHours(0, minute, 0, 0)
  if (weekday >= 0) at.setUTCDate(at.getUTCDate() + ((weekday - at.getUTCDay() + 7) % 7))
  return { day: weekday < 0 ? -1 : at.getDay(), hour: at.getHours() }
}

function toUTC(day: number, hour: number) {
  const at = new Date()
  at.setHours(hour, 0, 0, 0)
  if (day >= 0) at.setDate(at.getDate() + ((day - at.getDay() + 7) % 7))
  return { weekday: day < 0 ? -1 : at.getUTCDay(), minute: at.getUTCHours() * 60 + at.getUTCMinutes() }
}

/** Picks the day and the hour a waiting release is installed at. */
function PlanButton({ onPlan }: { onPlan: (at: Date) => Promise<void> }) {
  const { t, locale } = useI18n()
  const [open, setOpen] = React.useState(false)
  const [day, setDay] = React.useState<Date | undefined>(undefined)
  const [hour, setHour] = React.useState(4)
  const [pending, setPending] = React.useState(false)
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const at = day && new Date(day.getFullYear(), day.getMonth(), day.getDate(), hour)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" data-setting="update-plan">
          <CalendarClockIcon />
          {t("update.plan")}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="start">
        <Calendar mode="single" selected={day} onSelect={setDay} disabled={{ before: today }} locale={locale === "ru" ? ru : enUS} />
        <div className="flex items-center justify-between gap-2 border-t p-2">
          <HourSelect value={hour} onChange={setHour} />
          <Button
            size="sm"
            disabled={!at || pending}
            onClick={async () => {
              if (!at) return
              setPending(true)
              try {
                await onPlan(at)
                setOpen(false)
              } finally {
                setPending(false)
              }
            }}
          >
            {t("update.plan.confirm")}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

/**
 * Whether a newer release is out, and when it gets installed: now, at a
 * moment picked for it, or by itself on a schedule. The panel does not
 * replace itself: it asks the updater, the container next to it, and comes
 * back as the new version.
 */
export function UpdateCard({ update, reload, canEdit }: { update: UpdateInfo; reload: () => void; canEdit: boolean }) {
  const { t, locale } = useI18n()
  const [confirm, confirmDialog] = useConfirm()
  const [checking, setChecking] = React.useState(false)
  const [saving, setSaving] = React.useState(false)
  const saved = toLocal(update.weekday, update.minute)
  const [auto, setAuto] = React.useState(update.auto)
  const [day, setDay] = React.useState(saved.day)
  const [hour, setHour] = React.useState(saved.hour)
  const isRelease = /^v\d/.test(update.current)
  const installing = update.installing
  const changed = auto !== update.auto || (auto && (day !== saved.day || hour !== saved.hour))

  // While a release is being installed the panel goes away and comes back
  // as another version, with another interface: the page follows it there.
  const current = update.current
  const was = React.useRef(current)
  React.useEffect(() => {
    if (current !== was.current) window.location.reload()
  }, [current])
  React.useEffect(() => {
    if (!installing) return
    const timer = setInterval(reload, 3000)
    return () => clearInterval(timer)
  }, [installing, reload])

  // Monday first, as the calendar next to it has it.
  const days = React.useMemo(() => {
    const name = new Intl.DateTimeFormat(locale, { weekday: "long" })
    return [1, 2, 3, 4, 5, 6, 0].map((d) => ({ value: d, label: name.format(new Date(2026, 1, 1 + d)) }))
  }, [locale])

  const act = async (what: () => Promise<unknown>, done?: string) => {
    try {
      await what()
      if (done) toast.success(done)
      reload()
    } catch (err) {
      toast.error(errorText(err, t))
    }
  }

  async function check() {
    setChecking(true)
    try {
      await api.checkUpdate()
      reload()
      // The menu keeps its own copy of the answer for its mark.
      window.dispatchEvent(new Event(updateChecked))
    } catch {
      // The card keeps what it shows; the next look will tell.
    } finally {
      setChecking(false)
    }
  }

  function install() {
    confirm({
      title: t("update.confirm.title", { latest: update.latest ?? "" }),
      description: t("update.confirm.description"),
      action: t("update.install"),
      onConfirm: () => act(() => api.installUpdate()),
    })
  }

  const description = update.off
    ? t("update.off")
    : update.available
      ? t("update.available", { latest: update.latest ?? "", current: update.current })
      : !update.latest
        ? t(update.error ? "update.failed" : "update.unknown", { current: update.current })
        : isRelease
          ? t("update.current", { current: update.current })
          : t("update.notRelease", { current: update.current, latest: update.latest })

  return (
    <Pane>
      {confirmDialog}
      <PaneHeader>
        <PaneTitle>{t(update.available ? "update.titleAvailable" : "update.title")}</PaneTitle>
        <PaneDescription>{description}</PaneDescription>
        {!update.off && canEdit && !installing && (
          <PaneAction>
            <Button variant="outline" size="sm" onClick={check} disabled={checking}>
              <RefreshCwIcon className={checking ? "animate-spin" : undefined} />
              {t("update.check")}
            </Button>
          </PaneAction>
        )}
      </PaneHeader>
      <PaneContent className="grid gap-7 text-sm">
        {installing && (
          <p className="flex items-center gap-2">
            <LoaderCircleIcon className="size-4 shrink-0 animate-spin text-muted-foreground" />
            {t("update.installing", { latest: installing })}
          </p>
        )}

        {update.available && !installing && update.updater && (
          <div className="grid gap-3">
            {update.installError && <p className="text-destructive">{t("update.installFailed", { error: update.installError })}</p>}
            {canEdit && (
              <div className="flex flex-wrap items-center gap-2">
                <Button data-setting="update-install" onClick={install}>
                  <DownloadIcon />
                  {t("update.installNow")}
                </Button>
                {update.planned ? (
                  <Button variant="outline" onClick={() => act(() => api.planUpdate(null))}>
                    {t("update.plan.cancel")}
                  </Button>
                ) : (
                  <PlanButton onPlan={(at) => act(() => api.planUpdate(at.toISOString()))} />
                )}
              </div>
            )}
            <p className="text-muted-foreground">
              {update.planned ? t("update.planned", { at: formatDateTime(update.planned, locale) }) + " " : ""}
              {t("update.howPanel")}
            </p>
          </div>
        )}

        {update.available && !update.updater && (
          <div className="grid gap-3">
            <Command value={updateCommand} />
            <p className="text-muted-foreground">{t("update.how")}</p>
          </div>
        )}

        {update.updater ? (
          <fieldset disabled={!canEdit || !!installing} className="grid gap-3">
            <SwitchField id="updateAuto" label={t("update.auto")} hint={t("update.autoHint")} checked={auto} onCheckedChange={setAuto} />
            {auto && (
              <div className="flex flex-wrap items-center gap-2">
                <Select value={String(day)} onValueChange={(v) => setDay(Number(v))}>
                  <SelectTrigger aria-label={t("update.day")} className="w-44 capitalize">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="-1">{t("update.everyDay")}</SelectItem>
                      {days.map((d) => (
                        <SelectItem key={d.value} value={String(d.value)} className="capitalize">
                          {d.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <HourSelect value={hour} onChange={setHour} />
              </div>
            )}
          </fieldset>
        ) : (
          <div className="grid gap-3">
            <p className="text-muted-foreground">{t("update.noUpdater")}</p>
            <Command value={updaterCommand} />
          </div>
        )}

        {update.checkedAt && (
          <p className="text-xs text-muted-foreground">
            {t(update.error && update.latest ? "update.checkedFailed" : "update.checked", { at: formatDateTime(update.checkedAt, locale) })}
          </p>
        )}
      </PaneContent>
      {canEdit && update.updater && (
        <PaneFooter className="border-t">
          <Button
            size="sm"
            disabled={!changed || saving || !!installing}
            onClick={async () => {
              setSaving(true)
              await act(() => api.saveUpdateSchedule({ auto, ...toUTC(day, hour) }), t("settings.saved"))
              setSaving(false)
            }}
          >
            {t("common.save")}
          </Button>
        </PaneFooter>
      )}
    </Pane>
  )
}
