import type { Inbound } from "@/lib/types"
import { Shield, Globe, Zap, Lock, Eye, Wifi, Server } from "lucide-react"
import type { LucideIcon } from "lucide-react"

export interface InboundPreset {
    id: string
    name: string
    description: string
    icon: LucideIcon
    defaults: Partial<Inbound>
}

export const INBOUND_PRESETS: InboundPreset[] = [
    {
        id: "vless-xhttp",
        name: "VLESS + XHTTP",
        description: "CDN-compatible multiplexed transport",
        icon: Globe,
        defaults: {
            protocol: "vless",
            port: 443,
            listen: "0.0.0.0",
            network: "xhttp",
            security: "none",
            transport_settings: {
                path: "/",
                mode: "auto",
            },
            sniffing_settings: {
                enabled: true,
                destOverride: ["http", "tls"],
                metadataOnly: false,
                routeOnly: false,
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
            listen: "0.0.0.0",
            network: "xhttp",
            security: "tls",
            transport_settings: {
                path: "/",
                mode: "auto",
            },
            sniffing_settings: {
                enabled: true,
                destOverride: ["http", "tls"],
                metadataOnly: false,
                routeOnly: false,
            },
        },
    },
    {
        id: "vless-xhttp-reality",
        name: "VLESS + XHTTP + Reality",
        description: "Best anti-detection with Reality fingerprinting",
        icon: Shield,
        defaults: {
            protocol: "vless",
            port: 443,
            listen: "0.0.0.0",
            network: "xhttp",
            security: "reality",
            vless_settings: {
                flow: "xtls-rprx-vision",
            },
            transport_settings: {
                path: "/",
                mode: "auto",
            },
            reality_settings: {
                fingerprint: "chrome",
                dest: "www.google.com:443",
                serverNames: ["www.google.com"],
            },
            sniffing_settings: {
                enabled: true,
                destOverride: ["http", "tls"],
                metadataOnly: false,
                routeOnly: false,
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
            listen: "0.0.0.0",
            network: "ws",
            security: "tls",
            transport_settings: {
                path: "/",
            },
            sniffing_settings: {
                enabled: true,
                destOverride: ["http", "tls"],
                metadataOnly: false,
                routeOnly: false,
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
            listen: "0.0.0.0",
            network: "ws",
            security: "tls",
            transport_settings: {
                path: "/",
            },
            sniffing_settings: {
                enabled: true,
                destOverride: ["http", "tls"],
                metadataOnly: false,
                routeOnly: false,
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
            listen: "0.0.0.0",
            network: "tcp",
            security: "none",
            shadowsocks_settings: {
                method: "2022-blake3-aes-128-gcm",
                network: "tcp,udp",
            },
            sniffing_settings: {
                enabled: true,
                destOverride: ["http", "tls"],
                metadataOnly: false,
                routeOnly: false,
            },
        },
    },
    {
        id: "wireguard",
        name: "WireGuard",
        description: "Fast kernel-level VPN tunnel",
        icon: Server,
        defaults: {
            protocol: "wireguard",
            port: 51820,
            listen: "0.0.0.0",
            network: "tcp",
            security: "none",
            wireguard_settings: {
                secretKey: "",
                mtu: 1420,
                endpoint: [],
                peers: [],
            },
            sniffing_settings: {
                enabled: true,
                destOverride: ["http", "tls"],
                metadataOnly: false,
                routeOnly: false,
            },
        },
    },
    {
        id: "hysteria2",
        name: "Hysteria2",
        description: "QUIC-based protocol with built-in TLS",
        icon: Zap,
        defaults: {
            protocol: "hysteria2",
            port: 443,
            listen: "0.0.0.0",
            network: "",
            security: "tls",
            tls_settings: {
                serverName: "",
            },
            hysteria_settings: {},
            sniffing_settings: {
                enabled: true,
                destOverride: ["http", "tls"],
                metadataOnly: false,
                routeOnly: false,
            },
        },
    },
]
