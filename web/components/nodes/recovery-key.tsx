"use client"

import * as React from "react"
import Link from "next/link"

import { CopyButton } from "@/components/copy-button"
import { Skeleton } from "@/components/ui/skeleton"
import { Note } from "@/components/wizard"
import { api, errorText, type Node } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { useI18n } from "@/lib/i18n"

/**
 * For a node that keeps a copy of the panel: the key that opens the copy
 * and when the node last received one.
 */
export function RecoveryKey({ node }: { node: Node }) {
  const { t, locale } = useI18n()
  const [key, setKey] = React.useState<string | null>(null)
  const [error, setError] = React.useState<string | null>(null)

  React.useEffect(() => {
    api.recovery().then(
      (r) => setKey(r.key),
      (err) => setError(errorText(err, t))
    )
  }, [t])

  if (error) return <Note tone="error">{error}</Note>
  return (
    <div className="grid gap-2 text-sm">
      <span className="font-medium">{t("nodes.recovery.title")}</span>
      <span className="text-muted-foreground">{t("nodes.recovery.hint")}</span>
      <div className="relative">
        {key ? (
          <pre className="rounded-lg bg-muted p-4 pr-12 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">{key}</pre>
        ) : (
          <Skeleton className="h-12 w-full rounded-lg" />
        )}
        {key && <CopyButton value={key} className="absolute top-2 right-2 text-muted-foreground" />}
      </div>
      <span className="text-muted-foreground">
        {node.status.copyAt ? t("nodes.recovery.copyAt", { date: formatDateTime(node.status.copyAt, locale) }) : t("nodes.recovery.noCopy")}{" "}
        <Link href="/docs/nodes/" className="text-foreground underline underline-offset-4">
          {t("nodes.recovery.how")}
        </Link>
      </span>
    </div>
  )
}
