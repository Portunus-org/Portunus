import * as React from "react"

import { Field, FieldContent, FieldDescription, FieldError, FieldLabel, FieldTitle } from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"
import { RowsContext } from "@/components/settings-pane"
import { FormErrorsContext } from "@/lib/form-errors"
import { cn } from "@/lib/utils"

/**
 * One labelled control, composed the way shadcn's Field documents it. When
 * something is wrong with it, the control itself is outlined in red
 * (aria-invalid, as shadcn's inputs show it) and the words of the error are
 * right under it, before any explanation.
 */
export function FormField({
  id,
  label,
  hint,
  error,
  className,
  wide = false,
  children,
}: {
  id: string
  label: React.ReactNode
  hint?: React.ReactNode
  error?: React.ReactNode
  className?: string
  /** In a settings pane: a control that needs the whole width (a certificate, a command) goes under its words, not beside them. */
  wide?: boolean
  children: React.ReactNode
}) {
  const rows = React.useContext(RowsContext)
  const around = React.useContext(FormErrorsContext)
  error ??= around[id]
  // The control is marked where it is given straight (keeping its key, so
  // that it is not made anew and what is typed in it stays); one inside a
  // wrapper (an input with a copy button) is reached by its id.
  if (error && React.isValidElement(children) && typeof children.type !== "string")
    children = React.cloneElement(children as React.ReactElement<{ "aria-invalid"?: boolean }>, { "aria-invalid": true })
  useInvalid(id, !!error)
  if (rows) {
    // A row of the settings: what it is and does on the left, the control
    // on the right; one under the other where there is no room for two.
    return (
      <Field
        data-invalid={error ? true : undefined}
        className={cn(
          wide ? "grid gap-3" : "grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,20rem)] md:items-start md:gap-x-10",
          className
        )}
      >
        <FieldContent>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          {hint && <FieldDescription>{hint}</FieldDescription>}
        </FieldContent>
        <div className="grid min-w-0 gap-2">
          {children}
          {error && <FieldError>{error}</FieldError>}
        </div>
      </Field>
    )
  }
  return (
    <Field data-invalid={error ? true : undefined} className={className}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {children}
      {error && <FieldError>{error}</FieldError>}
      {hint && <FieldDescription>{hint}</FieldDescription>}
    </Field>
  )
}

// The control with this id, wherever it lies inside the field, says it is
// wrong (aria-invalid): shadcn's Input, Textarea and SelectTrigger are drawn
// red by it, and a screen reader says so.
function useInvalid(id: string, invalid: boolean) {
  React.useEffect(() => {
    const el = document.getElementById(id)
    if (!el || !invalid || el.hasAttribute("aria-invalid")) return
    el.setAttribute("aria-invalid", "true")
    return () => el.removeAttribute("aria-invalid")
  }, [id, invalid])
}

/**
 * A setting that is on or off, with its name and what it does. Composed as
 * shadcn's "Choice Card", a Field inside its FieldLabel: the switch and its
 * words are one outlined object that takes the click as a whole. A bare
 * horizontal field leaves the switch at the far edge of a wide form, cut off
 * from the words it belongs to.
 */
export function SwitchField({
  id,
  label,
  hint,
  checked,
  onCheckedChange,
  disabled,
  children,
}: {
  id: string
  label: React.ReactNode
  hint?: React.ReactNode
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  /** Extra content under the explanation (a link, for example). */
  children?: React.ReactNode
}) {
  // In a settings pane every setting is a row, and a switch sits at the
  // right end of its row like every other control there.
  if (React.useContext(RowsContext)) {
    return (
      <Field orientation="horizontal" className="gap-10">
        <FieldContent>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          {hint && <FieldDescription>{hint}</FieldDescription>}
          {children}
        </FieldContent>
        <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
      </Field>
    )
  }
  return (
    <FieldLabel htmlFor={id}>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldTitle>{label}</FieldTitle>
          {hint && <FieldDescription>{hint}</FieldDescription>}
          {children}
        </FieldContent>
        <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
      </Field>
    </FieldLabel>
  )
}

/** The error of a whole form, shown above its buttons. */
export function FormError({ children }: { children?: React.ReactNode }) {
  if (!children) return null
  return <FieldError>{children}</FieldError>
}
