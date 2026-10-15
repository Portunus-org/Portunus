"use client"

import * as React from "react"
import { useRouter } from "next/navigation"
import { ArrowRightIcon, LoaderIcon } from "lucide-react"

import { Handwriting } from "@/components/brand/handwriting"
import { LogoMark } from "@/components/brand/logo"
import { Access } from "@/components/clients/client-drawer"
import { DomainStatus, useDomainCheck } from "@/components/domain-check"
import { InboundWizard } from "@/components/inbounds/inbound-wizard"
import { PanelAccessStatus, PanelAddress, usePanelAccessChecks } from "@/components/panel-access-card"
import { ChoiceCard, Note, StepFrame } from "@/components/wizard"
import { FormField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup } from "@/components/ui/radio-group"
import { api, ApiError, errorText, type Client, type PanelAccess, type PublicAddress } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { useFormErrors } from "@/lib/form-errors"
import { SessionGate, takeHandoffPassword, useSession } from "@/lib/session"
import { useResource } from "@/lib/use-resource"
import { cn } from "@/lib/utils"

// The welcome has two halves. First the basics: a password of one's own and
// where the server is reachable (with a domain, the panel is opened by it
// from then on). Then what makes the server of use: a connection, made by
// the same wizard as any later one, and a first client. The second half can
// be skipped, and is picked up again after signing in at the panel's own
// domain; the overview keeps its hints for whoever skipped it.
type Step = "hello" | "password" | "server" | "access" | "continue" | "inbound" | "client" | "connect" | "done"

// --------------------------------------------------------------------- hello

function Hello({ onNext }: { onNext: () => void }) {
  const { t } = useI18n()
  const [ready, setReady] = React.useState(false)
  return (
    <div className="flex w-full max-w-2xl flex-col items-center gap-10 text-center">
      <Handwriting text={t("welcome.greeting")} className="max-h-40 text-foreground" onDone={() => setReady(true)} />
      <div
        className={cn("flex flex-col items-center gap-8 transition-all duration-700", ready ? "opacity-100" : "translate-y-2 opacity-0")}
      >
        <p className="max-w-md text-balance text-muted-foreground">{t("welcome.lead")}</p>
        <Button size="lg" onClick={onNext} disabled={!ready}>
          {t("welcome.start")}
          <ArrowRightIcon />
        </Button>
      </div>
    </div>
  )
}

// ------------------------------------------------------------------ password

function PasswordStep({ step, total, invited, onNext }: { step: number; total: number; invited: boolean; onNext: () => void }) {
  const { t } = useI18n()
  const [current] = React.useState(takeHandoffPassword)
  const [pending, setPending] = React.useState(false)
  const { errors, wrong, clear, fail } = useFormErrors()

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const next = String(f.get("next"))
    if (!current && !f.get("current")) return wrong("current", t("error.required"))
    if (next.length < 12) return wrong("next", t("welcome.password.short"))
    if (next !== f.get("repeat")) return wrong("repeat", t("welcome.password.mismatch"))
    setPending(true)
    clear()
    try {
      await api.changePassword(current || String(f.get("current")), next)
      onNext()
    } catch (err) {
      if (err instanceof ApiError && err.code === "current_password_invalid") wrong("current", t("welcome.password.wrongCurrent"))
      else fail(err, ["current", "next", "repeat"])
      setPending(false)
    }
  }

  return (
    <form onSubmit={onSubmit} onInput={clear} noValidate className="contents">
      <StepFrame
        step={step}
        total={total}
        title={t("welcome.password.title")}
        lead={t(invited ? "welcome.password.leadInvited" : "welcome.password.lead")}
        footer={
          <>
            <span />
            <Button type="submit" disabled={pending}>
              {pending ? t("common.saving") : t("common.next")}
              <ArrowRightIcon />
            </Button>
          </>
        }
      >
        {!current && (
          <FormField id="current" label={t("welcome.password.current")} hint={t("welcome.password.currentHint")} error={errors.current}>
            <Input id="current" name="current" type="password" autoComplete="current-password" required />
          </FormField>
        )}
        <FormField id="next" label={t("welcome.password.new")} hint={t("welcome.password.hint")} error={errors.next}>
          <Input id="next" name="next" type="password" autoComplete="new-password" autoFocus required />
        </FormField>
        <FormField id="repeat" label={t("welcome.password.repeat")} error={errors.repeat}>
          <Input id="repeat" name="repeat" type="password" autoComplete="new-password" required />
        </FormField>
        <FormError>{errors.form}</FormError>
      </StepFrame>
    </form>
  )
}

