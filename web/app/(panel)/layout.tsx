"use client"

import * as React from "react"
import { usePathname } from "next/navigation"

import { AppSidebar } from "@/components/app-sidebar"
import { SiteHeader } from "@/components/site-header"
import { SidebarInset, SidebarProvider, useSidebar } from "@/components/ui/sidebar"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"
import { SessionGate } from "@/lib/session"

const titles: [string, MessageKey][] = [
  ["/clients/", "clients.title"],
  ["/inbounds/new/", "wizard.title"],
  ["/inbounds/", "inbounds.title"],
  ["/settings/", "settings.title"],
  ["/docs/", "docs.title"],
  ["/account/", "account.title"],
]

/** On phones the sidebar is a sheet over the page: close it after navigating. */
function CloseSidebarOnNavigate() {
  const pathname = usePathname()
  const { setOpenMobile } = useSidebar()
  React.useEffect(() => {
    setOpenMobile(false)
  }, [pathname, setOpenMobile])
  return null
}

function Shell({ children }: { children: React.ReactNode }) {
  const { t } = useI18n()
  const pathname = usePathname()
  const p = pathname.endsWith("/") ? pathname : pathname + "/"
  const title = titles.find(([prefix]) => p.startsWith(prefix))?.[1] ?? "overview.title"

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 72)",
          "--header-height": "calc(var(--spacing) * 12)",
        } as React.CSSProperties
      }
    >
      <CloseSidebarOnNavigate />
      <AppSidebar variant="inset" />
      <SidebarInset>
        <SiteHeader title={t(title)} />
        <div className="flex flex-1 flex-col">
          <div className="@container/main flex flex-1 flex-col gap-2">
            <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">{children}</div>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

export default function PanelLayout({ children }: LayoutProps<"/">) {
  return (
    <SessionGate>
      <Shell>{children}</Shell>
    </SessionGate>
  )
}
