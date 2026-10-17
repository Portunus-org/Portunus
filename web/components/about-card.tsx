"use client"

import Link from "next/link"
import { ChevronRightIcon, ExternalLinkIcon } from "lucide-react"

import { LogoMark } from "@/components/brand/logo"
import { Badge } from "@/components/ui/badge"
import { Item, ItemActions, ItemContent, ItemDescription, ItemGroup, ItemSeparator, ItemTitle } from "@/components/ui/item"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useSettingsWindow } from "@/lib/settings-dialog"
import { useResource } from "@/lib/use-resource"

const repo = "https://github.com/Portunus-org/Portunus"

/**
 * What this panel is, the way an "About" window of a desktop application
 * says it: the mark, the name, the version, and where to go next (the
 * code, the releases, a place to report a problem, the help, the licence).
 */
export function AboutCard() {
  const { t } = useI18n()
  const win = useSettingsWindow()
  const { data: system } = useResource(api.system)
  const { data: singbox } = useResource(api.singbox)
  const commit = system?.commit && system.commit !== "unknown" ? system.commit.slice(0, 7) : ""

  const outside = (href: string, title: string, description: string, setting: string) => (
    <Item asChild size="sm" className="rounded-none px-4 py-3">
      <a href={href} target="_blank" rel="noreferrer" data-setting={setting}>
        <ItemContent>
          <ItemTitle>{title}</ItemTitle>
          <ItemDescription>{description}</ItemDescription>
        </ItemContent>
        <ItemActions>
          <ExternalLinkIcon className="size-4 text-muted-foreground" />
        </ItemActions>
      </a>
    </Item>
  )

  return (
    <section className="mx-auto flex max-w-md flex-col items-center gap-8 pt-6">
      <div className="flex flex-col items-center gap-3 text-center">
        <div className="mb-2 flex size-24 items-center justify-center rounded-3xl bg-primary text-primary-foreground shadow-sm">
          <LogoMark className="size-14" strokeWidth={1.5} />
        </div>
        <h2 className="font-heading text-3xl font-medium">Portunus</h2>
        <p className="text-sm text-muted-foreground">{t("about.tagline")}</p>
        <div className="flex flex-col items-center gap-2" data-setting="about-version">
          {system ? (
            <Badge variant="secondary" className="h-6 px-3 font-mono text-sm">
              {system.version}
              {commit && <span className="text-muted-foreground">{commit}</span>}
            </Badge>
          ) : (
            <Skeleton className="h-6 w-20 rounded-full" />
          )}
          <span className="text-xs text-muted-foreground" data-setting="about-singbox">
            {t("about.singbox", { version: singbox?.status.version || t("settings.singbox.notFound") })}
          </span>
        </div>
      </div>

      <ItemGroup className="w-full gap-0 overflow-hidden rounded-xl border has-data-[size=sm]:gap-0">
        {outside(repo, t("about.code"), t("about.codeHint"), "about-code")}
        <ItemSeparator className="my-0" />
        {outside(`${repo}/releases`, t("about.releases"), t("about.releasesHint"), "about-releases")}
        <ItemSeparator className="my-0" />
        {outside(`${repo}/issues`, t("about.issue"), t("about.issueHint"), "about-issue")}
      </ItemGroup>

      <ItemGroup className="w-full gap-0 overflow-hidden rounded-xl border has-data-[size=sm]:gap-0">
        <Item asChild size="sm" className="rounded-none px-4 py-3">
          {/* The help is a page of its own: the window gives way to it. */}
          <Link href="/docs/" data-setting="about-help" onClick={() => win.close()}>
            <ItemContent>
              <ItemTitle>{t("nav.help")}</ItemTitle>
              <ItemDescription>{t("about.helpHint")}</ItemDescription>
            </ItemContent>
            <ItemActions>
              <ChevronRightIcon className="size-4 text-muted-foreground" />
            </ItemActions>
          </Link>
        </Item>
        <ItemSeparator className="my-0" />
        {outside(`${repo}/blob/main/LICENSE`, t("about.license"), "GNU AGPL v3", "about-license")}
      </ItemGroup>
    </section>
  )
}
