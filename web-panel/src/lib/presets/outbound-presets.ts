import type { Outbound } from "@/lib/types"
import { Shield, Globe, Zap, Lock, Eye, Wifi, Ban, Key } from "lucide-react"
import type { LucideIcon } from "lucide-react"

export interface OutboundPreset {
    id: string
    name: string
    description: string
    icon: LucideIcon
    defaults: Partial<Outbound>
}

export const OUTBOUND_PRESETS: OutboundPreset[] = [
    {
        id: "freedom-direct",
        name: "Freedom (Direct)",
        description: "Send traffic directly to destination",
        icon: Globe,
        defaults: {
            protocol: "freedom",
            network: "tcp",
            security: "none",
            freedom_settings: {
                domainStrategy: "AsIs",
            },
        },
    },
    {
        id: "blackhole-block",
        name: "Blackhole (Block)",
        description: "Drop all traffic silently",
        icon: Ban,
        defaults: {
            protocol: "blackhole",
            network: "tcp",
            security: "none",
            blackhole_settings: {
                responseType: "none",
            },
        },
    },
    {
        id: "vless-xhttp-tls",
        name: "VLESS + XHTTP + TLS",
        description: "Multiplexed transport with TLS encryption",
        icon: Zap,
        defaults: {
            protocol: "vless",
            port: 443,
            network: "xhttp",
            security: "tls",
            transport_settings: {
                path: "/",
                mode: "auto",
            },
        },
    },
    {
        id: "vless-reality",
        name: "VLESS + Reality",
        description: "Best anti-detection with Reality fingerprinting",
        icon: Shield,
        defaults: {
            protocol: "vless",
            port: 443,
            network: "tcp",
            security: "reality",
            vless_settings: {
                flow: "xtls-rprx-vision",
            },
            reality_settings: {
                fingerprint: "chrome",
            },
        },
    },
    {
        id: "vmess-ws-tls",
        name: "VMess + WS + TLS",
        description: "Classic VMess with WebSocket and TLS",
        icon: Lock,
        defaults: {
            protocol: "vmess",
            port: 443,
            network: "ws",
            security: "tls",
            transport_settings: {
                path: "/",
            },
        },
    },
    {
        id: "trojan-ws-tls",
        name: "Trojan + WS + TLS",
        description: "Trojan protocol with WebSocket transport",
        icon: Eye,
        defaults: {
            protocol: "trojan",
            port: 443,
            network: "ws",
            security: "tls",
            transport_settings: {
                path: "/",
            },
        },
    },
    {
        id: "shadowsocks",
        name: "Shadowsocks",
        description: "Simple and fast encrypted proxy",
        icon: Wifi,
        defaults: {
            protocol: "shadowsocks",
            port: 8388,
            network: "tcp",
            security: "none",
            shadowsocks_settings: {
                method: "2022-blake3-aes-128-gcm",
                network: "tcp,udp",
            },
        },
    },
    {
        id: "wireguard-warp",
        name: "WireGuard (WARP)",
        description: "Cloudflare WARP tunnel over WireGuard",
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
        description: "QUIC-based protocol",
        icon: Zap,
        defaults: {
            protocol: "hysteria2",
            port: 443,
            network: "",
            security: "tls",
            tls_settings: {
                serverName: "",
                allowInsecure: false,
            },
            hysteria_settings: {
                auth: "",
            },
        },
    },
]
