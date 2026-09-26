"use client"

import * as React from "react"
import { useRouter } from "next/navigation"
import { ArrowRightIcon, LoaderIcon } from "lucide-react"

import { Handwriting } from "@/components/brand/handwriting"
import { LogoMark } from "@/components/brand/logo"
import { DomainStatus, useDomainCheck } from "@/components/domain-check"
import { ChoiceCard, StepFrame } from "@/components/wizard"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup } from "@/components/ui/radio-group"
import { api, ApiError, errorText, type PublicAddress } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { SessionGate, takeHandoffPassword, useSession } from "@/lib/session"
import { cn } from "@/lib/utils"

// The welcome covers only the basics: a password of one's own and where the
// server is reachable. Connections are created afterwards by the inbound
// wizard, one protocol at a time.
type Step = "hello" | "password" | "server" | "done"

// --------------------------------------------------------------------- hello

function Hello({ onNext }: { onNext: () => void }) {
  const { t } = useI18n()
  const [ready, setReady] = React.useState(false)
  return (
    <div className="flex w-full max-w-2xl flex-col items-center gap-10 text-center">
      <Handwriting text={t("welcome.greeting")} className="max-h-40 text-foreground" onDone={() => setReady(true)} />
      <div className={cn("flex flex-col items-center gap-8 transition-all duration-700", ready ? "opacity-100" : "translate-y-2 opacity-0")}>
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
      setError(
        err instanceof ApiError && err.code === "current_password_invalid"
          ? t("welcome.password.wrongCurrent")
          : errorText(err, t)
      )
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
          <div className="grid gap-3">
            <Label htmlFor="current">{t("welcome.password.current")}</Label>
            <Input id="current" name="current" type="password" autoComplete="current-password" required />
            <p className="text-sm text-muted-foreground">{t("welcome.password.currentHint")}</p>
          </div>
        )}
        <div className="grid gap-3">
          <Label htmlFor="next">{t("welcome.password.new")}</Label>
          <Input id="next" name="next" type="password" autoComplete="new-password" autoFocus required />
          <p className="text-sm text-muted-foreground">{t("welcome.password.hint")}</p>
        </div>
        <div className="grid gap-3">
          <Label htmlFor="repeat">{t("welcome.password.repeat")}</Label>
          <Input id="repeat" name="repeat" type="password" autoComplete="new-password" required />
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
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
}: {
  step: number
  total: number
  onNext: () => void
  onSkip: () => void
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
      onNext()
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
            disabled={pending || !loaded || (hasDomain === "yes" ? !domain.trim() : !host.trim())}
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
          <ChoiceCard
            value="no"
            id="domain-no"
            title={t("welcome.server.noDomain")}
            description={t("welcome.server.noDomainHint")}
          />
          <ChoiceCard
            value="yes"
            id="domain-yes"
            title={t("welcome.server.withDomain")}
            description={t("welcome.server.withDomainHint")}
          />
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

      {error && <p className="text-sm text-destructive">{error}</p>}
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
      <div className={cn("flex flex-col items-center gap-8 transition-all duration-700", ready ? "opacity-100" : "translate-y-2 opacity-0")}>
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

  const index = steps.indexOf(step) + 1
  const total = steps.length
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
      <header className="flex items-center gap-2 p-6 text-sm font-medium md:px-10">
        <LogoMark />
        Portunus
      </header>
      <div className="flex flex-1 items-center justify-center px-6 pb-16">
        {step === "hello" && <Hello onNext={() => setStep(steps[0])} />}
        {step === "password" && <PasswordStep step={index} total={total} onNext={() => next("password")} />}
        {step === "server" && <ServerStep step={index} total={total} onNext={() => next("server")} onSkip={skip} />}
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
