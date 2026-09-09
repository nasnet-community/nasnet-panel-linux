import type { Outbound } from "@/lib/types"
import { Shield, Globe, Zap, Lock, Eye, Wifi, Ban, Key, Server, Repeat, Network } from "lucide-react"
import type { LucideIcon } from "lucide-react"

export type OutboundPresetGroup = "builtin" | "proxy"

export const OUTBOUND_PRESET_GROUPS: { id: OutboundPresetGroup; label: string }[] = [
    { id: "builtin", label: "Built-in" },
    { id: "proxy", label: "Proxy to a server" },
]

export interface OutboundPreset {
    id: string
    name: string
    description: string
    /** Stack tokens shown on the card and matched by the template search. */
    tokens: string[]
    group: OutboundPresetGroup
    icon: LucideIcon
    defaults: Partial<Outbound>
}

// The original nine stay first in their original order; built-ins are the
// routing sinks xray ships (freedom, blackhole, dns, loopback), everything
// else dials a remote server.
export const OUTBOUND_PRESETS: OutboundPreset[] = [
    {
        id: "freedom-direct",
        name: "Freedom · Direct",
        description: "Send traffic directly to its destination",
        tokens: ["direct"],
        group: "builtin",
        icon: Globe,
        defaults: {
            protocol: "freedom",
            network: "tcp",
            security: "none",
            freedom_settings: { domainStrategy: "AsIs" },
        },
    },
    {
        id: "blackhole-block",
        name: "Blackhole · Block",
        description: "Drop all traffic silently",
        tokens: ["drop"],
        group: "builtin",
        icon: Ban,
        defaults: {
            protocol: "blackhole",
            network: "tcp",
            security: "none",
            blackhole_settings: { responseType: "none" },
        },
    },
    {
        id: "vless-xhttp-tls",
        name: "VLESS + XHTTP + TLS",
        description: "Multiplexed transport with TLS",
        tokens: ["xhttp", "tls"],
        group: "proxy",
        icon: Zap,
        defaults: {
            protocol: "vless",
            port: 443,
            network: "xhttp",
            security: "tls",
            transport_settings: { path: "/", mode: "auto" },
        },
    },
    {
        id: "vless-reality",
        name: "VLESS + Reality",
        description: "Raw TCP, Reality and XTLS Vision flow",
        tokens: ["tcp", "reality", "vision"],
        group: "proxy",
        icon: Shield,
        defaults: {
            protocol: "vless",
            port: 443,
            network: "tcp",
            security: "reality",
            vless_settings: { flow: "xtls-rprx-vision" },
            reality_settings: { fingerprint: "chrome" },
        },
    },
    {
        id: "vmess-ws-tls",
        name: "VMess + WS + TLS",
        description: "Classic VMess over WebSocket with TLS",
        tokens: ["ws", "tls"],
        group: "proxy",
        icon: Lock,
        defaults: {
            protocol: "vmess",
            port: 443,
            network: "ws",
            security: "tls",
            transport_settings: { path: "/" },
        },
    },
    {
        id: "trojan-ws-tls",
        name: "Trojan + WS + TLS",
        description: "Trojan over WebSocket with TLS",
        tokens: ["ws", "tls"],
        group: "proxy",
        icon: Eye,
        defaults: {
            protocol: "trojan",
            port: 443,
            network: "ws",
            security: "tls",
            transport_settings: { path: "/" },
        },
    },
    {
        id: "shadowsocks",
        name: "Shadowsocks",
        description: "Shadowsocks 2022 with BLAKE3 AES-128-GCM",
        tokens: ["tcp", "2022-aes-128"],
        group: "proxy",
        icon: Wifi,
        defaults: {
            protocol: "shadowsocks",
            port: 8388,
            network: "tcp",
            security: "none",
            shadowsocks_settings: { method: "2022-blake3-aes-128-gcm", network: "tcp,udp" },
        },
    },
    {
        id: "wireguard-warp",
        name: "WireGuard · WARP",
        description: "Cloudflare WARP tunnel over WireGuard",
        tokens: ["udp", "cloudflare"],
        group: "proxy",
        icon: Key,
        defaults: {
            protocol: "wireguard",
            network: "",
            security: "",
            wireguard_settings: {
                secretKey: "",
                mtu: 1280,
                endpoint: ["172.16.0.2/32", "2606:4700:110:8a36:df92:29f:fe04:8cf0/128"],
                reserved: [0, 0, 0],
                domainStrategy: "forceip",
                peers: [{
                    publicKey: "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=",
                    endpoint: "engage.cloudflareclient.com:2408",
                    allowedIps: ["0.0.0.0/0", "::/0"],
                }],
            },
        },
    },
    {
        id: "hysteria2",
        name: "Hysteria2",
        description: "QUIC-based protocol with built-in TLS",
        tokens: ["quic", "tls"],
        group: "proxy",
        icon: Zap,
        defaults: {
            protocol: "hysteria2",
            port: 443,
            network: "",
            security: "tls",
            tls_settings: { serverName: "", allowInsecure: false },
            hysteria_settings: { auth: "" },
        },
    },

    // ---- Added ----
    {
        id: "dns",
        name: "DNS",
        description: "Answer or forward DNS queries routed to this outbound",
        tokens: ["dns"],
        group: "builtin",
        icon: Server,
        defaults: {
            protocol: "dns",
            network: "tcp",
            security: "none",
            dns_settings: {},
        },
    },
    {
        id: "loopback",
        name: "Loopback",
        description: "Re-enter routing through another inbound tag",
        tokens: ["loop"],
        group: "builtin",
        icon: Repeat,
        defaults: {
            protocol: "loopback",
            network: "tcp",
            security: "none",
            loopback_settings: { inboundTag: "" },
        },
    },
    {
        id: "socks-http",
        name: "SOCKS5 / HTTP proxy",
        description: "Dial an upstream SOCKS5 proxy, optionally with username and password",
        tokens: ["tcp", "auth"],
        group: "proxy",
        icon: Network,
        defaults: {
            protocol: "socks",
            port: 1080,
            network: "tcp",
            security: "none",
            socks_settings: {},
        },
    },
]
