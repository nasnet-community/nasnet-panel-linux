import { useState } from "react"
import { SlidersHorizontal } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import {
    ConnectionChecksDialog,
    type CheckSibling,
} from "@/components/router/connection-checks-dialog"
import { UplinkDetailSheet } from "@/components/router/uplink-detail-sheet"
import { CopyableText } from "@/components/ui/copyable-text"
import { PortName } from "@/components/network/port-name"
import { RttSparkline } from "@/components/router/rtt-sparkline"
import { cn } from "@/lib/utils"
import { groupAddresses, isDomesticSlot } from "@/lib/network-labels"
import { useRouterHealth, useSetUplinkForce } from "@/lib/queries/use-router-health"
import type {
    HealthSample,
    TunnelHealth,
    UplinkHealth,
    UplinkVerdict,
    VPNPoolHealth,
} from "@/lib/types/health"
import type { NetworkInterfaceView, UplinkSlot } from "@/lib/types/network"

// Closed over UplinkVerdict so a new verdict is a compile error.
// Severities match linkTone.
const VERDICT_LABEL: Record<UplinkVerdict, string> = {
    "up": "online",
    "degraded": "degraded",
    "no-internet": "no internet",
    "no-gateway": "no gateway",
    "no-carrier": "no carrier",
    "forced-up": "forced up",
    "forced-down": "forced down",
    "": "waiting",
}

const TONE_OK = "bg-status-success-soft text-status-success"
const TONE_WARN = "bg-status-warning-soft text-status-warning"
const TONE_DOWN = "bg-status-danger-soft text-status-danger"
const TONE_MUTED = "bg-surface-3 text-text-tertiary"

const VERDICT_TONE: Record<UplinkVerdict, string> = {
    "up": TONE_OK,
    "degraded": TONE_WARN,
    "no-internet": TONE_DOWN,
    "no-gateway": TONE_DOWN,
    "no-carrier": TONE_DOWN,
    "forced-up": TONE_WARN,
    "forced-down": TONE_DOWN,
    "": TONE_MUTED,
}

const LAYER_BAR: Record<string, string> = {
    up: "bg-status-success",
    down: "bg-status-danger",
    unknown: "bg-status-neutral",
}

function VerdictBadge({ verdict }: { verdict: UplinkVerdict }) {
    return (
        <span
            data-verdict={verdict}
            className={cn(
                "rounded-full px-2.5 py-0.5 text-xs font-medium whitespace-nowrap",
                VERDICT_TONE[verdict] ?? TONE_DOWN,
            )}
        >
            {VERDICT_LABEL[verdict] ?? verdict}
        </span>
    )
}

// The point of the page: not "is it up" but "which layer broke".
function Ladder({ carrier, gateway, internet }: { carrier: string; gateway: string; internet: string }) {
    const layers = [
        { key: "carrier", label: "carrier", status: carrier || "unknown" },
        { key: "gateway", label: "gateway", status: gateway || "unknown" },
        { key: "internet", label: "internet", status: internet || "unknown" },
    ]
    return (
        <div className="grid grid-cols-3 gap-1.5">
            {layers.map((l) => (
                <div key={l.key} className="space-y-1">
                    <div
                        data-layer={l.key}
                        data-layer-status={l.status}
                        className={cn("h-1.5 rounded-full", LAYER_BAR[l.status] ?? LAYER_BAR.unknown)}
                    />
                    <p className="text-text-tertiary text-center font-mono text-[11px] tracking-wide">
                        {l.label}
                    </p>
                </div>
            ))}
        </div>
    )
}

// Median RTT, last 15 minutes.

