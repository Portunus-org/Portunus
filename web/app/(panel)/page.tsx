"use client"

import * as React from "react"

import { PageHeader } from "@/components/page-header"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, type SystemInfo } from "@/lib/api"
import { formatBytes, formatDuration } from "@/lib/format"

const REFRESH_MS = 10_000

function useSystemInfo() {
  const [info, setInfo] = React.useState<SystemInfo | null>(null)
  React.useEffect(() => {
    let alive = true
    const load = () =>
      api.system().then(
        (s) => alive && setInfo(s),
        () => {}
      )
    load()
    const t = setInterval(load, REFRESH_MS)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])
  return info
}

function Stat({ label, value, mono }: { label: string; value?: string | null; mono?: boolean }) {
  return (
    <Card>
      <CardHeader>
        <CardDescription>{label}</CardDescription>
        <CardTitle className={mono ? "font-mono text-xl font-normal" : "text-2xl font-medium tabular-nums"}>
          {value ?? "—"}
        </CardTitle>
      </CardHeader>
    </Card>
  )
}

export default function OverviewPage() {
  const info = useSystemInfo()

  return (
    <>
      <PageHeader title="Overview" description="This node and the panel running on it." />

      {info && !info.listenLocal && (
        <div role="alert" className="mx-4 flex gap-3 rounded-lg border border-signal/40 px-4 py-3 text-sm lg:mx-6">
          <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-signal" />
          <p>
            The panel listens on <span className="font-mono">{info.listen}</span>, which is not a loopback
            address. Make sure it is not reachable from the internet, or bind it to 127.0.0.1 and use an SSH
            tunnel.
          </p>
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 px-4 lg:px-6 @5xl/main:grid-cols-4">
        <Stat label="Uptime" value={info && formatDuration(info.uptimeSeconds)} />
        <Stat label="Panel memory (RSS)" value={info && formatBytes(info.memory.rssBytes)} />
        <Stat label="Database" value={info && formatBytes(info.database.sizeBytes)} />
        <Stat label="Version" value={info?.version} mono />
      </div>

      <div className="px-4 lg:px-6">
        <Card className="gap-0 py-0">
          <CardHeader className="border-b py-3">
            <CardTitle className="text-sm">Nodes</CardTitle>
          </CardHeader>
          <CardContent className="px-0">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-4">Name</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead className="hidden md:table-cell">Panel address</TableHead>
                  <TableHead className="hidden sm:table-cell">Data plane</TableHead>
                  <TableHead className="pr-4 text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow>
                  <TableCell className="pl-4 font-mono">main</TableCell>
                  <TableCell>entry</TableCell>
                  <TableCell className="hidden font-mono text-muted-foreground md:table-cell">{info?.listen ?? "—"}</TableCell>
                  <TableCell className="hidden text-muted-foreground sm:table-cell">not configured</TableCell>
                  <TableCell className="pr-4 text-right">
                    <span className="inline-flex items-center gap-2">
                      <span className="size-1.5 rounded-full bg-foreground" />
                      {info ? "online" : "—"}
                    </span>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>

      <div className="mx-4 flex flex-col gap-2 border-t pt-8 lg:mx-6">
        <p className="font-display text-xl">No inbounds yet.</p>
        <p className="max-w-prose text-sm text-muted-foreground">
          sing-box will start on this node once the first inbound is created. Additional hop and backup
          nodes join with a single command.
        </p>
      </div>
    </>
  )
}
