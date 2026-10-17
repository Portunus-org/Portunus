"use client"

import * as React from "react"
import Link from "next/link"
import { LayoutDashboardIcon, RadioTowerIcon, ServerIcon, SplitIcon, UsersIcon } from "lucide-react"

import { LogoMark } from "@/components/brand/logo"
import { NavMain } from "@/components/nav-main"
import { NavUser } from "@/components/nav-user"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { useI18n } from "@/lib/i18n"

export function AppSidebar(props: React.ComponentProps<typeof Sidebar>) {
  const { t } = useI18n()

  const navMain = [
    { title: t("nav.overview"), url: "/", icon: <LayoutDashboardIcon /> },
    { title: t("nav.clients"), url: "/clients/", icon: <UsersIcon /> },
    { title: t("nav.inbounds"), url: "/inbounds/", icon: <RadioTowerIcon /> },
    { title: t("nav.nodes"), url: "/nodes/", icon: <ServerIcon /> },
    { title: t("nav.routing"), url: "/routing/", icon: <SplitIcon /> },
  ]

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild className="data-[slot=sidebar-menu-button]:p-1.5!">
              <Link href="/">
                <LogoMark className="size-5!" />
                <span className="font-heading text-lg font-semibold">Portunus</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavMain items={navMain} />
      </SidebarContent>
      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
    </Sidebar>
  )
}
