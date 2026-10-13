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
  /** The exit the client is bound to; null while there are no exits. */
  exitId: number | null
  createdAt: string
  /** Connected and passing traffic right now, at these speeds in bytes per second. */
  online: boolean
  downRate: number
  upRate: number
  /** When traffic of the client was last counted; null: never. */
  lastSeenAt: string | null
}

/** Traffic of all clients together within one hour, which starts at `at`. */
export interface TrafficPoint {
  at: string
  upload: number
  download: number
}

export interface ClientInput {
  name: string
  enabled: boolean
  trafficLimit: number
  expiresAt: string | null
  note: string
  /** Left out: keep the exit. 0: have one chosen anew. */
  exitId?: number
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
    /** The server the link leads to: the main one or a backup entry. */
    host: string
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

/** How the panel is opened by its domain. */
export interface PanelAccess {
  enabled: boolean
  domain: string
  port: number
  mode: "acme" | "own"
  /** A certificate of the operator's own is stored. */
  hasCertificate: boolean
  /** The secret entry address ("" when off). */
  url: string
  /** The site shown to everybody who has not opened that address. */
  decoy: string
  /** A VLESS connection is on the same port and passes browsers on. */
  shared: boolean
  error?: string
}

export interface PanelAccessInput {
  enabled: boolean
  port?: number
  mode?: "acme" | "own"
  certificate?: string
  key?: string
  decoy?: string
}

export interface PanelAccessCheck {
  ok: boolean
  issuer?: string
  notAfter?: string
  /** A browser accepts the certificate without a warning. */
  trusted?: boolean
  error?: string
  /** Waiting will not help: something has to be changed first. */
  final?: boolean
  /** Nothing is wrong that the server knows of: the certificate is not there yet. */
  waiting?: boolean
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
  /**
   * The panel's domain, when the connection can share the panel's port and
   * present it: the first, recommended entry of `reality`. A connection
   * that presents another site goes on `otherPort` instead.
   */
  sharedWith?: string
  otherPort?: number
}

/** Where a server is, for the map. */
export interface GeoLocation {
  lat: number
  lon: number
  city?: string
  country?: string
  countryCode?: string
  /** Placed by hand rather than looked up. */
  manual?: boolean
}

export type NodeKind = "agent" | "link"
export type NodeState = "online" | "offline" | "pending" | "rejected"

/** Another server of the installation, or a proxy imported from a link. */
export interface Node {
  id: number
  name: string
  kind: NodeKind
  address: string
  /** Traffic can leave through it. */
  isExit: boolean
  /** Clients can connect through it when the main server is down. */
  isEntry: boolean
  enabled: boolean
  agentPort?: number
  hopPort?: number
  /** The server has answered the panel at least once. */
  joined: boolean
  /** Keeps a sealed copy of the panel. */
  standby: boolean
  protocol?: string
  port?: number
  location?: GeoLocation
  createdAt: string
  status: {
    state: NodeState
    error?: string
    checkedAt?: string
    version?: string
    singBox?: string
    /** Time of a small request through the exit, from the main server. */
    latencyMs?: number
    /** When the node last received a copy of the panel. */
    copyAt?: string
    /** The node still shows a certificate of an earlier version: its join command should be run again. */
    rejoin?: boolean
    /** The node has an updater next to it and follows the panel's version by itself. */
    updater?: boolean
    updating?: boolean
    updateError?: string
  }
  /** Clients bound to this exit. */
  clients: number
}

/** What the map component has to know about the tiles the panel serves. */
export interface MapStyle {
  /** The provider has a light and a dark version of its own. */
  themed: boolean
  /** Place names come as a separate layer. */
  labels: boolean
  /** The drawn map can be zoomed in to towns, from vector tiles. */
  detail: boolean
  credit: string
}

export interface NodeInput {
  name: string
  kind?: NodeKind
  address?: string
  isExit?: boolean
  isEntry?: boolean
  enabled?: boolean
  hopPort?: number
  agentPort?: number
  link?: string
  standby?: boolean
}

/** What to run on a new server. */
export interface NodeJoin {
  image: string
  /** Set when the image is a local build that has to be copied over first. */
  copyImage?: string
  run: string
  /** For a server that has no Docker yet. */
  installDocker: string
  agentPort: number
  hopPort?: number
}

export type RouteAction = "direct" | "block" | "auto" | "node"

export interface RouteTarget {
  action: RouteAction
  nodeId?: number
}

export interface RouteList {
  id: number
  name: string
  kind: "manual" | "url"
  url?: string
  /** "ruleset" for a sing-box rule-set, "text" for a plain list. */
  format: "text" | "ruleset"
  /** Typed lists only. */
  content?: string
  domains: number
  addresses: number
  /** Lines that could not be read. */
  skipped: number
  fetchedAt?: string
  fetchError?: string
}

export interface RouteListInput {
  name: string
  kind: "manual" | "url"
  url?: string
  content?: string
}

export interface RouteRule extends RouteTarget {
  id: number
  listId: number
  enabled: boolean
}

export interface Routing {
  default: RouteTarget
  rules: RouteRule[]
  lists: RouteList[]
}

/** Whether a release newer than the running one is out. */
export interface UpdateInfo {
  /** The running version; "dev" for a build that is not a release. */
  current: string
  /** The newest release published, once the panel has learnt it. */
  latest?: string
  available: boolean
  checkedAt?: string
  /** Why the last check failed. */
  error?: string
  /** The check is turned off on the server. */
  off?: boolean
  /** Releases install themselves, on weekday (0 is Sunday, -1 any day) at minute of the day, both in UTC. */
  auto: boolean
  weekday: number
  minute: number
  /** The moment chosen for the release that waits. */
  planned?: string
  /** The container that installs updates is next to the panel. */
  updater: boolean
  /** The release being installed right now. */
  installing?: string
  installError?: string
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

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
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
  login: (username: string, password: string) => request<SessionInfo>("POST", "/api/auth/login", { username, password }),
  logout: () => request<void>("POST", "/api/auth/logout"),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<void>("POST", "/api/auth/password", {
      currentPassword,
      newPassword,
    }),
  system: () => request<SystemInfo>("GET", "/api/system"),
  /** What the panel last learnt about newer releases. */
  update: () => request<UpdateInfo>("GET", "/api/update"),
  /** Asks the registry now. */
  checkUpdate: () => request<UpdateInfo>("POST", "/api/update/check"),
  saveUpdateSchedule: (body: { auto: boolean; weekday: number; minute: number }) => request<UpdateInfo>("PUT", "/api/update", body),
  /** Sets the moment the waiting release is installed at (an ISO time); null takes it back. */
  planUpdate: (at: string | null) => request<UpdateInfo>("PUT", "/api/update/plan", { at }),
  /** Asks the updater next to the panel to install the newest release. */
  installUpdate: () => request<UpdateInfo>("POST", "/api/update/install"),
  traffic: () => request<TrafficPoint[]>("GET", "/api/traffic"),

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
  updateSettings: (input: SettingsInput) => request<Settings>("PUT", "/api/settings", input),

