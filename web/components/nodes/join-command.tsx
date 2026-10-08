"use client"

import * as React from "react"

import { CopyButton } from "@/components/copy-button"
import { Note } from "@/components/wizard"
import { api, errorText, type Node, type NodeJoin } from "@/lib/api"
import { useI18n } from "@/lib/i18n"

function Command({ label, hint, value }: { label: string; hint?: string; value: string }) {
  return (
    <div className="grid gap-2">
      <span className="font-medium">{label}</span>
      {hint && <span className="text-muted-foreground">{hint}</span>}
      <div className="relative">
        <pre className="max-h-32 overflow-auto rounded-lg bg-muted p-4 pr-12 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">
          {value}
        </pre>
        <CopyButton value={value} className="absolute top-2 right-2 text-muted-foreground" />
      </div>
    </div>
  )
}

/**
 * What to run to start a node of ours: which ports to open, how to bring
 * the image over when it is a local build, and the command itself.
 */
export function JoinCommand({ node }: { node: Node }) {
  const { t } = useI18n()
  const [join, setJoin] = React.useState<NodeJoin | null>(null)
  const [error, setError] = React.useState<string | null>(null)

  React.useEffect(() => {
    api.nodeJoin(node.id).then(setJoin, (err) => setError(errorText(err, t)))
  }, [node.id, t])

  if (error) return <Note tone="error">{error}</Note>
  if (!join) return <Note tone="pending">{t("common.loading")}</Note>

  const ports = [join.agentPort, ...(join.hopPort ? [join.hopPort] : [])].join(", ")
  return (
    <div className="grid gap-5 text-sm">
      <div className="grid gap-1 text-muted-foreground">
        <p>{t("nodeWizard.run.firewall", { ports })}</p>
        {node.isEntry && <p>{t("nodeWizard.run.firewallEntry")}</p>}
      </div>
      {join.copyImage && <Command label={t("nodeWizard.run.copyImage")} hint={t("nodeWizard.run.copyImageHint")} value={join.copyImage} />}
      <Command
        label={t(join.copyImage ? "nodeWizard.run.commandNumbered" : "nodeWizard.run.command")}
        hint={t("nodeWizard.run.secret")}
        value={join.run}
      />
      <Command label={t("nodeWizard.run.docker")} hint={t("nodeWizard.run.dockerHint")} value={join.installDocker} />
    </div>
  )
}
