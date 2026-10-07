/**
 * Thin client for the Portunus JSON API. The session lives in an HttpOnly
 * cookie; state-changing requests additionally carry the per-session CSRF
 * token, which the server hands out from /api/auth/me and /api/auth/login.
 */

import type { Translate } from "@/lib/i18n"
import { messages, type MessageKey } from "@/lib/messages"


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

export type TLSMode = "self-signed" | "acme" | "file"

export interface CertificateInfo {
  issuer: string
  notAfter: string
}

export type ACMEChallenge = "http" | "proxy" | "cloudflare"

export type Protocol = "vless" | "hysteria2" | "trojan" | "shadowsocks" | "tuic"

export interface Inbound {
  id: number
  tag: string
  name: string
  protocol: Protocol
  port: number
  enabled: boolean
  realityServer?: string
  realityServerPort?: number
  realityPublicKey?: string
  realityShortId?: string
  serverName?: string
  /** For Hysteria2, TUIC, Trojan. */
  tlsMode?: TLSMode
  certificate?: CertificateInfo
  certificatePath?: string
  keyPath?: string
  strictTls?: boolean
  /** Expiry of the private certificate (issued by the panel's CA). */
  privateCertExpires?: string
  certSha256?: string
  masqueradeUrl?: string
  obfs?: boolean
  method?: string
}

export interface InboundInput {
  protocol?: Protocol
  name: string
  port: number
  enabled: boolean
  realityServer?: string
  realityServerPort?: number
  serverName?: string
  tlsMode?: TLSMode
  certificatePath?: string
  keyPath?: string
  strictTls?: boolean
  masqueradeUrl?: string
  obfs?: boolean
}

/** The editable fields of an inbound, as the API accepts them back. */
export function inboundInput(inb: Inbound): InboundInput {
  return {
    name: inb.name,
    port: inb.port,
    enabled: inb.enabled,
    realityServer: inb.realityServer,
    realityServerPort: inb.realityServerPort,
    serverName: inb.serverName,
    tlsMode: inb.tlsMode,
    certificatePath: inb.certificatePath,
    keyPath: inb.keyPath,
    strictTls: inb.strictTls,
    masqueradeUrl: inb.masqueradeUrl,
    obfs: inb.obfs,
  }
}

export type ClientStatus = "active" | "disabled" | "expired" | "limited"

export interface Client {
  id: number
  name: string
  enabled: boolean
  status: ClientStatus
  trafficLimit: number
  upload: number
  download: number
  expiresAt: string | null
  note: string
  createdAt: string
}

export interface ClientInput {
  name: string
  enabled: boolean
  trafficLimit: number
  expiresAt: string | null
  note: string
}

export interface ClientAccess {
  publicHost: string
  subscriptionUrl?: string
  subscriptionQr?: string
  links: {
    inboundId: number
    label: string
    /** The inbound's own name, "" when unnamed. */
    name: string
    port: number
    protocol: Protocol
    url: string
    qr: string
  }[]
}

export interface Settings {
  publicHost: string
  domain: string
  subscriptionBaseUrl: string
  subscriptionListen: string
  subscriptionPort: number
  /** Effective origin of subscription links ("" when off). */
  subscriptionUrl: string
  subscriptionError?: string
  acmeEmail: string
  acmeChallenge: ACMEChallenge
  acmeHttpPort: number
  cloudflareTokenSet: boolean
}

export interface SettingsInput {
  publicHost: string
  subscriptionBaseUrl: string
  domain?: string
  subscriptionPort?: number
  acmeEmail?: string
  acmeChallenge?: ACMEChallenge
  acmeHttpPort?: number
  /** Omit to keep the stored token. */
  cloudflareToken?: string
}

export interface SingBoxInfo {
  status: {
    state: "stopped" | "starting" | "running" | "failed"
    version: string
    pid?: number
    startedAt?: string
    restarts: number
    lastError?: string
    configAppliedAt?: string
  }
  /** 0 unless running. */
  uptimeSeconds: number
  applyError: string
}

export interface PublicAddress {
  ipv4?: string
  ipv6?: string
  source: "interface" | "lookup" | ""
}

export interface DomainCheck {
  domain: string
  addresses: string[]
  pointsHere: boolean
}

export interface RealityProbe {
  host: string
  ok: boolean
  rttMs?: number
  reason?: string
}

export interface CertificatePlan {
  recommended: "acme" | "self-signed"
  challenge: ACMEChallenge
  challengeSaved: boolean
  httpServer80: string
  proxyPort: number
  cloudflareTokenSet: boolean
}

