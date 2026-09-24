import { SidebarTrigger } from "@/components/ui/sidebar"

export function SiteHeader() {
  return (
    <header className="flex h-(--header-height) shrink-0 items-center border-b px-4 lg:px-6">
      <SidebarTrigger className="-ml-1" />
    </header>
  )
}
