// The words of the interface live in locales/<code>.json, one file per
// language, all with the same keys. en.json is the reference: it defines
// the keys, and every other language must have each of them (the type
// below refuses to build otherwise). {name} in a text is filled in when it
// is shown; plural forms are separated by "|" in the order of CLDR plural
// categories for that language (en: one|other, ru: one|few|many).
//
// A new language is a new locales/<code>.json and one line in `locales`.

import en from "@/locales/en.json"
import ru from "@/locales/ru.json"

export type MessageKey = keyof typeof en
export type Messages = Record<MessageKey, string>

export const locales = {
  en: { name: "English", messages: en satisfies Messages },
  ru: { name: "Русский", messages: ru satisfies Messages },
} as const

export type Locale = keyof typeof locales

export const messages = Object.fromEntries(Object.entries(locales).map(([code, l]) => [code, l.messages])) as Record<Locale, Messages>
export const localeNames = Object.fromEntries(Object.entries(locales).map(([code, l]) => [code, l.name])) as Record<Locale, string>
export const localeCodes = Object.keys(locales) as Locale[]
