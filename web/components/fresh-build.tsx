"use client"

import * as React from "react"

/**
 * Reloads the page when the panel has been upgraded under an open tab. The
 * scripts of a page stay those it was opened with; after an upgrade they no
 * longer fit what the server sends, and fixes do not show until a reload.
 * The server names its build (/build-id); the tab remembers the one it was
 * loaded from and checks when it comes back into view, and now and then.
 */
export function FreshBuild() {
  React.useEffect(() => {
    let mine: string | null = null
    let alive = true
    const check = () =>
      fetch("/build-id", { cache: "no-store" })
        .then((r) => (r.ok ? r.text() : null))
        .then((id) => {
          if (!alive || !id) return
          if (mine === null) mine = id
          else if (id !== mine) window.location.reload()
        })
        .catch(() => {})
    check()
    const onShow = () => !document.hidden && check()
    document.addEventListener("visibilitychange", onShow)
    const timer = setInterval(onShow, 5 * 60_000)
    return () => {
      alive = false
      document.removeEventListener("visibilitychange", onShow)
      clearInterval(timer)
    }
  }, [])
  return null
}
