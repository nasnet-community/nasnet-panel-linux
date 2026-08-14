import type { VPNStatus } from "@/lib/types/network"

/** handshakeLabel turns the only liveness signal WireGuard offers into words.
 *  There is no link state to read: the interface is up whether or not anyone
 *  is on the other end. */
export function handshakeLabel(status: VPNStatus): string {
    if (status.handshake_age_seconds === null) return "No handshake yet"
    const s = status.handshake_age_seconds
    if (s < 60) return "Last handshake just now"
    if (s < 3600) return `Last handshake ${Math.floor(s / 60)} min ago`
    return `Last handshake ${Math.floor(s / 3600)} h ago`
}

export function formatBytes(n: number): string {
    if (n < 1024) return `${n} B`
    const units = ["KB", "MB", "GB", "TB"]
    let v = n / 1024
    let i = 0
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024
        i++
    }
    return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

/** detectFormat tells the two accepted forms apart. They cannot be confused, so
 *  the operator never has to say which one they pasted. */
export function detectFormat(raw: string): "uri" | "conf" | "" {
    const t = raw.trim().toLowerCase()
    if (t.startsWith("wireguard://")) return "uri"
    if (t.includes("[interface]")) return "conf"
    return ""
}
