"use client"

import { AppSidebar } from "@/components/app-sidebar"
import { SiteHeader } from "@/components/site-header"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { SessionGate } from "@/lib/session"

export default function PanelLayout({ children }: LayoutProps<"/">) {
  return (
    <SessionGate>
      <SidebarProvider
        style={
          {
            "--sidebar-width": "calc(var(--spacing) * 60)",
            "--header-height": "calc(var(--spacing) * 12)",
          } as React.CSSProperties
        }
      >
        <AppSidebar variant="inset" />
        <SidebarInset>
          <SiteHeader />
          <div className="@container/main flex flex-1 flex-col gap-8 py-6 md:py-8">{children}</div>
        </SidebarInset>
      </SidebarProvider>
    </SessionGate>
  )
}
