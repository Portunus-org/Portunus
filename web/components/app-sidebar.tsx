"use client"

import * as React from "react"
import Link from "next/link"
import { BookOpenIcon, LayoutDashboardIcon, RadioTowerIcon, ServerIcon, Settings2Icon, SplitIcon, UsersIcon } from "lucide-react"

import { LogoMark } from "@/components/brand/logo"
import { NavMain } from "@/components/nav-main"
import { NavSecondary } from "@/components/nav-secondary"
import { NavUser } from "@/components/nav-user"
import { updateChecked } from "@/components/update-card"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { api } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useResource } from "@/lib/use-resource"

export function AppSidebar(props: React.ComponentProps<typeof Sidebar>) {
  const { t } = useI18n()

  const navMain = [
    { title: t("nav.overview"), url: "/", icon: <LayoutDashboardIcon /> },
    { title: t("nav.clients"), url: "/clients/", icon: <UsersIcon /> },
    { title: t("nav.inbounds"), url: "/inbounds/", icon: <RadioTowerIcon /> },
    { title: t("nav.nodes"), url: "/nodes/", icon: <ServerIcon /> },
    { title: t("nav.routing"), url: "/routing/", icon: <SplitIcon /> },
  ]
  // A newer release is marked on the way to where it is installed from.
  const { data: update, reload: reloadUpdate } = useResource(api.update, 30 * 60 * 1000)
  React.useEffect(() => {
    window.addEventListener(updateChecked, reloadUpdate)
    return () => window.removeEventListener(updateChecked, reloadUpdate)
  }, [reloadUpdate])
  const navSecondary = [
    {
      title: t("nav.settings"),
      url: "/settings/",
      icon: <Settings2Icon />,
      mark: update?.available ? t("update.tooltip") : undefined,
    },
    { title: t("nav.docs"), url: "/docs/", icon: <BookOpenIcon /> },
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
        <NavSecondary items={navSecondary} className="mt-auto" />
      </SidebarContent>
      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
    </Sidebar>
  )
}
