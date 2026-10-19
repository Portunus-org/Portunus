"use client"

import * as React from "react"

import { CircleAlertIcon, CircleXIcon, Undo2Icon, XIcon } from "lucide-react"

import { CopyButton } from "@/components/copy-button"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"

type Level = "error" | "warn" | "info" | "plain"

interface Line {
  /** HH:MM:SS in the viewer's time; "" for a line without a time. */
  time: string
  level: Level
  text: string
}

// "+0000 2026-10-13 11:34:14 ERROR [2663089431 5.0s] connection: open …"
const shape = /^([+-]\d{4}) (\d{4}-\d\d-\d\d) (\d\d:\d\d:\d\d) (TRACE|DEBUG|INFO|WARN|ERROR|FATAL|PANIC) (?:\[\d+ [^\]]*\] )?(.*)$/

// What sing-box prints before its log is set up, and when it stops:
// "ERROR[29286] sing-box did not closed properly: …"
const early = /^(TRACE|DEBUG|INFO|WARN|ERROR|FATAL|PANIC)\[\d+\] (.*)$/

const levelOf = (word: string): Level =>
  word === "ERROR" || word === "FATAL" || word === "PANIC" ? "error" : word === "WARN" ? "warn" : "info"

function parse(raw: string): Line {
  const m = shape.exec(raw)
  if (!m) {
    const e = early.exec(raw)
    return e ? { time: "", level: levelOf(e[1]), text: e[2] } : { time: "", level: "plain", text: raw }
  }
  const [, zone, day, clock, level, text] = m
  const at = new Date(`${day}T${clock}${zone.slice(0, 3)}:${zone.slice(3)}`)
  return {
    time: Number.isNaN(at.getTime()) ? clock : at.toLocaleTimeString([], { hour12: false }),
    level: levelOf(level),
    text,
  }
}

// What a message most likely means, for the ones that keep coming up. The
// first group of the pattern, when there is one, is the address it is about.
// One problem is often written in several ways: the resolver and the router
// each report a name that does not exist, a stranger knocks from a new port
// every time. The key is what makes them one row.
function keyOf(text: string) {
  const name = /lookup (?:failed for )?(\S+?): .*NXDOMAIN/.exec(text)
  if (name) return "nxdomain " + name[1]
  const knock = /^(\S+): process connection from \S+: .*REALITY: processed invalid connection/.exec(text)
  if (knock) return "knock " + knock[1]
  // The port a connection came from is new each time and says nothing.
  return text.replace(/from (\[[0-9a-fA-F:]+\]|[0-9.]+):\d+/g, "from $1")
}

const hints: [RegExp, MessageKey][] = [
  [/did not closed properly/, "logs.hint.stopped"],
  [/certificate-provider\/acme.*context canceled/, "logs.hint.acmeCanceled"],
  [/lookup (?:failed for )?(\S+?): .*NXDOMAIN/, "logs.hint.noName"],
  [/using outbound\/direct\[[^\]]*\]: dial (?:tcp|udp) (\S+): i\/o timeout/, "logs.hint.timeout"],
  [/dial (?:tcp|udp) (\S+): connect: connection refused/, "logs.hint.refused"],
  [/dial (?:tcp|udp) (\S+): connect: (?:network is unreachable|no route to host)/, "logs.hint.unreachable"],
  [/bind: address already in use/, "logs.hint.inUse"],
  [/bind: permission denied/, "logs.hint.bindDenied"],
  [/lookup (\S+).*no such host/, "logs.hint.noHost"],
  [
    /REALITY: processed invalid connection|tls: first record does not look like a TLS handshake|unknown user|authentication failed/i,
    "logs.hint.stranger",
  ],
]

function hintFor(text: string, t: ReturnType<typeof useI18n>["t"]) {
  for (const [pattern, key] of hints) {
    const m = pattern.exec(text)
    if (m) return t(key, { address: m[1] ?? "" })
  }
  return null
}

