"use client"

import * as React from "react"
import { CalendarIcon, ChevronLeftIcon, ChevronRightIcon } from "lucide-react"
import { enUS, ru } from "react-day-picker/locale"

import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { useI18n } from "@/lib/i18n"

const pad = (n: number) => String(n).padStart(2, "0")
const toValue = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const toDate = (value: string) => {
  const [y, m, d] = value.split("-").map(Number)
  return y && m && d ? new Date(y, m - 1, d) : undefined
}

const yearsPerPage = 12
// The size of the days of a month: seven cells across, six weeks down.
const gridClass = "grid h-[15.625rem] w-[13.25rem] grid-cols-3 grid-rows-4 gap-1 p-2"

/**
 * A date in a form: shadcn's date picker (a button that opens a calendar),
 * with the day kept in a hidden field as YYYY-MM-DD, the way a native date
 * input would send it. No day is a value too ("never"), so the calendar has
 * a way back to it.
 */
export function DateField({
  id,
  name,
  defaultValue = "",
  placeholder,
  clearLabel,
}: {
  id: string
  name: string
  /** YYYY-MM-DD, or "" for no date. */
  defaultValue?: string
  placeholder: string
  /** The words for "no date", shown under the calendar when one is set. */
  clearLabel: string
}) {
  const { t, locale } = useI18n()
  const [value, setValue] = React.useState(defaultValue)
  const [open, setOpen] = React.useState(false)
  const date = toDate(value)
  // What the calendar shows: the days of a month, the months of its year to
  // pick from, or a page of years. A date years away is three clicks (year,
  // month, day) instead of a long leafing through months.
  const [view, setView] = React.useState<"days" | "months" | "years">("days")
  const [month, setMonth] = React.useState(() => date ?? new Date())
  const [firstYear, setFirstYear] = React.useState(0)
  const today = new Date()
  const shown = { y: month.getFullYear(), m: month.getMonth() }
  const step = (by: number) => {
    if (view === "years") setFirstYear(firstYear + by * yearsPerPage)
    else setMonth(view === "months" ? new Date(shown.y + by, shown.m) : new Date(shown.y, shown.m + by))
  }
  const monthName = (m: number, month: "long" | "short") => new Intl.DateTimeFormat(locale, { month }).format(new Date(2000, m))
  const cell = (current: boolean, now: boolean) => (current ? "default" : now ? "secondary" : "ghost")
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setView("days")
          setMonth(date ?? new Date())
        }
        setOpen(next)
      }}
    >
      <input type="hidden" name={name} value={value} />
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          data-empty={!date}
          className="w-full justify-between text-left font-normal tabular-nums data-[empty=true]:text-muted-foreground"
        >
          {date ? new Intl.DateTimeFormat(locale, { day: "numeric", month: "short", year: "numeric" }).format(date) : placeholder}
          <CalendarIcon className="text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="end">
        <div className="flex items-center gap-1 px-2 pt-2">
          <Button type="button" variant="ghost" size="icon-sm" aria-label={t("calendar.earlier")} onClick={() => step(-1)}>
            <ChevronLeftIcon />
          </Button>
          <div className="flex flex-1 justify-center gap-0.5">
            <Button
              type="button"
              variant={view === "months" ? "secondary" : "ghost"}
              size="sm"
              className="px-2 capitalize"
              aria-label={t("calendar.month")}
              aria-pressed={view === "months"}
              onClick={() => setView(view === "months" ? "days" : "months")}
            >
              {monthName(shown.m, "long")}
            </Button>
            <Button
              type="button"
              variant={view === "years" ? "secondary" : "ghost"}
              size="sm"
              className="px-2 tabular-nums"
              aria-label={t("calendar.year")}
              aria-pressed={view === "years"}
              onClick={() => {
                // The shown year sits in the second row: an end date is nearly
                // always ahead, so most of the page is the years to come.
                setFirstYear(shown.y - 4)
                setView(view === "years" ? "days" : "years")
              }}
            >
              {shown.y}
            </Button>
          </div>
          <Button type="button" variant="ghost" size="icon-sm" aria-label={t("calendar.later")} onClick={() => step(1)}>
            <ChevronRightIcon />
          </Button>
        </div>
        {view === "days" ? (
          <Calendar
            mode="single"
            selected={date}
            month={month}
            onMonthChange={setMonth}
            // The heading above stands in for the calendar's own, and six
            // weeks always keep the three views one size.
            hideNavigation
            fixedWeeks
            classNames={{ month_caption: "hidden" }}
            locale={locale === "ru" ? ru : enUS}
            onSelect={(day) => {
              if (day) setValue(toValue(day))
              setOpen(false)
            }}
          />
        ) : (
          <div className={gridClass}>
            {view === "months"
              ? Array.from({ length: 12 }, (_, m) => (
                  <Button
                    key={m}
                    type="button"
                    size="sm"
                    variant={cell(m === shown.m, m === today.getMonth() && shown.y === today.getFullYear())}
                    className="h-full font-normal capitalize"
                    onClick={() => {
                      setMonth(new Date(shown.y, m))
                      setView("days")
                    }}
                  >
                    {monthName(m, "short").replace(".", "")}
                  </Button>
                ))
              : Array.from({ length: yearsPerPage }, (_, i) => firstYear + i).map((y) => (
                  <Button
                    key={y}
                    type="button"
                    size="sm"
                    variant={cell(y === shown.y, y === today.getFullYear())}
                    className="h-full font-normal tabular-nums"
                    onClick={() => {
                      setMonth(new Date(y, shown.m))
                      setView("months")
                    }}
                  >
                    {y}
                  </Button>
                ))}
          </div>
        )}
        {date && (
          <div className="border-t p-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="w-full"
              onClick={() => {
                setValue("")
                setOpen(false)
              }}
            >
              {clearLabel}
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}
