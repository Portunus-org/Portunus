"use client"

import * as React from "react"

import { localeCodes, messages, type Locale, type MessageKey } from "@/lib/messages"

type Params = Record<string, string | number>
export type Translate = (key: MessageKey, params?: Params) => string

interface I18n {
  locale: Locale
  setLocale: (l: Locale) => void
  t: Translate
  /** Plural form of key for n ({n} is filled in). */
  tp: (key: MessageKey, n: number, params?: Params) => string
}

const STORAGE_KEY = "portunus.locale"

function detectLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved && (localeCodes as string[]).includes(saved)) return saved as Locale
  } catch {
    // storage unavailable
  }
  // The first of the browser's languages there is a file for.
  const wanted = typeof navigator === "undefined" ? [] : (navigator.languages ?? [navigator.language])
  for (const tag of wanted) {
    const code = tag.toLowerCase().split("-")[0]
    if ((localeCodes as string[]).includes(code)) return code as Locale
  }
  return "en"
}

// Which of a text's plural forms n takes, by the language's own rules
// (Intl.PluralRules): the forms are written in the order of CLDR's
// categories, as many as the language uses.
const categoryOrder = ["zero", "one", "two", "few", "many", "other"]
function pluralIndex(locale: Locale, n: number): number {
  const rules = new Intl.PluralRules(locale)
  const used = categoryOrder.filter((c) => rules.resolvedOptions().pluralCategories.includes(c as Intl.LDMLPluralRule))
  return Math.max(0, used.indexOf(rules.select(n)))
}

function fill(s: string, params?: Params) {
  return params ? s.replace(/\{(\w+)\}/g, (_, k) => (k in params ? String(params[k]) : `{${k}}`)) : s
}

const I18nContext = React.createContext<I18n | null>(null)

// Locale lives outside React so it can be read synchronously on the client.
let current: Locale = "en"
const listeners = new Set<() => void>()
function subscribe(fn: () => void) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}
let initialized = false
function getSnapshot(): Locale {
  if (!initialized) {
    initialized = true
    current = detectLocale()
  }
  return current
}

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const locale = React.useSyncExternalStore(subscribe, getSnapshot, () => "en" as Locale)

  React.useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  const value = React.useMemo<I18n>(() => {
    const dict = messages[locale]
    const t: Translate = (key, params) => fill(dict[key] ?? messages.en[key] ?? key, params)
    return {
      locale,
      setLocale: (l) => {
        current = l
        try {
          localStorage.setItem(STORAGE_KEY, l)
        } catch {
          // storage unavailable
        }
        listeners.forEach((fn) => fn())
      },
      t,
      tp: (key, n, params) => {
        const forms = (dict[key] ?? messages.en[key]).split("|")
        return fill(forms[Math.min(pluralIndex(locale, n), forms.length - 1)], { n, ...params })
      },
    }
  }, [locale])

  return <I18nContext value={value}>{children}</I18nContext>
}

export function useI18n() {
  const ctx = React.useContext(I18nContext)
  if (!ctx) throw new Error("useI18n must be used inside <I18nProvider>")
  return ctx
}
