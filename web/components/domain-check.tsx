"use client"

import * as React from "react"

import { Note } from "@/components/wizard"
import { api, type DomainCheck } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

export type DomainState = { state: "idle" } | { state: "checking" } | { state: "invalid" } | { state: "done"; check: DomainCheck }

// How often a domain that does not point here yet is looked up again.
const RECHECK_MS = 10_000

/**
 * Checks where a domain points while the user types (debounced), and keeps
 * checking afterwards: a record changed at the registrar has to show up
 * here by itself. An answer that the domain points elsewhere is looked up
 * again every few seconds, and any answer is refreshed when the tab is
 * returned to, so that a page left open never shows yesterday's result.
 */
export function useDomainCheck(domain: string, initial?: DomainCheck): DomainState {
  const d = domain.trim().toLowerCase()
  const [result, setResult] = React.useState<{
    domain: string
    value: DomainState
  }>(() =>
    initial && initial.domain === d ? { domain: d, value: { state: "done", check: initial } } : { domain: "", value: { state: "idle" } }
  )

  React.useEffect(() => {
    if (!d.includes(".") || result.domain === d) return
    const timer = setTimeout(() => {
      api.checkDomain(d).then(
        (check) => setResult({ domain: d, value: { state: "done", check } }),
        () => setResult({ domain: d, value: { state: "invalid" } })
      )
    }, 600)
    return () => clearTimeout(timer)
  }, [d, result.domain])

  const settled = result.domain === d && result.value.state === "done"
  const pointsHere = settled && result.value.state === "done" && result.value.check.pointsHere
  React.useEffect(() => {
    if (!settled) return
    let alive = true
    // The previous answer stays on screen until the new one is in.
    const again = () => {
      if (document.hidden) return
      api.checkDomain(d).then(
        (check) => alive && setResult((r) => (r.domain === d ? { domain: d, value: { state: "done", check } } : r)),
        () => {}
      )
    }
    const timer = pointsHere ? undefined : setInterval(again, RECHECK_MS)
    const onShow = () => !document.hidden && again()
    document.addEventListener("visibilitychange", onShow)
    window.addEventListener("focus", again)
    return () => {
      alive = false
      clearInterval(timer)
      document.removeEventListener("visibilitychange", onShow)
      window.removeEventListener("focus", again)
    }
  }, [d, settled, pointsHere])

  if (!d) return { state: "idle" }
  if (!d.includes(".")) return { state: "invalid" }
  return result.domain === d ? result.value : { state: "checking" }
}

export function DomainStatus({ domain, status, ip }: { domain: string; status: DomainState; ip: string }) {
  const { t } = useI18n()
  const d = domain.trim()
  switch (status.state) {
    case "idle":
      return null
    case "checking":
      return <Note tone="pending">{t("dns.checking", { domain: d })}</Note>
    case "invalid":
      return <Note tone="error">{t("error.invalid_hostname")}</Note>
    case "done": {
      const c = status.check
      if (c.pointsHere) return <Note tone="ok">{t("dns.ok", { domain: d })}</Note>
      if (c.addresses.length > 0)
        return (
          <Note tone="warn">
            {t("dns.elsewhere", {
              domain: d,
              addresses: c.addresses.join(", "),
              ip,
            })}
          </Note>
        )
      return <Note tone="warn">{t("dns.none", { domain: d, ip })}</Note>
    }
  }
}
