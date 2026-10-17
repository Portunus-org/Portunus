"use client"

import * as React from "react"

import { cn } from "@/lib/utils"

/**
 * One section of the settings, as the right side of the settings page
 * shows it: a title with what the section is for, then the settings one per
 * row, then the button that saves them. The same parts as a Card, so a
 * section reads like one, without a frame of its own inside the page's.
 *
 * Inside a pane, FormField and SwitchField lay themselves out as rows: what
 * a setting is called and does on the left, its control on the right.
 */
export const RowsContext = React.createContext(false)

export function Pane({ className, ...props }: React.ComponentProps<"section">) {
  return <section data-slot="pane" className={cn("relative flex flex-col gap-8", className)} {...props} />
}

export function PaneHeader({ className, children, ...props }: React.ComponentProps<"header">) {
  return (
    <header data-slot="pane-header" className={cn("grid grid-cols-[1fr_auto] items-start gap-x-4 gap-y-1", className)} {...props}>
      {children}
    </header>
  )
}

/**
 * A title already said above the pane (on a phone, the bar of the screen
 * names the section): a pane title that says the same is kept for screen
 * readers only.
 */
export const SaidAboveContext = React.createContext<string | null>(null)

export function PaneTitle({ className, children, ...props }: React.ComponentProps<"h2">) {
  const said = React.useContext(SaidAboveContext)
  return (
    <h2
      data-slot="pane-title"
      className={cn("col-start-1 font-heading text-xl font-medium", said !== null && children === said && "sr-only", className)}
      {...props}
    >
      {children}
    </h2>
  )
}

export function PaneDescription({ className, ...props }: React.ComponentProps<"p">) {
  return <p data-slot="pane-description" className={cn("col-start-1 max-w-2xl text-sm text-muted-foreground", className)} {...props} />
}

export function PaneAction({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="pane-action" className={cn("col-start-2 row-span-2 row-start-1 self-start", className)} {...props} />
}

export function PaneContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <RowsContext.Provider value={true}>
      <div data-slot="pane-content" className={cn("grid gap-7", className)} {...props} />
    </RowsContext.Provider>
  )
}

/** A heading over a few rows that belong together, as "Account" or "Language" on the reference. */
export function PaneGroup({ title, children, className }: { title: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("grid gap-6", className)}>
      <h3 className="text-sm font-medium text-muted-foreground">{title}</h3>
      {children}
    </div>
  )
}

export function PaneFooter({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="pane-footer" className={cn("flex items-center gap-2 border-t pt-6", className)} {...props} />
}
