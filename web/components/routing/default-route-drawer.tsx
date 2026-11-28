"use client"

import * as React from "react"
import { toast } from "@/lib/toast"

import { ChainDefaultSelect, TargetSelect } from "@/components/routing/target-select"
import { FormError, FormField } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerFooter, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { useIsMobile } from "@/hooks/use-mobile"
import { api, type Chain, type Node, type RouteTarget } from "@/lib/api"
import { FormErrorsContext, useFormErrors } from "@/lib/form-errors"
import { useI18n } from "@/lib/i18n"
import { drawerScroll } from "@/lib/utils"

/**
 * Edit where unmatched ("all other") traffic goes globally or for one chain.
 * Mirrors RuleDrawer so the fallback rule behaves like every other row in the table.
 */
export function DefaultRouteDrawer({
  open,
  onOpenChange,
  initialScope = 0,
  routingDefault,
  nodes,
  chains,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initialScope?: number
  routingDefault: RouteTarget
  nodes: Node[]
  chains: Chain[]
  onSaved: () => Promise<void> | void
}) {
  const { t } = useI18n()
  const isMobile = useIsMobile()
  const targetForScope = (s: number): RouteTarget | null =>
    s === 0 ? routingDefault : (chains.find((c) => c.id === s)?.default ?? null)

  const [scope, setScope] = React.useState<number>(initialScope)
  const [target, setTarget] = React.useState<RouteTarget | null>(() => targetForScope(initialScope))
  const [pending, setPending] = React.useState(false)
  const { errors, clear, fail } = useFormErrors()

  const handleScopeChange = (v: string) => {
    const next = Number(v)
    setScope(next)
    setTarget(targetForScope(next))
    clear()
  }

  async function save() {
    setPending(true)
    clear()
    try {
      if (scope === 0) {
        await api.setDefaultRoute(target ?? routingDefault)
      } else {
        await api.setDefaultRoute(target ?? { action: "" }, scope)
      }
      toast.success(t("routing.toast.defaultSaved"))
      await onSaved()
      onOpenChange(false)
    } catch (err) {
      fail(err, ["default-scope", "default-target"], {
        chainId: "default-scope",
        node: "default-target",
        action: "default-target",
      })
    } finally {
      setPending(false)
    }
  }

  const effective = target ?? routingDefault

  return (
    <Drawer open={open} onOpenChange={onOpenChange} direction={isMobile ? "bottom" : "right"} repositionInputs={false}>
      <DrawerContent>
        <DrawerHeader className="gap-1 text-left!">
          <DrawerTitle>{t("routing.default.title")}</DrawerTitle>
          <DrawerDescription>{t("routing.default.description")}</DrawerDescription>
        </DrawerHeader>

        <ScrollArea className={drawerScroll}>
          <div className="flex flex-col gap-4 px-4 text-sm" onInput={clear}>
            <FormErrorsContext.Provider value={errors}>
              {chains.length > 0 && (
                <>
                  <FormField id="default-scope" label={t("routing.rule.scope")}>
                    <Select value={String(scope)} onValueChange={handleScopeChange}>
                      <SelectTrigger id="default-scope">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value="0">{t("routing.rule.scope.global")}</SelectItem>
                          {chains.map((c) => (
                            <SelectItem key={c.id} value={String(c.id)}>
                              {t("routing.rule.scope.chain", { name: c.name })}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    {scope !== 0 && <p className="text-muted-foreground">{t("routing.rule.scope.hint")}</p>}
                  </FormField>
                  <Separator />
                </>
              )}

              <FormField id="default-target" label={t("routing.rule.target")}>
                {scope === 0 ? (
                  <TargetSelect
                    id="default-target"
                    value={target ?? routingDefault}
                    onChange={setTarget}
                    nodes={nodes}
                    chains={chains}
                  />
                ) : (
                  <ChainDefaultSelect
                    id="default-target"
                    value={target}
                    globalTarget={routingDefault}
                    onChange={setTarget}
                    nodes={nodes}
                  />
                )}
                {effective.action === "auto" && <p className="text-muted-foreground">{t("routing.autoNote")}</p>}
                {effective.action === "node" && <p className="text-muted-foreground">{t("routing.failClosed")}</p>}
                {effective.action === "direct" && nodes.some((n) => n.isEntry && n.enabled) && (
                  <p className="text-muted-foreground">{t("routing.directNote")}</p>
                )}
              </FormField>
            </FormErrorsContext.Provider>
            <FormError>{errors.form}</FormError>
          </div>
        </ScrollArea>

        <DrawerFooter>
          <Button onClick={save} disabled={pending}>
            {pending ? t("common.saving") : t("common.save")}
          </Button>
          <DrawerClose asChild>
            <Button variant="outline">{t("common.cancel")}</Button>
          </DrawerClose>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  )
}
