"use client"

import * as React from "react"
import { usePathname } from "next/navigation"

/**
 * The settings are a window over whatever page is open, not a page of
 * their own: opened from the menu, closed with its cross or Esc, and the
 * page behind is where it was. Which section it shows is in the address
 * (#settings/updates), so a link, or an old bookmark of /settings/, opens
 * the window at the section it is about.
 */
interface SettingsWindow {
  open: boolean
  section: string | null
  openSettings: (section?: string) => void
  setSection: (section: string) => void
  close: () => void
}

const Ctx = React.createContext<SettingsWindow | null>(null)

const prefix = "#settings"

function fromAddress(): { open: boolean; section: string | null } {
  const hash = typeof window === "undefined" ? "" : window.location.hash
  if (!hash.startsWith(prefix)) return { open: false, section: null }
  const rest = hash.slice(prefix.length).replace(/^\//, "")
  return { open: true, section: rest || null }
}

function writeAddress(section: string | null, open: boolean) {
  const base = window.location.pathname + window.location.search
  window.history.replaceState(window.history.state, "", open ? `${base}${prefix}${section ? "/" + section : ""}` : base)
}

export function SettingsWindowProvider({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const [state, setState] = React.useState<{ open: boolean; section: string | null }>({ open: false, section: null })

  // The address decides when the page loads and when a link to
  // #settings/… is followed.
  React.useEffect(() => {
    const follow = () => setState(fromAddress())
    follow()
    window.addEventListener("hashchange", follow)
    return () => window.removeEventListener("hashchange", follow)
  }, [])

  // A page that arrives late, after the gear was pressed on the way to it,
  // writes its own address over the one of the window: the window stays as
  // it is and puts its section back into the address. (Nothing inside the
  // window leads to another page, so a change of page never closes it.)
  const current = React.useRef(state)
  React.useEffect(() => {
    current.current = state
  }, [state])
  React.useEffect(() => {
    // An old address of the settings (/settings/, /users/) comes here as a
    // change of page to #settings/…, which is no hashchange.
    if (window.location.hash.startsWith(prefix)) {
      const frame = requestAnimationFrame(() => setState(fromAddress()))
      return () => cancelAnimationFrame(frame)
    }
    const { open, section } = current.current
    if (open) writeAddress(section, true)
  }, [pathname])

  const value = React.useMemo<SettingsWindow>(
    () => ({
      ...state,
      openSettings: (section) => {
        setState({ open: true, section: section ?? null })
        writeAddress(section ?? null, true)
      },
      setSection: (section) => {
        setState({ open: true, section })
        writeAddress(section, true)
      },
      close: () => {
        setState((s) => ({ ...s, open: false }))
        writeAddress(null, false)
      },
    }),
    [state]
  )
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

// Outside the panel (the welcome pages reuse some of its parts) there is
// no window to open here: the panel is opened with it instead.
const elsewhere: SettingsWindow = {
  open: false,
  section: null,
  openSettings: (section) => window.location.assign("/" + prefix + (section ? "/" + section : "")),
  setSection: (section) => window.location.assign("/" + prefix + "/" + section),
  close: () => {},
}

export function useSettingsWindow(): SettingsWindow {
  return React.useContext(Ctx) ?? elsewhere
}
