/**
 * Thin client for the Portunus JSON API. The session lives in an HttpOnly
 * cookie; state-changing requests additionally carry the per-session CSRF
 * token, which the server hands out from /api/auth/me and /api/auth/login.
 */

export type Role = "admin" | "operator" | "viewer"

export interface User {
  id: number
  username: string
  role: Role
  mustChangePassword: boolean
}

export interface SessionInfo {
  user: User
  csrfToken: string
  expiresAt: string
}

export interface SystemInfo {
  version: string
  commit: string
  goVersion: string
  startedAt: string
  uptimeSeconds: number
  memory: {
    rssBytes: number
    heapBytes: number
    sysBytes: number
    goroutines: number
  }
  database: { sizeBytes: number }
  listen: string
  listenLocal: boolean
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    public retryAfter?: number
  ) {
    super(code)
  }
}

let csrfToken = ""

export function setCsrfToken(token: string) {
  csrfToken = token
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown
): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers["Content-Type"] = "application/json"
  if (method !== "GET" && csrfToken) headers["X-Portunus-CSRF"] = csrfToken

  const res = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
    cache: "no-store",
  })

  if (!res.ok) {
    let code = "http_" + res.status
    try {
      const data = (await res.json()) as { error?: string }
      if (data.error) code = data.error
    } catch {
      // non-JSON error body
    }
    const retry = Number(res.headers.get("Retry-After")) || undefined
    throw new ApiError(res.status, code, retry)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  me: () => request<SessionInfo>("GET", "/api/auth/me"),
  login: (username: string, password: string) =>
    request<SessionInfo>("POST", "/api/auth/login", { username, password }),
  logout: () => request<void>("POST", "/api/auth/logout"),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<void>("POST", "/api/auth/password", {
      currentPassword,
      newPassword,
    }),
  system: () => request<SystemInfo>("GET", "/api/system"),
}
