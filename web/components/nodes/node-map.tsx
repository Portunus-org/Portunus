"use client"

import * as React from "react"
import { useTheme } from "next-themes"
import { LocateFixedIcon, MinusIcon, PlusIcon } from "lucide-react"
import type * as Leaflet from "leaflet"
import "leaflet/dist/leaflet.css"

import { Button } from "@/components/ui/button"
import { ButtonGroup } from "@/components/ui/button-group"
import type { GeoLocation, Node } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"
import { cn } from "@/lib/utils"

export type MapState = "main" | "online" | "offline" | "pending" | "off"

export interface MapPoint {
  id: string
  name: string
  detail?: string
  state: MapState
  location: GeoLocation
}

/** The state of a node as the map shows it. */
export function nodeMapState(n: Node): MapState {
  if (!n.enabled) return "off"
  if (n.status.state === "online") return "online"
  if (n.status.state === "pending") return "pending"
  return "offline"
}

// One plain dot per server. The fill says the state; so do the legend, the
// label on hover and the table under the map, so colour never stands alone.
const dot: Record<MapState, string> = {
  main: "size-3.5 bg-foreground",
  online: "size-3 bg-emerald-500",
  offline: "size-3 bg-destructive",
  pending: "size-3 border-2 border-muted-foreground bg-background",
  off: "size-3 border-2 border-muted-foreground/60 bg-background",
}

function Dot({ state, className }: { state: MapState; className?: string }) {
  return <span className={cn("block rounded-full ring-2 ring-background", dot[state], className)} />
}

const MAX_ZOOM = 12

/**
 * Servers on a map that can be dragged and zoomed. Detailed tiles (cities,
 * regions) come through the panel; without them (`tiles` false) the map
 * falls back to plain country outlines bundled with the panel.
 *
 * With onPick the map is a picker: a click reports the place under it.
 */
