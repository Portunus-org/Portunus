"use client"

import * as React from "react"
import Link from "next/link"
import { usePathname } from "next/navigation"

import { isActivePath, type NavItem } from "@/components/nav-main"
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"

export function NavSecondary({
  items,
  ...props
}: { items: (NavItem & { external?: boolean })[] } & React.ComponentPropsWithoutRef<typeof SidebarGroup>) {
  const pathname = usePathname()
  return (
    <SidebarGroup {...props}>
      <SidebarGroupContent>
        <SidebarMenu>
          {items.map((item) => (
            <SidebarMenuItem key={item.url}>
              <SidebarMenuButton asChild isActive={!item.external && isActivePath(pathname, item.url)}>
                {item.external ? (
                  <a href={item.url} target="_blank" rel="noreferrer">
                    {item.icon}
                    <span>{item.title}</span>
                  </a>
                ) : (
                  <Link href={item.url}>
                    {item.icon}
                    <span>{item.title}</span>
                  </Link>
                )}
              </SidebarMenuButton>
            </SidebarMenuItem>
          ))}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}
