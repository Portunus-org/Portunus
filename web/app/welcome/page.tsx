"use client"

import * as React from "react"
import { useRouter } from "next/navigation"
import { ArrowRightIcon, LoaderIcon } from "lucide-react"

import { Handwriting } from "@/components/brand/handwriting"
import { LogoMark } from "@/components/brand/logo"
import { DomainStatus, useDomainCheck } from "@/components/domain-check"
import { PanelAccessStatus, PanelAddress, usePanelAccessChecks } from "@/components/panel-access-card"
import { ChoiceCard, Note, StepFrame } from "@/components/wizard"
import { FormField, FormError } from "@/components/form-field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup } from "@/components/ui/radio-group"
import { api, ApiError, errorText, type PanelAccess, type PublicAddress } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { SessionGate, takeHandoffPassword, useSession } from "@/lib/session"
import { cn } from "@/lib/utils"

// The welcome covers only the basics: a password of one's own and where the
// server is reachable. Connections are created afterwards by the inbound
// wizard, one protocol at a time.
type Step = "hello" | "password" | "server" | "access" | "done"

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

function PasswordStep({ step, total, onNext }: { step: number; total: number; onNext: () => void }) {
  const { t } = useI18n()
  const [current] = React.useState(takeHandoffPassword)
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const next = String(f.get("next"))
    if (next.length < 12) return setError(t("welcome.password.short"))
    if (next !== f.get("repeat")) return setError(t("welcome.password.mismatch"))
    setPending(true)
    setError(null)
    try {
      await api.changePassword(current || String(f.get("current")), next)
      onNext()
    } catch (err) {
      setError(err instanceof ApiError && err.code === "current_password_invalid" ? t("welcome.password.wrongCurrent") : errorText(err, t))
      setPending(false)
    }
  }

  return (
    <form onSubmit={onSubmit} noValidate className="contents">
      <StepFrame
        step={step}
        total={total}
        title={t("welcome.password.title")}
        lead={t("welcome.password.lead")}
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
          <FormField id="current" label={t("welcome.password.current")} hint={t("welcome.password.currentHint")}>
            <Input id="current" name="current" type="password" autoComplete="current-password" required />
          </FormField>
        )}
        <FormField id="next" label={t("welcome.password.new")} hint={t("welcome.password.hint")}>
          <Input id="next" name="next" type="password" autoComplete="new-password" autoFocus required />
        </FormField>
        <FormField id="repeat" label={t("welcome.password.repeat")}>
          <Input id="repeat" name="repeat" type="password" autoComplete="new-password" required />
        </FormField>
        <FormError>{error}</FormError>
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
  const [error, setError] = React.useState<string | null>(null)
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
    setError(null)
    try {
      const d = hasDomain === "yes" ? domain.trim() : ""
      // With a domain, links use it: they survive a change of IP address.
      await api.setupBasics(d || host.trim(), d)
      onNext(!!d)
    } catch (err) {
      setError(errorText(err, t))
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
            <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="203.0.113.10" spellCheck={false} />
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
              onChange={(e) => setDomain(e.target.value)}
              placeholder={t("welcome.server.domainPlaceholder")}
              aria-label={t("welcome.server.domain")}
              autoCapitalize="none"
              spellCheck={false}
              autoFocus
            />
            <DomainStatus domain={domain} status={dns} ip={ip || host} />
          </div>
        )}
      </div>

      <FormError>{error}</FormError>
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

// ---------------------------------------------------------------------- done

function DoneStep({ onCreate, onLater, canCreate }: { onCreate: () => void; onLater: () => void; canCreate: boolean }) {
  const { t } = useI18n()
  const [ready, setReady] = React.useState(false)
  return (
    <div className="flex w-full max-w-xl flex-col items-center gap-10 text-center">
      <Handwriting text={t("welcome.done.greeting")} className="max-h-28 text-foreground" onDone={() => setReady(true)} />
      <div
        className={cn("flex flex-col items-center gap-8 transition-all duration-700", ready ? "opacity-100" : "translate-y-2 opacity-0")}
      >
        <p className="max-w-md text-balance text-muted-foreground">{t("welcome.done.lead")}</p>
        <div className="flex flex-wrap justify-center gap-3">
          <Button size="lg" variant="ghost" onClick={onLater}>
            {t("welcome.done.later")}
          </Button>
          {canCreate && (
            <Button size="lg" onClick={onCreate}>
              {t("welcome.done.create")}
              <ArrowRightIcon />
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------- flow

function WelcomeFlow() {
  const router = useRouter()
  const { user, setupCompleted } = useSession()

  // Which steps this user goes through.
  const [steps] = React.useState(() => {
    const s: Step[] = []
    if (user.mustChangePassword) s.push("password")
    if (user.role === "admin" && !setupCompleted) s.push("server")
    return s
  })
  const [step, setStep] = React.useState<Step>("hello")

  // Nothing to do here (e.g. opened by hand later): go to the panel.
  React.useEffect(() => {
    if (steps.length === 0) router.replace("/")
  }, [steps, router])

  // Opening the panel by its domain follows the step that asks for a
  // domain, and takes two: the panel being set up, then its address handed
  // over, the last step. They are counted from the moment a domain is
  // chosen: the number of steps and the bar follow the choice as it is
  // made, and without a domain the setup ends where it says it does.
  const [withDomain, setWithDomain] = React.useState(false)
  const index = step === "access" ? steps.length + 1 : steps.indexOf(step) + 1
  const total = steps.length + (withDomain ? 2 : 0)
  const next = (after: Step) => {
    const i = steps.indexOf(after)
    setStep(i + 1 < steps.length ? steps[i + 1] : "done")
  }
  const skip = async () => {
    await api.setupSkip().catch(() => {})
    router.replace("/")
  }

  if (steps.length === 0) return null

  return (
    <main className="flex min-h-svh flex-col bg-muted">
      <header className="flex items-center gap-2 p-6 font-heading text-lg font-semibold md:px-10">
        <LogoMark />
        Portunus
      </header>
      <div className="flex flex-1 items-center justify-center px-6 pb-16">
        {step === "hello" && <Hello onNext={() => setStep(steps[0])} />}
        {step === "password" && <PasswordStep step={index} total={total} onNext={() => next("password")} />}
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
        {step === "access" && <AccessStep step={index} total={total} onNext={() => setStep("done")} />}
        {step === "done" && (
          <DoneStep
            canCreate={user.role !== "viewer"}
            onCreate={() => router.replace("/inbounds/new/")}
            onLater={() => router.replace("/")}
          />
        )}
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