// -------------------------------------------------------------------- server

function ServerStep({
  step,
  total,
  onNext,
  onSkip,
  onChoice,
}: {
  step: number
  total: number
  /** domain: the server has one, and the panel can be opened by it. */
  onNext: (domain: boolean) => void
  onSkip: () => void
  /** Told whenever "the server has a domain" is chosen or dropped: more steps follow with one. */
  onChoice: (domain: boolean) => void
}) {
  const { t } = useI18n()
  const [detected, setDetected] = React.useState<PublicAddress | null>(null)
  const [loaded, setLoaded] = React.useState(false)
  const [host, setHost] = React.useState("")
  const [hasDomain, setHasDomain] = React.useState<"no" | "yes">("no")
  const [domain, setDomain] = React.useState("")
  const [pending, setPending] = React.useState(false)
  const { errors, clear, fail } = useFormErrors()
  const dns = useDomainCheck(hasDomain === "yes" ? domain : "")
  React.useEffect(() => onChoice(hasDomain === "yes"), [hasDomain, onChoice])

  React.useEffect(() => {
    api.setupAddress().then(
      (a) => {
        setDetected(a.detected)
        setHost(a.publicHost || a.detected.ipv4 || a.detected.ipv6 || "")
        if (a.domain) {
          setHasDomain("yes")
          setDomain(a.domain)
        }
        setLoaded(true)
      },
      () => setLoaded(true)
    )
  }, [])

  const ip = detected?.ipv4 || detected?.ipv6 || ""

  async function save() {
    setPending(true)
    clear()
    try {
      const d = hasDomain === "yes" ? domain.trim() : ""
      // With a domain, links use it: they survive a change of IP address.
      await api.setupBasics(d || host.trim(), d)
      onNext(!!d)
    } catch (err) {
      // The domain, or the address typed by hand, is shown red where it was typed.
      fail(err, ["domain", "host"], { publicHost: "host" })
      setPending(false)
    }
  }

  return (
    <StepFrame
      step={step}
      total={total}
      title={t("welcome.server.title")}
      lead={t("welcome.server.lead")}
      aside={
        <button type="button" onClick={onSkip} className="hover:text-foreground hover:underline">
          {t("welcome.skip")}
        </button>
      }
      footer={
        <>
          <span />
          <Button
            onClick={save}
            disabled={
              pending || !loaded || (hasDomain === "yes" ? !domain.trim() || !(dns.state === "done" && dns.check.pointsHere) : !host.trim())
            }
          >
            {pending ? t("common.saving") : t("welcome.server.continue")}
            <ArrowRightIcon />
          </Button>
        </>
      }
    >
      <div className="grid gap-1.5 rounded-lg border bg-muted/50 p-4">
        <span className="text-sm text-muted-foreground">{t("welcome.server.address")}</span>
        {!loaded ? (
          <span className="flex items-center gap-2 text-sm">
            <LoaderIcon className="size-4 animate-spin" />
            {t("welcome.server.detecting")}
          </span>
        ) : ip ? (
          <>
            <span className="font-mono text-lg font-medium">{ip}</span>
            <span className="text-sm text-muted-foreground">
              {t(detected?.source === "lookup" ? "welcome.server.addressNat" : "welcome.server.addressInterface")}
            </span>
          </>
        ) : (
          <div className="grid gap-3">
            <span className="text-sm">{t("welcome.server.addressUnknown")}</span>
            <Input
              value={host}
              onChange={(e) => (setHost(e.target.value), clear())}
              placeholder="203.0.113.10"
              spellCheck={false}
              aria-invalid={!!errors.host}
            />
            <FormError>{errors.host}</FormError>
          </div>
        )}
      </div>

      <div className="grid gap-3">
        <Label>{t("welcome.server.domainQuestion")}</Label>
        <RadioGroup value={hasDomain} onValueChange={(v) => setHasDomain(v as "no" | "yes")} className="gap-3">
          <ChoiceCard value="no" id="domain-no" title={t("welcome.server.noDomain")} description={t("welcome.server.noDomainHint")} />
          <ChoiceCard value="yes" id="domain-yes" title={t("welcome.server.withDomain")} description={t("welcome.server.withDomainHint")} />
        </RadioGroup>
        {hasDomain === "yes" && (
          <div className="grid gap-3 duration-300 animate-in fade-in">
            <Input
              value={domain}
              onChange={(e) => (setDomain(e.target.value), clear())}
              aria-invalid={!!errors.domain}
              placeholder={t("welcome.server.domainPlaceholder")}
              aria-label={t("welcome.server.domain")}
              autoCapitalize="none"
              spellCheck={false}
              autoFocus
            />
            <FormError>{errors.domain}</FormError>
            <DomainStatus domain={domain} status={dns} ip={ip || host} />
          </div>
        )}
      </div>

      <FormError>{errors.form}</FormError>
    </StepFrame>
  )
}

