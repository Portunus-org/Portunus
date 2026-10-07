"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { CirclePlusIcon } from "lucide-react"

import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar"
import { NEW_CLIENT_EVENT } from "@/components/clients/client-drawer"
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
  const { setOpenMobile } = useSidebar()

  return (
    <SidebarGroup>
      <SidebarGroupContent className="flex flex-col gap-2">
        <SidebarMenu>
          <SidebarMenuItem>
            {canEdit && (
              <SidebarMenuButton
                asChild
                tooltip={t("nav.newClient")}
                className="min-w-8 bg-primary text-primary-foreground duration-200 ease-linear hover:bg-primary/90 hover:text-primary-foreground active:bg-primary/90 active:text-primary-foreground"
              >
                <Link
                  href="/clients/#new"
                  onClick={(e) => {
                    // Already on the page: the hash would not change, so ask it directly.
                    if (!isActivePath(pathname, "/clients/")) return
                    e.preventDefault()
                    setOpenMobile(false)
                    window.dispatchEvent(new Event(NEW_CLIENT_EVENT))
                  }}
                >
                  <CirclePlusIcon />
                  <span>{t("nav.newClient")}</span>
                </Link>
              </SidebarMenuButton>
            )}
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
