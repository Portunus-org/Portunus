"use client"

import * as React from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"

/** The colors a chain line may take on the map and in the chains list. */
export const CHAIN_COLORS = ["#0ea5e9", "#8b5cf6", "#10b981", "#f59e0b", "#f43f5e", "#84cc16"]

const COLOR_KEYS: Record<string, MessageKey> = {
  "": "chains.color.default",
  "#0ea5e9": "chains.color.sky",
  "#8b5cf6": "chains.color.violet",
  "#10b981": "chains.color.emerald",
  "#f59e0b": "chains.color.amber",
  "#f43f5e": "chains.color.rose",
  "#84cc16": "chains.color.lime",
}

function ColorDot({ color }: { color: string }) {
  return (
    <span
      className="size-2.5 shrink-0 rounded-full bg-foreground"
      style={color ? { backgroundColor: color } : undefined}
    />
  )
}

/**
 * Native shadcn Select for choosing a chain's display color in the editor toolbar.
 */
export function ChainColorSelect({
  value,
  onPick,
  className,
}: {
  value: string
  onPick: (color: string) => void
  className?: string
}) {
  const { t } = useI18n()
  return (
    <Select value={value || "default"} onValueChange={(v) => onPick(v === "default" ? "" : v)}>
      <SelectTrigger size="sm" className={className ?? "w-full sm:w-fit sm:min-w-36"} aria-label={t("chains.color")}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {["", ...CHAIN_COLORS].map((color) => (
            <SelectItem key={color || "default"} value={color || "default"}>
              <ColorDot color={color} />
              <span>{t(COLOR_KEYS[color] ?? "chains.color.default")}</span>
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

/**
 * Compact shadcn DropdownMenu color picker for a chain row in DataTable.
 */
export function ChainColorPicker({
  value,
  label,
  onPick,
}: {
  value: string
  label: string
  defaultLabel?: string
  onPick: (color: string) => void
}) {
  const { t } = useI18n()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className="shrink-0 rounded-full"
          aria-label={label}
          title={label}
          onClick={(e) => e.stopPropagation()}
        >
          <ColorDot color={value} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-44" onClick={(e) => e.stopPropagation()}>
        <DropdownMenuRadioGroup value={value || "default"} onValueChange={(v) => onPick(v === "default" ? "" : v)}>
          {["", ...CHAIN_COLORS].map((color) => (
            <DropdownMenuRadioItem key={color || "default"} value={color || "default"}>
              <ColorDot color={color} />
              <span>{t(COLOR_KEYS[color] ?? "chains.color.default")}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
