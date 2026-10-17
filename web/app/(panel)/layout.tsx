"use client"

import * as React from "react"
import { usePathname } from "next/navigation"

import { AppSidebar } from "@/components/app-sidebar"
import { SettingsDialog } from "@/components/settings-dialog"
import { SiteHeader } from "@/components/site-header"
import { ScrollArea } from "@/components/ui/scroll-area"
import { SidebarInset, SidebarProvider, useSidebar } from "@/components/ui/sidebar"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"
import { SessionGate } from "@/lib/session"
import { SettingsWindowProvider } from "@/lib/settings-dialog"

const titles: [string, MessageKey][] = [
  ["/clients/", "clients.title"],
  ["/inbounds/new/", "wizard.title"],
  ["/inbounds/", "inbounds.title"],
  ["/nodes/new/", "nodeWizard.title"],
  ["/nodes/", "nodes.title"],
  ["/routing/", "routing.title"],
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
    // The window itself never scrolls: the frame (sidebar, header) stays
    // put and only the page content below the header moves.
    <SidebarProvider
      className="h-svh overflow-hidden"
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 72)",
          "--header-height": "calc(var(--spacing) * 12)",
        } as React.CSSProperties
      }
    >
      <CloseSidebarOnNavigate />
      <AppSidebar variant="inset" />
      <SidebarInset className="min-h-0 overflow-hidden">
        <SiteHeader title={t(title)} />
        {/* A new page starts at its top, with its scrollbar announced anew. */}
        <ScrollArea key={pathname} className="min-h-0 flex-1 [&_[data-slot=scroll-area-viewport]>div]:block!">
          <div className="@container/main flex flex-1 flex-col gap-2">
            <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">{children}</div>
          </div>
        </ScrollArea>
      </SidebarInset>
    </SidebarProvider>
  )
}

export default function PanelLayout({ children }: LayoutProps<"/">) {
  return (
    <SessionGate>
      <SettingsWindowProvider>
        <Shell>{children}</Shell>
        <SettingsDialog />
      </SettingsWindowProvider>
    </SessionGate>
  )
}
