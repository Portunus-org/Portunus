export { cn } from "cn"

/**
 * Classes for the Scroll Area that holds a drawer's body, between its header
 * and footer. A drawer from the bottom has no fixed height, only a maximum,
 * so the area and its viewport take part in the drawer's column layout and
 * shrink with it, instead of the viewport asking for 100% of an unknown
 * height. The last class undoes the table layout Radix gives the content,
 * which would let long lines widen the drawer.
 */
export const drawerScroll =
  "flex min-h-0 flex-1 flex-col [&_[data-slot=scroll-area-viewport]]:h-auto! [&_[data-slot=scroll-area-viewport]]:min-h-0 [&_[data-slot=scroll-area-viewport]]:flex-1 [&_[data-slot=scroll-area-viewport]>div]:block!"
