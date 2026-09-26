"use client"

import * as React from "react"
import { CircleAlertIcon, CircleCheckIcon, LoaderIcon } from "lucide-react"

import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { RadioGroupItem } from "@/components/ui/radio-group"
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"

/** One step of a "Next, Next, Done" wizard: progress, a card, a footer. */
export function StepFrame({
  step,
  total,
  title,
  lead,
  children,
  footer,
  aside,
  className,
}: {
  step: number
  total: number
  title: string
  lead?: string
  children: React.ReactNode
  footer: React.ReactNode
  /** Right side of the progress line (e.g. a skip link). */
  aside?: React.ReactNode
  className?: string
}) {
  const { t } = useI18n()
  return (
    <div
      key={step}
      className={cn("flex w-full max-w-xl flex-col gap-6 duration-500 animate-in fade-in slide-in-from-bottom-2", className)}
    >
      <div className="flex items-center justify-between text-sm text-muted-foreground">
        <span>{t("welcome.step", { n: step, total })}</span>
        {aside}
      </div>
      <Progress value={(step / total) * 100} className="h-1" />
      <Card>
        <CardHeader>
          <CardTitle className="text-xl">{title}</CardTitle>
          {lead && <CardDescription>{lead}</CardDescription>}
        </CardHeader>
        <CardContent className="grid gap-6">{children}</CardContent>
        <CardFooter className="justify-between gap-2 border-t">{footer}</CardFooter>
      </Card>
    </div>
  )
}

/** A large radio option: title, explanation and optional badges. */
export function ChoiceCard({
  value,
  id,
  title,
  description,
  badges,
  children,
}: {
  value: string
  id: string
  title: React.ReactNode
  description?: React.ReactNode
  badges?: React.ReactNode
  children?: React.ReactNode
}) {
  return (
    <Label
      htmlFor={id}
      className="flex cursor-pointer items-start gap-3 rounded-lg border p-4 font-normal has-data-[state=checked]:border-primary has-data-[state=checked]:bg-muted/50"
    >
      <RadioGroupItem value={value} id={id} className="mt-0.5" />
      <div className="grid min-w-0 flex-1 gap-1.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{title}</span>
          {badges}
        </div>
        {description && <span className="text-sm text-muted-foreground">{description}</span>}
        {children}
      </div>
    </Label>
  )
}

export type TaskState = "pending" | "running" | "done" | "slow" | "failed"

/** A line of a progress checklist. */
export function Task({ state, label, note }: { state: TaskState; label: string; note?: string }) {
  return (
    <li className="flex items-start gap-3">
      {state === "done" && <CircleCheckIcon className="mt-0.5 size-5 shrink-0 fill-green-500 text-background dark:fill-green-400" />}
      {(state === "running" || state === "slow") && <LoaderIcon className="mt-0.5 size-5 shrink-0 animate-spin text-muted-foreground" />}
      {state === "pending" && <span className="mt-0.5 size-5 shrink-0 rounded-full border-2 border-muted" />}
      {state === "failed" && <CircleAlertIcon className="mt-0.5 size-5 shrink-0 fill-red-500 text-background" />}
      <div className="grid gap-1">
        <span className={cn(state === "pending" && "text-muted-foreground")}>{label}</span>
        {note && <span className="text-sm text-muted-foreground">{note}</span>}
      </div>
    </li>
  )
}

/** A short status line: ok (green), warn (amber), or in progress. */
export function Note({ tone, children }: { tone: "ok" | "warn" | "pending" | "error"; children: React.ReactNode }) {
  return (
    <p className="flex items-start gap-2 text-sm">
      {tone === "ok" && <CircleCheckIcon className="mt-0.5 size-4 shrink-0 fill-green-500 text-background dark:fill-green-400" />}
      {tone === "warn" && <CircleAlertIcon className="mt-0.5 size-4 shrink-0 fill-amber-500 text-background dark:fill-amber-400" />}
      {tone === "error" && <CircleAlertIcon className="mt-0.5 size-4 shrink-0 fill-red-500 text-background" />}
      {tone === "pending" && <LoaderIcon className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground" />}
      <span className={cn(tone === "pending" && "text-muted-foreground")}>{children}</span>
    </p>
  )
}
