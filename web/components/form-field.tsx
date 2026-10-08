import * as React from "react"

import { Field, FieldContent, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"

/**
 * One labelled control, composed the way shadcn's Field documents it:
 * label, control, helper text, error.
 */
export function FormField({
  id,
  label,
  hint,
  error,
  className,
  children,
}: {
  id: string
  label: React.ReactNode
  hint?: React.ReactNode
  error?: React.ReactNode
  className?: string
  children: React.ReactNode
}) {
  return (
    <Field data-invalid={error ? true : undefined} className={className}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {children}
      {hint && <FieldDescription>{hint}</FieldDescription>}
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

/** A switch with its label and explanation beside it. */
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
  return (
    <Field orientation="horizontal">
      <FieldContent>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        {hint && <FieldDescription>{hint}</FieldDescription>}
        {children}
      </FieldContent>
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
    </Field>
  )
}

/** The error of a whole form, shown above its buttons. */
export function FormError({ children }: { children?: React.ReactNode }) {
  if (!children) return null
  return <FieldError>{children}</FieldError>
}
