"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

import { api, ApiError, setCsrfToken, type User } from "@/lib/api"

interface SessionContextValue {
  user: User
  setupCompleted: boolean
  /** The first setup is to be continued: a connection and a client. */
  guide: boolean
  refresh: () => Promise<void>
  logout: () => Promise<void>
}

const SessionContext = React.createContext<SessionContextValue | null>(null)

export function useSession() {
  const ctx = React.useContext(SessionContext)
  if (!ctx) throw new Error("useSession must be used inside <SessionGate>")
  return ctx
}

/** Operators and admins may change configuration; viewers only look. */
export function useCanEdit() {
  const { user } = useSession()
  return user.role === "admin" || user.role === "operator"
}

// The password typed on the login page, kept in memory only, so the welcome
// flow can replace a generated password without asking for it again.
let handoffPassword = ""
export function setHandoffPassword(pw: string) {
  handoffPassword = pw
}
export function takeHandoffPassword() {
  const pw = handoffPassword
  handoffPassword = ""
  return pw
}

/**
 * Loads the session and renders children for a signed-in user; otherwise
 * sends the browser to /login/. In "panel" mode it also routes first-time
 * users to /welcome/: to replace a generated password, and admins to run the
 * setup wizard. This is a UX guard; the API enforces access itself.
 */
export function SessionGate({ children, mode = "panel" }: { children: React.ReactNode; mode?: "panel" | "welcome" }) {
  const router = useRouter()
  const [state, setState] = React.useState<{ user: User; setupCompleted: boolean; guide: boolean } | null>(null)

  const load = React.useCallback(
    () =>
      api.me().then(
        async (s) => {
          setCsrfToken(s.csrfToken)
          let setupCompleted = true
          let guide = false
          if (s.user.role === "admin") {
            const setup = await api.setupState().catch(() => ({ completed: true, guide: false }))
            setupCompleted = setup.completed
            guide = !!setup.guide
          }
          setState({ user: s.user, setupCompleted, guide })
        },
        (err) => {
          if (err instanceof ApiError && err.status === 401) router.replace("/login/")
          else throw err
        }
      ),
    [router]
  )

  React.useEffect(() => {
    load()
  }, [load])

  const logout = React.useCallback(async () => {
    try {
      await api.logout()
    } finally {
      setCsrfToken("")
      router.replace("/login/")
    }
  }, [router])

  // Also when the first setup was begun and not ended: it goes on with a
  // connection and a client, wherever the operator signs in next (after the
  // address is saved that is usually the panel's own domain, a new place
  // for the browser).
  const needsWelcome =
    mode === "panel" &&
    !!state &&
    (state.user.mustChangePassword || (state.user.role === "admin" && (!state.setupCompleted || state.guide)))
  React.useEffect(() => {
    if (needsWelcome) router.replace("/welcome/")
  }, [needsWelcome, router])

  const value = React.useMemo(
    () => (state ? { user: state.user, setupCompleted: state.setupCompleted, guide: state.guide, refresh: load, logout } : null),
    [state, load, logout]
  )

  if (!value || needsWelcome) return null
  return <SessionContext value={value}>{children}</SessionContext>
}
