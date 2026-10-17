"use client"

import * as React from "react"
import { CheckIcon, CopyIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "@/components/ui/input-group"
import { cn } from "@/lib/utils"
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

function useCopy(value: string) {
  const [copied, setCopied] = React.useState(false)
  React.useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1500)
    return () => clearTimeout(timer)
  }, [copied])
  return { copied, copy: () => copyText(value).then((done) => done && setCopied(true)) }
}

// The button stays where it is when pressed: only the icon changes, to a
// check mark, and back.
const still = "active:not-aria-[haspopup]:translate-y-0"

// The two icons lie on top of each other and cross-fade, the one coming in
// growing slightly from smaller.
function CopyIcons({ copied }: { copied: boolean }) {
  const icon = "col-start-1 row-start-1 transition-[opacity,scale] duration-200 ease-out motion-reduce:transition-none"
  return (
    <span className="grid place-items-center">
      <CopyIcon className={cn(icon, copied ? "scale-75 opacity-0" : "scale-100 opacity-100")} />
      <CheckIcon className={cn(icon, copied ? "scale-100 opacity-100" : "scale-75 opacity-0")} />
    </span>
  )
}

export function CopyButton({ value, label, className }: { value: string; label?: string; className?: string }) {
  const { t } = useI18n()
  const { copied, copy } = useCopy(value)
  const name = copied ? t("common.copied") : (label ?? t("common.copy"))
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      className={cn(still, className ?? "text-muted-foreground")}
      title={name}
      aria-label={name}
      onClick={copy}
    >
      <CopyIcons copied={copied} />
    </Button>
  )
}

/** A read-only field with a copy button at its end, as shadcn's input group shows it. */
export function CopyField({ id, value, className }: { id?: string; value: string; className?: string }) {
  const { t } = useI18n()
  const { copied, copy } = useCopy(value)
  const name = copied ? t("common.copied") : t("common.copy")
  return (
    <InputGroup className={className}>
      <InputGroupInput id={id} readOnly value={value} className="font-mono text-xs" />
      <InputGroupAddon align="inline-end">
        <InputGroupButton size="icon-xs" className={cn(still, "text-muted-foreground")} title={name} aria-label={name} onClick={copy}>
          <CopyIcons copied={copied} />
        </InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  )
}