// -------------------------------------------------------------------- access

/**
 * With a domain, the panel is opened by it from here on. First it is set
 * up, and the step says so: the panel starts answering on HTTPS, gets its
 * certificate, and this browser confirms that the domain leads it here.
 * Only then is there an address worth handing over, and "Next" shows it,
 * with the way to carry on there; until then the tunnel this page came
 * through stays the way in.
 */
function AccessStep({ step, total, onNext }: { step: number; total: number; onNext: () => void }) {
  const { t } = useI18n()
  const [access, setAccess] = React.useState<PanelAccess | null>(null)
  const [error, setError] = React.useState<string | null>(null)
  const { check, reached, retry, waiting } = usePanelAccessChecks(!!access?.enabled)

  React.useEffect(() => {
    api.setPanelAccess({ enabled: true, mode: "acme", port: 443 }).then(setAccess, (err) => setError(errorText(err, t)))
  }, [t])

  const [ready, setReady] = React.useState(false)
  const failed = error !== null || (check !== null && !check.ok)
  if (ready && access) {
    return (
      <StepFrame
        step={step + 1}
        total={total}
        title={t("welcome.access.readyTitle")}
        lead={t("welcome.access.readyLead")}
        footer={
          <>
            <Button variant="outline" onClick={onNext}>
              {t("welcome.access.stay")}
            </Button>
            <Button asChild>
              <a href={access.url}>
                {t("welcome.access.go")}
                <ArrowRightIcon />
              </a>
            </Button>
          </>
        }
      >
        <PanelAddress access={access} />
      </StepFrame>
    )
  }
  return (
    <StepFrame
      step={step}
      total={total}
      title={t("welcome.access.title")}
      lead={t("welcome.access.lead")}
      aside={
        <button type="button" onClick={onNext} className="hover:text-foreground hover:underline">
          {t("welcome.access.later")}
        </button>
      }
      footer={
        <>
          {failed && !error ? (
            <Button variant="outline" onClick={retry}>
              {t("common.retry")}
            </Button>
          ) : (
            <span />
          )}
          <Button onClick={() => setReady(true)} disabled={!reached}>
            {t("common.next")}
            <ArrowRightIcon />
          </Button>
        </>
      }
    >
      {error ? (
        <Note tone="error">{t("welcome.access.unavailable", { error })}</Note>
      ) : !access ? (
        <Note tone="pending">{t("welcome.access.starting")}</Note>
      ) : (
        <>
          <PanelAccessStatus check={check} reached={reached} domain={access.domain} waiting={waiting} />
          {check?.ok && !reached && <p className="text-sm text-muted-foreground">{t("welcome.access.dnsHint")}</p>}
          {reached && <p className="text-sm text-muted-foreground">{t("welcome.access.allSet")}</p>}
        </>
      )}
    </StepFrame>
  )
}

// ------------------------------------------------------------------ the rest

/**
 * Between the two halves of the first setup: the server has its address,
 * and what makes it of use comes next, a connection and a client. This is
 * also where the setup is picked up after the browser has moved from the
 * tunnel to the panel's own domain.
 */
function ContinueStep({ hasInbound, onNext, onSkip }: { hasInbound: boolean; onNext: () => void; onSkip: () => void }) {
  const { t } = useI18n()
  return (
    <div className="flex w-full max-w-xl flex-col items-center gap-8 text-center duration-500 animate-in fade-in slide-in-from-bottom-2">
      <h1 className="font-heading text-4xl font-semibold text-balance">{t("welcome.continue.title")}</h1>
      <p className="max-w-md text-balance text-muted-foreground">
        {t(hasInbound ? "welcome.continue.leadClient" : "welcome.continue.lead")}
      </p>
      <div className="flex flex-wrap justify-center gap-3">
        <Button size="lg" variant="ghost" onClick={onSkip}>
          {t("common.skip")}
        </Button>
        <Button size="lg" onClick={onNext}>
          {t("welcome.continue.go")}
          <ArrowRightIcon />
        </Button>
      </div>
    </div>
  )
}

