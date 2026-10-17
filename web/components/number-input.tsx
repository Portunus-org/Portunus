"use client"

import * as React from "react"
import { ChevronDownIcon, ChevronUpIcon } from "lucide-react"

import { FieldError } from "@/components/ui/field"
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "@/components/ui/input-group"
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"

// Set through the input's own setter, past React's record of the value.
function setValue(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(el, value)
}

/**
 * A number field with arrows of the interface's own instead of the
 * browser's: an input group, as shadcn composes an input with buttons. The
 * field stays a plain number input (its name, min, max and step, typing and
 * the arrow keys work as before); the arrows step it the same way, and a
 * press held down keeps stepping. What is not a number is not let in, and
 * the field says so at once: outlined in red, with the words under it,
 * until a number is typed or the field is emptied.
 */
export function NumberInput({ className, ...props }: Omit<React.ComponentProps<"input">, "type">) {
  const { t } = useI18n()
  const ref = React.useRef<HTMLInputElement>(null)
  const timer = React.useRef<ReturnType<typeof setTimeout>>(undefined)
  const [rejected, setRejected] = React.useState(false)

  const step = (dir: 1 | -1) => {
    const el = ref.current
    if (!el || el.disabled || el.readOnly) return
    const by = el.step && el.step !== "any" ? Number(el.step) : 1
    const min = el.min === "" ? -Infinity : Number(el.min)
    const max = el.max === "" ? Infinity : Number(el.max)
    // An empty field goes up to its first step (or its minimum); down, it stays empty.
    if (el.value === "" && dir < 0) return
    const next =
      el.value === "" ? Math.min(max, Math.max(min, by)) : Math.min(max, Math.max(min, +(Number(el.value) + dir * by).toFixed(6)))
    if (Number.isNaN(next)) return
    // Announced, so that React sees the change in a controlled field too.
    setValue(el, String(next))
    el.dispatchEvent(new Event("input", { bubbles: true }))
  }
  const stop = () => clearTimeout(timer.current)
  const hold = (dir: 1 | -1) => (e: React.PointerEvent) => {
    if (e.button !== 0) return
    e.preventDefault()
    ref.current?.focus()
    step(dir)
    const again = (wait: number) => {
      timer.current = setTimeout(() => {
        step(dir)
        again(60)
      }, wait)
    }
    again(400)
  }
  React.useEffect(() => stop, [])

  // Only a number can be typed or pasted: digits, and a decimal point where
  // the field takes fractions. Some browsers (Firefox) let a number field
  // hold any text and then send it as empty. The browser's own event is
  // used: React's stand-in for it cannot always be cancelled.
  React.useEffect(() => {
    const el = ref.current
    if (!el) return
    const filter = (e: InputEvent) => {
      const data = e.data ?? e.dataTransfer?.getData("text") ?? null
      if (data == null) return
      const fractions = el.step === "any" || (el.step !== "" && !Number.isInteger(Number(el.step)))
      const ok = fractions ? /^[0-9]*[.,]?[0-9]*$/ : /^[0-9]*$/
      if (!ok.test(data) || (fractions && /[.,]/.test(data) && /[.,]/.test(el.value))) {
        e.preventDefault()
        setRejected(true)
      }
    }
    // What cannot be cancelled (text composed with an input method, as a
    // "№" is in Firefox) is taken back as soon as it lands: the field
    // returns to the last number it held, before React reads it.
    let last = el.value
    const restore = (e: Event) => {
      if (el.validity.badInput) {
        setValue(el, last)
        setRejected(true)
        return
      }
      // An empty field is taken as meant only when text was deleted: while
      // composing, Firefox briefly reports the field as empty.
      const deleted = e instanceof InputEvent && e.inputType.startsWith("delete")
      if (el.value !== "" || deleted) {
        last = el.value
        setRejected(false)
      } else if (last !== "") setValue(el, last)
    }
    el.addEventListener("beforeinput", filter)
    el.addEventListener("input", restore)
    el.addEventListener("compositionend", restore)
    return () => {
      el.removeEventListener("beforeinput", filter)
      el.removeEventListener("input", restore)
      el.removeEventListener("compositionend", restore)
    }
  }, [])

  const own = rejected && !props["aria-invalid"]
  React.useEffect(() => {
    const field = ref.current?.closest('[data-slot="field"]')
    if (!field || !own) return
    const had = field.getAttribute("data-invalid")
    field.setAttribute("data-invalid", "true")
    return () => {
      if (had === null) field.removeAttribute("data-invalid")
    }
  }, [own])

  const arrow = "h-3 w-5 rounded-[calc(var(--radius)-5px)] text-muted-foreground active:not-aria-[haspopup]:translate-y-0 [&>svg]:size-3"
  return (
    <>
      <InputGroup className={className}>
        <InputGroupInput
          ref={ref}
          type="number"
          inputMode={props.step === "any" ? "decimal" : "numeric"}
          className="tabular-nums [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
          {...props}
          aria-invalid={own || props["aria-invalid"]}
        />
        <InputGroupAddon align="inline-end" className="flex-col gap-0 py-0">
          {(
            [
              [1, ChevronUpIcon, t("common.increase")],
              [-1, ChevronDownIcon, t("common.decrease")],
            ] as const
          ).map(([dir, Icon, label]) => (
            <InputGroupButton
              key={dir}
              size="icon-xs"
              tabIndex={-1}
              aria-label={label}
              disabled={props.disabled}
              // The chevrons lean towards each other, so the pair reads as one control.
              className={cn(arrow, dir > 0 ? "[&>svg]:translate-y-px" : "[&>svg]:-translate-y-px")}
              onPointerDown={hold(dir)}
              onPointerUp={stop}
              onPointerLeave={stop}
              onPointerCancel={stop}
            >
              <Icon />
            </InputGroupButton>
          ))}
        </InputGroupAddon>
      </InputGroup>
      {own && <FieldError>{t("error.not_a_number")}</FieldError>}
    </>
  )
}