function ForceControl({
    ifName,
    force,
    slot,
    hasSibling,
}: {
    ifName: string
    force: string
    slot: UplinkSlot
    hasSibling: boolean
}) {
    const set = useSetUplinkForce()
    const confirm = useConfirm()
    const states = [
        { state: "" as const, label: "auto" },
        { state: "up" as const, label: "force up" },
        { state: "down" as const, label: "force down" },
    ]

    function apply(state: "" | "up" | "down") {
        set.mutate(
            { ifName, state },
            {
                onSuccess: () =>
                    toast.success(
                        state === "" ? `${ifName} back on auto` : `${ifName} forced ${state}`,
                    ),
                onError: (e) =>
                    toast.error(e instanceof Error ? e.message : "Failed to set the override"),
            },
        )
    }

    // Down is the one override that withdraws routes, so it asks first. Up and
    // auto only hand the decision back to the prober.
    async function choose(state: "" | "up" | "down") {
        if (state !== "down") {
            apply(state)
            return
        }
        const ok = await confirm({
            title: `Force ${ifName} down`,
            description:
                isDomesticSlot(slot)
                    ? hasSibling
                        ? "Its routes are withdrawn until you set it back. Domestic traffic " +
                          "moves to the other domestic line, and the panel still answers there."
                        : "Its routes are withdrawn until you set it back. This is the uplink " +
                          "the panel answers on, so you may lose access to this panel and have " +
                          "to undo it from the console."
                    : "Its routes are withdrawn until you set it back, and any tunnel riding " +
                      "it stops until it is re-homed or the uplink returns.",
            confirmLabel: "Force it down",
            variant: "warning",
        })
        if (ok) apply(state)
    }
    return (
        <div
            className="divide-border border-border flex w-fit divide-x overflow-hidden rounded-md border"
            role="group"
            aria-label={`Override ${ifName}`}
        >
            {states.map((s) => (
                <button
                    key={s.label}
                    type="button"
                    disabled={set.isPending}
                    onClick={() => void choose(s.state)}
                    className={cn(
                        "px-2.5 py-1 text-xs transition-colors",
                        force === s.state
                            ? "bg-surface-3 text-text-primary font-medium"
                            : "text-text-tertiary hover:text-text-secondary hover:bg-surface-3/50",
                    )}
                >
                    {s.label}
                </button>
            ))}
        </div>
    )
}

function StatsRow({ loss, rtt, history }: { loss: number; rtt: number; history: HealthSample[] }) {
    return (
        <div className="flex items-end justify-between gap-3">
            <p className="text-text-secondary text-sm tabular-nums">
                {loss}% loss · {rtt}ms
            </p>
            <RttSparkline history={history} />
        </div>
    )
}

