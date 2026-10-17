"use client"

import * as React from "react"

// The nearest box around el that scrolls: a drawer's or a window's body, or the page.
function scroller(el: HTMLElement): HTMLElement {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const { overflowY } = getComputedStyle(p)
    if ((overflowY === "auto" || overflowY === "scroll") && p.scrollHeight > p.clientHeight) return p
  }
  return document.scrollingElement as HTMLElement
}

/**
 * On a phone the keyboard takes the lower part of the screen. When it opens
 * over the field being typed in, that field is brought into the part that
 * is still seen, on every page, in drawers and windows alike. What is seen
 * is where the box the field scrolls in meets the screen above the
 * keyboard: a drawer's buttons under its body cover the rest.
 */
export function KeyboardAware() {
  React.useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const typing = () => {
      const el = document.activeElement
      return el instanceof HTMLElement && el.matches("input:not([type=checkbox]):not([type=radio]), textarea, [contenteditable=true]")
        ? el
        : null
    }
    const margin = 12
    const bring = () => {
      const el = typing()
      if (!el) return
      const box = scroller(el)
      const page = box === document.scrollingElement
      const r = el.getBoundingClientRect()
      const b = page ? { top: 0, bottom: window.innerHeight } : box.getBoundingClientRect()
      const top = Math.max(b.top, vv.offsetTop) + margin
      const bottom = Math.min(b.bottom, vv.offsetTop + vv.height) - margin
      if (r.top >= top && r.bottom <= bottom) return
      const by = r.top + r.height / 2 - (top + bottom) / 2
      if (page) window.scrollBy({ top: by, behavior: "smooth" })
      else box.scrollBy({ top: by, behavior: "smooth" })
    }
    // Once the screen has changed, and once more a moment later: some
    // browsers report the keyboard before the page has been laid out anew.
    let frame = 0
    let later: ReturnType<typeof setTimeout> | undefined
    const follow = () => {
      cancelAnimationFrame(frame)
      clearTimeout(later)
      frame = requestAnimationFrame(bring)
      later = setTimeout(bring, 300)
    }
    vv.addEventListener("resize", follow)
    // A field chosen while the keyboard is already open.
    document.addEventListener("focusin", follow)
    return () => {
      cancelAnimationFrame(frame)
      clearTimeout(later)
      vv.removeEventListener("resize", follow)
      document.removeEventListener("focusin", follow)
    }
  }, [])
  return null
}
