"use client"

import { toast as sonner, type ExternalToast } from "sonner"

type Position = NonNullable<ExternalToast["position"]>

/**
 * Where a notification goes, decided at the moment it appears, so that it
 * never lies on top of what the person is working in:
 *
 * - bottom right, out of the way of the menu and of the page's first words;
 * - bottom left while a panel is open on the right (a client, a node, a
 *   connection, a rule), whose buttons sit exactly in that corner;
 * - top centre on a phone while such a panel is open: there it rises from
 *   the bottom and takes the whole width.
 *
 * A panel that closes as soon as its form is saved does not count: the
 * question is whether it is there once things have settled, so the look is
 * taken a moment after the call. With the panel gone the menu on the left is
 * in full view again, and a notification there would lie on top of it; with
 * the panel still open the menu is dimmed behind it and the left is free.
 *
 * A notification keeps the place it appeared in: it does not jump across
 * the screen when the panel is closed later.
 */
function place(): Position {
  if (typeof document === "undefined") return "bottom-right"
  const panel = document.querySelector<HTMLElement>('[data-slot="drawer-content"][data-state="open"]')
  if (!panel) return "bottom-right"
  return panel.getAttribute("data-vaul-drawer-direction") === "bottom" ? "top-center" : "bottom-left"
}

const placed = (options?: ExternalToast): ExternalToast => ({ position: place(), ...options })

// Long enough for a panel told to close to say so in the page, short enough
// to go unnoticed.
const settle = 90

function later(show: () => void) {
  if (typeof window === "undefined") return
  window.setTimeout(show, settle)
}

type Message = Parameters<typeof sonner.success>[0]

/** The notifications of the panel: sonner's, each put where it is not in the way. */
export const toast = {
  success: (message: Message, options?: ExternalToast) => later(() => sonner.success(message, placed(options))),
  error: (message: Message, options?: ExternalToast) => later(() => sonner.error(message, placed(options))),
  info: (message: Message, options?: ExternalToast) => later(() => sonner.info(message, placed(options))),
  dismiss: sonner.dismiss,
}