// The level is a small mark before the line, the same marks statuses have
// elsewhere in the panel; the text itself stays one colour. What is not a
// problem steps back instead.
function Mark({ level }: { level: Level }) {
  return (
    <span className="flex h-[1lh] w-3.5 shrink-0 items-center">
      {level === "error" && <CircleXIcon className="size-3.5 fill-red-500 text-background dark:fill-red-400" />}
      {level === "warn" && <CircleAlertIcon className="size-3.5 text-muted-foreground" />}
    </span>
  )
}

const isProblem = (l: Line) => l.level === "error" || l.level === "warn"

// The columns for the time and for the mark are there only when some line
// on the screen has one: a log of lines without either starts at the edge.
function Row({ line, time, mark, children }: { line: Line; time: boolean; mark: boolean; children?: React.ReactNode }) {
  const problem = isProblem(line)
  return (
    <div className="flex gap-2.5">
      {time && <span className="w-[8ch] shrink-0 text-muted-foreground tabular-nums">{line.time}</span>}
      {mark && <Mark level={line.level} />}
      <span className={`min-w-0 break-words whitespace-pre-wrap ${problem ? "" : "text-muted-foreground"}`}>{line.text}</span>
      {children}
    </div>
  )
}

// Problems that were looked at and put away, kept in this browser. A
// message is put away by its text: it stays away while it keeps repeating,
// which is the point, and can be brought back.
const putAwayKey = "portunus.logs.putAway"

function usePutAway() {
  // Read once, when the log is first shown: that happens in the browser,
  // after the page has loaded what to show.
  const [hidden, setHidden] = React.useState<string[]>(() => {
    try {
      const saved: unknown = typeof window === "undefined" ? [] : JSON.parse(window.localStorage.getItem(putAwayKey) ?? "[]")
      return Array.isArray(saved) ? saved.filter((v): v is string => typeof v === "string") : []
    } catch {
      // Nothing saved, or not ours: start with nothing put away.
      return []
    }
  })
  const save = React.useCallback((next: string[]) => {
    // Old entries go first when the list grows long: their messages left the log long ago.
    next = next.slice(-200)
    setHidden(next)
    try {
      window.localStorage.setItem(putAwayKey, JSON.stringify(next))
    } catch {
      // Storage is off: it holds for this page only.
    }
  }, [])
  return [hidden, save] as const
}

/**
 * sing-box's output, readable: every line wrapped whole, problems marked
 * and the rest stepped back, and a view of the problems alone where a
 * message that repeats is one row with a count and, when it is a known
 * one, a word on what it means. A problem that was dealt with is put away
 * with a click.
 */
