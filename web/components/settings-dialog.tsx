"use client"

import * as React from "react"
import {
  ArrowLeftIcon,
  ChevronRightIcon,
  XIcon,
  CpuIcon,
  DownloadIcon,
  InfoIcon,
  GlobeLockIcon,
  KeyRoundIcon,
  RotateCwIcon,
  ScrollTextIcon,
  SearchIcon,
  ServerIcon,
  ShieldCheckIcon,
  UsersRoundIcon,
} from "lucide-react"
import { toast } from "@/lib/toast"

import { AuditCard } from "@/components/audit-card"
import { CAInstall } from "@/components/ca-install"
import { CopyButton } from "@/components/copy-button"
import { PanelAccessCard } from "@/components/panel-access-card"
import { SingboxLogs } from "@/components/singbox-logs"
import { UpdateCard } from "@/components/update-card"
import { AccountsCard } from "@/components/users/accounts-card"
import { SSOCard } from "@/components/users/sso-card"
import { DomainCard } from "@/components/domain-card"
import { AboutCard } from "@/components/about-card"
import { StatusBadge } from "@/components/status"
import { FormField, FormError } from "@/components/form-field"
import { NumberInput } from "@/components/number-input"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Pane,
  PaneAction,
  PaneContent,
  PaneDescription,
  PaneFooter,
  PaneHeader,
  PaneTitle,
  SaidAboveContext,
} from "@/components/settings-pane"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, errorText, type ACMEChallenge, type Settings } from "@/lib/api"
import { formatDuration } from "@/lib/format"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { useIsMobile } from "@/hooks/use-mobile"
import { useI18n } from "@/lib/i18n"
import { FormErrorsContext, useFormErrors } from "@/lib/form-errors"
import type { MessageKey } from "@/lib/messages"
import { useCanEdit, useSession } from "@/lib/session"
import { useResource } from "@/lib/use-resource"
import { cn } from "@/lib/utils"
import { useSettingsWindow } from "@/lib/settings-dialog"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"

/** A settings card whose form saves one part of the settings. */
function SettingsCard({
  title,
  description,
  onSubmit,
  children,
}: {
  title: string
  description: string
  onSubmit: (f: FormData) => Promise<void>
  children: React.ReactNode
}) {
  const { t } = useI18n()
  const { user } = useSession()
  const isAdmin = user.role === "admin"
  const [pending, setPending] = React.useState(false)
  const { errors, clear, fail, numbers } = useFormErrors()

  return (
    <Pane>
      <form
        className="contents"
        noValidate
        onInput={clear}
        onSubmit={async (e) => {
          e.preventDefault()
          const form = e.currentTarget
          if (!numbers(form)) return
          setPending(true)
          clear()
          try {
            await onSubmit(new FormData(form))
            toast.success(t("settings.saved"))
          } catch (err) {
            // The server says which setting is wrong; it is shown at its
            // row when this section has it. The domain is typed as the
            // server's address.
            fail(err, form, { domain: "publicHost" })
          } finally {
            setPending(false)
          }
        }}
      >
        <PaneHeader>
          <PaneTitle>{title}</PaneTitle>
          <PaneDescription>{description}</PaneDescription>
        </PaneHeader>
        <PaneContent>
          <fieldset disabled={!isAdmin} className="grid gap-7">
            <FormErrorsContext.Provider value={errors}>{children}</FormErrorsContext.Provider>
          </fieldset>
          <FormError>{errors.form}</FormError>
        </PaneContent>
        {isAdmin && (
          <PaneFooter className="border-t">
            <Button type="submit" size="sm" disabled={pending}>
              {pending ? t("common.saving") : t("common.save")}
            </Button>
          </PaneFooter>
        )}
      </form>
    </Pane>
  )
}

/**
 * The server as clients know it: the address in their links, and the
 * subscriptions their apps refresh from. The domain and its certificate
 * are a section of their own.
 */
