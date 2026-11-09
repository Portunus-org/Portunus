"use client"

import * as React from "react"
import { EllipsisVerticalIcon, Trash2Icon } from "lucide-react"
import { toast } from "@/lib/toast"

import { useConfirm } from "@/components/confirm"
import { emptySource, SourceFields, sourceReady, sourceToList, type Source } from "@/components/routing/source-fields"
import { TargetSelect } from "@/components/routing/target-select"
import { FormField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerFooter, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { drawerScroll } from "@/lib/utils"
import { Separator } from "@/components/ui/separator"
import { useIsMobile } from "@/hooks/use-mobile"
import { api, errorText, type Chain, type Node, type RouteList, type RouteRule, type RouteTarget } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { FormErrorsContext, useFormErrors } from "@/lib/form-errors"

/** A filter queued on a new chain before the chain itself is saved. */
export interface DraftChainRule {
  key: string
  source: Source
  target: RouteTarget
}

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
  chains,
  initialScope = 0,
  onSaved,
  draftRule,
  draftChainName,
  onSaveDraft,
  onDeleteDraft,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  rule: RouteRule | null
  /** The list of the rule being edited. */
  list: RouteList | null
  nodes: Node[]
  chains: Chain[]
  /** The chain a brand-new rule starts scoped to (the page's active filter). */
  initialScope?: number
  onSaved: () => void
  /** When set, the drawer edits an in-memory filter of an unsaved chain. */
  draftRule?: DraftChainRule | null
  draftChainName?: string
  onSaveDraft?: (draft: DraftChainRule) => void
  onDeleteDraft?: () => void
}) {
  const { t } = useI18n()
  const isMobile = useIsMobile()
  const [confirm, confirmDialog] = useConfirm()
  const exits = nodes.filter((n) => n.isExit)
  const [source, setSource] = React.useState<Source>(
    draftRule
      ? draftRule.source
      : list
        ? {
            mode: list.kind,
            name: list.name,
            url: list.url ?? "",
            content: list.content ?? "",
          }
        : emptySource
  )
  const [target, setTarget] = React.useState<RouteTarget>(
    draftRule
      ? draftRule.target
      : rule
        ? { action: rule.action, nodeId: rule.nodeId }
        : initialScope > 0 || onSaveDraft
          ? { action: "direct" }
          : exits.length
            ? { action: "auto" }
            : { action: "direct" }
  )
  // 0 = every client; otherwise the id of the chain the rule belongs to.
  const [scope, setScope] = React.useState<number>(rule?.chainId ?? initialScope)
  const [pending, setPending] = React.useState(false)
  const { errors, clear, fail } = useFormErrors()

  async function save() {
    if (onSaveDraft) {
      onSaveDraft({
        key: draftRule?.key ?? `d-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
        source: {
          ...source,
          name: source.name.trim(),
          url: source.url.trim(),
        },
        target,
      })
      onOpenChange(false)
      return
    }
    setPending(true)
    clear()
    try {
      if (rule && list) {
        await api.updateRouteList(list.id, sourceToList(source))
        await api.updateRouteRule(rule.id, {
          ...target,
          listId: list.id,
          chainId: scope,
          enabled: rule.enabled,
        })
      } else {
        const created = await api.createRouteList(sourceToList(source))
        try {
          await api.createRouteRule({ ...target, listId: created.id, chainId: scope })
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
      // The list and the rule are saved one after the other; either one's
      // refusal names the field of this form it is about.
      fail(err, ["source-name", "source-content", "source-url", "rule-scope", "rule-target"], {
        name: "source-name",
        content: "source-content",
        url: "source-url",
        chain: "rule-scope",
        node: "rule-target",
        action: "rule-target",
      })
    } finally {
      setPending(false)
    }
  }

  async function removeRule() {
    if (onDeleteDraft) {
      onDeleteDraft()
      onOpenChange(false)
      return
    }
    if (!rule) return
    setPending(true)
    try {
      await api.deleteRouteRule(rule.id)
      toast.success(t("routing.toast.ruleDeleted"))
      onSaved()
      onOpenChange(false)
    } catch (err) {
      toast.error(errorText(err, t))
    } finally {
      setPending(false)
    }
  }

  const isEditing = !!(rule || draftRule)

  return (
    <>
      <Drawer open={open} onOpenChange={onOpenChange} direction={isMobile ? "bottom" : "right"} repositionInputs={false}>
        <DrawerContent>
          <DrawerHeader className="gap-1 text-left!">
            <DrawerTitle>{isEditing ? source.name || t("routing.rule.edit") : t("routing.rule.new")}</DrawerTitle>
            <DrawerDescription>{t("routing.rule.hint")}</DrawerDescription>
          </DrawerHeader>

          <ScrollArea className={drawerScroll}>
            <div className="flex flex-col gap-4 px-4 text-sm" onInput={clear}>
              <FormErrorsContext.Provider value={errors}>
                <div className="flex flex-col gap-3">
                  <Label>{t("routing.rule.what")}</Label>
                  <SourceFields value={source} onChange={setSource} fixed={!!rule} />
                </div>
                <Separator />
                {onSaveDraft ? (
                  <>
                    <FormField id="rule-scope" label={t("routing.rule.scope")}>
                      <Select value="draft" disabled>
                        <SelectTrigger id="rule-scope">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value="draft">
                              {t("routing.rule.scope.chain", { name: draftChainName?.trim() || t("chains.new") })}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <p className="text-muted-foreground">{t("routing.rule.scope.hint")}</p>
                    </FormField>
                    <Separator />
                  </>
                ) : (
                  chains.length > 0 && (
                    <>
                      <FormField id="rule-scope" label={t("routing.rule.scope")}>
                        <Select value={String(scope)} onValueChange={(v) => setScope(Number(v))}>
                          <SelectTrigger id="rule-scope">
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
                  )
                )}
                <FormField id="rule-target" label={t("routing.rule.target")}>
                  <TargetSelect id="rule-target" value={target} onChange={setTarget} nodes={nodes} allowBlock />
                  {(target.action === "node" || target.action === "auto") && (
                    <p className="text-muted-foreground">{t("routing.failClosed")}</p>
                  )}
                  {target.action === "direct" && nodes.some((n) => n.isEntry && n.enabled) && (
                    <p className="text-muted-foreground">{t("routing.directNote")}</p>
                  )}
                </FormField>
              </FormErrorsContext.Provider>
              <FormError>{errors.form}</FormError>
            </div>
          </ScrollArea>

          <DrawerFooter>
            <Button onClick={save} disabled={pending || !sourceReady(source)}>
              {pending ? t("common.saving") : isEditing ? t("common.save") : t("routing.rule.create")}
            </Button>
            <div className="flex gap-2">
              <DrawerClose asChild>
                <Button variant="outline" className="flex-1">
                  {t("common.cancel")}
                </Button>
              </DrawerClose>
              {(rule || onDeleteDraft) && (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="outline" size="icon" aria-label={t("common.actions")} disabled={pending}>
                      <EllipsisVerticalIcon />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-44">
                    <DropdownMenuItem
                      variant="destructive"
                      onSelect={() => {
                        if (onDeleteDraft) {
                          removeRule()
                        } else {
                          confirm({
                            title: t("routing.confirm.deleteRule"),
                            description: t("routing.confirm.deleteRuleText"),
                            action: t("common.delete"),
                            destructive: true,
                            onConfirm: removeRule,
                          })
                        }
                      }}
                    >
                      <Trash2Icon />
                      {t("common.delete")}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </div>
          </DrawerFooter>
        </DrawerContent>
      </Drawer>
      {confirmDialog}
    </>
  )
}
