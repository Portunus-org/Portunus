"use client"

import * as React from "react"

import { Note } from "@/components/wizard"
import { api, type DomainCheck } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

export type DomainState = { state: "idle" } | { state: "checking" } | { state: "invalid" } | { state: "done"; check: DomainCheck }

/** Checks where a domain points while the user types (debounced). */
export function useDomainCheck(domain: string, initial?: DomainCheck): DomainState {
  const d = domain.trim().toLowerCase()
  const [result, setResult] = React.useState<{ domain: string; value: DomainState }>(() =>
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
        return <Note tone="warn">{t("dns.elsewhere", { domain: d, addresses: c.addresses.join(", "), ip })}</Note>
      return <Note tone="warn">{t("dns.none", { domain: d, ip })}</Note>
    }
  }
}