function ServerCard({ settings, onSaved }: { settings: Settings; onSaved: () => void }) {
  const { t } = useI18n()
  return (
    <SettingsCard
      title={t("settings.server.title")}
      description={t("settings.server.description")}
      onSubmit={async (f) => {
        await api.updateSettings({
          publicHost: String(f.get("publicHost") ?? "")
            .trim()
            .toLowerCase(),
          subscriptionBaseUrl: settings.subscriptionBaseUrl,
          ...(settings.subscriptionListen ? {} : { subscriptionPort: Number(f.get("subscriptionPort") || 0) }),
        })
        onSaved()
      }}
    >
      <FormField id="publicHost" label={t("settings.server.publicHost")} hint={t("settings.server.publicHostHint")}>
        <Input
          id="publicHost"
          name="publicHost"
          defaultValue={settings.publicHost}
          placeholder="vpn.example.com"
          autoCapitalize="none"
          spellCheck={false}
        />
      </FormField>
      {settings.subscriptionListen ? (
        <p className="text-sm text-muted-foreground">{t("settings.subs.env", { addr: settings.subscriptionListen })}</p>
      ) : (
        <FormField id="subscriptionPort" label={t("settings.subs.port")} hint={t("settings.subs.portHint")}>
          <NumberInput
            id="subscriptionPort"
            name="subscriptionPort"
            min={0}
            max={65535}
            className="max-w-32 tabular-nums"
            defaultValue={settings.subscriptionPort}
          />
        </FormField>
      )}
      {settings.subscriptionUrl && (
        <p className="text-sm text-muted-foreground">
          {t("settings.subs.effective", { url: "" })}
          <span className="font-mono text-foreground">{settings.subscriptionUrl}</span>
        </p>
      )}
      {settings.subscriptionError && (
        <Alert variant="destructive">
          <AlertDescription className="font-mono text-xs">{settings.subscriptionError}</AlertDescription>
        </Alert>
      )}
    </SettingsCard>
  )
}

const challenges: {
  value: ACMEChallenge
  label: MessageKey
  hint: MessageKey
}[] = [
  {
    value: "http",
    label: "settings.certs.http",
    hint: "settings.certs.httpHint",
  },
  {
    value: "proxy",
    label: "settings.certs.proxy",
    hint: "settings.certs.proxyHint",
  },
  {
    value: "cloudflare",
    label: "settings.certs.cloudflare",
    hint: "settings.certs.cloudflareHint",
  },
]

function nginxSnippet(port: number) {
  return `location /.well-known/acme-challenge/ {
    proxy_pass http://127.0.0.1:${port};
    proxy_set_header Host $host;
}`
}