/** The last step: the first client, by a name alone. */
function ClientStep({
  step,
  total,
  onNext,
  onSkip,
}: {
  step: number
  total: number
  onNext: (client: Client) => void
  onSkip: () => void
}) {
  const { t } = useI18n()
  const [name, setName] = React.useState("")
  const [pending, setPending] = React.useState(false)
  const { errors, clear, fail } = useFormErrors()

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setPending(true)
    clear()
    try {
      onNext(await api.createClient({ name: name.trim(), trafficLimit: 0, expiresAt: null, note: "", enabled: true }))
    } catch (err) {
      fail(err, ["client-name"], { name: "client-name" })
    } finally {
      setPending(false)
    }
  }

  return (
    <form onSubmit={onSubmit} onInput={clear} className="flex w-full justify-center">
      <StepFrame
        step={step}
        total={total}
        title={t("welcome.client.title")}
        lead={t("welcome.client.lead")}
        aside={
          <button type="button" onClick={onSkip} className="hover:text-foreground hover:underline">
            {t("common.skip")}
          </button>
        }
        footer={
          <>
            <span />
            <Button type="submit" disabled={pending || !name.trim()}>
              {pending ? t("common.saving") : t("welcome.client.create")}
              <ArrowRightIcon />
            </Button>
          </>
        }
      >
        <FormField id="client-name" label={t("clients.form.name")} hint={t("welcome.client.nameHint")} error={errors["client-name"]}>
          <Input
            id="client-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("welcome.client.placeholder")}
            autoFocus
          />
        </FormField>
        <FormError>{errors.form}</FormError>
      </StepFrame>
    </form>
  )
}

/**
 * The client is made; now it is connected. The step shows what the device
 * needs (a code to scan, links to copy) and waits for the proof that it
 * worked: traffic of this client counted by the server. Only then is there
 * a "Next", because only then is the server known to do what it is for.
 * Whoever does not want to wait skips, as anywhere else.
 */
