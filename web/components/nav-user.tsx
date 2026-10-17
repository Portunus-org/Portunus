"use client"

import Link from "next/link"
import { useTheme } from "next-themes"
import * as React from "react"
import { CircleHelpIcon, CircleUserRoundIcon, LanguagesIcon, LogOutIcon, PaletteIcon, Settings2Icon } from "lucide-react"

import { updateChecked } from "@/components/update-card"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem, useSidebar } from "@/components/ui/sidebar"
import { useI18n } from "@/lib/i18n"
import { localeCodes, localeNames, type Locale } from "@/lib/messages"
import { useSession } from "@/lib/session"
import { api } from "@/lib/api"
import { useSettingsWindow } from "@/lib/settings-dialog"
import { useResource } from "@/lib/use-resource"

export function NavUser() {
  const { setOpenMobile } = useSidebar()
  const win = useSettingsWindow()
  // On a phone the menu is a panel of its own over the page: it is put
  // away, so that the window is the only thing open.
  const openSettings = (section?: string) => {
    setOpenMobile(false)
    win.openSettings(section)
  }
  // A newer release is marked on the gear, the way to where it is installed.
  const { data: update, reload: reloadUpdate } = useResource(api.update, 30 * 60 * 1000)
  React.useEffect(() => {
    window.addEventListener(updateChecked, reloadUpdate)
    return () => window.removeEventListener(updateChecked, reloadUpdate)
  }, [reloadUpdate])
  const { user, logout } = useSession()
  const { t, locale, setLocale } = useI18n()
  const gearLabel = update?.available ? `${t("nav.settings")}: ${t("update.tooltip").toLowerCase()}` : t("nav.settings")
  const { theme, setTheme } = useTheme()
  const initials = user.username.slice(0, 2).toUpperCase()

  const identity = (
    <>
      <Avatar>
        <AvatarFallback>{initials}</AvatarFallback>
      </Avatar>
      <div className="grid flex-1 text-left text-sm leading-tight">
        <span className="truncate font-medium">{user.username}</span>
        <span className="truncate text-xs text-muted-foreground">{t(`role.${user.role}`)}</span>
      </div>
    </>
  )

  return (
    <SidebarMenu>
      {/* The name is the whole row, the gear included, so the row lights up
          as one under the pointer; the gear lies on top of its right end and
          lights up alone when the pointer is on it. */}
      <SidebarMenuItem className="relative">
        {/* Not modal: with the menu open, a press on the gear opens the
            settings at once instead of only closing the menu. */}
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="pr-12 data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              {identity}
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg"
            side="top"
            align="start"
            sideOffset={4}
          >
            <DropdownMenuLabel className="p-0 font-normal">
              <div className="flex items-center gap-2 px-1 py-1.5 text-left text-sm">{identity}</div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem asChild>
                <Link href="/account/">
                  <CircleUserRoundIcon />
                  {t("nav.account")}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <PaletteIcon />
                  {t("nav.theme")}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent>
                  <DropdownMenuRadioGroup value={theme} onValueChange={setTheme}>
                    <DropdownMenuRadioItem value="system">{t("nav.theme.system")}</DropdownMenuRadioItem>
                    <DropdownMenuRadioItem value="light">{t("nav.theme.light")}</DropdownMenuRadioItem>
                    <DropdownMenuRadioItem value="dark">{t("nav.theme.dark")}</DropdownMenuRadioItem>
                  </DropdownMenuRadioGroup>
                </DropdownMenuSubContent>
              </DropdownMenuSub>
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <LanguagesIcon />
                  {t("nav.language")}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent>
                  <DropdownMenuRadioGroup value={locale} onValueChange={(v) => setLocale(v as Locale)}>
                    {localeCodes.map((l) => (
                      <DropdownMenuRadioItem key={l} value={l}>
                        {localeNames[l]}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => openSettings()}>
              <Settings2Icon />
              {t("nav.settings")}
            </DropdownMenuItem>
            {/* The documentation is help to turn to, not a page of the work. */}
            <DropdownMenuItem asChild>
              <Link href="/docs/">
                <CircleHelpIcon />
                {t("nav.help")}
              </Link>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => logout()}>
              <LogOutIcon />
              {t("nav.signOut")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              className="absolute inset-y-0 right-2 my-auto text-sidebar-foreground active:not-aria-[haspopup]:translate-y-0 hover:bg-sidebar-border hover:text-sidebar-accent-foreground dark:hover:bg-sidebar-border"
              aria-label={gearLabel}
              onClick={() => openSettings(update?.available ? "updates" : undefined)}
            >
              <Settings2Icon />
              {update?.available && <span className="absolute -top-0.5 -right-0.5 size-2 rounded-full bg-primary ring-2 ring-sidebar" />}
            </Button>
          </TooltipTrigger>
          <TooltipContent side="top">{gearLabel}</TooltipContent>
        </Tooltip>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}
