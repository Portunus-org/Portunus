"use client"

import * as React from "react"

/**
 * Loads data with `fetcher` and re-loads on demand or on an interval.
 * Keeps the previous data while reloading, so tables do not flash.
 */
export function useResource<T>(fetcher: () => Promise<T>, intervalMs?: number) {
  const [data, setData] = React.useState<T | undefined>(undefined)
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
    []
  )

  React.useEffect(() => {
    let alive = true
    const load = () =>
      fetchRef.current().then(
        (d) => alive && (setData(d), setError(null)),
        (e) => alive && setError(e)
      )
    load()
    if (!intervalMs) return () => void (alive = false)
    const t = setInterval(load, intervalMs)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [intervalMs])

  return { data, error, reload, loading: data === undefined && !error }
}
