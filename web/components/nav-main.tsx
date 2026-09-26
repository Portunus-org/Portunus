"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useTheme } from "next-themes"
import { CirclePlusIcon, MoonIcon, SunIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { useI18n } from "@/lib/i18n"
import { useCanEdit } from "@/lib/session"

export interface NavItem {
  title: string
  url: string
  icon: React.ReactNode
}

export function isActivePath(pathname: string, url: string) {
  const p = pathname.endsWith("/") ? pathname : pathname + "/"
  return url === "/" ? p === "/" : p.startsWith(url)
}

export function NavMain({ items }: { items: NavItem[] }) {
  const { t } = useI18n()
  const pathname = usePathname()
  const canEdit = useCanEdit()
  const { resolvedTheme, setTheme } = useTheme()

  return (
    <SidebarGroup>
      <SidebarGroupContent className="flex flex-col gap-2">
        <SidebarMenu>
          <SidebarMenuItem className="flex items-center gap-2">
            {canEdit && (
              <SidebarMenuButton
                asChild
                tooltip={t("nav.newClient")}
                className="min-w-8 bg-primary text-primary-foreground duration-200 ease-linear hover:bg-primary/90 hover:text-primary-foreground active:bg-primary/90 active:text-primary-foreground"
              >
                <Link href="/clients/#new">
                  <CirclePlusIcon />
                  <span>{t("nav.newClient")}</span>
                </Link>
              </SidebarMenuButton>
            )}
            <Button
              size="icon"
              className="size-8 group-data-[collapsible=icon]:opacity-0"
              variant="outline"
              onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
            >
              {resolvedTheme === "dark" ? <SunIcon /> : <MoonIcon />}
              <span className="sr-only">{t("nav.theme")}</span>
            </Button>
          </SidebarMenuItem>
        </SidebarMenu>
        <SidebarMenu>
          {items.map((item) => (
            <SidebarMenuItem key={item.url}>
              <SidebarMenuButton asChild tooltip={item.title} isActive={isActivePath(pathname, item.url)}>
                <Link href={item.url}>
                  {item.icon}
                  <span>{item.title}</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          ))}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}
