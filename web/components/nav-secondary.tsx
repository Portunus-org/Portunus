"use client"

import * as React from "react"
import Link from "next/link"
import { usePathname } from "next/navigation"

import { isActivePath, type NavItem } from "@/components/nav-main"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"

export function NavSecondary({
  items,
  ...props
}: {
  /** mark: something behind the item waits for attention; the words say what. */
  items: (NavItem & { external?: boolean; mark?: string })[]
} & React.ComponentPropsWithoutRef<typeof SidebarGroup>) {
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
              {item.mark && (
                // The badge itself lets clicks through to the item; the dot
                // takes the pointer back, to say what it stands for.
                <SidebarMenuBadge>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Link href={item.url} aria-label={item.mark} className="pointer-events-auto flex size-5 items-center justify-center">
                        <span className="size-2 rounded-full bg-primary" />
                      </Link>
                    </TooltipTrigger>
                    <TooltipContent side="right">{item.mark}</TooltipContent>
                  </Tooltip>
                </SidebarMenuBadge>
              )}
            </SidebarMenuItem>
          ))}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}
