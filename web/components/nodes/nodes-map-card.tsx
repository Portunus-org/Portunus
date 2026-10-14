"use client"

import * as React from "react"
import { MapPinIcon } from "lucide-react"
import { toast } from "@/lib/toast"

import { NodeMap, NodeMapLegend, nodeMapState, type MapPoint, type MapState } from "@/components/nodes/node-map"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api, errorText, type GeoLocation, type Node, type MapStyle } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

function place(l: GeoLocation): string {
  return [l.city, l.country].filter(Boolean).join(", ")
}

/** The main server and one node as points for the map. */
export function mapPoints(
  main: { host: string; location: GeoLocation | null } | undefined,
  nodes: Node[],
  t: ReturnType<typeof useI18n>["t"]
): MapPoint[] {
  const points: MapPoint[] = []
  if (main?.location) {
    points.push({
      id: "main",
      name: t("nodes.map.state.main"),
      detail: [main.host, place(main.location)].filter(Boolean).join(", "),
      state: "main",
      location: main.location,
    })
  }
  for (const n of nodes) {
    if (!n.location) continue
    const detail = [n.address, place(n.location), n.status.latencyMs !== undefined && t("nodes.latency", { ms: n.status.latencyMs })]
    points.push({
      id: `n${n.id}`,
      name: n.name,
      detail: detail.filter(Boolean).join(", "),
      state: nodeMapState(n),
      location: n.location,
    })
  }
  return points
}

/** The map of servers shown above the table of nodes. */
export function NodesMapCard({
  main,
  tiles,
  mapStyle,
  nodes,
  canEdit,
  onChanged,
}: {
  /** Detailed map tiles are available through the panel. */
  tiles: boolean
  mapStyle?: MapStyle
  main: { host: string; location: GeoLocation | null } | undefined
  nodes: Node[]
  canEdit: boolean
  onChanged: () => void
}) {
  const { t } = useI18n()
  // Moving the main server: a click only shows where it would go, and it
  // goes there when that is saved. A stray click must not move a server.
  const [moving, setMoving] = React.useState(false)
  const [draft, setDraft] = React.useState<{ lat: number; lon: number } | null>(null)
  const stopMoving = () => {
    setMoving(false)
    setDraft(null)
  }
  const points = mapPoints(main && draft ? { ...main, location: { ...draft, manual: true } } : main, nodes, t)
  const states = [...new Set(points.map((p) => p.state))] as MapState[]
  const unplaced = nodes.filter((n) => !n.location).map((n) => n.name)

  async function moveMain(location: { lat: number; lon: number }) {
    stopMoving()
    try {
      await api.setServerLocation(location)
      toast.success(t("nodes.location.saved"))
      onChanged()
    } catch (err) {
      toast.error(errorText(err, t))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("nodes.map.title")}</CardTitle>
        <CardDescription>{moving ? t("nodes.map.pickHint") : t("nodes.map.description")}</CardDescription>
        {canEdit && (
          <CardAction className="flex gap-2">
            {moving ? (
              <>
                <Button size="sm" disabled={!draft} onClick={() => draft && moveMain(draft)}>
                  {t("nodes.location.save")}
                </Button>
                <Button variant="outline" size="sm" onClick={stopMoving}>
                  {t("common.cancel")}
                </Button>
              </>
            ) : (
              <Button variant="outline" size="sm" onClick={() => setMoving(true)}>
                <MapPinIcon />
                <span className="sr-only lg:not-sr-only">{t("nodes.map.moveMain")}</span>
              </Button>
            )}
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="grid gap-3">
        <NodeMap points={points} tiles={tiles} mapStyle={mapStyle} onPick={moving ? setDraft : undefined} />
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
          <NodeMapLegend states={states} />
          <span className="text-xs text-muted-foreground">{t("nodes.map.autoHint")}</span>
        </div>
        {unplaced.length > 0 && <p className="text-xs text-muted-foreground">{t("nodes.map.unplaced", { names: unplaced.join(", ") })}</p>}
      </CardContent>
    </Card>
  )
}
