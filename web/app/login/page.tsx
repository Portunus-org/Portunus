"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

import { LogoMark } from "@/components/brand/logo"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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
    api.me().then(() => router.replace("/"), () => {})
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
    <main className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center gap-2 self-center font-medium">
          <LogoMark />
          Portunus
        </div>
        <Card>
          <CardHeader className="text-center">
            <CardTitle className="text-xl">{t("login.title")}</CardTitle>
            <CardDescription>{t("login.subtitle")}</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={onSubmit} className="grid gap-6" noValidate>
              <div className="grid gap-3">
                <Label htmlFor="username">{t("login.username")}</Label>
                <Input id="username" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} required autoFocus />
              </div>
              <div className="grid gap-3">
                <Label htmlFor="password">{t("login.password")}</Label>
                <Input id="password" name="password" type="password" autoComplete="current-password" required />
              </div>
              {error && <p className="text-sm text-destructive">{error}</p>}
              <Button type="submit" className="w-full" disabled={pending}>
                {pending ? t("login.pending") : t("login.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </main>
  )
}
