"use client"

import * as React from "react"
import Link from "next/link"
import { EllipsisVerticalIcon, KeyRoundIcon, PencilIcon, PlusIcon, PowerIcon, Trash2Icon } from "lucide-react"
import { toast } from "@/lib/toast"

import { useConfirm } from "@/components/confirm"
import { columnHelper, DataTable } from "@/components/data-table"
import { InboundDrawer } from "@/components/inbounds/inbound-drawer"
import { StatusBadge } from "@/components/status"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { api, errorText, inboundInput, type Inbound } from "@/lib/api"
import { daysUntil, formatDate } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { protocols } from "@/lib/protocols"
import { useCanEdit } from "@/lib/session"
import { useResource } from "@/lib/use-resource"

const col = columnHelper<Inbound>()

export default function InboundsPage() {
  const { t, locale } = useI18n()
  const canEdit = useCanEdit()
  const { data, reload } = useResource(api.inbounds, 15_000)
  const [view, setView] = React.useState("all")
  const [editing, setEditing] = React.useState<Inbound | null>(null)
  const [drawerOpen, setDrawerOpen] = React.useState(false)
  const [confirm, confirmDialog] = useConfirm()

  const open = React.useCallback((inbound: Inbound) => {
    // Rendered in the background and shown when ready: building the drawer
    // must not hold up the frame of the click.
    React.startTransition(() => {
      setEditing(inbound)
      setDrawerOpen(true)
    })
  }, [])

  const act = React.useCallback(
    async (fn: () => Promise<unknown>, done: string) => {
      try {
        await fn()
        toast.success(done)
        reload()
      } catch (err) {
        toast.error(errorText(err, t))
      }
    },
    [reload, t]
  )

  const columns = React.useMemo(
    () =>
      col.columns([
        col.accessor("name", {
          header: t("inbounds.col.name"),
          enableHiding: false,
          cell: ({ row }) => (
            <Button variant="link" className="w-fit px-0 text-left text-foreground" onClick={() => open(row.original)}>
              {row.original.name || protocols[row.original.protocol].label}
            </Button>
          ),
        }),
        col.accessor("protocol", {
          header: t("inbounds.col.protocol"),
          cell: ({ row }) => (
            <div className="flex gap-1.5">
              <Badge variant="outline" className="px-1.5 text-muted-foreground">
                {protocols[row.original.protocol].label}
              </Badge>
              {row.original.obfs && (
                <Badge variant="outline" className="px-1.5 text-muted-foreground">
                  obfs
                </Badge>
              )}
            </div>
          ),
        }),
        col.accessor((r) => r.tlsMode ?? "", {
          id: "certificate",
          header: t("inbounds.col.certificate"),
          enableSorting: false,
          cell: ({ row }) => {
            const r = row.original
            if (!r.tlsMode)
              return (
                <span className="text-muted-foreground">
                  {r.protocol === "vless" ? t("inbounds.cert.reality") : t("inbounds.cert.none")}
                </span>
              )
            if (r.tlsMode === "self-signed")
              return (
                <StatusBadge tone="ok">
                  {r.privateCertExpires
                    ? t("inbounds.cert.privateUntil", {
                        date: formatDate(r.privateCertExpires, locale),
                      })
                    : t("inbounds.cert.selfSigned")}
                </StatusBadge>
              )
            if (!r.certificate) return <StatusBadge tone="pending">{t("inbounds.cert.pending")}</StatusBadge>
            return (
              <StatusBadge tone={daysUntil(r.certificate.notAfter) > 14 ? "ok" : "error"}>
                {t("inbounds.cert.until", {
                  date: formatDate(r.certificate.notAfter, locale),
                })}
              </StatusBadge>
            )
          },
        }),
        col.accessor("port", {
          header: t("inbounds.col.port"),
          cell: ({ row }) => (
            <div className="tabular-nums">
              <span className="text-muted-foreground">{protocols[row.original.protocol].transport}</span> {row.original.port}
            </div>
          ),
        }),
        col.accessor("enabled", {
          header: t("inbounds.col.status"),
          cell: ({ getValue }) => (
            <StatusBadge tone={getValue() ? "ok" : "off"}>{t(getValue() ? "common.enabled" : "common.disabled")}</StatusBadge>
          ),
        }),
        col.display({
          id: "actions",
          cell: ({ row }) => {
            const inb = row.original
            if (!canEdit) return null
            return (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="flex size-8 text-muted-foreground data-[state=open]:bg-muted" size="icon">
                    <EllipsisVerticalIcon />
                    <span className="sr-only">{t("common.actions")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-44">
                  <DropdownMenuItem onSelect={() => open(inb)}>
                    <PencilIcon />
                    {t("common.edit")}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onSelect={() =>
                      act(
                        () =>
                          api.updateInbound(inb.id, {
                            ...inboundInput(inb),
                            enabled: !inb.enabled,
                          }),
                        t(inb.enabled ? "inbounds.toast.disabled" : "inbounds.toast.enabled")
                      )
                    }
                  >
                    <PowerIcon />
                    {t(inb.enabled ? "common.disable" : "common.enable")}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onSelect={() =>
                      confirm({
                        title: t("inbounds.confirm.rotate"),
                        description: t("inbounds.confirm.rotateText"),
                        action: t("inbounds.rotateKeys"),
                        onConfirm: () => act(() => api.rotateInboundKeys(inb.id), t("inbounds.toast.rotated")),
                      })
                    }
                  >
                    <KeyRoundIcon />
                    {t("inbounds.rotateKeys")}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() =>
                      confirm({
                        title: t("inbounds.confirm.delete", {
                          name: inb.name || protocols[inb.protocol].label,
                        }),
                        description: t("inbounds.confirm.deleteText"),
                        action: t("common.delete"),
                        destructive: true,
                        onConfirm: () => act(() => api.deleteInbound(inb.id), t("inbounds.toast.deleted")),
                      })
                    }
                  >
                    <Trash2Icon />
                    {t("common.delete")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )
          },
        }),
      ]),
    [act, canEdit, confirm, open, t, locale]
  )

  const inbounds = data ?? []
  const enabled = inbounds.filter((i) => i.enabled)
  const rows = view === "enabled" ? enabled : view === "disabled" ? inbounds.filter((i) => !i.enabled) : inbounds

  return (
    <>
      <DataTable
        data={rows}
        columns={columns}
        getRowId={(r) => String(r.id)}
        views={[
          { value: "all", label: t("inbounds.view.all") },
          {
            value: "enabled",
            label: t("inbounds.view.enabled"),
            count: enabled.length,
          },
          {
            value: "disabled",
            label: t("inbounds.view.disabled"),
            count: inbounds.length - enabled.length,
          },
        ]}
        view={view}
        onViewChange={setView}
        columnLabels={{
          protocol: t("inbounds.col.protocol"),
          certificate: t("inbounds.col.certificate"),
          port: t("inbounds.col.port"),
          enabled: t("inbounds.col.status"),
        }}
        toolbar={
          canEdit && (
            <Button variant="outline" size="sm" asChild>
              <Link href="/inbounds/new/">
                <PlusIcon />
                <span className="sr-only lg:not-sr-only">{t("inbounds.add")}</span>
              </Link>
            </Button>
          )
        }
        loading={!data}
        empty={t("inbounds.empty")}
      />

      {editing && <InboundDrawer key={editing.id} open={drawerOpen} onOpenChange={setDrawerOpen} inbound={editing} onSaved={reload} />}
      {confirmDialog}
    </>
  )
}
