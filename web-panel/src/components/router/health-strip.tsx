import { toast } from "sonner"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { CopyableText } from "@/components/ui/copyable-text"
import { PortName } from "@/components/network/port-name"
import { RttSparkline } from "@/components/router/rtt-sparkline"
import { cn } from "@/lib/utils"
import { groupAddresses } from "@/lib/network-labels"
import { useRouterHealth, useSetUplinkForce } from "@/lib/queries/use-router-health"
import type {
    HealthSample,
    TunnelHealth,
    UplinkHealth,
    UplinkVerdict,
    VPNPoolHealth,
} from "@/lib/types/health"
import type { NetworkInterfaceView } from "@/lib/types/network"

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

function ForceControl({ ifName, force, slot }: { ifName: string; force: string; slot: string }) {
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
                slot === "domestic"
                    ? "Its routes are withdrawn until you set it back. This is the uplink " +
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
function UplinkCard({ up, iface }: { up: UplinkHealth; iface?: NetworkInterfaceView }) {
    const kind = up.slot === "domestic" ? "domestic WAN" : "secondary WAN"
    const { primary } = groupAddresses(iface?.addrs)
    return (
        <div
            data-uplink={up.if_name}
            className="bg-surface-2 border-border flex flex-col gap-3 rounded-lg border p-4"
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
                <VerdictBadge verdict={up.verdict} />
            </div>
            <Ladder carrier={up.carrier} gateway={up.gateway} internet={up.internet} />
            {/* A diagnosis, not an event, so it sits here rather than in a toast. */}
            {up.note && <p className="text-text-tertiary text-xs">{up.note}</p>}
            <StatsRow loss={up.loss_pct} rtt={up.median_rtt_ms} history={up.history} />
            <ForceControl ifName={up.if_name} force={up.force_state} slot={up.slot} />
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
export function HealthStrip({ interfaces = [] }: { interfaces?: NetworkInterfaceView[] }) {
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
    return (
        <section aria-label="Uplink health" className="space-y-3">
            <div className="flex items-baseline justify-between gap-4">
                <h2 className="text-base font-medium">Uplink health</h2>
                <p className="text-text-tertiary text-sm">Probed every 5 seconds</p>
            </div>
            {failover_active && (
                <div className="bg-status-warning-soft text-status-warning rounded-md px-3 py-2 text-sm font-medium">
                    Domestic internet is down — traffic is riding the VPN pool until it recovers.
                </div>
            )}
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {uplinks.map((up) => (
                    <UplinkCard key={up.if_name} up={up} iface={byIfName.get(up.if_name)} />
                ))}
                {vpn?.present && <PoolCard vpn={vpn} />}
            </div>
        </section>
    )
}
