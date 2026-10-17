"use client"

import * as React from "react"
import { useTheme } from "next-themes"
import { LocateFixedIcon, MinusIcon, PlusIcon } from "lucide-react"
import type * as Leaflet from "leaflet"

import type { MapStyle } from "@/lib/api"
import { mapCities, mapCityLatin, mapCountries } from "@/lib/map-places"
import { mapFontFamily } from "@/lib/map-fonts"
import { canDraw } from "@/lib/map-scripts"
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
// The built-in country shapes hold up to this zoom; with detail from vector
// tiles the map goes on to where towns a few kilometres apart stand clear.
const DRAWN_MAX_ZOOM = 6
const DETAIL_MAX_ZOOM = 13

/** Places with a lower rank are the more important ones. */
const byRank = (a: Record<string, unknown>, b: Record<string, unknown>) => Number(a.rank ?? 99) - Number(b.rank ?? 99)
// The same for tiles that carry the number of inhabitants and no rank.
const byPeople = (a: Record<string, unknown>, b: Record<string, unknown>) => Number(b.population ?? 0) - Number(a.population ?? 0)

type Countries = { features: { geometry: { type: string; coordinates: number[][][] | number[][][][] } | null }[] }

/**
 * The built-in map: all the land of the world as one shape, painted on a
 * canvas that covers the map, anew in every frame the view changes.
 *
 * Leaflet's own way with shapes is to work out, for the part in view, the
 * screen position of every point, and to do it again whenever the view has
 * moved: the land then ends in a straight edge while the map is dragged,
 * and every change of zoom stops for the reckoning. Here the shape is built
 * once, in coordinates of the whole world, and a frame is two calls with a
 * scale and an offset. The whole world is always there, at any zoom, and
 * the map moves as often as the screen is refreshed.
 */
function worldCanvas(m: Leaflet.Map, fine: Countries, coarse: Countries, colours: () => { land: string; border: string }) {
  const y = (lat: number) => {
    const phi = (Math.max(-85.0511, Math.min(85.0511, lat)) * Math.PI) / 180
    return (1 - Math.log(Math.tan(Math.PI / 4 + phi / 2)) / Math.PI) / 2
  }
  // Each country as a shape of its own, with the box it lies in: a frame
  // paints only those in view. And twice over: the outlines drawn for a
  // view of the whole world carry a tenth of the points, and nobody can
  // tell them apart at that scale.
  type Shape = { path: Path2D; west: number; east: number; north: number; south: number }
  const shapes = (countries: Countries): Shape[] =>
    countries.features.flatMap((f) => {
      if (!f.geometry) return []
      const polygons = (f.geometry.type === "Polygon" ? [f.geometry.coordinates] : f.geometry.coordinates) as number[][][][]
      const shape: Shape = { path: new Path2D(), west: Infinity, east: -Infinity, north: Infinity, south: -Infinity }
      for (const polygon of polygons)
        for (const ring of polygon) {
          ring.forEach(([lon, lat], i) => {
            const px = (lon + 180) / 360
            const py = y(lat)
            if (i) shape.path.lineTo(px, py)
            else shape.path.moveTo(px, py)
            shape.west = Math.min(shape.west, px)
            shape.east = Math.max(shape.east, px)
            shape.north = Math.min(shape.north, py)
            shape.south = Math.max(shape.south, py)
          })
          shape.path.closePath()
        }
      return [shape]
    })
  const detailed = shapes(fine)
  const rough = shapes(coarse)
  const canvas = document.createElement("canvas")
  canvas.className = "portunus-map-world"
  const ctx = canvas.getContext("2d")!
  const paint = colours()
  let plain = false
  const draw = () => {
    const size = m.getSize()
    const dpr = window.devicePixelRatio || 1
    if (canvas.width !== Math.round(size.x * dpr) || canvas.height !== Math.round(size.y * dpr)) {
      canvas.width = Math.round(size.x * dpr)
      canvas.height = Math.round(size.y * dpr)
    }
    const scale = 256 * 2 ** m.getZoom() // the width of the world in pixels
    const corner = m.getPixelBounds().min! // where the view's corner is in it
    ctx.setTransform(1, 0, 0, 1, 0, 0)
    ctx.clearRect(0, 0, canvas.width, canvas.height)
    ctx.fillStyle = paint.land
    if (plain) {
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      return
    }
    ctx.strokeStyle = paint.border
    ctx.lineJoin = "round"
    // The world repeats sideways; draw the copies that are in view. (Land
    // across the 180th meridian reaches a little past its own copy.)
    const world = m.getZoom() < 2.8 ? rough : detailed
    const top = corner.y / scale
    const bottom = (corner.y + size.y) / scale
    for (let copy = Math.floor(corner.x / scale) - 1; copy * scale - corner.x < size.x; copy++) {
      ctx.setTransform(dpr * scale, 0, 0, dpr * scale, dpr * (copy * scale - corner.x), -dpr * corner.y)
      ctx.lineWidth = 0.7 / scale
      const left = corner.x / scale - copy
      const right = (corner.x + size.x) / scale - copy
      for (const c of world) {
        if (c.east < left || c.west > right || c.south < top || c.north > bottom) continue
        ctx.fill(c.path)
        ctx.stroke(c.path)
      }
    }
  }
  let shown = false
  return {
    addTo() {
      if (shown) return
      shown = true
      m.getContainer().prepend(canvas)
      m.on("move zoom viewreset resize", draw)
      draw()
    },
    remove() {
      if (!shown) return
      shown = false
      m.off("move zoom viewreset resize", draw)
      canvas.remove()
    },
    /**
     * Under the detailed view the canvas is a plain sheet in the colour of
     * land: tiles scaled to a zoom between two whole ones leave hairline
     * gaps, and what shows through them should not be the sea.
     */
    sheet(on: boolean) {
      if (plain === on) return
      plain = on
      if (shown) draw()
    },
  }
}

