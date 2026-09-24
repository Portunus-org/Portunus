"use client"

import * as React from "react"
import { usePathname, useRouter } from "next/navigation"

import { api, ApiError, setCsrfToken, type User } from "@/lib/api"

interface SessionContextValue {
  user: User
  refresh: () => Promise<void>
  logout: () => Promise<void>
}

const SessionContext = React.createContext<SessionContextValue | null>(null)

export function useSession() {
  const ctx = React.useContext(SessionContext)
  if (!ctx) throw new Error("useSession must be used inside <SessionGate>")
  return ctx
}

/**
 * Renders children only for an authenticated user; otherwise sends the
 * browser to /login/. This is a UX guard — the API enforces auth itself.
 */
export function SessionGate({ children }: { children: React.ReactNode }) {
  const router = useRouter()
  const pathname = usePathname()
  const [user, setUser] = React.useState<User | null>(null)

  const load = React.useCallback(
    () =>
      api.me().then(
        (s) => {
          setCsrfToken(s.csrfToken)
          setUser(s.user)
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

  const refresh = load

  const logout = React.useCallback(async () => {
    try {
      await api.logout()
    } finally {
      setCsrfToken("")
      router.replace("/login/")
    }
  }, [router])

  // Mirrors the server rule: a generated password must be replaced first.
  const mustChange = user?.mustChangePassword && pathname !== "/account/"
  React.useEffect(() => {
    if (mustChange) router.replace("/account/")
  }, [mustChange, router])

  const value = React.useMemo(
    () => (user ? { user, refresh, logout } : null),
    [user, refresh, logout]
  )

  if (!value || mustChange) return null
  return <SessionContext value={value}>{children}</SessionContext>
}
