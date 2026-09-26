"use client"

import { StatusBadge, type Tone } from "@/components/status"
import type { ClientStatus as Status } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

const tone: Record<Status, Tone> = { active: "ok", disabled: "off", expired: "error", limited: "error" }

export function ClientStatus({ status }: { status: Status }) {
  const { t } = useI18n()
  return <StatusBadge tone={tone[status]}>{t(`clients.status.${status}`)}</StatusBadge>
}