// The card is the uplink's own card: health, name, address and the rename in one
// place, so no summary below the tabs has to repeat any of it.
function UplinkCard({
    up,
    iface,
    viaLabel,
    hasSibling,
    siblings,
    group,
    labelOf,
    vpn,
    onConfigure,
}: {
    onConfigure?: (iface: NetworkInterfaceView) => void
    up: UplinkHealth
    iface?: NetworkInterfaceView
    /** Who is carrying this line right now, already resolved to a label. */
    viaLabel: string | null
    hasSibling: boolean
    /** The other lines a shared check list would also change. */
    siblings: CheckSibling[]
    /** Every line in the same group, in slot order, this one included. */
    group: UplinkHealth[]
    labelOf: (ifName: string) => string
    vpn?: VPNPoolHealth | null
}) {
    const kind = isDomesticSlot(up.slot) ? "domestic WAN" : "secondary WAN"
    const { primary } = groupAddresses(iface?.addrs)
    const [checksOpen, setChecksOpen] = useState(false)
    const [detailOpen, setDetailOpen] = useState(false)
    const lineLabel = iface?.label || up.if_name
    return (
        <div
            data-uplink={up.if_name}
            className="bg-surface-2 border-border hover:border-border-strong flex flex-col gap-3 rounded-lg border p-4 transition-colors"
        >
            <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                    {iface ? (
                        <PortName iface={iface} variant="title" />
                    ) : (
                        <p className="font-mono text-base font-medium">{up.if_name}</p>
                    )}
                    <p className="text-text-tertiary text-xs">
                        {/* The name already is the interface name when unnamed. */}
                        {iface?.label ? `${up.if_name} · ${kind}` : kind}
                    </p>
                    {primary ? (
                        <CopyableText text={primary} className="mt-1 font-mono text-xs" />
                    ) : (
                        <p className="text-text-tertiary mt-1 text-xs">no address</p>
                    )}
                </div>
                <div className="flex flex-col items-end gap-1">
                    <VerdictBadge verdict={up.verdict} />
                    {viaLabel && (
                        <span
                            data-via={up.via}
                            className={cn(
                                "rounded-full px-2.5 py-0.5 text-xs font-medium whitespace-nowrap",
                                TONE_WARN,
                            )}
                        >
                            riding {viaLabel}
                        </span>
                    )}
                </div>
            </div>
            {/* Only the readout: the name and the address above it are their own
                controls, and a button cannot hold another button. */}
            <button
                type="button"
                aria-label={`Details for ${lineLabel}`}
                onClick={() => setDetailOpen(true)}
                className="focus-visible:ring-ring -m-1 flex flex-col gap-3 rounded-md p-1 text-left focus-visible:ring-2 focus-visible:outline-none"
            >
                <Ladder carrier={up.carrier} gateway={up.gateway} internet={up.internet} />
                {/* A diagnosis, not an event, so it sits here rather than in a toast. */}
                {up.note && <p className="text-text-tertiary text-xs">{up.note}</p>}
                <StatsRow loss={up.loss_pct} rtt={up.median_rtt_ms} history={up.history} />
            </button>
            <div className="flex items-center justify-between gap-2">
                <ForceControl
                    ifName={up.if_name}
                    force={up.force_state}
                    slot={up.slot}
                    hasSibling={hasSibling}
                />
                <div className="flex items-center gap-1">
                    <Button
                        variant="ghost"
                        size="xs"
                        aria-label={`Connection checks for ${lineLabel}`}
                        onClick={() => setChecksOpen(true)}
                    >
                        <SlidersHorizontal className="h-3 w-3" />
                        Checks
                    </Button>
                    <Button variant="ghost" size="xs" onClick={() => setDetailOpen(true)}>
                        Details
                    </Button>
                </div>
            </div>
            <ConnectionChecksDialog
                open={checksOpen}
                onOpenChange={setChecksOpen}
                slot={up.slot}
                lineLabel={lineLabel}
                siblings={siblings}
            />
            <UplinkDetailSheet
                open={detailOpen}
                onOpenChange={setDetailOpen}
                up={up}
                iface={iface}
                group={group}
                labelOf={labelOf}
                vpn={vpn}
                onConfigure={onConfigure && iface && !iface.source.startsWith("wwan_") && iface.wan?.method !== "rawip" ? () => { setDetailOpen(false); onConfigure(iface) } : undefined}
            />
        </div>
    )
}

// in_pool is not health: a dead pool keeps its carrier routed, so the verdict
// decides the colour.
function answering(t: TunnelHealth): boolean {
    return t.verdict === "up" || t.verdict === "degraded"
}