/**
 * The map's colours as the canvas needs them: the theme's CSS colours worked
 * out to plain rgb() by the browser itself.
 */
function mapPalette(at: HTMLElement) {
  const probe = document.createElement("span")
  probe.style.display = "none"
  at.append(probe)
  const canvas = document.createElement("canvas")
  canvas.width = canvas.height = 1
  const ctx = canvas.getContext("2d", { willReadFrequently: true })!
  const rgb = (css: string) => {
    probe.style.color = css
    ctx.clearRect(0, 0, 1, 1)
    ctx.fillStyle = getComputedStyle(probe).color
    ctx.fillRect(0, 0, 1, 1)
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data
    return `rgb(${r}, ${g}, ${b})`
  }
  const palette = {
    sea: rgb("var(--card)"),
    land: rgb("color-mix(in oklab, var(--foreground) 9%, var(--card))"),
    border: rgb("color-mix(in oklab, var(--foreground) 22%, var(--card))"),
    text: rgb("var(--muted-foreground)"),
    faint: rgb("color-mix(in oklab, var(--muted-foreground) 70%, var(--card))"),
  }
  probe.remove()
  return palette
}

/**
 * Servers on a map that can be dragged and zoomed. Detailed tiles (cities,
 * regions) come through the panel; without them (`tiles` false) the map
 * falls back to plain country outlines bundled with the panel.
 *
 * With onPick the map is a picker: a click reports the place under it.
 */
