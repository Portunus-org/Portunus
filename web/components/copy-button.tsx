"use client"

import * as React from "react"
import { CheckIcon, CopyIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useI18n } from "@/lib/i18n"

/**
 * Puts text on the clipboard. The browser's clipboard exists only on pages
 * it calls secure (HTTPS, or this very machine); a panel opened by a plain
 * address on port 2053 is not one, and there the old way is used: a hidden
 * field, selected and copied.
 */
async function copyText(value: string) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return true
    } catch {
      // Refused: try the old way.
    }
  }
  const field = document.createElement("textarea")
  field.value = value
  field.setAttribute("readonly", "")
  field.style.position = "fixed"
  field.style.opacity = "0"
  document.body.appendChild(field)
  field.select()
  let done = false
  try {
    done = document.execCommand("copy")
  } catch {
    done = false
  }
  field.remove()
  return done
}

export function CopyButton({ value, label, className }: { value: string; label?: string; className?: string }) {
  const { t } = useI18n()
  const [copied, setCopied] = React.useState(false)
  React.useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1500)
    return () => clearTimeout(timer)
  }, [copied])

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      className={className ?? "text-muted-foreground"}
      onClick={() => copyText(value).then((done) => done && setCopied(true))}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
      <span className="sr-only">{copied ? t("common.copied") : (label ?? t("common.copy"))}</span>
    </Button>
  )
}
