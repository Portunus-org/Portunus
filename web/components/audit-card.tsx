"use client"

import * as React from "react"
import { CircleXIcon, SearchIcon } from "lucide-react"

import { columnHelper, DataTable } from "@/components/data-table"
import { Pane, PaneDescription, PaneHeader, PaneTitle } from "@/components/settings-pane"
import { Input } from "@/components/ui/input"
import { api, type AuditEntry } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { messages, type Locale, type MessageKey } from "@/lib/messages"
import { useResource } from "@/lib/use-resource"

const col = columnHelper<AuditEntry>()

// Sign-ins that did not go through: shown marked, and a view of their own.
const failures = new Set(["auth.login_failed", "auth.sso_failed", "auth.sso_denied"])

type View = "all" | "signins" | "changes" | "failures"
const inView: Record<View, (a: AuditEntry) => boolean> = {
  all: () => true,
  signins: (a) => a.action.startsWith("auth."),
  changes: (a) => !a.action.startsWith("auth."),
  failures: (a) => failures.has(a.action),
}

// The date with the year only when it is not this one, and the time to the second.
function when(iso: string, locale: Locale) {
  const d = new Date(iso)
  return new Intl.DateTimeFormat(locale, {
    year: d.getFullYear() === new Date().getFullYear() ? undefined : "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(d)
}

/**
 * The audit log: who signed in, and who changed what, when and from where,
 * as the panel's other tables show their rows. The latest entries come from
 * the server; sorting, the views and the search work on them here.
 */
export function AuditCard() {
  const { t, locale } = useI18n()
  const { data, error } = useResource(api.audit, 15_000)
  const [view, setView] = React.useState<View>("all")
  const [query, setQuery] = React.useState("")

  const event = React.useCallback(
    (action: string) => {
      const key = `audit.action.${action}` as MessageKey
      return key in messages.en ? t(key) : action
    },
    [t]
  )
  const actor = React.useCallback((a: AuditEntry) => (a.actor === "cli" ? t("audit.console") : a.actor), [t])
  // The details in words: the name of each one, and its value (yes or no,
  // a role by its name); a detail the panel has no name for shows as it is.
  const details = React.useCallback(
    (a: AuditEntry) =>
      Object.entries(a.details ?? {})
        .map(([k, v]) => {
          const name = `audit.detail.${k}` as MessageKey
          const role = `role.${v}` as MessageKey
          const value =
            v === true ? t("audit.yes") : v === false ? t("audit.no") : k === "role" && role in messages.en ? t(role) : String(v ?? "")
          return `${name in messages.en ? t(name) : k}: ${value}`
        })
        .join(", "),
    [t]
  )

  const entries = React.useMemo(() => data?.entries ?? [], [data])
  const shown = React.useMemo(() => {
    const q = query.trim().toLowerCase()
    return entries.filter(
      (a) => inView[view](a) && (!q || [event(a.action), a.target, actor(a), a.ip].some((s) => s.toLowerCase().includes(q)))
    )
  }, [entries, view, query, event, actor])

  const columns = React.useMemo(
    () =>
      col.columns([
        col.accessor((a) => event(a.action), {
          id: "event",
          header: t("audit.col.event"),
          enableHiding: false,
          // A sign-in that failed is marked by an icon before its words.
          cell: ({ row, getValue }) => (
            <span className="flex min-w-40 items-center gap-1.5 font-medium whitespace-normal">
              {failures.has(row.original.action) && (
                <CircleXIcon className="size-4 shrink-0 fill-red-500 text-background dark:fill-red-400" />
              )}
              {getValue()}
            </span>
          ),
        }),
        col.accessor("target", {
          header: t("audit.col.target"),
          cell: ({ getValue }) => <span className="block max-w-48 break-words whitespace-normal">{getValue()}</span>,
        }),
        col.accessor(actor, {
          id: "actor",
          header: t("audit.col.actor"),
          cell: ({ getValue }) => <span className="text-muted-foreground">{getValue()}</span>,
        }),
        col.accessor((a) => new Date(a.at).getTime(), {
          id: "at",
          header: t("audit.col.at"),
          cell: ({ row }) => <span className="whitespace-nowrap text-muted-foreground tabular-nums">{when(row.original.at, locale)}</span>,
        }),
        col.accessor("ip", {
          header: t("audit.col.ip"),
          cell: ({ getValue }) => <span className="font-mono text-xs text-muted-foreground">{getValue()}</span>,
        }),
        col.accessor(details, {
          id: "details",
          header: t("audit.col.details"),
          enableSorting: false,
          // Long details wrap rather than push the table out of its pane.
          cell: ({ getValue }) => <span className="block max-w-56 break-words whitespace-normal text-muted-foreground">{getValue()}</span>,
        }),
      ]),
    [t, locale, event, actor, details]
  )

  const count = (v: View) => entries.filter(inView[v]).length
  return (
    <Pane>
      <PaneHeader>
        <PaneTitle>{t("audit.title")}</PaneTitle>
        <PaneDescription>{t("audit.description")}</PaneDescription>
      </PaneHeader>
      <div className="grid gap-3" data-setting="audit-table">
        <DataTable
          inset={false}
          data={shown}
          columns={columns}
          getRowId={(a) => String(a.id)}
          loading={!data && !error}
          views={(["all", "signins", "changes", "failures"] as const).map((v) => ({
            value: v,
            label: t(`audit.view.${v}`),
            count: v === "all" ? undefined : count(v),
          }))}
          view={view}
          onViewChange={(v) => setView(v as View)}
          initialSorting={[{ id: "at", desc: true }]}
          pageSize={10}
          initialHidden={["details"]}
          columnLabels={{
            target: t("audit.col.target"),
            actor: t("audit.col.actor"),
            at: t("audit.col.at"),
            ip: t("audit.col.ip"),
            details: t("audit.col.details"),
          }}
          empty={t("audit.empty")}
          toolbar={
            <div className="relative">
              <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t("common.search")}
                aria-label={t("common.search")}
                className="h-7 w-32 pl-8 text-sm lg:w-48"
              />
            </div>
          }
        />
        {data && data.entries.length >= data.limit && (
          <p className="text-sm text-muted-foreground">{t("audit.limited", { n: data.limit })}</p>
        )}
      </div>
    </Pane>
  )
}
