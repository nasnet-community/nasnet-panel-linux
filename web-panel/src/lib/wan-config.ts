import type { NetworkInterfaceView, UplinkSlot, WANConfig } from "@/lib/types/network"

export function defaultWAN(slot: UplinkSlot): WANConfig {
    return {
        method: "dhcp4",
        static_address: "",
        static_gateway: "",
        gateway_on_link: false,
        dns_mode: slot.startsWith("secondary") ? "vpn" : "default",
        dns_servers: [],
    }
}

export function savedWAN(iface: NetworkInterfaceView, slot: UplinkSlot): WANConfig {
    const config = { ...defaultWAN(slot), ...iface.wan }
    if (slot.startsWith("secondary")) {
        config.dns_mode = "vpn"
        config.dns_servers = []
    } else if (config.dns_mode === "vpn") config.dns_mode = "default"
    return normalizeWAN(config)
}

export function normalizeWAN(config: WANConfig): WANConfig {
    return {
        ...config,
        static_address: config.method === "static" ? config.static_address.trim() : "",
        static_gateway: config.method === "static" ? config.static_gateway.trim() : "",
        gateway_on_link: config.method === "static" && config.gateway_on_link,
        dns_servers:
            config.dns_mode === "custom"
                ? config.dns_servers.map((s) => s.trim()).filter(Boolean)
                : [],
    }
}

function ipv4(value: string): number | null {
    if (!/^(0|[1-9]\d{0,2})(\.(0|[1-9]\d{0,2})){3}$/.test(value)) return null
    const a = value.split(".").map(Number)
    if (
        a.some((n) => n > 255) ||
        a[0] === 0 ||
        a[0] === 127 ||
        a[0] >= 224 ||
        (a[0] === 169 && a[1] === 254)
    )
        return null
    return ((a[0] << 24) | (a[1] << 16) | (a[2] << 8) | a[3]) >>> 0
}

export function validateWAN(config: WANConfig): Record<string, string> {
    const errors: Record<string, string> = {}
    if (config.method === "static") {
        const [address, bitsText] = config.static_address.split("/")
        const ip = ipv4(address)
        const bits = Number(bitsText)
        const validPrefix = /^\d{1,2}$/.test(bitsText ?? "") && bits >= 1 && bits <= 32
        const mask = (0xffffffff << (32 - bits)) >>> 0
        const network = ip === null ? 0 : (ip & mask) >>> 0
        const broadcast = (network | ~mask) >>> 0
        if (ip === null) errors.address = "Enter a unicast IPv4 address."
        else if (validPrefix && bits < 31 && (ip === network || ip === broadcast))
            errors.address = "Use a host address, not the network or broadcast address."
        if (!validPrefix) errors.prefix = "Use a prefix length from 1 to 32."
        const gw = ipv4(config.static_gateway.trim())
        if (gw === null) errors.gateway = "Enter a unicast IPv4 gateway."
        else if (gw === ip) errors.gateway = "The gateway must differ from the WAN address."
        else if (ip !== null && validPrefix) {
            const within = (gw & mask) >>> 0 === network
            if (within && bits < 31 && (gw === network || gw === broadcast))
                errors.gateway = "The gateway cannot be the network or broadcast address."
            else if (!within && !config.gateway_on_link)
                errors.gateway =
                    "Enable Gateway outside this subnet in Advanced addressing if your ISP requires it."
        }
    }
    if (config.dns_mode === "custom") {
        if (ipv4(config.dns_servers[0]?.trim() ?? "") === null)
            errors.dns1 = "Enter a unicast IPv4 DNS server."
        if (config.dns_servers[1]?.trim() && ipv4(config.dns_servers[1].trim()) === null)
            errors.dns2 = "Enter a unicast IPv4 DNS server."
        if (
            config.dns_servers[0]?.trim() &&
            config.dns_servers[0].trim() === config.dns_servers[1]?.trim()
        )
            errors.dns2 = "Choose a different DNS server."
    }
    return errors
}

export function describeWAN(config: WANConfig): Record<string, string> {
    return {
        Connection:
            config.method === "static"
                ? "Static IPv4"
                : config.method === "rawip"
                  ? "Cellular"
                  : "Automatic (DHCP)",
        Address: config.method === "static" ? config.static_address : "Assigned automatically",
        Gateway: config.method === "static" ? config.static_gateway : "Assigned automatically",
        "Gateway outside subnet": config.gateway_on_link ? "Enabled · directly reachable" : "Off",
        DNS:
            config.dns_mode === "vpn"
                ? "Managed through VPN"
                : config.dns_mode === "custom"
                  ? config.dns_servers.join(", ")
                  : "Router defaults",
    }
}

export function recoveryURL(config: WANConfig, location: string): string | null {
    const address = config.static_address.split("/")[0]
    if (config.method !== "static" || ipv4(address) === null) return null
    const url = new URL(location)
    if (url.hostname === address) return null
    url.hostname = address
    return url.toString()
}
