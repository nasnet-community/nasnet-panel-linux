import type { Outbound, OutboundTestEntry, RoutingRule, BalancingRule } from "@/lib/types"

export interface OutboundDetail {
    label: string
    value: string
}

// Protocols with a stream layer (network + security). The rest either dial the
// kernel directly or are internal targets, so "tcp · none" would be noise.
const NO_STREAM_TRANSPORT = new Set(["freedom", "blackhole", "dns", "loopback", "wireguard"])
export const hasStreamTransport = (protocol: string) => !NO_STREAM_TRANSPORT.has(protocol)

// Where the traffic actually goes — the row's second line. The old column read
// address:port and nothing else, so a freedom outbound pinned to a WireGuard
// interface said "Direct" and a WireGuard outbound with a peer said "—".
export function describeOutboundRoute(o: Outbound): string {
    switch (o.protocol) {
        case "freedom": {
            const parts: string[] = []
            const so = o.sockopt_settings
            if (so?.interface) parts.push(`via ${so.interface}`)
            if (so?.mark) parts.push(`fwmark ${so.mark}`)
            if (o.send_through) parts.push(`from ${o.send_through}`)
            if (o.freedom_settings?.redirect) parts.push(`→ ${o.freedom_settings.redirect}`)
            return parts.length > 0 ? parts.join(" · ") : "Direct"
        }
        case "blackhole":
            return "Drops traffic"
        case "dns": {
            const d = o.dns_settings
            if (!d?.address) return "DNS · system resolver"
            return `DNS · ${d.address}${d.port ? `:${d.port}` : ""}`
        }
        case "loopback":
            return o.loopback_settings?.inboundTag
                ? `Loopback → ${o.loopback_settings.inboundTag}`
                : "Loopback"
        case "wireguard": {
            const endpoint = o.wireguard_settings?.peers?.find(p => p.endpoint)?.endpoint
            return endpoint || "No peer endpoint"
        }
        default:
            return o.address ? `${o.address}${o.port ? `:${o.port}` : ""}` : "No server address"
    }
}

// The handful of fields that identify an upstream. The first rides in the row
// after the route; the rest wait in the panel's Details tab.
export function buildOutboundDetails(o: Outbound): OutboundDetail[] {
    const details: OutboundDetail[] = []
    if (o.security === "tls" && o.tls_settings?.serverName) {
        details.push({ label: "SNI", value: o.tls_settings.serverName })
    }
    if (o.security === "reality" && o.reality_settings?.serverNames?.[0]) {
        details.push({ label: "SNI", value: o.reality_settings.serverNames[0] })
    }
    if (o.transport_settings?.host) details.push({ label: "Host", value: o.transport_settings.host })
    if (o.transport_settings?.path) details.push({ label: "Path", value: o.transport_settings.path })
    if (o.transport_settings?.serviceName) details.push({ label: "Service", value: o.transport_settings.serviceName })
    if (o.vless_settings?.flow) details.push({ label: "Flow", value: o.vless_settings.flow })
    return details
}

export interface OutboundUsage {
    rules: RoutingRule[]
    balancers: BalancingRule[]
}

// Who sends traffic here. Rules name the tag outright; balancers match by
// selector prefix, the way Xray's balancer does, or name it as the fallback.
export function buildOutboundUsage(
    outbounds: Outbound[],
    rules: RoutingRule[],
    balancers: BalancingRule[],
): Map<string, OutboundUsage> {
    const map = new Map<string, OutboundUsage>()
    for (const o of outbounds) {
        map.set(o.tag, {
            rules: rules.filter(r => r.outbound_tag === o.tag),
            balancers: balancers.filter(b =>
                b.fallback_tag === o.tag ||
                (b.outbound_selectors ?? []).some(sel => sel !== "" && o.tag.startsWith(sel)),
            ),
        })
    }
    return map
}

export function usageCount(u: OutboundUsage | undefined): number {
    return u ? u.rules.length + u.balancers.length : 0
}

// Xray hands unmatched traffic to the first outbound in the config. The list
// arrives in config order, including generated router outbounds before stored
// rows. Generated rows share id=0, so identity must use the unique tag.
export function defaultOutboundTag(outbounds: Outbound[]): string | null {
    return outbounds.find(o => !o.is_disabled)?.tag ?? null
}

export function outboundTraffic(o: Outbound): number {
    return (o.uplink || 0) + (o.downlink || 0)
}

// Results live on the outbound itself, so a fresh test and a value loaded from
// the server render through the same path.
export function testEntryOf(o: Outbound): OutboundTestEntry | null {
    return o.last_test_result && o.last_tested_at
        ? { result: o.last_test_result, tested_at: o.last_tested_at }
        : null
}

// Generated router rows have no database identity and must never enter CRUD,
// selection, or probe queues. Keep them visible in the read-only router list.
export function partitionOutbounds(outbounds: Outbound[]) {
    return {
        managedOutbounds: outbounds.filter(o => o.managed),
        outbounds: outbounds.filter(o => !o.managed && o.id > 0),
    }
}

export function canTestOutbound(outbound: Outbound): boolean {
    return !outbound.managed && outbound.id > 0 &&
        !["blackhole", "dns", "loopback", "http"].includes(outbound.protocol)
}
