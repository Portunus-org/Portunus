import type { Protocol } from "@/lib/api"

export interface ProtocolInfo {
  label: string
  transport: "TCP" | "UDP" | "TCP + UDP"
  defaultPort: number
}

// VLESS+REALITY first: the throughput profile. Hysteria2 second: the one
// that gets through lossy or throttled links.
export const protocols: Record<Protocol, ProtocolInfo> = {
  vless: {
    label: "VLESS · REALITY",
    transport: "TCP",
    defaultPort: 443,
  },
  hysteria2: {
    label: "Hysteria2",
    transport: "UDP",
    defaultPort: 443,
  },
  tuic: {
    label: "TUIC v5",
    transport: "UDP",
    defaultPort: 8443,
  },
  trojan: {
    label: "Trojan",
    transport: "TCP",
    defaultPort: 8443,
  },
  shadowsocks: {
    label: "Shadowsocks 2022",
    transport: "TCP + UDP",
    defaultPort: 8388,
  },
}

export const protocolOrder: Protocol[] = ["vless", "hysteria2", "tuic", "trojan", "shadowsocks"]