export function SingboxLogs({ lines, empty }: { lines: string[]; empty: string }) {
  const { t } = useI18n()
  const [view, setView] = React.useState<"all" | "problems">("all")
  const [wantHidden, setShowHidden] = React.useState(false)
  const [hidden, setHidden] = usePutAway()
  const parsed = React.useMemo(() => lines.map(parse), [lines])
  const groups = React.useMemo(() => {
    const seen = new Map<string, { key: string; line: Line; count: number }>()
    for (const line of parsed) {
      if (line.level !== "error" && line.level !== "warn") continue
      const key = keyOf(line.text)
      const group = seen.get(key)
      // The newest goes last, as in the full log.
      seen.delete(key)
      seen.set(key, { key, line, count: (group?.count ?? 0) + 1 })
    }
    return [...seen.values()]
  }, [parsed])
  const away = React.useMemo(() => new Set(hidden), [hidden])
  const open = groups.filter((g) => !away.has(g.key))
  const closed = groups.length - open.length
  // With nothing hidden there is nothing to show "as well".
  const showHidden = wantHidden && closed > 0
  const shown = showHidden ? groups : open
  const timed = shown.some((g) => g.line.time)

  // In the full log the newest lines are at the bottom: keep them in view.
  // The problems are few and read from the top; they stay where they are.
  const box = React.useRef<HTMLDivElement>(null)
  React.useEffect(() => {
    const el = box.current?.querySelector<HTMLElement>("[data-slot=scroll-area-viewport]")
    if (el) el.scrollTop = view === "all" ? el.scrollHeight : 0
  }, [parsed, view])

  // What is on the screen, as text: for a message to someone who can help.
  const text =
    view === "all"
      ? lines.join("\n")
      : shown.map(({ line, count }) => `${line.time} ${line.level.toUpperCase()} ${line.text}${count > 1 ? ` (×${count})` : ""}`).join("\n")

  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup type="single" variant="outline" size="sm" value={view} onValueChange={(v) => v && setView(v as typeof view)}>
          <ToggleGroupItem value="all">{t("logs.all")}</ToggleGroupItem>
          <ToggleGroupItem value="problems">
            {t("logs.problems")}
            {open.length > 0 && <span className="text-muted-foreground tabular-nums">{open.length}</span>}
          </ToggleGroupItem>
        </ToggleGroup>
        <div className="ml-auto flex items-center gap-1">
          {view === "problems" && open.length > 1 && !showHidden && (
            <Button
              variant="ghost"
              size="sm"
              className="text-muted-foreground"
              onClick={() => setHidden([...hidden, ...open.map((g) => g.key)])}
            >
              {t("logs.putAwayAll")}
            </Button>
          )}
          {view === "problems" && closed > 0 && (
            <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={() => setShowHidden(!showHidden)}>
              {showHidden ? t("logs.hidePutAway") : t("logs.showPutAway", { n: closed })}
            </Button>
          )}
          <CopyButton value={text} label={t("logs.copy")} />
        </div>
      </div>
      <ScrollArea ref={box} className="h-72 rounded-lg bg-muted">
        <div className="p-4 font-mono text-xs leading-relaxed">
          {view === "all"
            ? parsed.length
              ? parsed.map((l, i) => <Row key={i} line={l} time={parsed.some((x) => x.time)} mark={parsed.some(isProblem)} />)
              : empty
            : shown.length
              ? shown.map(({ key, line, count }) => {
                  const hint = hintFor(line.text, t)
                  const isAway = away.has(key)
                  return (
                    <div
                      key={key}
                      data-away={isAway}
                      className="grid gap-1.5 border-b py-3 first:pt-0 last:border-b-0 last:pb-0 data-[away=true]:opacity-60"
                    >
                      <Row line={line} time={timed} mark>
                        <span className="ml-auto flex shrink-0 items-start gap-1 font-sans">
                          {count > 1 && (
                            <Badge variant="outline" className="text-muted-foreground tabular-nums">
                              ×{count}
                            </Badge>
                          )}
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                variant="ghost"
                                size="icon-xs"
                                className="-my-0.5 text-muted-foreground"
                                aria-label={t(isAway ? "logs.bringBack" : "logs.putAway")}
                                onClick={() => {
                                  setHidden(isAway ? hidden.filter((h) => h !== key) : [...hidden, key])
                                  // The last hidden one came back: the view of hidden ones is over.
                                  if (isAway && closed === 1) setShowHidden(false)
                                }}
                              >
                                {isAway ? <Undo2Icon /> : <XIcon />}
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>{t(isAway ? "logs.bringBack" : "logs.putAway")}</TooltipContent>
                          </Tooltip>
                        </span>
                      </Row>
                      {hint && (
                        <div className={timed ? "pr-8 pl-[calc(8ch+2.125rem)]" : "pr-8 pl-6"}>
                          <p className="font-sans text-sm text-muted-foreground">{hint}</p>
                        </div>
                      )}
                    </div>
                  )
                })
              : t(closed > 0 ? "logs.allPutAway" : "logs.noProblems")}
        </div>
      </ScrollArea>
    </div>
  )
}
