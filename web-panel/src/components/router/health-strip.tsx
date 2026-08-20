import { cn } from "@/lib/utils"
import { useRouterHealth, useSetUplinkForce } from "@/lib/queries/use-router-health"
import type { HealthSample, UplinkHealth, UplinkVerdict, VPNHealth } from "@/lib/types/health"

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
function Sparkline({ history }: { history: HealthSample[] }) {
    const samples = history.slice(-180)
    if (samples.length < 2) return null
    const max = Math.max(...samples.map((s) => s.rtt_ms), 1)
    const w = 112
    const h = 28
    const step = w / (samples.length - 1)
    const pts = samples.map(
        (s, i) =>
            [(i * step).toFixed(1), (h - (s.rtt_ms / max) * (h - 3) - 1).toFixed(1)] as const,
    )
    const line = pts.map((p) => p.join(",")).join(" ")
    const area = `0,${h} ${line} ${w},${h}`
    return (
        <svg
            viewBox={`0 0 ${w} ${h}`}
            className="text-chart-2 h-7 w-28 shrink-0"
            role="img"
            aria-label="Round-trip time, last 15 minutes"
        >
            <polygon points={area} fill="currentColor" opacity="0.12" />
            <polyline points={line} fill="none" stroke="currentColor" strokeWidth="1.5" />
        </svg>
    )
}

function ForceControl({ ifName, force }: { ifName: string; force: string }) {
    const set = useSetUplinkForce()
    const states = [
        { state: "" as const, label: "auto" },
        { state: "up" as const, label: "force up" },
        { state: "down" as const, label: "force down" },
    ]
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
                    onClick={() => set.mutate({ ifName, state: s.state })}
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
            <Sparkline history={history} />
        </div>
    )
}

function UplinkCard({ up }: { up: UplinkHealth }) {
    return (
        <div
            data-uplink={up.if_name}
            className="bg-surface-2 border-border flex flex-col gap-3 rounded-lg border p-4"
        >
            <div className="flex items-start justify-between gap-2">
                <div>
                    <p className="font-mono text-base font-medium">{up.if_name}</p>
                    <p className="text-text-tertiary text-xs">
                        {up.slot === "domestic" ? "domestic WAN" : "secondary WAN"}
                    </p>
                </div>
                <VerdictBadge verdict={up.verdict} />
            </div>
            <Ladder carrier={up.carrier} gateway={up.gateway} internet={up.internet} />
            <StatsRow loss={up.loss_pct} rtt={up.median_rtt_ms} history={up.history} />
            <ForceControl ifName={up.if_name} force={up.force_state} />
        </div>
    )
}

function VPNCard({ vpn }: { vpn: VPNHealth }) {
    // Total loss means nothing came back through the tunnel.
    const reachable = vpn.loss_pct < 100
    return (
        <div
            data-uplink="vpn"
            className="bg-surface-2 border-border flex flex-col gap-3 rounded-lg border p-4"
        >
            <div className="flex items-start justify-between gap-2">
                <div>
                    <p className="font-mono text-base font-medium">tunnel</p>
                    <p className="text-text-tertiary text-xs">probed through the VPN</p>
                </div>
                <VerdictBadge verdict={reachable ? "up" : "no-internet"} />
            </div>
            <div className="space-y-1">
                <div
                    data-layer="tunnel"
                    data-layer-status={reachable ? "up" : "down"}
                    className={cn("h-1.5 rounded-full", reachable ? LAYER_BAR.up : LAYER_BAR.down)}
                />
                <p className="text-text-tertiary text-center font-mono text-[11px] tracking-wide">
                    through the tunnel
                </p>
            </div>
            <div className="mt-auto">
                <StatsRow loss={vpn.loss_pct} rtt={vpn.median_rtt_ms} history={vpn.history} />
            </div>
        </div>
    )
}

// Reads the same loop the failover acts on: what you see is what it decided from.
export function HealthStrip() {
    const health = useRouterHealth()

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
                    Domestic internet is down — traffic is riding the VPN until it recovers.
                </div>
            )}
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {uplinks.map((up) => (
                    <UplinkCard key={up.if_name} up={up} />
                ))}
                {vpn?.present && <VPNCard vpn={vpn} />}
            </div>
        </section>
    )
}
