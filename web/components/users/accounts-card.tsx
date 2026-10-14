"use client"

import * as React from "react"
import { MoreVerticalIcon, PlusIcon } from "lucide-react"

import { useConfirm } from "@/components/confirm"
import { CopyButton } from "@/components/copy-button"
import { FormError, FormField } from "@/components/form-field"
import { StatusBadge } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Pane, PaneAction, PaneContent, PaneDescription, PaneHeader, PaneTitle } from "@/components/settings-pane"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { api, errorText, type Account, type Role } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { useFormErrors } from "@/lib/form-errors"
import { toast } from "@/lib/toast"
import { useResource } from "@/lib/use-resource"

const roles: Role[] = ["admin", "operator", "viewer"]

/** A password the panel made up, shown once for the administrator to pass on. */
function OneTimePassword({ secret, onClose }: { secret: { username: string; password: string } | null; onClose: () => void }) {
  const { t } = useI18n()
  return (
    <Dialog open={!!secret} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("users.password.title", { name: secret?.username ?? "" })}</DialogTitle>
          <DialogDescription>{t("users.password.hint")}</DialogDescription>
        </DialogHeader>
        <div className="relative">
          <pre className="rounded-lg bg-muted p-4 pr-12 font-mono text-sm break-all whitespace-pre-wrap">{secret?.password}</pre>
          <CopyButton value={secret?.password ?? ""} className="absolute top-2 right-2 text-muted-foreground" />
        </div>
        <DialogFooter>
          <Button onClick={onClose}>{t("common.close")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AddAccount({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (username: string, password: string) => void
}) {
  const { t } = useI18n()
  const [role, setRole] = React.useState<Role>("operator")
  const [pending, setPending] = React.useState(false)
  const { errors, clear, fail } = useFormErrors()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form
          onInput={clear}
          className="grid gap-6"
          noValidate
          onSubmit={async (e) => {
            e.preventDefault()
            const username = String(new FormData(e.currentTarget).get("username") ?? "").trim()
            setPending(true)
            clear()
            try {
              const made = await api.createAccount(username, role)
              onOpenChange(false)
              onCreated(made.user.username, made.password)
            } catch (err) {
              fail(err, ["username", "role"])
            } finally {
              setPending(false)
            }
          }}
        >
          <DialogHeader>
            <DialogTitle>{t("users.add.title")}</DialogTitle>
            <DialogDescription>{t("users.add.hint")}</DialogDescription>
          </DialogHeader>
          <FormField id="username" error={errors.username} label={t("login.username")}>
            <Input
              id="username"
              name="username"
              maxLength={64}
              autoCapitalize="none"
              spellCheck={false}
              autoComplete="off"
              required
              autoFocus
            />
          </FormField>
          <FormField id="role" error={errors.role} label={t("users.col.role")} hint={t(`users.role.${role}`)}>
            <Select value={role} onValueChange={(v) => setRole(v as Role)}>
              <SelectTrigger id="role" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {roles.map((r) => (
                    <SelectItem key={r} value={r}>
                      {t(`role.${r}`)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </FormField>
          <FormError>{errors.form}</FormError>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={pending}>
              {t("users.add.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** The accounts of the panel: who they are, how they sign in and what they may do. */
export function AccountsCard() {
  const { t, locale } = useI18n()
  const { data: accounts, reload } = useResource(api.accounts)
  const { data: methods } = useResource(api.authMethods)
  const [confirm, confirmDialog] = useConfirm()
  const [adding, setAdding] = React.useState(false)
  const [secret, setSecret] = React.useState<{ username: string; password: string } | null>(null)

  const act = async (what: () => Promise<unknown>, done: string) => {
    try {
      await what()
      toast.success(done)
      reload()
    } catch (err) {
      toast.error(errorText(err, t))
    }
  }

  return (
    <Pane>
      {confirmDialog}
      <AddAccount
        open={adding}
        onOpenChange={setAdding}
        onCreated={(username, password) => {
          setSecret({ username, password })
          reload()
        }}
      />
      <OneTimePassword secret={secret} onClose={() => setSecret(null)} />
      <PaneHeader>
        <PaneTitle>{t("users.title")}</PaneTitle>
        <PaneDescription>{t("users.description")}</PaneDescription>
        <PaneAction>
          <Button variant="outline" size="sm" onClick={() => setAdding(true)} data-setting="accounts-add">
            <PlusIcon />
            {t("users.add")}
          </Button>
        </PaneAction>
      </PaneHeader>
      <PaneContent>
        <div className="overflow-hidden rounded-lg border" data-setting="accounts-table">
          <Table>
            <TableHeader className="bg-muted">
              <TableRow>
                <TableHead>{t("users.col.name")}</TableHead>
                <TableHead>{t("users.col.signIn")}</TableHead>
                <TableHead>{t("users.col.role")}</TableHead>
                <TableHead>{t("users.col.lastLogin")}</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(accounts ?? []).map((a: Account) => (
                <TableRow key={a.id} data-disabled={a.disabled} className="data-[disabled=true]:text-muted-foreground">
                  <TableCell>
                    <div className="grid">
                      <span className="font-medium">
                        {a.username}
                        {a.self && <span className="ml-2 font-normal text-muted-foreground">{t("users.you")}</span>}
                      </span>
                      {a.displayName && <span className="text-muted-foreground">{a.displayName}</span>}
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {a.sso ? (methods?.sso?.name ?? t("users.signIn.sso")) : t("users.signIn.password")}
                  </TableCell>
                  <TableCell>{a.disabled ? <StatusBadge tone="off">{t("users.disabled")}</StatusBadge> : t(`role.${a.role}`)}</TableCell>
                  <TableCell className="text-muted-foreground tabular-nums">
                    {a.lastLoginAt ? formatDateTime(a.lastLoginAt, locale) : t("users.never")}
                  </TableCell>
                  <TableCell>
                    {!a.self && (
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon-sm" className="text-muted-foreground">
                            <MoreVerticalIcon />
                            <span className="sr-only">{t("users.actions")}</span>
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-56">
                          <DropdownMenuLabel>{t("users.col.role")}</DropdownMenuLabel>
                          <DropdownMenuRadioGroup
                            value={a.role}
                            onValueChange={(role) =>
                              act(() => api.updateAccount(a.id, { role: role as Role, disabled: a.disabled }), t("users.toast.role"))
                            }
                          >
                            {roles.map((r) => (
                              <DropdownMenuRadioItem key={r} value={r}>
                                {t(`role.${r}`)}
                              </DropdownMenuRadioItem>
                            ))}
                          </DropdownMenuRadioGroup>
                          <DropdownMenuSeparator />
                          {!a.sso && (
                            <DropdownMenuItem
                              onSelect={() =>
                                confirm({
                                  title: t("users.reset.title", { name: a.username }),
                                  description: t("users.reset.description"),
                                  action: t("users.reset"),
                                  onConfirm: async () => {
                                    try {
                                      const { password } = await api.resetAccountPassword(a.id)
                                      setSecret({ username: a.username, password })
                                    } catch (err) {
                                      toast.error(errorText(err, t))
                                    }
                                  },
                                })
                              }
                            >
                              {t("users.reset")}
                            </DropdownMenuItem>
                          )}
                          <DropdownMenuItem
                            onSelect={() =>
                              act(
                                () => api.updateAccount(a.id, { role: a.role, disabled: !a.disabled }),
                                t(a.disabled ? "users.toast.enabled" : "users.toast.disabled")
                              )
                            }
                          >
                            {t(a.disabled ? "users.enable" : "users.disable")}
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            variant="destructive"
                            onSelect={() =>
                              confirm({
                                title: t("users.delete.title", { name: a.username }),
                                description: t(a.sso ? "users.delete.descriptionSso" : "users.delete.description"),
                                action: t("common.delete"),
                                destructive: true,
                                onConfirm: () => act(() => api.deleteAccount(a.id), t("users.toast.deleted")),
                              })
                            }
                          >
                            {t("common.delete")}
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </PaneContent>
    </Pane>
  )
}
