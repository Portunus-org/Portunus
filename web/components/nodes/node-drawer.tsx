"use client"

import * as React from "react"
import { toast } from "sonner"

import { JoinCommand } from "@/components/nodes/join-command"
import { RecoveryKey } from "@/components/nodes/recovery-key"
import { NodeMap } from "@/components/nodes/node-map"
import { mapPoints } from "@/components/nodes/nodes-map-card"
import { NodeStatus } from "@/components/nodes/node-status"
import { FormField, SwitchField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerFooter, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { useIsMobile } from "@/hooks/use-mobile"
import { api, errorText, type GeoLocation, type Node } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { useSession } from "@/lib/session"

function Value({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 text-right break-words">{children}</span>
    </div>
  )
}

/** Details of a node: state, settings, and (for our servers) the join command. */
export function NodeDrawer({
  open,
  onOpenChange,
  node,
  showJoin,
  main,
  tiles,
  nodes,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  node: Node
  /** Start on the connection command (from the row menu). */
  showJoin?: boolean
  /** The other servers, as context on the small map. */
  main?: { host: string; location: GeoLocation | null }
  tiles: boolean
  nodes: Node[]
  onSaved: () => void
}) {
  const { t, locale } = useI18n()
  const isMobile = useIsMobile()
  const isAdmin = useSession().user.role === "admin"
  const [isExit, setIsExit] = React.useState(node.isExit)
  const [isEntry, setIsEntry] = React.useState(node.isEntry)
  const [standby, setStandby] = React.useState(node.standby)
  const [enabled, setEnabled] = React.useState(node.enabled)
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const agent = node.kind === "agent"
  const [location, setLocation] = React.useState(node.location)

  async function place(next: { lat: number; lon: number } | null) {
    try {
      await api.setNodeLocation(node.id, next)
      setLocation(next ? { ...next, manual: true } : undefined)
      toast.success(t("nodes.location.saved"))
      onSaved()
    } catch (err) {
      toast.error(errorText(err, t))
    }
  }

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    setPending(true)
    setError(null)
    try {
      await api.updateNode(node.id, {
        name: String(f.get("name") ?? ""),
        isExit,
        isEntry,
        standby,
        enabled,
        hopPort: agent ? Number(f.get("hopPort")) : undefined,
        agentPort: agent ? Number(f.get("agentPort")) : undefined,
      })
      toast.success(t("nodes.toast.saved"))
      onSaved()
      onOpenChange(false)
    } catch (err) {
      setError(errorText(err, t))
    } finally {
      setPending(false)
    }
  }

  return (
    <Drawer open={open} onOpenChange={onOpenChange} direction={isMobile ? "bottom" : "right"}>
      <DrawerContent>
        <DrawerHeader className="gap-1">
          <div className="flex items-center justify-between gap-2">
            <DrawerTitle className="truncate">{node.name}</DrawerTitle>
            <NodeStatus node={node} />
          </div>
          <DrawerDescription>
            {agent
              ? t("nodes.drawer.subtitleAgent", { address: node.address })
              : t("nodes.drawer.subtitleLink", {
                  protocol: node.protocol ?? "",
                  address: `${node.address}:${node.port}`,
                })}
          </DrawerDescription>
        </DrawerHeader>

        <div className="flex flex-col gap-4 overflow-y-auto px-4 text-sm">
          <div className="grid gap-2">
            {node.status.latencyMs !== undefined && (
              <Value label={t("nodes.col.latency")}>{t("nodes.latency", { ms: node.status.latencyMs })}</Value>
            )}
            {node.status.version && <Value label={t("nodes.drawer.version")}>{node.status.version}</Value>}
            {node.status.checkedAt && <Value label={t("nodes.drawer.checked")}>{formatDateTime(node.status.checkedAt, locale)}</Value>}
            {node.enabled && node.status.error && (
              <Value label={t("nodes.drawer.error")}>
                <span className="text-destructive">{node.status.error}</span>
              </Value>
            )}
          </div>
          <Separator />

          <form id="node-form" onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
            <fieldset disabled={!isAdmin} className="contents">
              <FormField id="name" label={t("nodes.form.name")}>
                <Input id="name" name="name" defaultValue={node.name} maxLength={64} required />
              </FormField>
              {agent && (
                <>
                  <FieldGroup className="gap-3">
                    <Field orientation="horizontal">
                      <Checkbox id="isExit" checked={isExit} onCheckedChange={(v) => setIsExit(!!v)} />
                      <FieldContent>
                        <FieldLabel htmlFor="isExit">{t("nodes.form.exit")}</FieldLabel>
                        <FieldDescription>{t("nodes.form.exitHint")}</FieldDescription>
                      </FieldContent>
                    </Field>
                    <Field orientation="horizontal">
                      <Checkbox id="isEntry" checked={isEntry} onCheckedChange={(v) => setIsEntry(!!v)} />
                      <FieldContent>
                        <FieldLabel htmlFor="isEntry">{t("nodes.form.entry")}</FieldLabel>
                        <FieldDescription>{t("nodes.form.entryHint")}</FieldDescription>
                      </FieldContent>
                    </Field>
                    <Field orientation="horizontal">
                      <Checkbox id="standby" checked={standby} onCheckedChange={(v) => setStandby(!!v)} />
                      <FieldContent>
                        <FieldLabel htmlFor="standby">{t("nodes.form.standby")}</FieldLabel>
                        <FieldDescription>{t("nodes.form.standbyHint")}</FieldDescription>
                      </FieldContent>
                    </Field>
                  </FieldGroup>
                  <div className="grid grid-cols-2 gap-4">
                    <FormField id="hopPort" label={t("nodes.form.hopPort")}>
                      <Input
                        id="hopPort"
                        name="hopPort"
                        type="number"
                        min={1}
                        max={65535}
                        className="tabular-nums"
                        defaultValue={node.hopPort}
                      />
                    </FormField>
                    <FormField id="agentPort" label={t("nodes.form.agentPort")}>
                      <Input
                        id="agentPort"
                        name="agentPort"
                        type="number"
                        min={1}
                        max={65535}
                        className="tabular-nums"
                        defaultValue={node.agentPort}
                      />
                    </FormField>
                  </div>
                </>
              )}
              <SwitchField id="node-enabled" label={t("nodes.form.enabled")} checked={enabled} onCheckedChange={setEnabled} />
            </fieldset>
            <FormError>{error}</FormError>
          </form>

          <Separator />
          <div className="grid gap-3">
            <span className="font-medium">{t("nodes.location.title")}</span>
            <NodeMap
              points={mapPoints(
                main,
                nodes.map((n) => (n.id === node.id ? { ...n, location } : n)),
                t
              )}
              tiles={tiles}
              onPick={isAdmin ? place : undefined}
              className="[&_.portunus-map]:h-56 md:[&_.portunus-map]:h-56"
            />
            <p className="text-muted-foreground">
              {location
                ? location.manual
                  ? t("nodes.location.manual")
                  : t("nodes.location.auto", {
                      place: [location.city, location.country].filter(Boolean).join(", "),
                    })
                : t("nodes.location.none")}
              {isAdmin && location && " " + t("nodes.location.pick")}
            </p>
            {isAdmin && location?.manual && (
              <Button variant="outline" size="sm" className="w-fit" onClick={() => place(null)}>
                {t("nodes.location.reset")}
              </Button>
            )}
          </div>

          {agent && isAdmin && (showJoin || !node.joined) && (
            <>
              <Separator />
              <div className="grid gap-3">
                <span className="font-medium">{t("nodes.join")}</span>
                <JoinCommand node={node} />
                {node.standby && <RecoveryKey node={node} />}
              </div>
            </>
          )}
        </div>

        <DrawerFooter>
          {isAdmin && (
            <Button type="submit" form="node-form" disabled={pending}>
              {pending ? t("common.saving") : t("common.save")}
            </Button>
          )}
          <DrawerClose asChild>
            <Button variant="outline">{t(isAdmin ? "common.cancel" : "common.close")}</Button>
          </DrawerClose>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  )
}