function CertificatesCard({ settings, onSaved }: { settings: Settings; onSaved: () => void }) {
  const { t } = useI18n()
  const [challenge, setChallenge] = React.useState<ACMEChallenge>(settings.acmeChallenge)
  const [port, setPort] = React.useState(settings.acmeHttpPort)
  const current = challenges.find((c) => c.value === challenge)!

  return (
    <SettingsCard
      title={t("settings.certs.title")}
      description={t("settings.certs.description")}
      onSubmit={async (f) => {
        const token = String(f.get("cloudflareToken") ?? "")
        await api.updateSettings({
          publicHost: settings.publicHost,
          subscriptionBaseUrl: settings.subscriptionBaseUrl,
          acmeEmail: String(f.get("acmeEmail") ?? ""),
          acmeChallenge: challenge,
          acmeHttpPort: port,
          cloudflareToken: token || undefined,
        })
        onSaved()
      }}
    >
      <FormField id="acmeChallenge" label={t("settings.certs.challenge")} hint={t(current.hint)}>
        <Select value={challenge} onValueChange={(v) => setChallenge(v as ACMEChallenge)}>
          <SelectTrigger id="acmeChallenge" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {challenges.map((c) => (
                <SelectItem key={c.value} value={c.value}>
                  {t(c.label)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </FormField>

      {challenge === "proxy" && (
        <>
          <FormField id="acmeHttpPort" label={t("settings.certs.localPort")}>
            <NumberInput
              id="acmeHttpPort"
              min={1024}
              max={65535}
              className="max-w-32 tabular-nums"
              value={port}
              onChange={(e) => setPort(Number(e.target.value))}
            />
          </FormField>
          <div className="grid gap-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">{t("settings.certs.nginx")}</span>
              <CopyButton value={nginxSnippet(port)} />
            </div>
            <pre className="overflow-x-auto rounded-lg bg-muted p-4 font-mono text-xs leading-relaxed">{nginxSnippet(port)}</pre>
            <p className="text-sm text-muted-foreground">{t("settings.certs.npmHint", { port })}</p>
          </div>
        </>
      )}

      {challenge === "cloudflare" && (
        <FormField id="cloudflareToken" label={t("settings.certs.token")} hint={t("settings.certs.tokenHint")}>
          <Input
            id="cloudflareToken"
            name="cloudflareToken"
            type="password"
            autoComplete="off"
            placeholder={settings.cloudflareTokenSet ? t("settings.certs.tokenStored") : ""}
          />
        </FormField>
      )}

      <FormField id="acmeEmail" label={t("settings.certs.email")} hint={t("settings.certs.emailHint")}>
        <Input id="acmeEmail" name="acmeEmail" type="email" defaultValue={settings.acmeEmail} placeholder={t("settings.certs.optional")} />
      </FormField>
    </SettingsCard>
  )
}

function SingBoxCard() {
  const { t, locale } = useI18n()
  const { user } = useSession()
  const canEdit = useCanEdit()
  const isAdmin = user.role === "admin"
  const { data, reload } = useResource(api.singbox, 5_000)
  const { data: logs, reload: reloadLogs } = useResource(() => (isAdmin ? api.singboxLogs() : Promise.resolve({ lines: [] })), 5_000)
  const { data: cfg, reload: reloadCfg } = useResource(() => (isAdmin ? api.singboxConfig() : Promise.resolve({ config: null })))
  const [restarting, setRestarting] = React.useState(false)

  const s = data?.status
  const problem = data?.applyError || (s?.state === "failed" ? s.lastError : "")

  async function restart() {
    setRestarting(true)
    try {
      await api.restartSingbox()
      toast.success(t("settings.singbox.restarted"))
      reload()
      reloadLogs()
    } catch (err) {
      toast.error(errorText(err, t))
    } finally {
      setRestarting(false)
    }
  }

  const rows: [string, React.ReactNode][] = [
    [
      t("settings.singbox.state"),
      s ? (
        <StatusBadge tone={s.state === "running" ? "ok" : s.state === "failed" ? "error" : s.state === "starting" ? "pending" : "off"}>
          {t(`state.${s.state}`)}
        </StatusBadge>
      ) : (
        <Skeleton className="h-5 w-20" />
      ),
    ],
    ...(s?.pid
      ? ([
          [
            t("settings.singbox.pid"),
            <span key="p" className="font-mono">
              {s.pid}
            </span>,
          ],
        ] as [string, React.ReactNode][])
      : []),
    ...(s?.state === "running" && data
      ? ([[t("settings.singbox.uptime"), formatDuration(data.uptimeSeconds, locale)]] as [string, React.ReactNode][])
      : []),
    [
      t("settings.singbox.restarts"),
      <span key="r" className="tabular-nums">
        {s?.restarts ?? 0}
      </span>,
    ],
  ]

  return (
    <Pane>
      <PaneHeader>
        <PaneTitle>{t("settings.singbox.title")}</PaneTitle>
        <PaneDescription>{t("settings.singbox.description")}</PaneDescription>
        {canEdit && (
          <PaneAction>
            <Button
              data-setting="singbox-restart"
              variant="outline"
              size="sm"
              onClick={restart}
              disabled={restarting || s?.state === "stopped"}
            >
              <RotateCwIcon />
              {t("settings.singbox.restart")}
            </Button>
          </PaneAction>
        )}
      </PaneHeader>
      <PaneContent className="grid gap-6">
        <dl
          data-setting="singbox-state"
          className="grid grid-cols-[10rem_1fr] items-center gap-y-3 text-sm md:grid-cols-[minmax(0,1fr)_auto]"
        >
          {rows.map(([k, v]) => (
            <React.Fragment key={k}>
              <dt className="font-medium">{k}</dt>
              <dd className="text-muted-foreground md:text-right">{v}</dd>
            </React.Fragment>
          ))}
        </dl>

        {problem && (
          <Alert variant="destructive">
            <AlertDescription className="font-mono text-xs break-all whitespace-pre-wrap">{problem}</AlertDescription>
          </Alert>
        )}

        {isAdmin && (
          <Tabs defaultValue="logs" onValueChange={(v) => (v === "config" ? reloadCfg() : reloadLogs())}>
            <TabsList>
              <TabsTrigger value="logs" data-setting="singbox-logs">
                {t("settings.singbox.logs")}
              </TabsTrigger>
              <TabsTrigger value="config" data-setting="singbox-config">
                {t("settings.singbox.config")}
              </TabsTrigger>
            </TabsList>
            <TabsContent value="logs">
              <SingboxLogs lines={logs?.lines ?? []} empty={t("settings.singbox.noOutput")} />
            </TabsContent>
            <TabsContent value="config">
              <ScrollArea className="h-72 rounded-lg bg-muted">
                <pre className="p-4 font-mono text-xs leading-relaxed">
                  {cfg?.config ? JSON.stringify(cfg.config, null, 2) : t("settings.singbox.notRunning")}
                </pre>
                <ScrollBar orientation="horizontal" />
              </ScrollArea>
              <p className="mt-2 text-sm text-muted-foreground">{t("settings.singbox.masked")}</p>
            </TabsContent>
          </Tabs>
        )}
      </PaneContent>
    </Pane>
  )
}

function CACard() {
  const { t } = useI18n()
  return (
    <Pane>
      <PaneHeader>
        <PaneTitle>{t("settings.ca.title")}</PaneTitle>
        <PaneDescription>{t("settings.ca.description")}</PaneDescription>
      </PaneHeader>
      <PaneContent>
        <CAInstall compact />
      </PaneContent>
    </Pane>
  )
}

type SectionId = "server" | "domain" | "access" | "accounts" | "sso" | "audit" | "updates" | "singbox" | "about"

// Sections that were merged into others, for links to them.
const mergedSections: Record<string, SectionId> = { subscriptions: "server", certificates: "domain", ca: "domain" }

interface Section {
  id: SectionId
  icon: React.ReactNode
  title: MessageKey
  description: MessageKey
  /**
   * The settings in it, for the search: what to point at in the section
   * (the id of a field, or a data-setting mark) and what it is called.
   */
  settings: [string, MessageKey][]
  adminOnly?: boolean
}

const sections: Section[] = [
  {
    id: "server",
    icon: <ServerIcon />,
    title: "settings.server.title",
    description: "settings.server.description",
    settings: [
      ["publicHost", "settings.server.publicHost"],
      ["subscriptionPort", "settings.subs.port"],
    ],
  },
  {
    id: "domain",
    icon: <ShieldCheckIcon />,
    title: "domain.section",
    description: "domain.description",
    settings: [
      ["domain-current", "domain.current"],
      ["domain", "domain.new"],
      ["domain-others", "domain.others"],
      ["acmeChallenge", "settings.certs.challenge"],
      ["acmeHttpPort", "settings.certs.localPort"],
      ["cloudflareToken", "settings.certs.token"],
      ["acmeEmail", "settings.certs.email"],
      ["ca-download", "ca.download"],
      ["ca-fingerprint", "ca.fingerprint"],
    ],
  },
  {
    id: "access",
    icon: <GlobeLockIcon />,
    title: "access.title",
    description: "access.description",
    settings: [
      ["panel-access", "access.title"],
      ["access-port", "access.port"],
      ["certificate", "access.certificate"],
      ["decoy", "access.decoy"],
      ["access-address", "access.address"],
    ],
  },
  {
    id: "accounts",
    icon: <UsersRoundIcon />,
    title: "users.title",
    description: "users.description",
    settings: [
      ["accounts-add", "users.add.title"],
      ["accounts-table", "users.col.role"],
    ],
    adminOnly: true,
  },
  {
    id: "sso",
    icon: <KeyRoundIcon />,
    title: "sso.title",
    description: "sso.description",
    settings: [
      ["sso-enabled", "sso.enabled"],
      ["issuer", "sso.issuer"],
      ["ssoName", "sso.name"],
      ["callback", "sso.callback"],
      ["clientId", "sso.clientId"],
      ["clientSecret", "sso.clientSecret"],
      ["defaultRole", "sso.fallback"],
      ["groupsClaim", "sso.groupsClaim"],
      ["scopes", "sso.scopes"],
    ],
    adminOnly: true,
  },
  {
    id: "audit",
    icon: <ScrollTextIcon />,
    title: "audit.title",
    description: "audit.description",
    settings: [["audit-table", "audit.title"]],
    adminOnly: true,
  },
  {
    id: "updates",
    icon: <DownloadIcon />,
    title: "update.title",
    description: "update.auto",
    settings: [
      ["update-install", "update.installNow"],
      ["update-plan", "update.plan"],
      ["updateAuto", "update.auto"],
    ],
  },
  {
    id: "singbox",
    icon: <CpuIcon />,
    title: "settings.singbox.title",
    description: "settings.singbox.description",
    settings: [
      ["singbox-restart", "settings.singbox.restart"],
      ["singbox-state", "settings.singbox.state"],
      ["singbox-logs", "settings.singbox.logs"],
      ["singbox-config", "settings.singbox.config"],
    ],
  },
  {
    id: "about",
    icon: <InfoIcon />,
    title: "about.title",
    description: "about.tagline",
    settings: [
      ["about-version", "about.version"],
      ["about-code", "about.code"],
      ["about-releases", "about.releases"],
      ["about-issue", "about.issue"],
      ["about-help", "nav.help"],
      ["about-license", "about.license"],
    ],
  },
]

/** The pane of a section while what it shows is on its way: its shape, in shadcn's Skeleton. */
function PaneSkeleton() {
  return (
    <div className="grid gap-8">
      <div className="grid gap-2">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="h-4 w-80 max-w-full" />
      </div>
      {[0, 1, 2].map((i) => (
        <div key={i} className="grid gap-3 md:grid-cols-[minmax(0,1fr)_20rem] md:gap-x-10">
          <div className="grid gap-2">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-4 w-64 max-w-full" />
          </div>
          <Skeleton className="h-9 w-full" />
        </div>
      ))}
    </div>
  )
}

const isSection = (id: string | null): id is SectionId => !!id && sections.some((s) => s.id === id)
const sectionOf = (id: string | null): SectionId => (isSection(id) ? id : (id && mergedSections[id]) || "server")

/**
 * The settings window: the sections on the left, with a search over them,
 * and the chosen one on the right, a setting to a row, as shadcn's Dialog
 * over the page that was open.
 */
export function SettingsDialog() {
  const { t } = useI18n()
  const win = useSettingsWindow()
  const { data: settings, reload } = useResource(api.settings)
  const isAdmin = useSession().user.role === "admin"
  const { data: update, reload: reloadUpdate } = useResource(api.update)
  const [query, setQuery] = React.useState("")
  const current: SectionId = sectionOf(win.section)
  const open = (id: SectionId) => win.setSection(id)

  const allowed = sections.filter((s) => isAdmin || !s.adminOnly)
  const q = query.trim().toLowerCase()
  const found = q
    ? allowed
        .map((s) => ({ section: s, hits: s.settings.filter(([, label]) => t(label).toLowerCase().includes(q)) }))
        .filter(({ section, hits }) => hits.length > 0 || t(section.title).toLowerCase().includes(q))
    : []

  // Going to a setting: its section, then the setting itself brought into
  // view and lit up for a moment, so the eye finds it.
  const [target, setTarget] = React.useState<{ anchor: string; at: number } | null>(null)
  // Each choice is a new one, also of the same setting twice.
  const choices = React.useRef(0)
  const isMobile = useIsMobile()
  const goTo = (id: SectionId, anchor?: string) => {
    open(id)
    // On a phone the results stand where the section is: they give way to it.
    if (isMobile) setQuery("")
    if (anchor) setTarget({ anchor, at: ++choices.current })
  }
  React.useEffect(() => {
    if (!target) return
    let tries = 0
    // The section may still be arriving: look for a moment before giving up.
    const timer = setInterval(() => {
      const el = document.getElementById(target.anchor) ?? document.querySelector(`[data-setting="${target.anchor}"]`)
      if (!el && ++tries < 20) return
      clearInterval(timer)
      if (!el) return
      const row = (el.closest('[data-slot="field"]') ?? el) as HTMLElement
      row.scrollIntoView({ block: "center", behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" })
      row.classList.remove("setting-found")
      void row.offsetWidth // start the animation again if it is the same row
      row.classList.add("setting-found")
      setTimeout(() => row.classList.remove("setting-found"), 2000)
    }, 100)
    return () => clearInterval(timer)
  }, [target])
  const section = allowed.find((s) => s.id === current) ?? allowed[0]

  let body: React.ReactNode
  switch (section.id) {
    case "server":
      body = settings ? (
        <ServerCard key={`server-${settings.publicHost}-${settings.subscriptionPort}`} settings={settings} onSaved={reload} />
      ) : null
      break
    case "domain":
      // The domain with its certificate, then how certificates are
      // obtained, then the authority for connections without a public one.
      body = (
        <div className="grid gap-16">
          <DomainCard />
          {settings && <CertificatesCard key={`certs-${settings.acmeChallenge}`} settings={settings} onSaved={reload} />}
          <CACard />
        </div>
      )
      break
    case "access":
      body = settings ? <PanelAccessCard key={`access-${settings.domain}`} canEdit={isAdmin} /> : null
      break
    case "accounts":
      body = <AccountsCard />
      break
    case "sso":
      body = <SSOCard />
      break
    case "audit":
      body = <AuditCard />
      break
    case "updates":
      body = update ? (
        <UpdateCard
          key={`update-${update.auto}-${update.weekday}-${update.minute}`}
          update={update}
          reload={reloadUpdate}
          canEdit={isAdmin}
        />
      ) : null
      break
    case "singbox":
      body = <SingBoxCard />
      break
    case "about":
      body = <AboutCard />
      break
  }

  // The search and the list of sections (or what the search found): beside
  // the section on a wide screen, above it on a phone.
  const searchBox = (
    <div className="relative">
      <SearchIcon className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={t("settings.search")}
        aria-label={t("settings.search")}
        className="bg-background pl-9"
      />
    </div>
  )
  const sectionNav = (
    <nav className="grid gap-0.5" aria-label={t("settings.title")}>
      {q &&
        found.map(({ section: s, hits }) => (
          <div key={s.id} className="grid gap-0.5">
            <button
              type="button"
              onClick={() => goTo(s.id)}
              className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground [&_svg]:size-4 [&_svg]:shrink-0"
            >
              {s.icon}
              <span className="min-w-0 flex-1 truncate">{t(s.title)}</span>
            </button>
            {hits.map(([anchor, label]) => (
              <button
                key={anchor}
                type="button"
                onClick={() => goTo(s.id, anchor)}
                className="w-full rounded-md py-1.5 pr-2.5 pl-9 text-left text-sm transition-colors hover:bg-accent hover:text-accent-foreground"
              >
                {t(label)}
              </button>
            ))}
          </div>
        ))}
      {!q &&
        allowed.map((s) => (
          <button
            key={s.id}
            type="button"
            data-active={s.id === section.id}
            onClick={() => open(s.id)}
            className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground data-[active=true]:bg-accent data-[active=true]:font-medium data-[active=true]:text-accent-foreground [&_svg]:size-4 [&_svg]:shrink-0"
          >
            {s.icon}
            <span className="min-w-0 flex-1 truncate">{t(s.title)}</span>
            {s.id === "updates" && update?.available && (
              <span className="size-2 rounded-full bg-primary" aria-label={t("update.tooltip")} />
            )}
          </button>
        ))}
      {q && found.length === 0 && <p className="px-2.5 py-2 text-sm text-muted-foreground">{t("common.nothingMatches")}</p>}
    </nav>
  )

  // On a phone the window takes the whole screen and works like the
  // settings of a phone app: a list of the sections in groups, and a
  // section on a screen of its own with a way back.
  const groups: SectionId[][] = [["server", "domain", "access"], ["accounts", "sso", "audit"], ["updates", "singbox"], ["about"]]
  const subtitle: Partial<Record<SectionId, string>> = {
    server: settings?.publicHost,
    domain: settings ? settings.domain || t("domain.none") : undefined,
    updates: update
      ? update.available && update.latest
        ? t("settings.updateReady", { version: update.latest })
        : update.current
      : undefined,
  }
  const phoneSection = win.section ? current : null
  // Which way the screens move: a section comes in from the right, and
  // going back brings the list in from the left, as on a phone. The window
  // opening on the list moves nothing; the window itself appears.
  const [cameBack, setCameBack] = React.useState(false)
  const slide = phoneSection
    ? "motion-safe:animate-in motion-safe:fade-in-0 motion-safe:slide-in-from-right-16"
    : cameBack
      ? "motion-safe:animate-in motion-safe:fade-in-0 motion-safe:slide-in-from-left-16"
      : ""
  const phoneHome = (
    <div className="grid gap-6 px-4 pt-2 pb-8">
      {searchBox}
      {q
        ? sectionNav
        : groups.map((ids) => {
            const shown = ids.flatMap((id) => allowed.filter((s) => s.id === id))
            if (!shown.length) return null
            return (
              <div key={ids[0]} className="overflow-hidden rounded-xl bg-card ring-1 ring-foreground/10">
                {shown.map((s, i) => (
                  <button
                    key={s.id}
                    type="button"
                    onClick={() => {
                      setCameBack(false)
                      open(s.id)
                    }}
                    className={cn(
                      "flex w-full items-center gap-4 px-4 py-3.5 text-left transition-colors active:bg-muted [&>svg]:size-5 [&>svg]:shrink-0",
                      i > 0 && "border-t"
                    )}
                  >
                    {s.icon}
                    <span className="grid min-w-0 flex-1 gap-0.5">
                      <span className="text-base">{t(s.title)}</span>
                      {subtitle[s.id] && <span className="truncate text-sm text-muted-foreground">{subtitle[s.id]}</span>}
                    </span>
                    {s.id === "updates" && update?.available && (
                      <span className="size-2 rounded-full bg-primary" aria-label={t("update.tooltip")} />
                    )}
                    <ChevronRightIcon className="text-muted-foreground" />
                  </button>
                ))}
              </div>
            )
          })}
    </div>
  )

  return (
    <Dialog open={win.open} onOpenChange={(o) => !o && (setCameBack(false), win.close())}>
      <DialogContent
        showCloseButton={!isMobile}
        // On a phone nothing is focused on opening: the first button would be
        // the close button, outlined before anything was touched.
        onOpenAutoFocus={(e) => {
          if (!isMobile) return
          e.preventDefault()
          ;(e.currentTarget as HTMLElement).focus()
        }}
        className={cn(
          "flex gap-0 overflow-hidden p-0",
          isMobile
            ? "h-dvh w-full max-w-none flex-col rounded-none bg-background ring-0 sm:max-w-none"
            : "h-[min(52rem,90svh)] w-[calc(100%-2rem)] max-w-none sm:max-w-6xl"
        )}
      >
        <DialogTitle className="sr-only">{t("settings.title")}</DialogTitle>
        <DialogDescription className="sr-only">{t(section.title)}</DialogDescription>
        {isMobile ? (
          <>
            <header className="grid h-14 shrink-0 grid-cols-[2.5rem_1fr_2.5rem] items-center px-2">
              {phoneSection ? (
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => {
                    setCameBack(true)
                    win.openSettings()
                  }}
                  aria-label={t("common.back")}
                >
                  <ArrowLeftIcon />
                </Button>
              ) : (
                <span />
              )}
              <h2
                key={phoneSection ?? "home"}
                className={cn(
                  "truncate text-center font-heading text-lg font-medium",
                  slide && "motion-safe:animate-in motion-safe:fade-in-0 motion-safe:duration-300"
                )}
              >
                {t(phoneSection ? section.title : "settings.title")}
              </h2>
              <Button variant="ghost" size="icon" onClick={() => (setCameBack(false), win.close())} aria-label={t("common.close")}>
                <XIcon />
              </Button>
            </header>
            {/* A screen of its own: it starts at its top, and moves in. */}
            <ScrollArea key={phoneSection ?? "home"} className="min-h-0 flex-1">
              <div className={cn(slide, "motion-safe:duration-300 motion-safe:ease-out")}>
                {phoneSection ? (
                  <SaidAboveContext.Provider value={t(section.title)}>
                    <div className="phone-settings px-4 pt-2 pb-10">{body ?? <PaneSkeleton />}</div>
                  </SaidAboveContext.Provider>
                ) : (
                  phoneHome
                )}
              </div>
            </ScrollArea>
          </>
        ) : (
          <>
            <aside className="flex w-64 shrink-0 flex-col gap-4 border-r bg-muted/40 p-3">
              {searchBox}
              {sectionNav}
            </aside>
            <ScrollArea className="min-w-0 flex-1">
              <div className="p-10">
                <div className="mx-auto max-w-3xl">{body ?? <PaneSkeleton />}</div>
              </div>
            </ScrollArea>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