export function NodeMap({
  points: given,
  tiles,
  mapStyle,
  onPick,
  className,
}: {
  points: MapPoint[]
  tiles: boolean
  /** How the tiles look and what goes with them; unknown until loaded. */
  mapStyle?: MapStyle
  onPick?: (location: { lat: number; lon: number }) => void
  className?: string
}) {
  const { t } = useI18n()
  const { resolvedTheme } = useTheme()
  // Callers build the list anew on every render (and pages re-render on
  // every poll): the map is redrawn only when the points really differ.
  const pointsKey = JSON.stringify(given)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const points = React.useMemo(() => given, [pointsKey])
  const el = React.useRef<HTMLDivElement>(null)
  const [L, setL] = React.useState<typeof Leaflet | null>(null)
  const map = React.useRef<Leaflet.Map | null>(null)
  const layer = React.useRef<Leaflet.LayerGroup | null>(null)
  const fitted = React.useRef("")
  const zoomBy = React.useRef<(levels: number) => void>(undefined)
  // What the base is made of, for the code that zooms (set up once).
  const tilesRef = React.useRef(false)
  const detailRef = React.useRef(false)
  const labels = tiles && !!mapStyle?.labels
  // The drawn map can be zoomed in past the scale of countries, into a
  // view made of tiles the server fetches from elsewhere. A server may be
  // unable to fetch them, so that is tried first, with one tile. Until it
  // is known to work the map is the built-in one alone, shapes and names
  // that come with the panel and depend on nothing, and it does not zoom
  // in further than they look good at.
  const wantsDetail = !tiles && !!mapStyle?.detail
  const [detailWorks, setDetailWorks] = React.useState(false)
  React.useEffect(() => {
    if (!wantsDetail || detailWorks) return
    let alive = true
    const probe = () =>
      fetch("/api/map/detail/4/9/4.pbf")
        .then(
          (r) => r.ok,
          () => false
        )
        .then((ok) => alive && ok && setDetailWorks(true))
    probe()
    // A failure is not for ever: the provider may be back, or reachable
    // again.
    const timer = setInterval(probe, 60_000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [wantsDetail, detailWorks])
  const detail = wantsDetail && detailWorks
  React.useEffect(() => {
    tilesRef.current = tiles
    detailRef.current = detail
  })
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
      // Any zoom is a zoom: the map is drawn from shapes, not from pictures
      // made for whole steps, and the wheel moves through them smoothly
      // (see below) instead of Leaflet's own stepping.
      zoomSnap: 0,
      scrollWheelZoom: false,
      // Leaflet animates a zoom by stretching what is on its panes with
      // CSS, in a rhythm of its own; the land is drawn outside the panes,
      // frame by frame. So every zoom is made here, the same way.
      zoomAnimation: false,
      fadeAnimation: false,
    }).setView([35, 20], 2)
    m.on("click", (e) =>
      pick.current?.({
        lat: Math.round(e.latlng.lat * 1000) / 1000,
        lon: Math.round(e.latlng.wrap().lng * 1000) / 1000,
      })
    )
    map.current = m
    m.createPane("links").style.zIndex = "420"
    m.getPane("links")!.style.pointerEvents = "none"
    layer.current = L.layerGroup().addTo(m)
    // Leaflet learns its size once. When the box changes (the sidebar is
    // folded, the window resized, a drawer finishes sliding in) it has to
    // be told, or the new part stays without tiles.
    const box = el.current
    const resized = new ResizeObserver(() => m.invalidateSize({ pan: false }))
    resized.observe(box)

    // The wheel zooms continuously, in and out: each turn sets where the
    // zoom is headed, and the map glides there frame by frame, the place
    // under the pointer staying under it. Leaflet's own wheel handling
    // jumps a step at a time and animates only between whole steps.
    const moving = m as unknown as {
      _moveStart: (zoomChanged: boolean, noMoveStart: boolean) => void
      _move: (center: Leaflet.LatLng, zoom: number) => void
      _moveEnd: (zoomChanged: boolean) => void
    }
    const calm = window.matchMedia("(prefers-reduced-motion: reduce)").matches
    let goal = m.getZoom()
    let frame = 0
    let under: Leaflet.Point | null = null
    let place: Leaflet.LatLng | null = null
    let last = performance.now()
    const glide = () => {
      const zoom = m.getZoom()
      const left = goal - zoom
      const done = calm || Math.abs(left) < 0.004
      // A share of what is left each frame, by the time gone: the same
      // pace at 60 frames a second and at 144.
      const now = performance.now()
      const share = 1 - Math.exp(-(now - last) / 55)
      last = now
      const next = done ? goal : zoom + left * share
      // The centre that keeps `place` under the pointer at this zoom.
      const centre = under && place ? m.unproject(m.project(place, next).subtract(under).add(m.getSize().divideBy(2)), next) : m.getCenter()
      moving._move(centre, next)
      if (done) {
        frame = 0
        moving._moveEnd(true)
      } else frame = requestAnimationFrame(glide)
    }
    // Where the view is made of tiles the zoom comes to rest on a whole
    // level: tiles are drawn for whole levels, and between two of them they
    // are scaled, a little soft and with hairline gaps. So a moment after
    // the wheel stops the map eases on to the nearest one.
    let settle: ReturnType<typeof setTimeout> | undefined
    const tiled = () => tilesRef.current || (detailRef.current && goal > DRAWN_MAX_ZOOM)
    const rest = () => {
      if (!tiled() || Number.isInteger(goal)) return
      goal = Math.max(m.getMinZoom(), Math.min(m.getMaxZoom(), Math.round(goal)))
      if (!frame) {
        last = performance.now()
        moving._moveStart(true, false)
        frame = requestAnimationFrame(glide)
      }
    }
    const wheel = (e: WheelEvent) => {
      e.preventDefault()
      clearTimeout(settle)
      settle = setTimeout(rest, 140)
      const r = box.getBoundingClientRect()
      under = L.point(e.clientX - r.left, e.clientY - r.top)
      place = m.containerPointToLatLng(under)
      // Lines (Firefox) or pixels; a pinch on a touchpad comes as a wheel
      // with Ctrl held and much smaller numbers.
      const pixels = e.deltaY * (e.deltaMode === 1 ? 33 : e.deltaMode === 2 ? 300 : 1)
      if (!frame) goal = m.getZoom()
      goal = Math.max(m.getMinZoom(), Math.min(m.getMaxZoom(), goal - pixels / (e.ctrlKey ? 60 : 260)))
      if (!frame) {
        last = performance.now()
        moving._moveStart(true, false)
        frame = requestAnimationFrame(glide)
      }
    }
    box.addEventListener("wheel", wheel, { passive: false })
    // The buttons: the same glide, about the middle of the map.
    zoomBy.current = (levels: number) => {
      under = place = null
      if (!frame) goal = m.getZoom()
      goal = Math.max(m.getMinZoom(), Math.min(m.getMaxZoom(), Math.round((goal + levels) * 2) / 2))
      if (tiled()) goal = Math.round(goal)
      if (!frame) {
        last = performance.now()
        moving._moveStart(true, false)
        frame = requestAnimationFrame(glide)
      }
    }
    return () => {
      clearTimeout(settle)
      box.removeEventListener("wheel", wheel)
      cancelAnimationFrame(frame)
      resized.disconnect()
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
    let stopNames = () => {}
    let stopFonts = () => {}
    // The built-in shapes are drawn for a view of countries, not of streets.
    m.setMaxZoom(tiles ? MAX_ZOOM : detail ? DETAIL_MAX_ZOOM : DRAWN_MAX_ZOOM)
    if (tiles) {
      const theme = resolvedTheme === "dark" ? "dark" : "light"
      const group = L.layerGroup().addTo(m)
      base = group
      L.tileLayer(`/api/map/tiles/${theme}/{z}/{x}/{y}.png`, { maxZoom: MAX_ZOOM }).addTo(group)
      // Names of places, over the base, from a few steps of zoom on: the
      // whole world at once needs no cities.
      if (labels) L.tileLayer(`/api/map/labels/${theme}/{z}/{x}/{y}.png`, { maxZoom: MAX_ZOOM, minZoom: 4 }).addTo(group)
    } else {
      // The panel's own map: countries drawn as shapes in the colours of
      // the theme, the sea left as the page behind them, names set in the
      // panel's type. Sharp on any screen, and nothing is fetched from
      // anywhere else.
      const group = L.layerGroup().addTo(m)
      base = group
      const places = L.layerGroup().addTo(group)
      if (!m.getPane("places")) {
        const pane = m.createPane("places")
        pane.style.zIndex = "450" // over the shapes, under the servers
        pane.style.pointerEvents = "none"
      }
      // A name as a marker without a box: just text at a point.
      const name = (text: string, at: [number, number], className: string) => {
        const span = document.createElement("span")
        span.className = className
        span.textContent = text
        return L.marker(at, {
          pane: "places",
          interactive: false,
          keyboard: false,
          icon: L.divIcon({ className: "", html: span, iconSize: [0, 0] }),
        })
      }
      type Named = { marker: Leaflet.Marker; shown: (zoom: number) => boolean }
      const named: Named[] = mapCities.map(([city, lat, lon, rank]) => ({
        // In its own writing where this computer can draw it.
        marker: name(canDraw(city) ? city : (mapCityLatin[city] ?? city), [lat, lon], "portunus-map-city"),
        shown: (zoom) => zoom >= [4, 5, 5.5, 6][rank - 1],
      }))
      // Past the scale of countries the built-in shapes are too coarse and
      // the built-in names too few: the view is then drawn from vector
      // tiles the panel fetches (coasts, borders of countries and regions,
      // names of cities, towns and villages as they are written there), in
      // the same colours, and as sharp. The library is loaded on first use.
      let land: ReturnType<typeof worldCanvas> | null = null
      let close: Leaflet.Layer | null = null
      let loadingClose = false
      const showClose = () => {
        if (close) return void close.addTo(group)
        if (loadingClose) return
        loadingClose = true
        import("protomaps-leaflet").then((pm) => {
          if (!alive) return
          // The library looks for Leaflet where a script tag would put it.
          ;(window as unknown as { L: typeof Leaflet }).L = L
          const c = mapPalette(el.current!)
          const family = `${getComputedStyle(document.body).fontFamily}, ${mapFontFamily}`
          const kind = (f: { props: Record<string, unknown> }) => String(f.props.class ?? "")
          const place = (f: { props: Record<string, unknown> }) => String(f.props.kind ?? "")
          // The local name where this computer has a font for it, else the
          // name in Latin letters that the tiles carry beside it.
          const localName = (_z: number, f?: { props: Record<string, unknown> }) =>
            typeof f?.props.name === "string" && !canDraw(f.props.name) ? ["name:latin", "name_en", "name"] : ["name"]
          const text = (weight: number, size: number, fill: string, extra: object = {}) =>
            new pm.CenteredTextSymbolizer({
              labelProps: localName,
              font: `${weight} ${size}px ${family}`,
              fill,
              stroke: c.land,
              width: 3,
              ...extra,
            })
          close = pm.leafletLayer({
            url: "/api/map/detail/{z}/{x}/{y}.pbf",
            maxDataZoom: 14,
            backgroundColor: c.land,
            // The tiles come in one of two layouts, whichever source the
            // server could reach: OpenMapTiles (water, boundary, place) or
            // Shortbread (ocean, water_polygons, boundaries, place_labels).
            // Both are drawn the same; rules for layers a tile does not
            // have do nothing.
            paintRules: [
              { dataLayer: "water", symbolizer: new pm.PolygonSymbolizer({ fill: c.sea }) },
              { dataLayer: "ocean", symbolizer: new pm.PolygonSymbolizer({ fill: c.sea }) },
              { dataLayer: "water_polygons", symbolizer: new pm.PolygonSymbolizer({ fill: c.sea }) },
              ...["boundary", "boundaries"].flatMap((dataLayer) => [
                {
                  dataLayer,
                  filter: (_z: number, f: { props: Record<string, unknown> }) => Number(f.props.admin_level) === 4 && !f.props.maritime,
                  symbolizer: new pm.LineSymbolizer({ color: c.border, width: 0.8, dash: [3, 3] }),
                },
                {
                  dataLayer,
                  filter: (_z: number, f: { props: Record<string, unknown> }) => Number(f.props.admin_level) === 2 && !f.props.maritime,
                  symbolizer: new pm.LineSymbolizer({ color: c.border, width: 1.4 }),
                },
              ]),
            ],
            labelRules: [
              { dataLayer: "place", filter: (_z, f) => kind(f) === "city", symbolizer: text(600, 13, c.text), sort: byRank },
              { dataLayer: "place", minzoom: 8, filter: (_z, f) => kind(f) === "town", symbolizer: text(500, 12, c.text), sort: byRank },
              {
                dataLayer: "place",
                maxzoom: 8,
                filter: (_z, f) => kind(f) === "country",
                symbolizer: text(500, 11, c.faint, { textTransform: "uppercase", letterSpacing: 1.5 }),
              },
              {
                dataLayer: "place",
                minzoom: 11,
                filter: (_z, f) => kind(f) === "village",
                symbolizer: text(400, 11, c.faint),
                sort: byRank,
              },
              {
                dataLayer: "place",
                minzoom: 12,
                filter: (_z, f) => kind(f) === "suburb",
                symbolizer: text(400, 11, c.faint),
                sort: byRank,
              },
              // The same names in the Shortbread layout.
              {
                dataLayer: "place_labels",
                filter: (_z, f) => ["city", "capital", "state_capital"].includes(place(f)),
                symbolizer: text(600, 13, c.text),
                sort: byPeople,
              },
              {
                dataLayer: "place_labels",
                minzoom: 8,
                filter: (_z, f) => place(f) === "town",
                symbolizer: text(500, 12, c.text),
                sort: byPeople,
              },
              {
                dataLayer: "boundary_labels",
                maxzoom: 8,
                filter: (_z, f) => Number(f.props.admin_level) === 2,
                symbolizer: text(500, 11, c.faint, { textTransform: "uppercase", letterSpacing: 1.5 }),
              },
              {
                dataLayer: "place_labels",
                minzoom: 11,
                filter: (_z, f) => ["village", "hamlet"].includes(place(f)),
                symbolizer: text(400, 11, c.faint),
                sort: byPeople,
              },
              {
                dataLayer: "place_labels",
                minzoom: 12,
                filter: (_z, f) => ["suburb", "quarter", "neighbourhood"].includes(place(f)),
                symbolizer: text(400, 11, c.faint),
                sort: byPeople,
              },
            ],
          }) as unknown as Leaflet.Layer
          if (m.getZoom() > DRAWN_MAX_ZOOM) close.addTo(group)
          // A canvas is not redrawn by itself when a font it asked for
          // arrives: names drawn as boxes meanwhile are drawn again.
          const redraw = () => {
            const layer = close as unknown as { clearLayout?: () => void; rerenderTiles?: () => void }
            layer.clearLayout?.()
            layer.rerenderTiles?.()
          }
          document.fonts.addEventListener("loadingdone", redraw)
          stopFonts = () => document.fonts.removeEventListener("loadingdone", redraw)
        })
      }
      // Names appear when there is room for them at the current zoom. Up
      // close it is the detailed view alone, over a plain sheet: the
      // built-in shapes are too coarse to lie under it.
      const refresh = () => {
        const zoom = m.getZoom()
        const near = detail && zoom > DRAWN_MAX_ZOOM
        land?.addTo()
        land?.sheet(near)
        if (near) showClose()
        else close?.remove()
        for (const n of named) {
          if (!near && n.shown(zoom)) n.marker.addTo(places)
          else n.marker.remove()
        }
      }
      m.on("zoomend", refresh)
      Promise.all([import("topojson-client"), import("world-atlas/countries-50m.json"), import("world-atlas/countries-110m.json")]).then(
        ([topo, world, small]) => {
          if (!alive) return
          type Topology = Parameters<typeof topo.feature>[0]
          const data = (world.default ?? world) as unknown as Topology
          const countries = topo.feature(data, data.objects.countries) as unknown as {
            features: { properties: { name?: string }; geometry: { type: string; coordinates: number[][][] | number[][][][] } }[]
          }
          // A piece of land that lies across the 180th meridian comes with
          // points on both edges of the map, and would be drawn as a band
          // right across the world. Its western points are moved past the
          // eastern edge instead, where they belong.
          const unwrap = (set: Countries) => {
            for (const f of set.features) {
              if (!f.geometry) continue
              const polygons = (f.geometry.type === "Polygon" ? [f.geometry.coordinates] : f.geometry.coordinates) as number[][][][]
              for (const polygon of polygons) {
                for (const ring of polygon) {
                  let west = 180, east = -180 // prettier-ignore
                  for (const [lon] of ring) {
                    west = Math.min(west, lon)
                    east = Math.max(east, lon)
                  }
                  if (east - west > 300) for (const point of ring) if (point[0] < 0) point[0] += 360
                }
              }
            }
            return set
          }
          unwrap(countries as unknown as Countries)
          const far = (small.default ?? small) as unknown as Topology
          const coarse = unwrap(topo.feature(far, far.objects.countries) as unknown as Countries)
          land = worldCanvas(m, countries as unknown as Countries, coarse, () => mapPalette(el.current!))
          for (const f of countries.features) {
            if (!f.properties.name || !f.geometry) continue
            // A country is named in the middle of its largest piece, once
            // that piece is wide enough on screen to hold the name.
            const rings = (f.geometry.type === "Polygon" ? [f.geometry.coordinates] : f.geometry.coordinates) as number[][][][]
            let best = { w: 0, h: 0, lat: 0, lon: 0 }
            for (const polygon of rings) {
              let west = 180, east = -180, south = 90, north = -90 // prettier-ignore
              for (const [lon, lat] of polygon[0]) {
                west = Math.min(west, lon)
                east = Math.max(east, lon)
                south = Math.min(south, lat)
                north = Math.max(north, lat)
              }
              if ((east - west) * (north - south) > best.w * best.h)
                best = { w: east - west, h: north - south, lat: (south + north) / 2, lon: (west + east) / 2 }
            }
            if (best.w === 0) continue
            const local = mapCountries[f.properties.name]
            const label = local && canDraw(local) ? local : f.properties.name
            // Letters are spaced out only in scripts that are written apart:
            // spacing would pull the joined letters of Arabic to pieces.
            const spaced = /^[\p{Script=Latin}\p{Script=Cyrillic}\p{Script=Greek}\p{Script=Armenian}\p{Script=Georgian}\s\p{P}]+$/u.test(
              label
            )
            named.push({
              marker: name(
                label,
                [best.lat, best.lon],
                spaced ? "portunus-map-country portunus-map-country-spaced" : "portunus-map-country"
              ),
              shown: (zoom) => {
                const px = (256 * 2 ** zoom) / 360
                return best.w * px > label.length * 9 + 24 && best.h * px > 28
              },
            })
          }
          refresh()
        }
      )
      stopNames = () => {
        m.off("zoomend", refresh)
        land?.remove()
      }
    }
    return () => {
      alive = false
      stopNames()
      stopFonts()
      base.remove()
    }
  }, [L, tiles, labels, detail, resolvedTheme])

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
            // Over the land, which is drawn later and would cover them.
            pane: "links",
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
    // (and once more when the detailed view turns out to be there, since
    // servers close to each other can then be told apart).
    const key = (detail ? "detail|" : "") + points.map((p) => `${p.id}:${p.location.lat},${p.location.lon}`).join("|")
    if (key !== fitted.current && points.length > 0) {
      fitted.current = key
      const bounds = L.latLngBounds(points.map((p) => [p.location.lat, p.location.lon]))
      m.fitBounds(bounds.pad(0.4), { maxZoom: detail ? 10 : 5, animate: false })
      if (tiles || m.getZoom() > DRAWN_MAX_ZOOM) m.setZoom(Math.floor(m.getZoom()), { animate: false })
    }
    place()
    m.on("zoomend", place)
    return () => {
      m.off("zoomend", place)
    }
  }, [L, points, t, detail, tiles])

  const fit = () => {
    if (!L || !map.current || points.length === 0) return
    const m = map.current
    m.fitBounds(L.latLngBounds(points.map((p) => [p.location.lat, p.location.lon])).pad(0.4), { maxZoom: detail ? 10 : 5, animate: false })
    if (tiles || m.getZoom() > DRAWN_MAX_ZOOM) m.setZoom(Math.floor(m.getZoom()), { animate: false })
  }

  return (
    <div className={cn("relative isolate overflow-hidden rounded-lg border", className)}>
      <div
        ref={el}
        // Leaflet keeps its own classes on this element, so the class list
        // given here never changes; what varies is said with data attributes.
        className="portunus-map h-80 w-full md:h-[26rem]"
        style={{ "--map-font": mapFontFamily } as React.CSSProperties}
        data-pick={onPick ? "" : undefined}
        data-tiles={tiles ? (mapStyle?.themed ? "themed" : "plain") : undefined}
        role="application"
        aria-label={t("nodes.map.title")}
        data-vaul-no-drag
      />
      <ButtonGroup orientation="vertical" className="absolute top-2 right-2 z-[500] rounded-lg shadow-xs">
        <Button variant="outline" size="icon-sm" className="bg-background" onClick={() => zoomBy.current?.(1)}>
          <PlusIcon />
          <span className="sr-only">{t("nodes.map.zoomIn")}</span>
        </Button>
        <Button variant="outline" size="icon-sm" className="bg-background" onClick={() => zoomBy.current?.(-1)}>
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
          {mapStyle?.credit || "© OpenStreetMap"}
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
