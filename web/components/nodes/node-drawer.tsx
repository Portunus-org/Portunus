"use client"

import * as React from "react"
import { toast } from "@/lib/toast"

import { JoinCommand } from "@/components/nodes/join-command"
import { RecoveryKey } from "@/components/nodes/recovery-key"
import { NodeMap } from "@/components/nodes/node-map"
import { mapPoints } from "@/components/nodes/nodes-map-card"
import { NodeStatus } from "@/components/nodes/node-status"
import { FormField, SwitchField, FormError } from "@/components/form-field"
import { NumberInput } from "@/components/number-input"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerFooter, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { drawerScroll } from "@/lib/utils"
import { Separator } from "@/components/ui/separator"
import { useDeferredMount } from "@/lib/use-resource"
import { useIsMobile } from "@/hooks/use-mobile"
import { api, type GeoLocation, type MapStyle, type Node } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { useI18n } from "@/lib/i18n"
import { useFormErrors } from "@/lib/form-errors"
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
  mapStyle,
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
  mapStyle?: MapStyle
  nodes: Node[]
  onSaved: () => void
}) {
  const { t, locale } = useI18n()
  const isMobile = useIsMobile()
  const isAdmin = useSession().user.role === "admin"
  // The map is built once the drawer is on its way in.
  const mapReady = useDeferredMount()
  const [isExit, setIsExit] = React.useState(node.isExit)
  const [isEntry, setIsEntry] = React.useState(node.isEntry)
  const [standby, setStandby] = React.useState(node.standby)
  const [enabled, setEnabled] = React.useState(node.enabled)
  const [pending, setPending] = React.useState(false)
  const { errors, clear, fail, numbers } = useFormErrors()
  const agent = node.kind === "agent"
  // The place on the map is one more field of this form: a click on the
  // map changes what will be saved, and nothing is saved before "Save".
  // undefined: left as it is; null: to be found by address again.
  const [draft, setDraft] = React.useState<{ lat: number; lon: number } | null | undefined>(undefined)
  const location: GeoLocation | undefined = draft ? { ...draft, manual: true } : node.location

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!numbers(e.currentTarget)) return
    const f = new FormData(e.currentTarget)
    setPending(true)
    clear()
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
      if (draft !== undefined) await api.setNodeLocation(node.id, draft)
      toast.success(t("nodes.toast.saved"))
      onSaved()
      onOpenChange(false)
    } catch (err) {
      fail(err, ["name", "hopPort", "agentPort"])
    } finally {
      setPending(false)
    }
  }

  // The page itself shrinks above the keyboard (interactive-widget=resizes-content);
  // vaul resizing the drawer as well left it short once the keyboard was gone.
  return (
    <Drawer open={open} onOpenChange={onOpenChange} direction={isMobile ? "bottom" : "right"} repositionInputs={false}>
      <DrawerContent>
        <DrawerHeader className="gap-1 text-left!">
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

        <ScrollArea className={drawerScroll}>
          <div className="flex flex-col gap-4 px-4 text-sm">
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
              {node.status.updating && <Value label={t("nodes.drawer.update")}>{t("nodes.drawer.updating")}</Value>}
              {node.status.updateError && (
                <Value label={t("nodes.drawer.update")}>
                  <span className="text-destructive">{node.status.updateError}</span>
                </Value>
              )}
              {node.status.rejoin && <p className="text-muted-foreground">{t("nodes.drawer.rejoin")}</p>}
              {agent && node.status.state === "online" && !node.status.updater && !node.status.rejoin && (
                <p className="text-muted-foreground">{t("nodes.drawer.noUpdater")}</p>
              )}
            </div>
            <Separator />

            <form id="node-form" onSubmit={onSubmit} onInput={clear} className="flex flex-col gap-4" noValidate>
              <fieldset disabled={!isAdmin} className="contents">
                <FormField id="name" error={errors.name} label={t("nodes.form.name")}>
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
                    <div className="grid items-start gap-4 sm:grid-cols-2">
                      <FormField id="hopPort" error={errors.hopPort} label={t("nodes.form.hopPort")}>
                        <NumberInput
                          id="hopPort"
                          name="hopPort"
                          min={1}
                          max={65535}
                          className="tabular-nums"
                          defaultValue={node.hopPort}
                        />
                      </FormField>
                      <FormField id="agentPort" error={errors.agentPort} label={t("nodes.form.agentPort")}>
                        <NumberInput
                          id="agentPort"
                          name="agentPort"
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
              <FormError>{errors.form}</FormError>
            </form>

            <Separator />
            <div className="grid gap-3">
              <span className="font-medium">{t("nodes.location.title")}</span>
              {mapReady ? (
                <NodeMap
                  points={mapPoints(
                    main,
                    nodes.map((n) => (n.id === node.id ? { ...n, location } : n)),
                    t
                  )}
                  tiles={tiles}
                  mapStyle={mapStyle}
                  onPick={isAdmin ? setDraft : undefined}
                  className="[&_.portunus-map]:h-56 md:[&_.portunus-map]:h-56"
                />
              ) : (
                <div className="h-56 rounded-lg border bg-muted/50" />
              )}
              <p className="text-muted-foreground">
                {draft === null
                  ? t("nodes.location.willLookUp")
                  : location
                    ? location.manual
                      ? t("nodes.location.manual")
                      : t("nodes.location.auto", {
                          place: [location.city, location.country].filter(Boolean).join(", "),
                        })
                    : t("nodes.location.none")}
                {isAdmin && " " + t(draft === undefined ? "nodes.location.pick" : "nodes.location.unsaved")}
              </p>
              {isAdmin && (draft !== undefined || node.location?.manual) && (
                <div className="flex flex-wrap gap-2">
                  {draft !== undefined && (
                    <Button variant="outline" size="sm" onClick={() => setDraft(undefined)}>
                      {t("nodes.location.undo")}
                    </Button>
                  )}
                  {draft !== null && (draft || node.location?.manual) && (
                    <Button variant="outline" size="sm" onClick={() => setDraft(null)}>
                      {t("nodes.location.reset")}
                    </Button>
                  )}
                </div>
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
        </ScrollArea>

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
