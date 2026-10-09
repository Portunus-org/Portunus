"use client"

import * as React from "react"
import { cn } from "@/lib/utils"
import { ScrollArea as ScrollAreaPrimitive } from "radix-ui"

/**
 * How long the scrollbar stays after the last scroll, and after the area
 * first has something to scroll.
 */
const REVEAL_MS = 1200

/**
 * The scrollbar behaves like the overlay scrollbars of Firefox and Zen: it
 * is out of sight until there is a reason to look at it. It shows while the
 * content is being scrolled and for a moment after; once when the area
 * first has something to scroll, so that one can see there is more; and
 * whenever the pointer is over it. The area is marked with data-scrolling
 * and the styles do the rest (globals.css).
 */
function useScrollbarReveal(root: React.RefObject<HTMLDivElement | null>) {
  React.useEffect(() => {
    const el = root.current
    const viewport = el?.querySelector<HTMLElement>(':scope > [data-slot="scroll-area-viewport"]')
    if (!el || !viewport) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const reveal = () => {
      el.dataset.scrolling = ""
      clearTimeout(timer)
      timer = setTimeout(() => delete el.dataset.scrolling, REVEAL_MS)
    }
    // Radix moves the thumb from a requestAnimationFrame loop that it starts
    // on the first scroll event. In the minified build that loop never
    // starts (the self-invoking function that holds it is dropped as
    // side-effect free), so the thumb moved once and then stood still until
    // scrolling paused. The thumb is therefore placed here, on every scroll
    // event, from the same proportions Radix uses.
    const place = () => {
      // The area's own scrollbars: a vertical one beside the viewport, a
      // horizontal one passed in as a child.
      const bars = el.querySelectorAll<HTMLElement>(
        ':scope > [data-slot="scroll-area-scrollbar"], :scope > [data-slot="scroll-area-viewport"] > div > [data-slot="scroll-area-scrollbar"]'
      )
      for (const bar of bars) {
        const thumb = bar.querySelector<HTMLElement>('[data-slot="scroll-area-thumb"]')
        if (!thumb) continue
        const style = getComputedStyle(bar)
        if (bar.dataset.orientation === "horizontal") {
          const track = bar.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
          const max = viewport.scrollWidth - viewport.clientWidth
          const at = max > 0 ? (track - thumb.offsetWidth) * Math.min(1, Math.max(0, viewport.scrollLeft / max)) : 0
          thumb.style.transform = `translate3d(${at}px, 0, 0)`
        } else {
          const track = bar.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom)
          const max = viewport.scrollHeight - viewport.clientHeight
          const at = max > 0 ? (track - thumb.offsetHeight) * Math.min(1, Math.max(0, viewport.scrollTop / max)) : 0
          thumb.style.transform = `translate3d(0, ${at}px, 0)`
        }
      }
    }
    const onScroll = () => {
      reveal()
      place()
    }
    viewport.addEventListener("scroll", onScroll, { passive: true })
    // Content often arrives after the area is on screen: announce the
    // scrollbar when it first becomes needed, and again if it was not
    // needed for a while.
    let scrollable = false
    const check = () => {
      const now = viewport.scrollHeight > viewport.clientHeight + 1 || viewport.scrollWidth > viewport.clientWidth + 1
      if (now && !scrollable) reveal()
      scrollable = now
      // Sizes changed: Radix resizes the thumb in its next render, place it
      // after that.
      requestAnimationFrame(place)
    }
    const observer = new ResizeObserver(check)
    observer.observe(viewport)
    if (viewport.firstElementChild) observer.observe(viewport.firstElementChild)
    check()
    return () => {
      viewport.removeEventListener("scroll", onScroll)
      observer.disconnect()
      clearTimeout(timer)
    }
  }, [root])
}

function ScrollArea({
  className,
  children,
  ref,
  ...props
}: React.ComponentProps<typeof ScrollAreaPrimitive.Root>) {
  const root = React.useRef<HTMLDivElement | null>(null)
  useScrollbarReveal(root)
  return (
    <ScrollAreaPrimitive.Root
      data-slot="scroll-area"
      // Always rendered, so its position is always right; whether it is
      // visible is a matter of style (see useScrollbarReveal).
      type="always"
      ref={(node) => {
        root.current = node
        if (typeof ref === "function") ref(node)
        else if (ref) ref.current = node
      }}
      className={cn("relative", className)}
      {...props}
    >
      <ScrollAreaPrimitive.Viewport
        data-slot="scroll-area-viewport"
        className="size-full rounded-[inherit] transition-[color,box-shadow] outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-1"
      >
        {children}
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar />
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  )
}

function ScrollBar({
  className,
  orientation = "vertical",
  ...props
}: React.ComponentProps<typeof ScrollAreaPrimitive.ScrollAreaScrollbar>) {
  return (
    <ScrollAreaPrimitive.ScrollAreaScrollbar
      data-slot="scroll-area-scrollbar"
      data-orientation={orientation}
      orientation={orientation}
      // A thin pill over the content, like the overlay scrollbars of Firefox
      // and Zen: 4px wide and faint at rest, wider and clearer under the
      // pointer or while dragged.
      className={cn(
        "group/scrollbar z-10 flex touch-none p-0.5 transition-[width,height] duration-150 select-none data-horizontal:h-2 data-horizontal:flex-col data-horizontal:px-1 data-horizontal:hover:h-3 data-vertical:h-full data-vertical:w-2 data-vertical:py-1 data-vertical:hover:w-3",
        className
      )}
      {...props}
    >
      <ScrollAreaPrimitive.ScrollAreaThumb
        data-slot="scroll-area-thumb"
        className="relative flex-1 rounded-full bg-foreground/20 transition-colors group-hover/scrollbar:bg-foreground/40 active:bg-foreground/50"
      />
    </ScrollAreaPrimitive.ScrollAreaScrollbar>
  )
}

export { ScrollArea, ScrollBar }
