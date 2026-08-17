import type { FlowNodeStatus, TraceStep, TraceView } from "@/lib/types/flow"

/** Mark word: group field 0x00FF0000 (1 domestic, 2 foreign), pin 0x0F000000. */
export function decodeMark(mark: number) {
    const groupBits = (mark & 0x00ff0000) >>> 16
    const group = groupBits === 1 ? "domestic" : groupBits === 2 ? "foreign" : ""
    return { group, pin: (mark & 0x0f000000) >>> 24, hex: "0x" + mark.toString(16) }
}

export function formatBytes(n: number): string {
    if (n < 1024) return `${Math.round(n)} B`
    const units = ["KB", "MB", "GB", "TB"]
    let v = n / 1024
    let i = 0
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024
        i++
    }
    return `${v.toFixed(1)} ${units[i]}`
}

export function formatRate(bytesPerSecond: number): string {
    if (bytesPerSecond <= 0) return "idle"
    return `${formatBytes(bytesPerSecond)}/s`
}

/** Seconds since an ISO timestamp, rendered the way a log reads. */
export function relativeTime(iso: string, now = Date.now()): string {
    const s = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000))
    if (s < 60) return `${s}s ago`
    if (s < 3600) return `${Math.floor(s / 60)}m ago`
    if (s < 86400) return `${Math.floor(s / 3600)}h ago`
    return `${Math.floor(s / 86400)}d ago`
}

export function nodeStatusTone(status: FlowNodeStatus): "ok" | "warn" | "bad" | "muted" {
    switch (status) {
        case "ok":
            return "ok"
        case "warn":
            return "warn"
        case "down":
            return "bad"
        default:
            return "muted"
    }
}

export function traceVerdictLabel(v: TraceView["final_verdict"]): string {
    switch (v) {
        case "delivered-vpn":
            return "Delivered through the VPN"
        case "delivered-domestic":
            return "Delivered over the domestic uplink"
        case "dropped":
            return "Dropped — never leaves the box"
        default:
            return "Would leave unprotected"
    }
}

export function stepTone(v: TraceStep["verdict"]): "ok" | "warn" | "bad" | "muted" {
    switch (v) {
        case "ok":
            return "ok"
        case "warn":
            return "warn"
        case "drop":
            return "bad"
        default:
            return "muted"
    }
}

/** Event names the timeline colours as trouble. */
const BAD_EVENTS = new Set(["wan.down", "vpn.down", "wan.apply_rolled_back", "wan.lease_warning"])
const GOOD_EVENTS = new Set(["wan.up", "vpn.up"])

export function eventTone(type: string): "ok" | "bad" | "muted" {
    if (BAD_EVENTS.has(type)) return "bad"
    if (GOOD_EVENTS.has(type)) return "ok"
    return "muted"
}