export interface InboundSuggestion {
  port: number
  preferredPortBusy: boolean
  reality?: RealityProbe[]
  masquerade?: string
  domain: string
  domainCheck?: DomainCheck
  certificate?: CertificatePlan
  publicHost: string
}

export interface CAInfo {
  subject: string
  notAfter: string
  /** SHA-256 of the certificate, to compare on the device. */
  fingerprint: string
  /** Public download link (through the subscription listener), if it is on. */
  downloadUrl?: string
  downloadQr?: string
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    public retryAfter?: number,
    /** For validation errors: the offending field and a machine code. */
    public field?: string,
    public detail?: string
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
    let field: string | undefined
    let detail: string | undefined
    try {
      const data = (await res.json()) as { error?: string; field?: string; code?: string; message?: string }
      if (data.error) code = data.error
      field = data.field
      detail = data.code ?? data.message
    } catch {
      // non-JSON error body
    }
    const retry = Number(res.headers.get("Retry-After")) || undefined
    throw new ApiError(res.status, code, retry, field, detail)
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

  inbounds: () => request<Inbound[]>("GET", "/api/inbounds"),
  createInbound: (input: InboundInput) => request<Inbound>("POST", "/api/inbounds", input),
  updateInbound: (id: number, input: InboundInput) => request<Inbound>("PUT", `/api/inbounds/${id}`, input),
  deleteInbound: (id: number) => request<void>("DELETE", `/api/inbounds/${id}`),
  rotateInboundKeys: (id: number) => request<Inbound>("POST", `/api/inbounds/${id}/rotate-keys`),

  clients: () => request<Client[]>("GET", "/api/clients"),
  createClient: (input: ClientInput) => request<Client>("POST", "/api/clients", input),
  updateClient: (id: number, input: ClientInput) => request<Client>("PUT", `/api/clients/${id}`, input),
  deleteClient: (id: number) => request<void>("DELETE", `/api/clients/${id}`),
  rotateClient: (id: number) => request<Client>("POST", `/api/clients/${id}/rotate`),
  resetClientTraffic: (id: number) => request<Client>("POST", `/api/clients/${id}/reset-traffic`),
  clientAccess: (id: number) => request<ClientAccess>("GET", `/api/clients/${id}/access`),

  settings: () => request<Settings>("GET", "/api/settings"),
  updateSettings: (input: SettingsInput) =>
    request<Settings>("PUT", "/api/settings", input),

  setupState: () => request<{ completed: boolean }>("GET", "/api/setup"),
  setupAddress: () =>
    request<{ detected: PublicAddress; publicHost: string; domain: string; error?: string }>("GET", "/api/setup/address"),
  setupBasics: (publicHost: string, domain: string) => request<void>("POST", "/api/setup/basics", { publicHost, domain }),
  setupSkip: () => request<void>("POST", "/api/setup/skip"),

  suggestInbound: (protocol: Protocol, domain = "") =>
    request<InboundSuggestion>("POST", "/api/inbounds/suggest", { protocol, domain }),
  checkDomain: (domain: string) => request<DomainCheck>("POST", "/api/domain/check", { domain }),
  verifyProxy: (domain: string, port: number) =>
    request<{ ok: boolean; message?: string }>("POST", "/api/acme/verify-proxy", { domain, port }),

  singbox: () => request<SingBoxInfo>("GET", "/api/singbox"),
  ca: () => request<CAInfo>("GET", "/api/ca"),
  /** Download URLs of the private CA certificate. */
  caCertificateUrl: (format: "pem" | "der" = "pem") => (format === "der" ? "/api/ca.crt?format=der" : "/api/ca.crt"),
  restartSingbox: () => request<void>("POST", "/api/singbox/restart"),
  singboxLogs: () => request<{ lines: string[] }>("GET", "/api/singbox/logs"),
  singboxConfig: () => request<{ config: unknown }>("GET", "/api/singbox/config"),
}

/** A readable sentence for an API error, for toasts and inline messages. */
export function errorText(err: unknown, t: Translate): string {
  if (!(err instanceof ApiError)) return t("error.network")
  if (err.code === "invalid") {
    const key = `error.${err.detail}` as MessageKey
    return key in messages.en ? t(key) : t("error.invalid")
  }
  const key = `error.${err.code}` as MessageKey
  if (key in messages.en) return t(key)
  return err.detail ?? t("error.generic")
}