  setupState: () => request<{ completed: boolean; guide?: boolean }>("GET", "/api/setup"),
  setupAddress: () => request<{ detected: PublicAddress; publicHost: string; domain: string; error?: string }>("GET", "/api/setup/address"),
  panelAccess: () => request<PanelAccess>("GET", "/api/panel-access"),
  setPanelAccess: (input: PanelAccessInput) => request<PanelAccess>("PUT", "/api/panel-access", input),
  /** Connects to the panel's HTTPS port from the server itself; with Let's Encrypt this obtains the certificate. */
  checkPanelAccess: () => request<PanelAccessCheck>("POST", "/api/panel-access/check"),
  rotatePanelGate: () => request<PanelAccess>("POST", "/api/panel-access/rotate"),
  /**
   * Whether this browser reaches the panel by its domain: fetches a one-off
   * address on the HTTPS port. A fresh DNS record may be visible to the
   * server and to Let's Encrypt and not yet to the operator's own computer.
   */
  browserReachesPanel: async (): Promise<boolean> => {
    const probe = await request<{ url: string; expect: string }>("POST", "/api/panel-access/probe")
    try {
      const res = await fetch(probe.url, { cache: "no-store", credentials: "omit" })
      return res.ok && (await res.text()) === probe.expect
    } catch {
      return false
    }
  },
  setupBasics: (publicHost: string, domain: string) => request<void>("POST", "/api/setup/basics", { publicHost, domain }),
  setupSkip: () => request<void>("POST", "/api/setup/skip"),
  /** The rest of the first setup (a connection, a client) is over: walked or declined. */
  guideDone: () => request<void>("POST", "/api/setup/guide/done"),

