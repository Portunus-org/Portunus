import { StatusBadge, type Tone } from "@/components/status"
import type { Node } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import type { MessageKey } from "@/lib/messages"

const tones: Record<Node["status"]["state"], Tone> = {
  online: "ok",
  offline: "error",
  pending: "pending",
  rejected: "error",
}

/** The connection state of a node, as the panel last saw it. */
export function NodeStatus({ node }: { node: Node }) {
  const { t } = useI18n()
  if (!node.enabled) return <StatusBadge tone="off">{t("nodes.state.disabled")}</StatusBadge>
  return <StatusBadge tone={tones[node.status.state]}>{t(`nodes.state.${node.status.state}` as MessageKey)}</StatusBadge>
}