export function NodeMap({
  points,
  tiles,
  onPick,
  className,
}: {
  points: MapPoint[]
  tiles: boolean
  onPick?: (location: { lat: number; lon: number }) => void
  className?: string
}) {
  const { t } = useI18n()
  const { resolvedTheme } = useTheme()
  const el = React.useRef<HTMLDivElement>(null)
  const [L, setL] = React.useState<typeof Leaflet | null>(null)
  const map = React.useRef<Leaflet.Map | null>(null)
  const layer = React.useRef<Leaflet.LayerGroup | null>(null)
  const fitted = React.useRef("")
  const pick = React.useRef(onPick)
  React.useEffect(() => {
    pick.current = onPick
  })

  // Leaflet touches `window` on import: load it in the browser only.
  React.useEffect(() => {
    let alive = true
    import("leaflet").then((m) => alive && setL(() => m.default ?? m))
    return () => {
      alive = false
    }
  }, [])

  // The map itself, once.
  React.useEffect(() => {
    if (!L || !el.current) return
    const m = L.map(el.current, {
      zoomControl: false,
      attributionControl: false,
      minZoom: 2,
      maxZoom: MAX_ZOOM,
      worldCopyJump: true,
      zoomSnap: 0.5,
    }).setView([35, 20], 2)
    m.on("click", (e) =>
      pick.current?.({
        lat: Math.round(e.latlng.lat * 1000) / 1000,
        lon: Math.round(e.latlng.wrap().lng * 1000) / 1000,
      })
    )
    map.current = m
    layer.current = L.layerGroup().addTo(m)
    return () => {
      m.remove()
      map.current = null
      layer.current = null
      fitted.current = ""
    }
  }, [L])

  // The base: tiles in the colours of the theme, or outlines.
  React.useEffect(() => {
    const m = map.current
    if (!L || !m) return
    let base: Leaflet.Layer
    let alive = true
    if (tiles) {
      base = L.tileLayer(`/api/map/tiles/${resolvedTheme === "dark" ? "dark" : "light"}/{z}/{x}/{y}.png`, { maxZoom: MAX_ZOOM }).addTo(m)
    } else {
      const group = L.layerGroup().addTo(m)
      base = group
      Promise.all([import("topojson-client"), import("world-atlas/countries-110m.json")]).then(([topo, world]) => {
        if (!alive) return
        type Topology = Parameters<typeof topo.feature>[0]
        const data = (world.default ?? world) as unknown as Topology
        L.geoJSON(topo.feature(data, data.objects.countries) as Parameters<typeof L.geoJSON>[0], {
          interactive: false,
          style: () => ({ className: "portunus-map-land" }),
        }).addTo(group)
      })
    }
    return () => {
      alive = false
      base.remove()
    }
  }, [L, tiles, resolvedTheme])

  // Servers and links.
  React.useEffect(() => {
    const m = map.current
    const g = layer.current
    if (!L || !m || !g) return
    g.clearLayers()
    const main = points.find((p) => p.state === "main")
    for (const p of points) {
      if (main && p !== main) {
        L.polyline(
          [
            [main.location.lat, main.location.lon],
            [p.location.lat, p.location.lon],
          ],
          {
            className: cn("portunus-map-link", p.state !== "online" && "portunus-map-link-idle"),
            interactive: false,
          }
        ).addTo(g)
      }
    }
    // Names stay on the map while there is room for them.
    const permanent = points.length <= 6
    // Built from text nodes: names are typed by people.
    const label = (p: MapPoint, full: boolean) => {
      const box = document.createElement("div")
      box.className = "grid gap-0.5"
      const name = document.createElement("span")
      name.className = "font-medium text-foreground"
      name.textContent = p.name
      box.append(name)
      if (full) {
        for (const line of [t(`nodes.map.state.${p.state}` as MessageKey), p.detail]) {
          if (!line) continue
          const row = document.createElement("span")
          row.textContent = line
          box.append(row)
        }
      }
      return box
    }
    const markers = points.map((p) => {
      const size = p.state === "main" ? 14 : 12
      const icon = L.divIcon({
        className: "",
        html: `<span class="block rounded-full ring-2 ring-background shadow-sm ${dot[p.state]}"></span>`,
        iconSize: [size, size],
        iconAnchor: [size / 2, size / 2],
      })
      const marker = L.marker([p.location.lat, p.location.lon], {
        icon,
        keyboard: true,
        title: p.name,
        zIndexOffset: p.state === "main" ? 100 : 0,
      }).addTo(g)
      if (permanent) {
        marker.on("mouseover focus", () => marker.setTooltipContent(label(p, true)))
        marker.on("mouseout blur", () => marker.setTooltipContent(label(p, false)))
      }
      return { p, marker }
    })
    // Put each name on the side of its dot where it does not cover another
    // name; servers in neighbouring cities would otherwise hide each other.
    const sides = ["top", "right", "bottom", "left"] as const
    const offsets = {
      top: [0, -8],
      right: [8, 0],
      bottom: [0, 8],
      left: [-8, 0],
    } as const
    const place = () => {
      const taken: { x1: number; y1: number; x2: number; y2: number }[] = []
      for (const { p, marker } of markers) {
        const at = m.latLngToContainerPoint([p.location.lat, p.location.lon])
        const w = p.name.length * 6.6 + 18
        const h = 26
        const boxes = {
          top: {
            x1: at.x - w / 2,
            y1: at.y - 12 - h,
            x2: at.x + w / 2,
            y2: at.y - 12,
          },
          right: {
            x1: at.x + 12,
            y1: at.y - h / 2,
            x2: at.x + 12 + w,
            y2: at.y + h / 2,
          },
          bottom: {
            x1: at.x - w / 2,
            y1: at.y + 12,
            x2: at.x + w / 2,
            y2: at.y + 12 + h,
          },
          left: {
            x1: at.x - 12 - w,
            y1: at.y - h / 2,
            x2: at.x - 12,
            y2: at.y + h / 2,
          },
        }
        const free = (b: (typeof taken)[number]) => !taken.some((o) => b.x1 < o.x2 && b.x2 > o.x1 && b.y1 < o.y2 && b.y2 > o.y1)
        const side = (permanent && sides.find((s) => free(boxes[s]))) || "top"
        taken.push(boxes[side])
        marker.unbindTooltip()
        marker.bindTooltip(label(p, !permanent), {
          permanent,
          direction: side,
          offset: [...offsets[side]],
          className: "portunus-map-label",
          opacity: 1,
        })
      }
    }
    // Frame the servers when the set of places changes, not on every refresh:
    // the operator may have moved the map.
    const key = points.map((p) => `${p.id}:${p.location.lat},${p.location.lon}`).join("|")
    if (key !== fitted.current && points.length > 0) {
      fitted.current = key
      const bounds = L.latLngBounds(points.map((p) => [p.location.lat, p.location.lon]))
      m.fitBounds(bounds.pad(0.4), { maxZoom: 5, animate: false })
    }
    place()
    m.on("zoomend", place)
    return () => {
      m.off("zoomend", place)
    }
  }, [L, points, t])

  const fit = () => {
    if (!L || !map.current || points.length === 0) return
    map.current.fitBounds(L.latLngBounds(points.map((p) => [p.location.lat, p.location.lon])).pad(0.4), { maxZoom: 5 })
  }

  return (
    <div className={cn("relative isolate overflow-hidden rounded-lg border", className)}>
      <div
        ref={el}
        className={cn("portunus-map h-80 w-full bg-muted/40 md:h-[26rem]", onPick && "portunus-map-pick")}
        role="application"
        aria-label={t("nodes.map.title")}
        data-vaul-no-drag
      />
      <ButtonGroup orientation="vertical" className="absolute top-2 right-2 z-[500] shadow-xs">
        <Button variant="outline" size="icon-sm" className="bg-background" onClick={() => map.current?.zoomIn()}>
          <PlusIcon />
          <span className="sr-only">{t("nodes.map.zoomIn")}</span>
        </Button>
        <Button variant="outline" size="icon-sm" className="bg-background" onClick={() => map.current?.zoomOut()}>
          <MinusIcon />
          <span className="sr-only">{t("nodes.map.zoomOut")}</span>
        </Button>
        <Button variant="outline" size="icon-sm" className="bg-background" onClick={fit}>
          <LocateFixedIcon />
          <span className="sr-only">{t("nodes.map.fit")}</span>
        </Button>
      </ButtonGroup>
      {tiles && (
        <span className="absolute right-1 bottom-1 z-[500] rounded bg-background/80 px-1.5 py-0.5 text-[10px] text-muted-foreground">
          © OpenStreetMap
        </span>
      )}
    </div>
  )
}

/** What the marks on the map mean. */
export function NodeMapLegend({ states }: { states: MapState[] }) {
  const { t } = useI18n()
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {states.map((s) => (
        <li key={s} className="flex items-center gap-2">
          <Dot state={s} className="size-2.5 ring-0" />
          {t(`nodes.map.state.${s}` as MessageKey)}
        </li>
      ))}
    </ul>
  )
}
