"use client"

import * as React from "react"

/**
 * On a phone the keyboard takes the lower part of the screen. When it opens
 * over the field being typed in, that field is brought into the part that
 * is still seen, on every page, in drawers and windows alike.
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
    let frame = 0
    const follow = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        const el = typing()
        if (!el) return
        const r = el.getBoundingClientRect()
        const bottom = vv.offsetTop + vv.height
        if (r.bottom > bottom - 16 || r.top < vv.offsetTop) el.scrollIntoView({ block: "center", behavior: "smooth" })
      })
    }
    vv.addEventListener("resize", follow)
    // A field chosen while the keyboard is already open.
    document.addEventListener("focusin", follow)
    return () => {
      cancelAnimationFrame(frame)
      vv.removeEventListener("resize", follow)
      document.removeEventListener("focusin", follow)
    }
  }, [])
  return null
}
