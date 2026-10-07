"use client"

import * as React from "react"
import { toast } from "sonner"

import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, ApiError, errorText } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useSession } from "@/lib/session"

const MIN_LENGTH = 12

export default function AccountPage() {
  const { t } = useI18n()
  const { user, refresh } = useSession()
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const formEl = e.currentTarget
    const f = new FormData(formEl)
    const next = String(f.get("next"))
    if (next.length < MIN_LENGTH) return setError(t("welcome.password.short"))
    if (next !== f.get("confirm")) return setError(t("welcome.password.mismatch"))
    setPending(true)
    setError(null)
    try {
      await api.changePassword(String(f.get("current")), next)
      formEl.reset()
      toast.success(t("account.changed"))
      await refresh()
    } catch (err) {
      setError(err instanceof ApiError && err.code === "current_password_invalid" ? t("welcome.password.wrongCurrent") : errorText(err, t))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="grid gap-4 px-4 lg:px-6">
      <Card>
        <CardHeader>
          <CardTitle>{t("account.profile")}</CardTitle>
        </CardHeader>
        <CardContent className="flex items-center gap-4">
          <Avatar className="size-12">
            <AvatarFallback>{user.username.slice(0, 2).toUpperCase()}</AvatarFallback>
          </Avatar>
          <div className="grid">
            <span className="font-medium">{user.username}</span>
            <span className="text-sm text-muted-foreground">{t(`role.${user.role}`)}</span>
          </div>
        </CardContent>
      </Card>
      <Card>
        <form onSubmit={onSubmit} className="contents" noValidate>
          <CardHeader>
            <CardTitle>{t("account.password")}</CardTitle>
            <CardDescription>{t("account.passwordHint")}</CardDescription>
          </CardHeader>
          <CardContent className="grid max-w-md gap-6">
            <div className="grid gap-3">
              <Label htmlFor="current">{t("account.current")}</Label>
              <Input id="current" name="current" type="password" autoComplete="current-password" required />
            </div>
            <div className="grid gap-3">
              <Label htmlFor="next">{t("account.new")}</Label>
              <Input id="next" name="next" type="password" autoComplete="new-password" minLength={MIN_LENGTH} required />
            </div>
            <div className="grid gap-3">
              <Label htmlFor="confirm">{t("account.repeat")}</Label>
              <Input id="confirm" name="confirm" type="password" autoComplete="new-password" required />
            </div>
            {error && <p className="text-sm text-destructive">{error}</p>}
          </CardContent>
          <CardFooter className="border-t">
            <Button type="submit" size="sm" disabled={pending}>
              {pending ? t("common.saving") : t("account.change")}
            </Button>
          </CardFooter>
        </form>
      </Card>
    </div>
  )
}
