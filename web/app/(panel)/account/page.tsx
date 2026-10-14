"use client"

import * as React from "react"
import { toast } from "@/lib/toast"

import { FormField, FormError } from "@/components/form-field"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { api, ApiError } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useFormErrors } from "@/lib/form-errors"
import { useSession } from "@/lib/session"

const MIN_LENGTH = 12

export default function AccountPage() {
  const { t } = useI18n()
  const { user, refresh } = useSession()
  const [pending, setPending] = React.useState(false)
  const { errors, wrong, clear, fail } = useFormErrors()

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const formEl = e.currentTarget
    const f = new FormData(formEl)
    const next = String(f.get("next"))
    if (!f.get("current")) return wrong("current", t("error.required"))
    if (next.length < MIN_LENGTH) return wrong("next", t("welcome.password.short"))
    if (next !== f.get("confirm")) return wrong("confirm", t("welcome.password.mismatch"))
    setPending(true)
    clear()
    try {
      await api.changePassword(String(f.get("current")), next)
      formEl.reset()
      toast.success(t("account.changed"))
      await refresh()
    } catch (err) {
      if (err instanceof ApiError && err.code === "current_password_invalid") wrong("current", t("welcome.password.wrongCurrent"))
      else fail(err, ["current", "next", "confirm"])
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
            <span className="font-medium">{user.displayName || user.username}</span>
            <span className="text-sm text-muted-foreground">
              {user.displayName ? `${user.username}, ` : ""}
              {t(`role.${user.role}`)}
            </span>
          </div>
        </CardContent>
      </Card>
      {user.sso ? (
        <Card>
          <CardHeader>
            <CardTitle>{t("account.password")}</CardTitle>
            <CardDescription>{t("account.ssoHint")}</CardDescription>
          </CardHeader>
        </Card>
      ) : (
        <Card>
          <form onSubmit={onSubmit} className="contents" noValidate>
            <CardHeader>
              <CardTitle>{t("account.password")}</CardTitle>
              <CardDescription>{t("account.passwordHint")}</CardDescription>
            </CardHeader>
            <CardContent className="grid max-w-md gap-6">
              <FormField id="current" error={errors.current} label={t("account.current")}>
                <Input id="current" name="current" type="password" autoComplete="current-password" required />
              </FormField>
              <FormField id="next" error={errors.next} label={t("account.new")}>
                <Input id="next" name="next" type="password" autoComplete="new-password" minLength={MIN_LENGTH} required />
              </FormField>
              <FormField id="confirm" error={errors.confirm} label={t("account.repeat")}>
                <Input id="confirm" name="confirm" type="password" autoComplete="new-password" required />
              </FormField>
              <FormError>{errors.form}</FormError>
            </CardContent>
            <CardFooter className="border-t">
              <Button type="submit" size="sm" disabled={pending}>
                {pending ? t("common.saving") : t("account.change")}
              </Button>
            </CardFooter>
          </form>
        </Card>
      )}
    </div>
  )
}
