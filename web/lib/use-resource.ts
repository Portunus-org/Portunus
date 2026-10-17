"use client"

import * as React from "react"

/**
 * Loads data with `fetcher` and re-loads on demand or on an interval.
 * Keeps the previous data while reloading, so tables do not flash.
 */
export function useResource<T>(fetcher: () => Promise<T>, intervalMs?: number) {
  const [data, setDataRaw] = React.useState<T | undefined>(undefined)
  // A poll that brings what is already shown changes nothing: the same
  // object is kept, so nothing re-renders.
  const last = React.useRef("")
  const setData = React.useCallback((d: T) => {
    const s = JSON.stringify(d)
    if (s === last.current) return
    last.current = s
    setDataRaw(d)
  }, [])
  const [error, setError] = React.useState<unknown>(null)
  const fetchRef = React.useRef(fetcher)
  React.useEffect(() => {
    fetchRef.current = fetcher
  })

  const reload = React.useCallback(
    () =>
      fetchRef.current().then(
        (d) => {
          setData(d)
          setError(null)
        },
        (e) => setError(e)
      ),
    [setData]
  )

  React.useEffect(() => {
    let alive = true
    const load = () =>
      // Nobody is looking at a hidden tab; it catches up when shown again.
      (document.hidden && last.current !== "" ? Promise.resolve() : fetchRef.current()).then(
        (d) => alive && d !== undefined && (setData(d as T), setError(null)),
        (e) => alive && setError(e)
      )
    load()
    if (!intervalMs) return () => void (alive = false)
    const t = setInterval(load, intervalMs)
    const onShow = () => !document.hidden && load()
    document.addEventListener("visibilitychange", onShow)
    return () => {
      alive = false
      clearInterval(t)
      document.removeEventListener("visibilitychange", onShow)
    }
  }, [intervalMs, setData])

  return { data, error, reload, loading: data === undefined && !error }
}

/**
 * False for the first moments after mount. Lets a panel start moving before
 * its heavy parts (a map, for example) are built.
 */
export function useDeferredMount(delayMs = 250) {
  const [ready, setReady] = React.useState(false)
  React.useEffect(() => {
    const t = setTimeout(() => setReady(true), delayMs)
    return () => clearTimeout(t)
  }, [delayMs])
  return ready
}
