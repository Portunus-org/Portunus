"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

import { LogoMark } from "@/components/brand/logo"
import { FormField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api, ApiError, setCsrfToken } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useFormErrors } from "@/lib/form-errors"
import { messages, type MessageKey } from "@/lib/messages"
import { setHandoffPassword } from "@/lib/session"

export default function LoginPage() {
  const { t } = useI18n()
  const router = useRouter()
  const [pending, setPending] = React.useState(false)
  const { errors, wrong, clear } = useFormErrors()
  // The provider to sign in through, when one is set up.
  const [sso, setSso] = React.useState<{ name: string } | null>(null)

  const [ssoReason, setSsoReason] = React.useState<MessageKey | null>(null)

  React.useEffect(() => {
    // Back from the provider without a session: say why, once.
    const reason = new URLSearchParams(window.location.search).get("sso")
    if (reason) window.history.replaceState(null, "", window.location.pathname)
    const key = `login.sso.${reason}` as MessageKey
    const why = reason ? (key in messages.en ? key : "login.sso.failed") : null
    api
      .authMethods()
      .then(
        (m) => setSso(m.sso ?? null),
        () => {}
      )
      .finally(() => why && setSsoReason(why))
  }, [])

  // Already signed in: skip the form.
  React.useEffect(() => {
    api.me().then(
      () => router.replace("/"),
      () => {}
    )
  }, [router])

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const password = String(f.get("password"))
    // A field left empty is said at it; a wrong name or password cannot be
    // told apart (on purpose), so that is said for the form as a whole.
    if (!String(f.get("username")).trim()) return wrong("username", t("error.required"))
    if (!password) return wrong("password", t("error.required"))
    setPending(true)
    clear()
    setSsoReason(null)
    try {
      const s = await api.login(String(f.get("username")), password)
      setCsrfToken(s.csrfToken)
      if (s.user.mustChangePassword) {
        setHandoffPassword(password)
        router.replace("/welcome/")
      } else {
        router.replace("/")
      }
    } catch (err) {
      if (err instanceof ApiError && err.code === "invalid_credentials") wrong("form", t("login.error.credentials"))
      else if (err instanceof ApiError && err.code === "rate_limited")
        wrong("form", t("login.error.rateLimited", { s: err.retryAfter ?? 30 }))
      else wrong("form", t("login.error.network"))
      setPending(false)
    }
  }

  return (
    // On a phone the form stands in the upper part of the screen, so that
    // the keyboard, which takes the lower part, covers neither field.
    <main className="flex min-h-dvh flex-col items-center bg-background p-6 pt-[12dvh] sm:justify-center sm:pt-6">
      <div className="flex w-full max-w-72 flex-col gap-10">
        <div className="flex flex-col items-center gap-5">
          <LogoMark className="size-9" strokeWidth={1.5} />
          <h1 className="font-serif text-3xl font-medium tracking-tight">Portunus</h1>
        </div>
        <form onSubmit={onSubmit} onInput={clear} className="grid gap-5" noValidate>
          <FormField id="username" error={errors.username} label={t("login.username")}>
            <Input id="username" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} required autoFocus />
          </FormField>
          <FormField id="password" error={errors.password} label={t("login.password")}>
            <Input id="password" name="password" type="password" autoComplete="current-password" required />
          </FormField>
          <FormError>{errors.form ?? (ssoReason && t(ssoReason))}</FormError>
          <Button type="submit" className="mt-2 w-full" disabled={pending}>
            {pending ? t("login.pending") : t("login.submit")}
          </Button>
          {sso && (
            // A plain link: the trip to the provider is a navigation, not a request.
            <Button asChild variant="outline" className="w-full">
              <a href="/api/auth/sso/start">{t("login.sso", { name: sso.name })}</a>
            </Button>
          )}
        </form>
      </div>
    </main>
  )
}
