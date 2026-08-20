import { cn } from "@/lib/utils"
import { ROLE_BAYS, bayHolder, groupAddresses, linkTone, type RoleBay } from "@/lib/network-labels"
import type { NetworkInterfaceView, NetworkState } from "@/lib/types/network"

const ACCENT: Record<RoleBay["accent"], string> = {
    domestic: "bg-chart-2",
    secondary: "bg-chart-6",
    none: "bg-border-strong",
}

const DOT: Record<string, string> = {
    up: "bg-status-success",
    warn: "bg-status-warning",
    down: "bg-status-danger",
    absent: "bg-status-neutral",
}

function LiveDot({ tone }: { tone: string }) {
    return (
        <span className="relative flex h-2 w-2 shrink-0" aria-hidden>
            {tone === "up" && (
                <span className="bg-status-success/60 absolute inline-flex h-full w-full animate-ping rounded-full motion-reduce:hidden" />
            )}
            <span className={cn("relative inline-flex h-2 w-2 rounded-full", DOT[tone])} />
        </span>
    )
}

interface Props {
    interfaces: NetworkInterfaceView[]
    state: NetworkState | undefined
}

/** The router's four role slots as physical bays. Filled bays are the current
 *  topology; empty ones are the remaining setup, so one object answers both
 *  "what is my network" and "what is left to do". */
export function RoleBays({ interfaces, state }: Props) {
    const members = interfaces.filter((i) => i.role === "lan_member").length

    return (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {ROLE_BAYS.map((bay) => {
                const held = bayHolder(interfaces, bay)
                const uplink = state?.uplinks?.find((u) => u.slot === bay.slot && bay.slot !== "")
                const addr = held ? groupAddresses(held.addrs).primary : null
                const tone = held ? linkTone(held, uplink) : "absent"

                return (
                    <div
                        key={`${bay.role}:${bay.slot}`}
                        className={cn(
                            "relative overflow-hidden rounded-lg border p-4 transition-colors",
                            held
                                ? "bg-surface-2 border-border"
                                : "border-border-subtle bg-transparent border-dashed",
                        )}
                    >
                        {held && (
                            <span
                                aria-hidden
                                className={cn(
                                    "absolute inset-x-0 top-0 h-[2px]",
                                    ACCENT[bay.accent],
                                )}
                            />
                        )}

                        <p className="text-text-tertiary text-xs font-medium uppercase tracking-[0.12em]">
                            {bay.label}
                        </p>

                        {held ? (
                            <div className="mt-2.5 space-y-1.5">
                                <div className="flex items-center gap-2">
                                    <LiveDot tone={tone} />
                                    <span className="font-mono text-base font-medium">
                                        {held.if_name}
                                    </span>
                                    {held.label && (
                                        <span className="text-text-secondary truncate text-sm">
                                            {held.label}
                                        </span>
                                    )}
                                </div>

                                <p className="text-text-secondary font-mono text-sm">
                                    {addr ?? "no address"}
                                    {uplink?.gateway && (
                                        <span className="text-text-tertiary">
                                            {" "}
                                            via {uplink.gateway}
                                        </span>
                                    )}
                                </p>

                                {bay.role === "lan" && members > 0 && (
                                    <p className="text-text-tertiary text-xs">
                                        + {members} bridged member{members > 1 ? "s" : ""}
                                    </p>
                                )}

                                {!held.present && (
                                    <p className="text-status-warning text-xs">
                                        Port missing — role held for its return
                                    </p>
                                )}

                                {/* The link can be healthy while the tunnel over it is dead. */}
                                {bay.slot === "secondary" && state?.vpn?.active && !state.vpn.connected && (
                                    <p className="text-status-warning text-xs">
                                        VPN not answering — traffic here is being dropped
                                    </p>
                                )}
                            </div>
                        ) : (
                            <div className="mt-2.5 space-y-1.5">
                                <p className="text-text-tertiary text-base">Not assigned</p>
                                <p className="text-text-tertiary text-sm leading-relaxed">
                                    {bay.hint}
                                </p>
                            </div>
                        )}
                    </div>
                )
            })}
        </div>
    )
}
