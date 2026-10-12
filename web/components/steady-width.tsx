"use client"

import * as React from "react"

import { cn } from "@/lib/utils"

/**
 * Holds a chart whose drawing follows its width with a delay.
 *
 * While the width changes (the side menu opening or closing) such a chart
 * is drawn again and again, a frame or two late each time, and visibly
 * trails the card it is in. Here it keeps the width it had and is stretched
 * to fit, which costs nothing, and is drawn anew once, when the width has
 * settled. What is given as `overlay` lies over the chart at its true width
 * the whole time: text that has to stay sharp and in place.
 */
export function SteadyWidth({
  children,
  overlay,
  className,
}: {
  children: React.ReactNode
  overlay?: React.ReactNode
  className?: string
}) {
  const box = React.useRef<HTMLDivElement>(null)
  const inner = React.useRef<HTMLDivElement>(null)
  const [width, setWidth] = React.useState<number>()
  const drawn = React.useRef<number>(undefined)
  React.useEffect(() => {
    const el = box.current
    if (!el) return
    let timer: ReturnType<typeof setTimeout> | undefined
    let now = 0 // the width of the box at this moment
    const fit = () => {
      if (inner.current) inner.current.style.transform = !drawn.current || now === drawn.current ? "" : `scaleX(${now / drawn.current})`
    }
    // Ask for a drawing at the width w, and stop stretching the old one in
    // the very frame the new one appears: until then the old one still fits.
    let redraw: ResizeObserver | undefined
    let giveUp: ReturnType<typeof setTimeout> | undefined
    const settle = (w: number) => {
      const done = () => {
        redraw?.disconnect()
        clearTimeout(giveUp)
        drawn.current = w
        fit()
      }
      redraw?.disconnect()
      clearTimeout(giveUp)
      const svg = inner.current?.querySelector("svg")
      if (!svg || w === drawn.current) return done()
      redraw = new ResizeObserver(([entry]) => Math.abs(entry.contentRect.width - w) < 1 && done())
      redraw.observe(svg)
      giveUp = setTimeout(done, 600)
      setWidth(w)
    }
    const ro = new ResizeObserver(([entry]) => {
      const w = entry.contentRect.width
      if (w <= 0) return
      now = w
      if (!drawn.current) {
        drawn.current = w
        setWidth(w)
        return
      }
      fit()
      clearTimeout(timer)
      timer = setTimeout(() => settle(w), 180)
    })
    ro.observe(el)
    return () => {
      ro.disconnect()
      clearTimeout(timer)
      clearTimeout(giveUp)
      redraw?.disconnect()
    }
  }, [])

  return (
    <div ref={box} className={cn("relative", className)}>
      {overlay}
      <div ref={inner} className="origin-left" style={{ width: width ?? "100%" }}>
        {children}
      </div>
    </div>
  )
}
