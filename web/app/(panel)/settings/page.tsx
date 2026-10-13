"use client"

import * as React from "react"
import { RotateCwIcon } from "lucide-react"
import { toast } from "sonner"

import { CAInstall } from "@/components/ca-install"
import { CopyButton } from "@/components/copy-button"
import { PanelAccessCard } from "@/components/panel-access-card"
import { UpdateCard } from "@/components/update-card"
import { StatusBadge } from "@/components/status"
import { FormField, FormError } from "@/components/form-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, errorText, type ACMEChallenge, type Settings } from "@/lib/api"
import { formatDuration } from "@/lib/format"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"
import { useCanEdit, useSession } from "@/lib/session"
import { useResource } from "@/lib/use-resource"

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
  const [error, setError] = React.useState<string | null>(null)

  return (
    <Card>
      <form
        className="contents"
        noValidate
        onSubmit={async (e) => {
          e.preventDefault()
          setPending(true)
          setError(null)
          try {
            await onSubmit(new FormData(e.currentTarget))
            toast.success(t("settings.saved"))
          } catch (err) {
            setError(errorText(err, t))
          } finally {
            setPending(false)
          }
        }}
      >
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
        <CardContent>
          <fieldset disabled={!isAdmin} className="grid max-w-xl gap-6">
            {children}
          </fieldset>
          <FormError>{error}</FormError>
        </CardContent>
        {isAdmin && (
          <CardFooter className="border-t">
            <Button type="submit" size="sm" disabled={pending}>
              {pending ? t("common.saving") : t("common.save")}
            </Button>
          </CardFooter>
        )}
      </form>
    </Card>
  )
}

function ServerCard({ settings, onSaved }: { settings: Settings; onSaved: () => void }) {
  const { t } = useI18n()
  return (
    <SettingsCard
      title={t("settings.server.title")}
      description={t("settings.server.description")}
      onSubmit={async (f) => {
        // One address: a domain is also what certificates are issued for
        // and what the panel is opened by; an IP address is only an address.
        const address = String(f.get("publicHost") ?? "")
          .trim()
          .toLowerCase()
        const isDomain = /[a-z]/.test(address) && !address.includes(":")
        await api.updateSettings({
          publicHost: address,
          domain: isDomain ? address : "",
          subscriptionBaseUrl: settings.subscriptionBaseUrl,
        })
        onSaved()
      }}
    >
      <FormField id="publicHost" label={t("settings.server.publicHost")} hint={t("settings.server.publicHostHint")}>
        <Input
          id="publicHost"
          name="publicHost"
          defaultValue={settings.domain || settings.publicHost}
          placeholder="vpn.example.com"
          autoCapitalize="none"
          spellCheck={false}
        />
      </FormField>
    </SettingsCard>
  )
}

function SubscriptionsCard({ settings, onSaved }: { settings: Settings; onSaved: () => void }) {
  const { t } = useI18n()
  return (
    <SettingsCard
      title={t("settings.subs.title")}
      description={t("settings.subs.description")}
      onSubmit={async (f) => {
        await api.updateSettings({
          publicHost: settings.publicHost,
          subscriptionBaseUrl: settings.subscriptionBaseUrl,
          ...(settings.subscriptionListen ? {} : { subscriptionPort: Number(f.get("subscriptionPort") || 0) }),
        })
        onSaved()
      }}
    >
      {settings.subscriptionListen ? (
        <p className="text-sm text-muted-foreground">{t("settings.subs.env", { addr: settings.subscriptionListen })}</p>
      ) : (
        <FormField id="subscriptionPort" label={t("settings.subs.port")} hint={t("settings.subs.portHint")}>
          <Input
            id="subscriptionPort"
            name="subscriptionPort"
            type="number"
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
            <Input
              id="acmeHttpPort"
              type="number"
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
  const { data: system } = useResource(api.system)
  const [restarting, setRestarting] = React.useState(false)
  const logRef = React.useRef<HTMLDivElement>(null)

  React.useEffect(() => {
    // The newest lines are at the bottom: keep them in view.
    const el = logRef.current?.querySelector<HTMLElement>("[data-slot=scroll-area-viewport]")
    if (el) el.scrollTop = el.scrollHeight
  }, [logs])

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
    [
      t("settings.panelVersion"),
      <span key="pv" className="font-mono">
        {system ? (system.commit && system.commit !== "unknown" ? `${system.version} (${system.commit.slice(0, 7)})` : system.version) : ""}
      </span>,
    ],
    [
      t("settings.singbox.version"),
      <span key="v" className="font-mono">
        {s?.version || t("settings.singbox.notFound")}
      </span>,
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
    <Card>
      <CardHeader>
        <CardTitle>{t("settings.singbox.title")}</CardTitle>
        <CardDescription>{t("settings.singbox.description")}</CardDescription>
        {canEdit && (
          <CardAction>
            <Button variant="outline" size="sm" onClick={restart} disabled={restarting || s?.state === "stopped"}>
              <RotateCwIcon />
              {t("settings.singbox.restart")}
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="grid gap-6">
        <dl className="grid max-w-xl grid-cols-[10rem_1fr] items-center gap-y-3 text-sm">
          {rows.map(([k, v]) => (
            <React.Fragment key={k}>
              <dt className="text-muted-foreground">{k}</dt>
              <dd>{v}</dd>
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
              <TabsTrigger value="logs">{t("settings.singbox.logs")}</TabsTrigger>
              <TabsTrigger value="config">{t("settings.singbox.config")}</TabsTrigger>
            </TabsList>
            <TabsContent value="logs">
              <ScrollArea ref={logRef} className="h-72 rounded-lg bg-muted">
                <pre className="p-4 font-mono text-xs leading-relaxed">
                  {logs?.lines?.length ? logs.lines.join("\n") : t("settings.singbox.noOutput")}
                </pre>
                <ScrollBar orientation="horizontal" />
              </ScrollArea>
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
      </CardContent>
    </Card>
  )
}

function CACard() {
  const { t } = useI18n()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("settings.ca.title")}</CardTitle>
        <CardDescription>{t("settings.ca.description")}</CardDescription>
      </CardHeader>
      <CardContent className="max-w-2xl">
        <CAInstall compact />
      </CardContent>
    </Card>
  )
}

export default function SettingsPage() {
  const { data: settings, reload } = useResource(api.settings)
  const isAdmin = useSession().user.role === "admin"
  const { data: update, reload: reloadUpdate } = useResource(api.update)
  return (
    <div className="grid gap-4 px-4 lg:px-6">
      {/* Always the first thing on the page, so it is found in one place
          whether there is something to install or not. */}
      {update && (
        <UpdateCard
          key={`update-${update.auto}-${update.weekday}-${update.minute}`}
          update={update}
          reload={reloadUpdate}
          canEdit={isAdmin}
        />
      )}
      {settings && (
        <>
          <ServerCard key={`server-${settings.publicHost}-${settings.domain}`} settings={settings} onSaved={reload} />
          <PanelAccessCard key={`access-${settings.domain}`} canEdit={isAdmin} />
          <SubscriptionsCard
            key={`subs-${settings.subscriptionPort}-${settings.subscriptionBaseUrl}`}
            settings={settings}
            onSaved={reload}
          />
          <CertificatesCard key={`certs-${settings.acmeChallenge}`} settings={settings} onSaved={reload} />
        </>
      )}
      <CACard />
      <SingBoxCard />
    </div>
  )
}
