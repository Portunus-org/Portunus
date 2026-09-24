"use client"

import * as React from "react"
import { toast } from "sonner"

import { PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, ApiError } from "@/lib/api"
import { useSession } from "@/lib/session"

const MIN_LENGTH = 12

function PasswordForm() {
  const { refresh } = useSession()
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const formEl = e.currentTarget
    const form = new FormData(formEl)
    const current = String(form.get("current"))
    const next = String(form.get("next"))
    if (next.length < MIN_LENGTH) {
      setError(`Use at least ${MIN_LENGTH} characters.`)
      return
    }
    if (next !== form.get("confirm")) {
      setError("The new passwords do not match.")
      return
    }
    setPending(true)
    setError(null)
    try {
      await api.changePassword(current, next)
      formEl.reset()
      toast.success("Password changed. Other sessions were signed out.")
      await refresh()
    } catch (err) {
      setError(
        err instanceof ApiError && err.code === "current_password_invalid"
          ? "The current password is incorrect."
          : err instanceof ApiError && err.code === "password_too_weak"
            ? `Use ${MIN_LENGTH}–256 characters.`
            : "Could not change the password."
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <form onSubmit={onSubmit} className="grid max-w-sm gap-4" noValidate>
      <div className="grid gap-1.5">
        <Label htmlFor="current">Current password</Label>
        <Input id="current" name="current" type="password" autoComplete="current-password" required />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="next">New password</Label>
        <Input id="next" name="next" type="password" autoComplete="new-password" minLength={MIN_LENGTH} required />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="confirm">Repeat new password</Label>
        <Input id="confirm" name="confirm" type="password" autoComplete="new-password" required />
      </div>
      {error && (
        <p role="alert" className="text-sm text-signal">
          {error}
        </p>
      )}
      <div>
        <Button type="submit" disabled={pending}>
          {pending ? "Saving…" : "Change password"}
        </Button>
      </div>
    </form>
  )
}

export default function AccountPage() {
  const { user } = useSession()

  return (
    <>
      <PageHeader title="Account" />

      {user.mustChangePassword && (
        <div role="alert" className="mx-4 flex gap-3 rounded-lg border border-signal/40 px-4 py-3 text-sm lg:mx-6">
          <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-signal" />
          <p>You are using a generated password. Set your own before continuing.</p>
        </div>
      )}

      <div className="grid gap-3 px-4 lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>Profile</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid max-w-sm grid-cols-[8rem_1fr] gap-y-2 text-sm">
              <dt className="text-muted-foreground">Username</dt>
              <dd className="font-mono">{user.username}</dd>
              <dt className="text-muted-foreground">Role</dt>
              <dd>{user.role}</dd>
            </dl>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Password</CardTitle>
            <CardDescription>Changing it signs out every other session.</CardDescription>
          </CardHeader>
          <CardContent>
            <PasswordForm />
          </CardContent>
        </Card>
      </div>
    </>
  )
}
