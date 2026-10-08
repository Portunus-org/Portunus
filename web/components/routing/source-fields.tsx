"use client"

import * as React from "react"
import { CheckIcon } from "lucide-react"

import { FormField } from "@/components/form-field"
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { Input } from "@/components/ui/input"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import type { RouteListInput } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"
import { routeCatalog, type CatalogItem } from "@/lib/route-catalog"

/** How the sites and addresses of a rule are given. */
export type SourceMode = "catalog" | "manual" | "url"

export interface Source {
  mode: SourceMode
  name: string
  url: string
  content: string
}

export const emptySource: Source = {
  mode: "catalog",
  name: "",
  url: "",
  content: "",
}

export function sourceReady(s: Source): boolean {
  if (s.mode === "manual") return !!s.name.trim() && !!s.content.trim()
  return !!s.name.trim() && !!s.url.trim()
}

export function sourceToList(s: Source): RouteListInput {
  return s.mode === "manual"
    ? { name: s.name.trim(), kind: "manual", content: s.content }
    : { name: s.name.trim(), kind: "url", url: s.url.trim() }
}

const groups: [CatalogItem["group"], MessageKey][] = [
  ["lists", "routing.catalog.lists"],
  ["services", "routing.catalog.services"],
  ["countries", "routing.catalog.countries"],
]

/**
 * What a rule applies to: a ready-made set from the catalog, entries typed
 * here, or a file by link. The way is chosen when the rule is created;
 * `fixed` keeps it when editing.
 */
export function SourceFields({ value, onChange, fixed = false }: { value: Source; onChange: (s: Source) => void; fixed?: boolean }) {
  const { t, locale } = useI18n()
  const set = (p: Partial<Source>) => onChange({ ...value, ...p })
  const catalog = React.useMemo(() => routeCatalog(locale), [locale])

  const name = (
    <FormField id="source-name" label={t("routing.source.name")}>
      <Input
        id="source-name"
        value={value.name}
        onChange={(e) => set({ name: e.target.value })}
        placeholder={t("routing.source.namePlaceholder")}
        maxLength={64}
      />
    </FormField>
  )
  const catalogPane = (
    <Command className="rounded-lg border">
      <CommandInput placeholder={t("routing.catalog.search")} />
      <CommandList className="max-h-72">
        <CommandEmpty>{t("common.nothingMatches")}</CommandEmpty>
        {groups.map(([group, heading]) => (
          <CommandGroup key={group} heading={t(heading)}>
            {catalog
              .filter((c) => c.group === group)
              .map((c) => (
                <CommandItem
                  key={c.id}
                  value={c.name[locale] + " " + c.name.en + " " + c.id}
                  onSelect={() =>
                    set({
                      url: c.url,
                      name:
                        group === "countries"
                          ? t("routing.catalog.countryName", {
                              name: c.name[locale],
                            })
                          : c.name[locale],
                    })
                  }
                >
                  {c.name[locale]}
                  {value.url === c.url && <CheckIcon className="ml-auto" />}
                </CommandItem>
              ))}
          </CommandGroup>
        ))}
      </CommandList>
    </Command>
  )
  const manualPane = (
    <>
      {name}
      <FormField id="source-content" label={t("routing.source.content")} hint={t("routing.source.contentHint")}>
        <Textarea
          id="source-content"
          value={value.content}
          onChange={(e) => set({ content: e.target.value })}
          rows={8}
          placeholder={"example.com\n203.0.113.0/24"}
          className="max-h-72 font-mono text-xs"
          autoCapitalize="none"
          spellCheck={false}
        />
      </FormField>
    </>
  )
  const urlPane = (
    <>
      {name}
      <FormField id="source-url" label={t("routing.source.link")} hint={t("routing.source.linkHint")}>
        <Input
          id="source-url"
          value={value.url}
          onChange={(e) => set({ url: e.target.value })}
          placeholder="https://raw.githubusercontent.com/…"
          autoCapitalize="none"
          spellCheck={false}
        />
      </FormField>
    </>
  )

  if (fixed) return <div className="flex flex-col gap-4">{value.mode === "manual" ? manualPane : urlPane}</div>

  return (
    <Tabs
      value={value.mode}
      // Each way starts clean: a set picked in the catalog is not carried into a typed list.
      onValueChange={(mode) => onChange({ ...emptySource, mode: mode as SourceMode })}
    >
      <TabsList>
        <TabsTrigger value="catalog">{t("routing.source.catalog")}</TabsTrigger>
        <TabsTrigger value="manual">{t("routing.source.manual")}</TabsTrigger>
        <TabsTrigger value="url">{t("routing.source.url")}</TabsTrigger>
      </TabsList>
      <TabsContent value="catalog" className="flex flex-col gap-4 pt-2">
        {catalogPane}
      </TabsContent>
      <TabsContent value="manual" className="flex flex-col gap-4 pt-2">
        {manualPane}
      </TabsContent>
      <TabsContent value="url" className="flex flex-col gap-4 pt-2">
        {urlPane}
      </TabsContent>
    </Tabs>
  )
}
