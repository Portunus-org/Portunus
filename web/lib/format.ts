import type { Locale } from "@/lib/messages"

const units: Record<Locale, string[]> = {
  en: ["B", "KB", "MB", "GB", "TB"],
  ru: ["Б", "КБ", "МБ", "ГБ", "ТБ"],
}

export function formatBytes(bytes: number, locale: Locale = "en"): string {
  const u = units[locale]
  if (!Number.isFinite(bytes) || bytes <= 0) return `0 ${u[0]}`
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), u.length - 1)
  const v = bytes / 1024 ** i
  const num = new Intl.NumberFormat(locale, { maximumFractionDigits: v >= 100 || i === 0 ? 0 : 1 }).format(v)
  return `${num} ${u[i]}`
}

export function formatDuration(seconds: number, locale: Locale = "en"): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const [dd, hh, mm, ss] = locale === "ru" ? ["д", "ч", "мин", "с"] : ["d", "h", "m", "s"]
  if (d > 0) return `${d} ${dd} ${h} ${hh}`
  if (h > 0) return `${h} ${hh} ${m} ${mm}`
  if (m > 0) return `${m} ${mm}`
  return `${Math.floor(seconds)} ${ss}`
}

export function formatDate(iso: string | null | undefined, locale: Locale = "en"): string {
  return iso ? new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", day: "numeric" }).format(new Date(iso)) : ""
}

/** Days until iso (negative when in the past). */
export function daysUntil(iso: string): number {
  return Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000)
}

export const GiB = 1024 ** 3

/** yyyy-mm-dd for an <input type="date"> from an ISO timestamp. */
export function toDateInput(iso: string | null): string {
  if (!iso) return ""
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** End of the chosen local day, as ISO, or null for an empty input. */
export function fromDateInput(value: string): string | null {
  if (!value) return null
  const [y, m, d] = value.split("-").map(Number)
  return new Date(y, m - 1, d, 23, 59, 59).toISOString()
}
