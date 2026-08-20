import { ArrowDown, ArrowUp, ShieldCheck, TriangleAlert } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { InfoPopover } from "@/components/ui/info-popover"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import { formatBytes, handshakeLabel } from "@/lib/vpn-labels"
import { useRouterHealth } from "@/lib/queries/use-router-health"
import type { VPNStatus } from "@/lib/types/network"

interface Props {
    status: VPNStatus | undefined
    loading: boolean
}

export function VpnStatusCard({ status, loading }: Props) {
    const health = useRouterHealth()
    if (loading) return <Skeleton className="h-44 w-full" />
    if (!status) return null

    const on = status.active_profile_id !== null
    const probe = health.data?.vpn

    return (
        <Card>
            <CardHeader className="pb-4">
                <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
                    <CardTitle className="flex items-center gap-2">
                        <span
                            aria-hidden
                            className={cn(
                                "h-2 w-2 shrink-0 rounded-full",
                                status.connected
                                    ? "bg-emerald-500"
                                    : on
                                      ? "bg-status-warning"
                                      : "bg-muted-foreground/40",
                            )}
                        />
                        {!on ? "No VPN in use" : status.connected ? "Connected" : "Not answering"}
                    </CardTitle>
                    {on && (
                        <span className="text-muted-foreground text-sm">{status.name}</span>
                    )}
                </div>
                {/* The banner above already says what that costs; repeating it
                    here is two paragraphs saying one thing. */}
                <CardDescription>
                    {on ? handshakeLabel(status) : "Nothing is running over the secondary uplink."}
                </CardDescription>
            </CardHeader>

            <CardContent className="space-y-4">
                {status.last_error && (
                    <p className="text-status-warning text-xs">{status.last_error}</p>
                )}

                {on && (
                    <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-4">
                        <div>
                            <dt className="text-text-tertiary text-xs">Server</dt>
                            <dd className="font-mono text-xs break-all">
                                {status.endpoint || "—"}
                            </dd>
                        </div>
                        <div>
                            <dt className="text-text-tertiary text-xs">Received</dt>
                            <dd className="flex items-center gap-1 tabular-nums">
                                <ArrowDown className="h-3 w-3" aria-hidden />
                                {formatBytes(status.rx_bytes)}
                            </dd>
                        </div>
                        <div>
                            <dt className="text-text-tertiary text-xs">Sent</dt>
                            <dd className="flex items-center gap-1 tabular-nums">
                                <ArrowUp className="h-3 w-3" aria-hidden />
                                {formatBytes(status.tx_bytes)}
                            </dd>
                        </div>
                        <div>
                            <dt className="text-text-tertiary flex items-center gap-1 text-xs">
                                Packet size / keepalive
                                {/* Both are usually filled in for the operator, so
                                    say where the numbers came from. */}
                                <InfoPopover label="About these settings">
                                    A config that does not say gets an MTU of 1420 and a keepalive
                                    of 25 seconds. The keepalive matters here: the dish sits behind
                                    a shared address, and without it the far end loses the way back.
                                </InfoPopover>
                            </dt>
                            <dd className="tabular-nums">
                                {status.mtu} / {status.keepalive_seconds}s
                            </dd>
                        </div>
                    </dl>
                )}

                {on && probe?.present && (
                    <p className="text-text-secondary text-sm tabular-nums">
                        Tunnel probe: {probe.loss_pct}% loss · median {probe.median_rtt_ms}ms
                    </p>
                )}

                {on && !status.connected && !status.secondary_uplink_up && (
                    <p className="text-muted-foreground flex items-start gap-2 text-xs">
                        <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
                        The secondary uplink is down, so the tunnel has nothing to run over yet.
                    </p>
                )}

                <p className="text-muted-foreground flex items-start gap-2 text-xs">
                    <ShieldCheck className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
                    The secondary uplink never carries traffic in the open. If the tunnel stops,
                    that traffic stops with it rather than falling back.
                </p>
            </CardContent>
        </Card>
    )
}
