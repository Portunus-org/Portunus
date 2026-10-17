import type { Protocol } from "@/lib/api"

export interface ProtocolInfo {
  label: string
  transport: "TCP" | "UDP" | "TCP + UDP"
  defaultPort: number
}

// VLESS+REALITY first: the throughput profile. Hysteria2 second: the one
// that keeps its speed on lossy and congested links.
export const protocols: Record<Protocol, ProtocolInfo> = {
  vless: {
    label: "VLESS Reality",
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

// Hysteria2 first: the recommended default in the inbound wizard.
export const protocolOrder: Protocol[] = ["hysteria2", "vless", "tuic", "trojan", "shadowsocks"]
