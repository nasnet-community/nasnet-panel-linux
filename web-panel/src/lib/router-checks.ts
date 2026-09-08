import type { UplinkSlot } from "@/lib/types/network"
import { isDomesticSlot } from "@/lib/network-labels"

/** One server a line is tested against. Mirrors usecase.ProbeTarget — `label`
 *  is ours, and the Go parser ignores it. */
export interface ProbeTarget {
    address: string
    proto: "tcp" | "dns"
    label?: string
}

/** Domestic lines are checked against Iranian servers, foreign ones abroad. */
export type CheckGroup = "domestic" | "foreign"

export const MAX_TARGETS = 4

export function groupOf(slot: UplinkSlot): CheckGroup {
    return isDomesticSlot(slot) ? "domestic" : "foreign"
}

// A line's own keys. The group keys kept their old names, so these carry a
// slot_ segment to stay clear of them. Must match health_config.go.
export function slotTargetsKey(slot: UplinkSlot): string {
    return `router_probe_targets_slot_${slot}`
}

export function slotRttKey(slot: UplinkSlot): string {
    return `router_degraded_rtt_ms_slot_${slot}`
}

export function slotLossKey(slot: UplinkSlot): string {
    return `router_degraded_loss_pct_slot_${slot}`
}

export function groupTargetsKey(slot: UplinkSlot): string {
    return `router_probe_targets_${groupOf(slot)}`
}

export function groupRttKey(slot: UplinkSlot): string {
    return `router_degraded_rtt_ms_${groupOf(slot)}`
}

export function groupLossKey(slot: UplinkSlot): string {
    return `router_degraded_loss_pct_${groupOf(slot)}`
}

/** Existing installations inherit this default until a group sets its own. */
export const LEGACY_LOSS_KEY = "router_degraded_loss_pct"

/** The shipped defaults, and the only addresses offered as a tick-box.
 *  Anything else is the operator's own, because we cannot vouch for it. */
export const CHECK_PRESETS: Record<CheckGroup, ProbeTarget[]> = {
    domestic: [
        { address: "217.218.155.155:53", proto: "dns", label: "Iran DNS 1" },
        { address: "178.22.122.100:53", proto: "dns", label: "Iran DNS 2" },
    ],
    foreign: [
        { address: "1.1.1.1:443", proto: "tcp", label: "Cloudflare" },
        { address: "8.8.8.8:443", proto: "tcp", label: "Google" },
    ],
}

/** Matches degradedRTTDefaults() in health_config.go. */
export const DEFAULT_RTT_MS: Record<CheckGroup, number> = { domestic: 300, foreign: 800 }
export const DEFAULT_LOSS_PCT = 25

export function protoLabel(proto: string): string {
    return proto === "dns" ? "DNS lookup" : "TCP Conn"
}

/** Default port for a protocol: a lookup goes to 53, a knock to 443. */
export function defaultPort(proto: "tcp" | "dns"): string {
    return proto === "dns" ? "53" : "443"
}

export function splitAddress(address: string): { host: string; port: string } {
    const at = address.lastIndexOf(":")
    if (at < 0) return { host: address, port: "" }
    return { host: address.slice(0, at), port: address.slice(at + 1) }
}

/** Identity is the address: the same server cannot be listed twice. */
export function sameTarget(a: ProbeTarget, b: ProbeTarget): boolean {
    return a.address === b.address
}

export function parseTargets(blob: string | undefined | null): ProbeTarget[] {
    if (!blob) return []
    try {
        const raw: unknown = JSON.parse(blob)
        if (!Array.isArray(raw)) return []
        return raw.flatMap((t) => {
            if (!t || typeof t !== "object") return []
            const o = t as Record<string, unknown>
            const address = typeof o.address === "string" ? o.address : ""
            const proto = o.proto === "dns" ? "dns" : "tcp"
            if (!address) return []
            const out: ProbeTarget = { address, proto }
            if (typeof o.label === "string" && o.label) out.label = o.label
            return [out]
        })
    } catch {
        return []
    }
}

export function serializeTargets(targets: ProbeTarget[]): string {
    return JSON.stringify(
        targets.map((t) => (t.label ? { address: t.address, proto: t.proto, label: t.label } : { address: t.address, proto: t.proto })),
    )
}

/** The prober drops anything that is not a literal v4 address and a real port,
 *  and says nothing about it, so the form has to refuse it first. */
export function targetError(host: string, port: string): string | null {
    const h = host.trim()
    if (!h) return "Enter an address"
    const parts = h.split(".")
    if (parts.length !== 4 || !parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255)) {
        return "Use a numeric address like 10.0.0.1"
    }
    const n = Number(port.trim())
    if (!/^\d+$/.test(port.trim()) || n < 1 || n > 65535) return "Port must be 1 to 65535"
    return null
}

/** The rows a line's dialog shows: its presets first, then anything the
 *  operator added, so the order never jumps when a box is ticked. */
export function checkRows(slot: UplinkSlot, active: ProbeTarget[]): ProbeTarget[] {
    const presets = CHECK_PRESETS[groupOf(slot)]
    const extra = active.filter((a) => !presets.some((p) => sameTarget(p, a)))
    return [...presets, ...extra]
}

export function isPreset(slot: UplinkSlot, t: ProbeTarget): boolean {
    return CHECK_PRESETS[groupOf(slot)].some((p) => sameTarget(p, t))
}
