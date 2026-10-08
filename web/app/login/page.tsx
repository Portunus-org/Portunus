"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

import { LogoMark } from "@/components/brand/logo"
import { FormField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api, ApiError, setCsrfToken } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { setHandoffPassword } from "@/lib/session"

export default function LoginPage() {
  const { t } = useI18n()
  const router = useRouter()
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)

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
    setPending(true)
    setError(null)
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
      if (err instanceof ApiError && err.code === "invalid_credentials") setError(t("login.error.credentials"))
      else if (err instanceof ApiError && err.code === "rate_limited") setError(t("login.error.rateLimited", { s: err.retryAfter ?? 30 }))
      else setError(t("login.error.network"))
      setPending(false)
    }
  }

  return (
    <main className="flex min-h-svh flex-col items-center justify-center bg-background p-6">
      <div className="flex w-full max-w-72 flex-col gap-10">
        <div className="flex flex-col items-center gap-5">
          <LogoMark className="size-9" strokeWidth={1.5} />
          <h1 className="font-serif text-3xl font-medium tracking-tight">Portunus</h1>
        </div>
        <form onSubmit={onSubmit} className="grid gap-5" noValidate>
          <FormField id="username" label={t("login.username")}>
            <Input id="username" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} required autoFocus />
          </FormField>
          <FormField id="password" label={t("login.password")}>
            <Input id="password" name="password" type="password" autoComplete="current-password" required />
          </FormField>
          <FormError>{error}</FormError>
          <Button type="submit" className="mt-2 w-full" disabled={pending}>
            {pending ? t("login.pending") : t("login.submit")}
          </Button>
        </form>
      </div>
    </main>
  )
}