  suggestInbound: (protocol: Protocol, domain = "") => request<InboundSuggestion>("POST", "/api/inbounds/suggest", { protocol, domain }),
  checkDomain: (domain: string) => request<DomainCheck>("POST", "/api/domain/check", { domain }),
  verifyProxy: (domain: string, port: number) =>
    request<{ ok: boolean; message?: string }>("POST", "/api/acme/verify-proxy", { domain, port }),

  nodes: () => request<Node[]>("GET", "/api/nodes"),
  /** Contacts every node now and returns the fresh list. */
  checkNodes: () => request<Node[]>("POST", "/api/nodes/check"),
  createNode: (input: NodeInput) => request<Node>("POST", "/api/nodes", input),
  updateNode: (id: number, input: NodeInput) => request<Node>("PUT", `/api/nodes/${id}`, input),
  deleteNode: (id: number) => request<void>("DELETE", `/api/nodes/${id}`),
  /** The key that opens the copies of the panel kept on nodes. */
  recovery: () => request<{ key: string; commands: string[] }>("GET", "/api/recovery"),
  nodeJoin: (id: number) => request<NodeJoin>("GET", `/api/nodes/${id}/join`),

  /** The main server, for the map of nodes. */
  topology: () => request<{ main: { host: string; location: GeoLocation | null }; tiles: boolean; map: MapStyle }>("GET", "/api/topology"),
  /** Places a server on the map by hand; null returns to automatic lookup. */
  setNodeLocation: (id: number, location: { lat: number; lon: number } | null) =>
    request<void>("PUT", `/api/nodes/${id}/location`, { location }),
  setServerLocation: (location: { lat: number; lon: number } | null) => request<void>("PUT", "/api/topology/location", { location }),

  routing: () => request<Routing>("GET", "/api/routing"),
  setDefaultRoute: (target: RouteTarget) => request<void>("PUT", "/api/routing/default", target),
  createRouteList: (input: RouteListInput) => request<RouteList>("POST", "/api/routing/lists", input),
  updateRouteList: (id: number, input: RouteListInput) => request<RouteList>("PUT", `/api/routing/lists/${id}`, input),
  deleteRouteList: (id: number) => request<void>("DELETE", `/api/routing/lists/${id}`),
  refreshRouteList: (id: number) => request<RouteList>("POST", `/api/routing/lists/${id}/refresh`),
  createRouteRule: (input: RouteTarget & { listId: number; enabled?: boolean }) => request<RouteRule>("POST", "/api/routing/rules", input),
  updateRouteRule: (id: number, input: RouteTarget & { listId: number; enabled: boolean }) =>
    request<void>("PUT", `/api/routing/rules/${id}`, input),
  deleteRouteRule: (id: number) => request<void>("DELETE", `/api/routing/rules/${id}`),
  orderRouteRules: (ids: number[]) => request<void>("PUT", "/api/routing/rules/order", { ids }),

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
