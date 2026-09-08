import type { UplinkHealth } from "@/lib/types/health"
import type { UplinkSlot } from "@/lib/types/network"
import { isDomesticSlot } from "@/lib/network-labels"

/** Plain words for one rung, given what the rung says and what it dials.
 *  The ladder's own strings are "up" / "down" / "unknown", which say which
 *  layer broke but not what that means to an operator. */
export function rungMeaning(
    rung: "carrier" | "gateway" | "internet",
    status: string,
    ctx: { gatewayIP?: string; okTargets: number; totalTargets: number; speedMbit?: number },
): string {
    if (rung === "carrier") {
        if (status === "up") {
            return hasSpeed(ctx.speedMbit)
                ? `Cable connected · ${speedLabel(ctx.speedMbit!)}`
                : "Cable connected"
        }
        if (status === "down") return "No cable detected"
        return "Not checked yet"
    }
    if (rung === "gateway") {
        const who = ctx.gatewayIP ? `${ctx.gatewayIP} answers` : "The upstream router answers"
        if (status === "up") return who
        if (status === "down") {
            return ctx.gatewayIP ? `${ctx.gatewayIP} is not answering` : "The upstream router is not answering"
        }
        return "Looking for the upstream router"
    }
    if (status === "up") return `Internet reachable · ${ctx.okTargets} of ${ctx.totalTargets} checks answered`
    if (status === "down") {
        return ctx.okTargets === 0
            ? "Not reachable · nothing answered"
            : `Not reachable · only ${ctx.okTargets} of ${ctx.totalTargets} checks answered`
    }
    return "Not checked yet"
}

/** A link that cannot report its speed says -1, not 0. */
export function hasSpeed(mbit: number | undefined): boolean {
    return typeof mbit === "number" && mbit > 0
}

export function speedLabel(mbit: number): string {
    return mbit >= 1000 ? `${mbit / 1000} Gbit/s` : `${mbit} Mbit/s`
}

/** "up for 3 days" / "down for 4 minutes", from the last transition. Sits
 *  beside the line's name, so it reads as part of the title. */
export function sinceLabel(up: UplinkHealth, now = Date.now()): string | null {
    if (!up.since_unix) return null
    const working = up.verdict === "up" || up.verdict === "degraded"
    const secs = Math.max(0, Math.round(now / 1000 - up.since_unix))
    return `${working ? "up" : "down"} for ${duration(secs)}`
}

export function duration(secs: number): string {
    if (secs < 60) return `${secs} seconds`
    const mins = Math.round(secs / 60)
    if (mins < 60) return plural(mins, "minute")
    const hours = Math.round(mins / 60)
    if (hours < 48) return plural(hours, "hour")
    return plural(Math.round(hours / 24), "day")
}

function plural(n: number, unit: string): string {
    return `${n} ${unit}${n === 1 ? "" : "s"}`
}

/** Cumulative counters, so the unit has to stretch a long way. */
export function bytesLabel(n: number): string {
    if (!n) return "0 B"
    const units = ["B", "KB", "MB", "GB", "TB"]
    let v = n
    let i = 0
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024
        i++
    }
    return `${trim(v, i)} ${units[i]}`
}

// 61.0 MB reads like a measurement nobody took; 1.21 GB is worth the decimals.
function trim(v: number, unitIndex: number): string {
    if (unitIndex === 0 || v >= 100) return String(Math.round(v))
    const fixed = v.toFixed(v >= 10 ? 1 : 2)
    return fixed.replace(/\.?0+$/, "")
}

/** How the group carries this line, in one sentence per member row. */
export function memberRole(self: UplinkHealth, member: UplinkHealth, carrierLabel: string | null): string {
    if (member.if_name === self.if_name) {
        if (member.via === "pool") return "Sending its traffic through the VPN"
        if (member.via) return `Sending its traffic through ${carrierLabel ?? member.via}`
        return "Carrying traffic now"
    }
    if (member.verdict === "no-carrier") return "No cable or SIM detected"
    if (member.verdict === "forced-down") return "Held down by you"
    if (member.via === "pool") return "Using the VPN"
    if (member.via) return "Using another line"
    if (member.verdict === "up" || member.verdict === "degraded") {
        return self.via && self.via === member.if_name
            ? `Carrying traffic now, including ${labelOr(self)}'s`
            : "Ready to take over"
    }
    return "Not usable right now"
}

function labelOr(up: UplinkHealth): string {
    return up.if_name
}

/** Priority reads off the slot number: domestic is 1, domestic2 is 2. */
export function slotPosition(slot: UplinkSlot): number {
    const n = /(\d)$/.exec(slot)
    return n ? Number(n[1]) : 1
}

export function lineKind(slot: UplinkSlot): string {
    return isDomesticSlot(slot) ? "Domestic" : "Foreign"
}

/** The header subtitle: what this line is for and where it sits in the order. */
export function lineSubtitle(slot: UplinkSlot, siblingCount: number): string {
    if (!isDomesticSlot(slot)) return "Foreign internet line · carries the VPN tunnels"
    const pos = slotPosition(slot)
    if (siblingCount === 0) return "Internet line"
    const ordinal = pos === 1 ? "used first" : pos === 2 ? "used second" : `used ${pos}th`
    return `${pos === 1 ? "Main" : "Backup"} internet line · ${ordinal}`
}