function ConnectStep({
  client,
  step,
  total,
  onNext,
  onSkip,
}: {
  client: Client
  step: number
  total: number
  onNext: () => void
  onSkip: () => void
}) {
  const { t } = useI18n()
  const [connected, setConnected] = React.useState(false)
  React.useEffect(() => {
    if (connected) return
    let alive = true
    // The server reads its counters every ten seconds: asking more often
    // than every few would learn nothing sooner.
    const look = () =>
      api.clients().then(
        (list) => {
          const now = list.find((c) => c.id === client.id)
          if (alive && now && (now.online || now.upload + now.download > 0)) setConnected(true)
        },
        () => {}
      )
    look()
    const timer = setInterval(look, 3000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [client.id, connected])

  return (
    <StepFrame
      step={step}
      total={total}
      title={t("welcome.connect.title", { name: client.name })}
      lead={t("welcome.connect.lead")}
      aside={
        <button type="button" onClick={onSkip} className="hover:text-foreground hover:underline">
          {t("common.skip")}
        </button>
      }
      footer={
        <>
          <span />
          <Button onClick={onNext} disabled={!connected}>
            {t("common.next")}
            <ArrowRightIcon />
          </Button>
        </>
      }
    >
      <div className="grid gap-4 text-sm">
        <Access clientId={client.id} />
      </div>
      {connected ? <Note tone="ok">{t("welcome.connect.done")}</Note> : <Note tone="pending">{t("welcome.connect.waiting")}</Note>}
    </StepFrame>
  )
}

// ---------------------------------------------------------------------- done

/** All set: the one way on is the panel. */
function DoneStep({ client, onPanel }: { client: Client | null; onPanel: () => void }) {
  const { t } = useI18n()
  const [ready, setReady] = React.useState(false)
  return (
    <div className="flex w-full max-w-xl flex-col items-center gap-10 text-center">
      <Handwriting text={t("welcome.done.greeting")} className="max-h-28 text-foreground" onDone={() => setReady(true)} />
      <div
        className={cn("flex flex-col items-center gap-8 transition-all duration-700", ready ? "opacity-100" : "translate-y-2 opacity-0")}
      >
        <p className="max-w-md text-balance text-muted-foreground">
          {client ? t("welcome.done.leadClient", { name: client.name }) : t("welcome.done.lead")}
        </p>
        <Button size="lg" onClick={onPanel}>
          {t("welcome.done.panel")}
          <ArrowRightIcon />
        </Button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------- flow

function WelcomeFlow() {
  const router = useRouter()
  const { user, setupCompleted, guide } = useSession()

  // Which steps this user goes through.
  const [steps] = React.useState(() => {
    const s: Step[] = []
    if (user.mustChangePassword) s.push("password")
    if (user.role === "admin" && !setupCompleted) s.push("server")
    return s
  })
  // Come back with the first half done (the usual case: signed in anew at
  // the panel's own domain): straight on with the second.
  // An account added to a panel that is already set up has one thing to do
  // here, its own password: no greeting, no setup, and then the panel.
  const [invited] = React.useState(setupCompleted)
  const [step, setStep] = React.useState<Step>(steps.length === 0 ? "continue" : invited ? steps[0] : "hello")
  const canGuide = user.role === "admin"

  // Nothing to do here (e.g. opened by hand later): go to the panel.
  const idle = steps.length === 0 && !guide
  React.useEffect(() => {
    if (idle) router.replace("/")
  }, [idle, router])

  // Opening the panel by its domain follows the step that asks for a
  // domain, and takes two: the panel being set up, then its address handed
  // over, the last step. They are counted from the moment a domain is
  // chosen: the number of steps and the bar follow the choice as it is
  // made, and without a domain the setup ends where it says it does.
  const [withDomain, setWithDomain] = React.useState(false)
  const index = step === "access" ? steps.length + 1 : steps.indexOf(step) + 1
  const total = steps.length + (withDomain ? 2 : 0)
  // After the first half: on with the second for whoever can make
  // connections; anybody else is done.
  const onwards = () => {
    if (invited && !(canGuide && guide)) router.replace("/")
    else setStep(canGuide ? "continue" : "done")
  }
  const next = (after: Step) => {
    const i = steps.indexOf(after)
    if (i + 1 < steps.length) setStep(steps[i + 1])
    else onwards()
  }
  const skip = async () => {
    await api.setupSkip().catch(() => {})
    router.replace("/")
  }

  // The second half: a connection (unless there is one), then a client.
  const { data: inbounds } = useResource(api.inbounds)
  const hasInbound = (inbounds?.length ?? 0) > 0
  const [taken, setTaken] = React.useState(0) // steps the connection wizard took
  const [client, setClient] = React.useState<Client | null>(null)
  const endGuide = async (to: string) => {
    await api.guideDone().catch(() => {})
    router.replace(to)
  }

  if (idle) return null

  return (
    <main className="flex min-h-svh flex-col bg-muted">
      <header className="flex items-center gap-2 p-6 font-heading text-lg font-semibold md:px-10">
        <LogoMark />
        Portunus
      </header>
      <div className="flex flex-1 items-start justify-center px-4 pb-16 sm:items-center sm:px-6">
        {step === "hello" && <Hello onNext={() => setStep(steps[0])} />}
        {step === "password" && <PasswordStep step={index} total={total} invited={invited} onNext={() => next("password")} />}
        {step === "server" && (
          <ServerStep
            step={index}
            total={total}
            onChoice={setWithDomain}
            onNext={(domain) => {
              if (domain) setStep("access")
              else next("server")
            }}
            onSkip={skip}
          />
        )}
        {step === "access" && <AccessStep step={index} total={total} onNext={onwards} />}
        {step === "continue" && (
          <ContinueStep hasInbound={hasInbound} onNext={() => setStep(hasInbound ? "client" : "inbound")} onSkip={() => endGuide("/")} />
        )}
        {step === "inbound" && (
          <InboundWizard
            guide={{
              more: 2,
              onCreated: (_inbound, wizardSteps) => {
                setTaken(wizardSteps)
                setStep("client")
              },
              onSkip: () => endGuide("/"),
            }}
          />
        )}
        {step === "client" && (
          <ClientStep
            step={taken + 1}
            total={taken + 2}
            onNext={(c) => {
              setClient(c)
              setStep("connect")
            }}
            onSkip={() => endGuide("/")}
          />
        )}
        {step === "connect" && client && (
          <ConnectStep
            client={client}
            step={taken + 2}
            total={taken + 2}
            onNext={() => {
              api.guideDone().catch(() => {})
              setStep("done")
            }}
            onSkip={() => endGuide("/")}
          />
        )}
        {step === "done" && <DoneStep client={client} onPanel={() => endGuide("/")} />}
      </div>
    </main>
  )
}

export default function WelcomePage() {
  return (
    <SessionGate mode="welcome">
      <WelcomeFlow />
    </SessionGate>
  )
}
