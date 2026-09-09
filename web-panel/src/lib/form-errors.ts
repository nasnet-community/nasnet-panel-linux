import type { FieldErrors } from "react-hook-form"

export interface FlatError {
    /** Dotted path, e.g. "reality_settings.privateKey" or "wireguard_settings.endpoint.0". */
    path: string
    message: string
}

// react-hook-form nests errors like the values; zod refine issues land at
// whatever path they name. Flatten to (path, message) pairs so tabs can count
// them and the footer can list them.
export function flattenErrors(errors: FieldErrors | undefined, prefix = ""): FlatError[] {
    if (!errors) return []
    const out: FlatError[] = []
    for (const [key, value] of Object.entries(errors)) {
        if (value === undefined || value === null || key === "ref") continue
        const path = prefix ? `${prefix}.${key}` : key
        if (typeof value === "object" && "message" in value && typeof (value as { message?: unknown }).message === "string") {
            out.push({ path, message: (value as { message: string }).message })
            // A field error can still carry nested errors (arrays of objects);
            // fall through to collect them too.
        }
        if (typeof value === "object") {
            const nested = Object.fromEntries(
                Object.entries(value as Record<string, unknown>).filter(([k]) => k !== "message" && k !== "type" && k !== "ref" && k !== "types"),
            )
            if (Object.keys(nested).length > 0) {
                out.push(...flattenErrors(nested as FieldErrors, path))
            }
        }
    }
    return out
}

// Human labels for the paths the schemas can produce. Anything unlisted falls
// back to the last path segment, title-cased.
const FIELD_LABELS: Record<string, string> = {
    tag: "Tag",
    port: "Port",
    address: "Address",
    network: "Network",
    security: "Security",
    "tls_settings.certificates": "TLS certificate",
    "reality_settings.serverNames": "Reality server names",
    "reality_settings.dest": "Reality destination",
    "reality_settings.privateKey": "Reality private key",
    "reality_settings.publicKey": "Reality public key",
    "reality_settings.shortId": "Reality short ID",
    "vless_settings.flow": "VLESS flow",
    "vless_settings.uuid": "VLESS UUID",
    "vmess_settings.uuid": "VMess UUID",
    "trojan_settings.password": "Trojan password",
    "shadowsocks_settings.method": "Shadowsocks method",
    "shadowsocks_settings.password": "Shadowsocks key",
    "wireguard_settings.secretKey": "WireGuard secret key",
    "wireguard_settings.peers": "WireGuard peers",
    "loopback_settings.inboundTag": "Loopback inbound tag",
    "hysteria_settings.auth": "Hysteria2 auth",
}

export function labelForPath(path: string): string {
    if (FIELD_LABELS[path]) return FIELD_LABELS[path]
    // wireguard_settings.endpoint.0 → "WireGuard address #1"
    const m = path.match(/^wireguard_settings\.endpoint\.(\d+)$/)
    if (m) return `WireGuard address #${Number(m[1]) + 1}`
    const fb = path.match(/^(vless|trojan)_settings\.fallbacks\.(\d+)\.(\w+)$/)
    if (fb) return `Fallback #${Number(fb[2]) + 1} ${fb[3]}`
    const peer = path.match(/^wireguard_settings\.peers\.(\d+)\.publicKey$/)
    if (peer) return `Peer #${Number(peer[1]) + 1} public key`
    const last = path.split(".").pop() ?? path
    return last.replace(/([a-z])([A-Z])/g, "$1 $2").replace(/^./, (c) => c.toUpperCase())
}