function PoolCard({ vpn }: { vpn: VPNPoolHealth }) {
    const total = vpn.tunnels.length
    // A standby is idle on purpose; only count what the strategy asked to carry.
    const expected = vpn.strategy === "spread" ? vpn.tunnels : vpn.tunnels.filter((t) => t.in_pool)
    const carrying = vpn.tunnels.filter((t) => t.in_pool && answering(t)).length
    const badge = vpn.tunnels.every((t) => t.verdict === "")
        ? { tone: TONE_MUTED, label: "waiting" }
        : carrying === 0
          ? { tone: TONE_DOWN, label: "down" }
          : carrying < expected.length
            ? { tone: TONE_WARN, label: `${carrying} of ${expected.length}` }
            : { tone: TONE_OK, label: "online" }
    const carrier = vpn.tunnels.find((t) => t.if_name === vpn.carrier)
    // One line: the tab owns per-member detail.
    const summary =
        carrying === 0
            ? "nothing carrying"
            : carrier
              ? `${carrier.name} carrying`
              : `${carrying} of ${total} sharing the traffic`
    return (
        <div
            data-uplink="vpn"
            className="bg-surface-2 border-border flex flex-col gap-3 rounded-lg border p-4"
        >
            <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                    <p className="font-mono text-base font-medium">VPN</p>
                    <p className="text-text-tertiary truncate text-xs">{summary}</p>
                </div>
                <span
                    data-pool-state={badge.label}
                    className={cn(
                        "rounded-full px-2.5 py-0.5 text-xs font-medium whitespace-nowrap",
                        badge.tone,
                    )}
                >
                    {badge.label}
                </span>
            </div>
            <div className="mt-auto">
                <StatsRow loss={vpn.loss_pct} rtt={vpn.median_rtt_ms} history={vpn.pool_history} />
            </div>
        </div>
    )
}

// Reads the same loop the failover acts on: what you see is what it decided from.
export function HealthStrip({ interfaces = [], onConfigure }: { interfaces?: NetworkInterfaceView[]; onConfigure?: (iface: NetworkInterfaceView) => void }) {
    const health = useRouterHealth()
    const byIfName = new Map(interfaces.map((i) => [i.if_name, i]))

    if (health.isLoading) {
        return <div className="bg-surface-2 h-32 animate-pulse rounded-lg" aria-hidden />
    }
    if (health.isError || !health.data) {
        return (
            <p className="text-text-tertiary text-sm">
                Uplink health is unavailable — the probe loop has not reported yet.
            </p>
        )
    }

    // No uplinks serialises as null, not [].
    const { vpn, failover_active } = health.data
    const uplinks = health.data.uplinks ?? []
    const domesticCount = uplinks.filter((u) => isDomesticSlot(u.slot)).length
    const labelOf = (ifName: string): string =>
        ifName ? byIfName.get(ifName)?.label || ifName : ""

    // Both the sheet and the checks dialog talk about the whole group: one
    // shows the backup order, the other says which lines a save reaches.
    const groupOf = (self: UplinkHealth): UplinkHealth[] =>
        uplinks.filter((u) => isDomesticSlot(u.slot) === isDomesticSlot(self.slot))

    const siblingsOf = (self: UplinkHealth): CheckSibling[] =>
        groupOf(self)
            .filter((u) => u.if_name !== self.if_name)
            .map((u) => ({ slot: u.slot, label: labelOf(u.if_name) }))

    // The operator's name for the carrier beats its kernel name.
    const viaLabel = (via: string): string | null => {
        if (!via) return null
        if (via === "pool") return "the VPN pool"
        return byIfName.get(via)?.label || via
    }
    return (
        <section aria-label="Uplink health" className="space-y-3">
            <div className="flex items-baseline justify-between gap-4">
                <h2 className="text-base font-medium">Uplink health</h2>
                <p className="text-text-tertiary text-sm">Probed every 5 seconds</p>
            </div>
            {failover_active && (
                <div className="bg-status-warning-soft text-status-warning rounded-md px-3 py-2 text-sm font-medium">
                    Every domestic line is down — traffic is riding the VPN pool until one
                    recovers.
                </div>
            )}
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {uplinks.map((up) => (
                    <UplinkCard
                        key={up.if_name}
                        up={up}
                        iface={byIfName.get(up.if_name)}
                        viaLabel={viaLabel(up.via)}
                        hasSibling={isDomesticSlot(up.slot) && domesticCount > 1}
                        siblings={siblingsOf(up)}
                        group={groupOf(up)}
                        labelOf={labelOf}
                        vpn={vpn}
                        onConfigure={onConfigure}
                    />
                ))}
                {vpn?.present && <PoolCard vpn={vpn} />}
            </div>
        </section>
    )
}
