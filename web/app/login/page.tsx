"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

import { LogoMark } from "@/components/brand/logo"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, ApiError, setCsrfToken } from "@/lib/api"

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case "invalid_credentials":
        return "Incorrect username or password."
      case "rate_limited":
        return `Too many attempts. Try again in ${err.retryAfter ?? 30}s.`
    }
  }
  return "Could not reach the panel. Check that it is running."
}

export default function LoginPage() {
  const router = useRouter()
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)

  // Already signed in: skip the form.
  React.useEffect(() => {
    api.me().then(() => router.replace("/"), () => {})
  }, [router])

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    setPending(true)
    setError(null)
    try {
      const s = await api.login(String(form.get("username")), String(form.get("password")))
      setCsrfToken(s.csrfToken)
      router.replace(s.user.mustChangePassword ? "/account/" : "/")
    } catch (err) {
      setError(errorMessage(err))
      setPending(false)
    }
  }

  return (
    <main className="flex min-h-svh items-center justify-center px-6">
      <div className="w-full max-w-xs">
        <div className="mb-10 flex flex-col items-center gap-4 text-center">
          <LogoMark className="size-10" />
          <h1 className="font-display text-3xl">Portunus</h1>
        </div>

        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <div className="grid gap-1.5">
            <Label htmlFor="username">Username</Label>
            <Input id="username" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} required autoFocus />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="password">Password</Label>
            <Input id="password" name="password" type="password" autoComplete="current-password" required />
          </div>
          {error && (
            <p role="alert" className="text-sm text-signal">
              {error}
            </p>
          )}
          <Button type="submit" className="mt-2 w-full" disabled={pending}>
            {pending ? "Signing in…" : "Sign in"}
          </Button>
        </form>
      </div>
    </main>
  )
}
