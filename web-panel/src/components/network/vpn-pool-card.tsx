import { TriangleAlert } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import { useRouterHealth } from "@/lib/queries/use-router-health"
import type { TunnelStatus, VPNPoolStatus } from "@/lib/types/network"
import type { TunnelHealth } from "@/lib/types/health"

interface Props {
    status: VPNPoolStatus | undefined
    loading: boolean
}

type Member = TunnelStatus & { health?: TunnelHealth }

/** A member answering, or at least not yet judged, can still take traffic. */
function usable(m: Member): boolean {
    const v = m.health?.verdict
    return v === "up" || v === "degraded" || v === "" || v === undefined
}

/** Segment tone says why a member is or isn't carrying traffic. */
function segmentTone(m: Member): string {
    if (!usable(m)) return "bg-status-danger/40"
    if (!m.in_pool) return "bg-surface-3"
    return m.health?.degraded ? "bg-status-warning" : "bg-status-success"
}

// Weights only mean something as a ratio inside one tier, so draw them as one.
function TierRow({ tier, members, state }: { tier: number; members: Member[]; state: string }) {
    return (
        <div className="flex items-center gap-3" data-tier={tier}>
            <span className="text-text-tertiary w-12 shrink-0 font-mono text-xs">tier {tier}</span>
            <div className="flex min-w-0 flex-1 gap-1">
                {members.map((m) => (
                    <div
                        key={m.if_name}
                        data-member={m.if_name}
                        title={`${m.name} · weight ${m.weight}${m.in_pool ? "" : " · out of the pool"}`}
                        style={{ flexGrow: m.weight }}
                        className={cn(
                            "flex h-6 min-w-0 items-center rounded px-2",
                            segmentTone(m),
                        )}
                    >
                        <span
                            className={cn(
                                "truncate text-xs font-medium",
                                m.in_pool && usable(m) ? "text-surface-1" : "text-text-secondary",
                            )}
                        >
                            {m.name}
                        </span>
                    </div>
                ))}
            </div>
            <span
                data-tier-state={state}
                className={cn(
                    "w-20 shrink-0 text-right text-xs",
                    state === "carrying" ? "text-status-success" : "text-text-tertiary",
                )}
            >
                {state}
            </span>
        </div>
    )
}

export function VpnPoolCard({ status, loading }: Props) {
    const health = useRouterHealth()
    if (loading) return <Skeleton className="h-40 w-full" />
    if (!status) return null

    const pool = health.data?.vpn
    const healthById = new Map((pool?.tunnels ?? []).map((t) => [t.profile_id, t]))
    const members: Member[] = status.tunnels.map((t) => ({ ...t, health: healthById.get(t.profile_id) }))

    const connected = members.some((m) => m.connected)
    const tiers = [...new Set(members.map((m) => m.priority))].sort((a, b) => a - b)
    const activeTier = tiers.find((p) => members.some((m) => m.priority === p && m.in_pool))

    // Count inside the active tier only. Standby tiers are idle by design, so
    // counting them as missing would leave a healthy ladder permanently amber.
    const active = members.filter((m) => m.priority === activeTier)
    const total = members.length
    const carrying = active.filter((m) => m.in_pool && usable(m)).length
    const whole = total > 0 && carrying === active.length

    return (
        <Card>
            <CardHeader className="pb-4">
                <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
                    <CardTitle className="flex items-center gap-2">
                        <span
                            aria-hidden
                            className={cn(
                                "h-2 w-2 shrink-0 rounded-full",
                                total === 0
                                    ? "bg-muted-foreground/40"
                                    : carrying === 0
                                      ? "bg-status-danger"
                                      : connected && whole
                                        ? "bg-emerald-500"
                                        : "bg-status-warning",
                            )}
                        />
                        {total === 0
                            ? "No VPN in the pool"
                            : carrying === 0
                              ? "No tunnel is carrying traffic"
                              : `${carrying} of ${active.length} carrying in tier ${activeTier}`}
                    </CardTitle>
                    {pool?.present && total > 0 && (
                        <span className="text-text-secondary text-sm tabular-nums">
                            {pool.loss_pct}% loss · {pool.median_rtt_ms}ms median
                        </span>
                    )}
                </div>
                <CardDescription>
                    {total === 0
                        ? "Nothing is running over the secondary uplink."
                        : "Bar widths are each tunnel's share of its tier."}
                </CardDescription>
            </CardHeader>

            <CardContent className="space-y-2">
                {tiers.map((p) => (
                    <TierRow
                        key={p}
                        tier={p}
                        members={members.filter((m) => m.priority === p)}
                        state={
                            p === activeTier
                                ? "carrying"
                                : members.some((m) => m.priority === p && usable(m))
                                  ? "standby"
                                  : "no answer"
                        }
                    />
                ))}

                {total > 0 && !connected && !status.uplinks.some((u) => u.up) && (
                    <p className="text-muted-foreground flex items-start gap-2 pt-1 text-xs">
                        <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
                        Every secondary uplink is down, so the tunnels have nothing to run over yet.
                    </p>
                )}
            </CardContent>
        </Card>
    )
}
