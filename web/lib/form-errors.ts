"use client"

import * as React from "react"

import { ApiError, errorText } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

/**
 * The errors of the form around, for a form whose fields are given to it
 * from outside (a section of the settings): a FormField with no error of
 * its own shows the one under its id.
 */
export const FormErrorsContext = React.createContext<Record<string, string>>({})

// Refusals that are about one field although the server does not name it.
const fieldOfCode: Record<string, string> = {
  current_password_invalid: "current",
  password_too_weak: "next",
}

/**
 * What is wrong with a form, said at the field it is about: the field is
 * outlined in red and the words are right under it. Only what belongs to
 * no field of the form is said at its foot ("form").
 *
 * `fail` takes a refusal of the server, which names the field it is about;
 * `fields` are the fields this form shows, by the names the server uses (or
 * the form itself, whose controls are looked up by id), and `as` renames
 * those the form calls otherwise. It returns the field it
 * put the words at, so a wizard can go back to the step that has it.
 */
export function useFormErrors() {
  const { t } = useI18n()
  const [errors, setErrors] = React.useState<Record<string, string>>({})

  const wrong = React.useCallback((field: string, message: string) => setErrors({ [field]: message }), [])
  const clear = React.useCallback(() => setErrors((e) => (Object.keys(e).length ? {} : e)), [])
  const fail = React.useCallback(
    (err: unknown, fields: readonly string[] | HTMLElement = [], as: Record<string, string> = {}) => {
      const named = err instanceof ApiError ? (err.field ?? fieldOfCode[err.code]) : undefined
      const field = named ? (as[named] ?? named) : undefined
      const shown = (f: string) =>
        Array.isArray(fields) ? fields.includes(f) : !!(fields as HTMLElement).querySelector(`#${CSS.escape(f)}`)
      const at = field && shown(field) ? field : "form"
      setErrors({ [at]: errorText(err, t) })
      return at
    },
    [t]
  )
  // The number fields of a form say what is wrong with them before it is
  // sent: something that is not a number (a browser that lets anything be
  // typed sends it as empty, which would read as "no limit"), a number out
  // of the field's range, or a fraction where a whole number is meant.
  const numbers = React.useCallback(
    (form: HTMLFormElement) => {
      for (const el of form.querySelectorAll<HTMLInputElement>('input[type="number"]')) {
        const v = el.validity
        const message = v.badInput
          ? t("error.not_a_number")
          : v.rangeUnderflow || v.rangeOverflow
            ? el.max
              ? t("error.number_range", { min: el.min, max: el.max })
              : t("error.number_min", { min: el.min })
            : v.stepMismatch
              ? t("error.whole_number")
              : ""
        if (message) {
          setErrors({ [el.id]: message })
          el.focus()
          return false
        }
      }
      return true
    },
    [t]
  )
  return { errors, wrong, clear, fail, numbers }
}
