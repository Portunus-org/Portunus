/**
 * The props of a card that opens what it shows when tapped anywhere: on a
 * phone the whole card is the target, not its name alone. A tap on a
 * control inside the card (its menu, a switch, a link) is that control's.
 */
export function cardTap(open: (() => void) | undefined) {
  if (!open) return {}
  return {
    role: "button",
    tabIndex: 0,
    onClick: (e: React.MouseEvent) => {
      if (
        (e.target as HTMLElement).closest(
          "button, a, input, textarea, label, [role=switch], [role=menu], [role=menuitem], [role=menuitemradio]"
        )
      )
        return
      open()
    },
    onKeyDown: (e: React.KeyboardEvent) => {
      if (e.target !== e.currentTarget || (e.key !== "Enter" && e.key !== " ")) return
      e.preventDefault()
      open()
    },
    className: "cursor-pointer transition-colors active:bg-muted/60 focus-visible:ring-[3px] focus-visible:ring-ring/50 outline-none",
  }
}
