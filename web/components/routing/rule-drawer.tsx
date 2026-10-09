"use client"

import * as React from "react"
import { toast } from "sonner"

import { emptySource, SourceFields, sourceReady, sourceToList, type Source } from "@/components/routing/source-fields"
import { TargetSelect } from "@/components/routing/target-select"
import { FormField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerFooter, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { drawerScroll } from "@/lib/utils"
import { Separator } from "@/components/ui/separator"
import { useIsMobile } from "@/hooks/use-mobile"
import { api, errorText, type Node, type RouteList, type RouteRule, type RouteTarget } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

/**
 * Create or edit a rule: what it applies to and where that traffic goes.
 * To the operator a rule and its list are one thing; behind it the list is
 * created, updated and removed together with the rule.
 */
export function RuleDrawer({
  open,
  onOpenChange,
  rule,
  list,
  nodes,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  rule: RouteRule | null
  /** The list of the rule being edited. */
  list: RouteList | null
  nodes: Node[]
  onSaved: () => void
}) {
  const { t } = useI18n()
  const isMobile = useIsMobile()
  const exits = nodes.filter((n) => n.isExit)
  const [source, setSource] = React.useState<Source>(
    list
      ? {
          mode: list.kind,
          name: list.name,
          url: list.url ?? "",
          content: list.content ?? "",
        }
      : emptySource
  )
  const [target, setTarget] = React.useState<RouteTarget>(
    rule ? { action: rule.action, nodeId: rule.nodeId } : exits.length ? { action: "auto" } : { action: "direct" }
  )
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)

  async function save() {
    setPending(true)
    setError(null)
    try {
      if (rule && list) {
        await api.updateRouteList(list.id, sourceToList(source))
        await api.updateRouteRule(rule.id, {
          ...target,
          listId: list.id,
          enabled: rule.enabled,
        })
      } else {
        const created = await api.createRouteList(sourceToList(source))
        try {
          await api.createRouteRule({ ...target, listId: created.id })
        } catch (err) {
          // No rule, no list: do not leave a list nobody can see.
          await api.deleteRouteList(created.id).catch(() => {})
          throw err
        }
      }
      toast.success(t(rule ? "routing.toast.ruleSaved" : "routing.toast.ruleCreated"))
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
        <DrawerHeader className="gap-1 text-left!">
          <DrawerTitle>{rule ? source.name || t("routing.rule.edit") : t("routing.rule.new")}</DrawerTitle>
          <DrawerDescription>{t("routing.rule.hint")}</DrawerDescription>
        </DrawerHeader>

        <ScrollArea className={drawerScroll}>
          <div className="flex flex-col gap-4 px-4 text-sm">
            <div className="flex flex-col gap-3">
              <Label>{t("routing.rule.what")}</Label>
              <SourceFields value={source} onChange={setSource} fixed={!!rule} />
            </div>
            <Separator />
            <FormField id="rule-target" label={t("routing.rule.target")}>
              <TargetSelect id="rule-target" value={target} onChange={setTarget} nodes={nodes} allowBlock />
              {(target.action === "node" || target.action === "auto") && <p className="text-muted-foreground">{t("routing.failClosed")}</p>}
            </FormField>
            <FormError>{error}</FormError>
          </div>
        </ScrollArea>

        <DrawerFooter>
          <Button onClick={save} disabled={pending || !sourceReady(source)}>
            {pending ? t("common.saving") : rule ? t("common.save") : t("routing.rule.create")}
          </Button>
          <DrawerClose asChild>
            <Button variant="outline">{t("common.cancel")}</Button>
          </DrawerClose>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  )
}
