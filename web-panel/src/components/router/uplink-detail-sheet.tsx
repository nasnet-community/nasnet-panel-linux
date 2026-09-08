import { useState } from "react"
import { ChevronDown, RefreshCw, SlidersHorizontal, TriangleAlert } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { CopyableText } from "@/components/ui/copyable-text"
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { PortName } from "@/components/network/port-name"
import { ConnectionChecksDialog, type CheckSibling } from "@/components/router/connection-checks-dialog"
import { RttSparkline } from "@/components/router/rtt-sparkline"
import { Wire } from "@/components/network/wire"
import { groupAddresses, isDomesticSlot } from "@/lib/network-labels"
import { attachmentLabel } from "@/lib/network-labels"
import { useFlowEvents } from "@/lib/queries/use-flow"
import { usePortForwards, usePortMapStatus } from "@/lib/queries/use-network"
import { protoLabel } from "@/lib/router-checks"
import {
    bytesLabel,
    hasSpeed,
    lineSubtitle,
    memberRole,
    rungMeaning,
    sinceLabel,
    slotPosition,
    speedLabel,
} from "@/lib/uplink-detail"
import { cn } from "@/lib/utils"
import type { UplinkHealth, VPNPoolHealth } from "@/lib/types/health"
import type { NetworkInterfaceView } from "@/lib/types/network"

const LAYER_BAR: Record<string, string> = {
    up: "bg-status-success",
    down: "bg-status-danger",
    unknown: "bg-status-neutral",
}

/** Section wrapper: every block reads the same, so the eye learns one shape. */
function Block({
    title,
    action,
    children,
    note,
}: {
    title?: string
    action?: React.ReactNode
    children: React.ReactNode
    note?: React.ReactNode
}) {
    return (
        <section className="border-border-subtle space-y-2.5 border-t pt-3.5 first:border-t-0 first:pt-0">
            {title && (
                <div className="flex items-center justify-between gap-3">
                    <h4 className="text-text-secondary text-xs font-medium">{title}</h4>
                    {action}
                </div>
            )}
            {children}
            {note && <p className="text-text-tertiary text-xs">{note}</p>}
        </section>
    )
}

function Row({ k, v }: { k: React.ReactNode; v: React.ReactNode }) {
    return (
        <div className="border-border-subtle flex items-baseline justify-between gap-3 border-b py-1.5 text-[13px] last:border-b-0">
            <span className="text-text-tertiary shrink-0">{k}</span>
            <span className="min-w-0 text-right">{v}</span>
        </div>
    )
}

/** The path out, drawn with the same wire the port-mapping card uses. A
 *  failing internet cuts the segment past the upstream router, so the picture
 *  carries the same diagnosis the ladder just gave. */
function AddressPath({ up, address }: { up: UplinkHealth; address: string | null }) {
    const gatewayUp = up.gateway === "up"
    const inetUp = up.internet !== "down"
    return (
        <div className="flex items-center gap-3.5 overflow-x-auto pb-4 font-mono text-xs whitespace-nowrap">
            <span className="text-text-tertiary shrink-0">internet</span>
            <Wire state={gatewayUp && inetUp ? "live" : gatewayUp ? "cut" : "off"} />
            <span className="relative shrink-0 tabular-nums">
                {up.gateway_ip || "unknown"}
                <span className="text-text-tertiary absolute top-[17px] left-1/2 -translate-x-1/2 font-sans text-[11px]">
                    upstream router
                </span>
            </span>
            <Wire state={gatewayUp ? "live" : "off"} />
            <span className="relative shrink-0 tabular-nums">
                {address ?? "no address"}
                <span className="text-text-tertiary absolute top-[17px] left-1/2 -translate-x-1/2 font-sans text-[11px]">
                    this box
                </span>
            </span>
        </div>
    )
}

/** Everything one uplink knows, in the order an operator asks it. */
export function UplinkDetailSheet({
    open,
    onOpenChange,
    up,
    iface,
    group,
    labelOf,
    vpn,
    onConfigure,
}: {
    onConfigure?: () => void
    open: boolean
    onOpenChange: (open: boolean) => void
    up: UplinkHealth
    iface?: NetworkInterfaceView
    /** Every line in the same group, in slot order, this one included. */
    group: UplinkHealth[]
    labelOf: (ifName: string) => string
    vpn?: VPNPoolHealth | null
}) {
    const [checksOpen, setChecksOpen] = useState(false)
    const [techOpen, setTechOpen] = useState(false)
    const forwards = usePortForwards(open)
    const portmap = usePortMapStatus(open)
    const events = useFlowEvents(open)

    const domestic = isDomesticSlot(up.slot)
    const lineLabel = iface?.label || up.if_name
    const { primary, extra } = groupAddresses(iface?.addrs)
    const okTargets = up.targets.filter((t) => t.ok).length
    const carrier = up.via && up.via !== "pool" ? labelOf(up.via) : null
    const siblings = group.filter((g) => g.if_name !== up.if_name)
    const since = sinceLabel(up)

    const rungs = [
        { key: "carrier" as const, status: up.carrier || "unknown" },
        { key: "gateway" as const, status: up.gateway || "unknown" },
        { key: "internet" as const, status: up.internet || "unknown" },
    ]

    const mine = forwards.data?.filter((f) => iface && f.uplink_key === iface.key) ?? []
    const pm = portmap.data?.wans?.find((w) => w.if_name === up.if_name)
    const mineEvents = (events.data ?? [])
        .filter((e) => {
            const p = e.payload as { if_name?: string } | null
            return !!p && typeof p === "object" && p.if_name === up.if_name
        })
        .slice(0, 6)

    const checkSiblings: CheckSibling[] = siblings.map((s) => ({
        slot: s.slot,
        label: labelOf(s.if_name),
    }))

    return (
        <>
            <Sheet open={open} onOpenChange={onOpenChange}>
                <SheetContent
                    side="right"
                    data-uplink-sheet={up.if_name}
                    className="w-full overflow-y-auto sm:max-w-lg"
                >
                    <SheetHeader className="pb-3">
                        <div className="flex items-start justify-between gap-3 pr-6">
                            <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5">
                                {iface ? (
                                    <PortName iface={iface} variant="title" />
                                ) : (
                                    <SheetTitle className="font-mono">{up.if_name}</SheetTitle>
                                )}
                                {since && (
                                    <span className="text-text-tertiary text-xs tabular-nums">
                                        ({since})
                                    </span>
                                )}
                            </div>
                            <div className="flex shrink-0 flex-col items-end gap-1">
                                <VerdictPill verdict={up.verdict} />
                                {carrier && (
                                    <span className="bg-status-warning-soft text-status-warning rounded-full px-2.5 py-0.5 text-xs font-medium">
                                        using {carrier}
                                    </span>
                                )}
                                {up.via === "pool" && (
                                    <span className="bg-status-warning-soft text-status-warning rounded-full px-2.5 py-0.5 text-xs font-medium">
                                        using the VPN
                                    </span>
                                )}
                            </div>
                        </div>
                        {iface && <SheetTitle className="sr-only">{lineLabel} details</SheetTitle>}
                        <SheetDescription>{lineSubtitle(up.slot, siblings.length)}</SheetDescription>
                        {onConfigure && <Button className="mt-3 self-start" variant="outline" size="sm" onClick={onConfigure}>Configure WAN</Button>}
                    </SheetHeader>

                    <div className="space-y-3.5 px-4 pb-6">
                        {up.via && (
                            <Alert variant="warning">
                                <TriangleAlert className="h-4 w-4" />
                                <AlertDescription>
                                    <span className="block">
                                        {lineLabel} can&apos;t reach the internet right now. Its
                                        traffic goes through{" "}
                                        {up.via === "pool" ? "the VPN" : carrier} until it recovers.
                                    </span>
                                    <span className="text-text-secondary mt-1 block text-xs">
                                        Nothing to do. The router keeps testing {lineLabel} and
                                        switches back on its own.
                                    </span>
                                </AlertDescription>
                            </Alert>
                        )}

                        <Block>
                            {rungs.map((r) => (
                                <div
                                    key={r.key}
                                    data-rung={r.key}
                                    data-rung-status={r.status}
                                    className="grid grid-cols-[44px_64px_minmax(0,1fr)] items-center gap-2.5 py-0.5"
                                >
                                    <div
                                        className={cn(
                                            "h-1.5 rounded-full",
                                            LAYER_BAR[r.status] ?? LAYER_BAR.unknown,
                                        )}
                                    />
                                    <span className="text-text-tertiary font-mono text-[11px] tracking-wide">
                                        {r.key}
                                    </span>
                                    <span
                                        className={cn(
                                            "text-[13px]",
                                            r.status === "down"
                                                ? "text-status-danger"
                                                : "text-text-secondary",
                                        )}
                                    >
                                        {rungMeaning(r.key, r.status, {
                                            gatewayIP: up.gateway_ip,
                                            okTargets,
                                            totalTargets: up.targets.length,
                                            speedMbit: iface?.speed_mbit,
                                        })}
                                    </span>
                                </div>
                            ))}
                        </Block>

                        {up.recovery && (
                            <Block
                                title="Recovery"
                                action={
                                    <span className="text-text-tertiary text-xs tabular-nums">
                                        {up.recovery.passes} of {up.recovery.needed} checks passed
                                    </span>
                                }
                                note={`After ${up.recovery.needed} passes in a row and a short settling time, traffic moves back to ${lineLabel}.`}
                            >
                                <div className="bg-surface-3 h-1.5 overflow-hidden rounded-full">
                                    <div
                                        data-recovery-bar
                                        className="bg-status-success h-full rounded-full transition-[width]"
                                        style={{
                                            width: `${Math.min(100, Math.round((up.recovery.passes / Math.max(1, up.recovery.needed)) * 100))}%`,
                                        }}
                                    />
                                </div>
                            </Block>
                        )}

                        <Block
                            title="Connection checks"
                            action={
                                <Button variant="ghost" size="xs" onClick={() => setChecksOpen(true)}>
                                    <SlidersHorizontal className="h-3 w-3" />
                                    Change
                                </Button>
                            }
                            note="Checked every 5 seconds."
                        >
                            {up.targets.length === 0 ? (
                                <p className="text-text-tertiary text-xs">
                                    No checks configured, so this line is never called down.
                                </p>
                            ) : (
                                <div className="divide-border-subtle divide-y">
                                    {up.targets.map((t) => (
                                        <div
                                            key={t.address}
                                            data-check={t.address}
                                            className="flex items-center gap-2.5 py-1.5 text-[13px]"
                                        >
                                            <span
                                                aria-hidden
                                                className={cn(
                                                    "h-2 w-2 shrink-0 rounded-full",
                                                    t.ok ? "bg-status-success" : "bg-status-danger",
                                                )}
                                            />
                                            <span
                                                className={cn(
                                                    "min-w-0 truncate",
                                                    !t.label && "font-mono text-xs",
                                                )}
                                            >
                                                {t.label || t.address}
                                            </span>
                                            {t.label && (
                                                <span className="text-text-tertiary shrink-0 font-mono text-xs">
                                                    {t.address}
                                                </span>
                                            )}
                                            <span className="bg-surface-3 text-text-tertiary shrink-0 rounded px-1.5 font-mono text-[11px]">
                                                {protoLabel(t.proto)}
                                            </span>
                                            <span
                                                className={cn(
                                                    "ml-auto shrink-0 tabular-nums",
                                                    t.ok
                                                        ? "text-text-secondary"
                                                        : "text-status-danger",
                                                )}
                                            >
                                                {t.ok ? `${t.rtt_ms} ms` : "no answer"}
                                            </span>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </Block>

                        <Block
                            title="Last 15 minutes"
                            action={
                                <span className="text-text-tertiary text-xs tabular-nums">
                                    {up.loss_pct}% failed
                                    {up.median_rtt_ms > 0 && ` · ${up.median_rtt_ms} ms`}
                                </span>
                            }
                        >
                            {up.history.length < 2 ? (
                                <p className="text-text-tertiary text-xs">Not enough samples yet.</p>
                            ) : (
                                <RttSparkline history={up.history} className="h-16 w-full" />
                            )}
                        </Block>

                        {domestic ? (
                            <Block title="Backup order">
                                <div className="space-y-1.5">
                                    {group.map((m) => (
                                        <div
                                            key={m.if_name}
                                            data-member={m.if_name}
                                            className={cn(
                                                "border-border flex items-center gap-2.5 rounded-md border p-2.5",
                                                m.if_name === up.if_name &&
                                                    "border-border-strong bg-surface-2",
                                            )}
                                        >
                                            <span className="bg-surface-3 text-text-secondary flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full font-mono text-[11px]">
                                                {slotPosition(m.slot)}
                                            </span>
                                            <div className="min-w-0">
                                                <p className="truncate text-[13px] font-medium">
                                                    {labelOf(m.if_name)}
                                                    {m.if_name === up.if_name && (
                                                        <span className="text-text-tertiary font-normal">
                                                            {" "}
                                                            · this line
                                                        </span>
                                                    )}
                                                </p>
                                                <p className="text-text-tertiary text-xs">
                                                    {memberRole(up, m, labelOf(m.via))}
                                                </p>
                                            </div>
                                            <div className="ml-auto shrink-0">
                                                <VerdictPill verdict={m.verdict} />
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </Block>
                        ) : (
                            <Block
                                title="VPN tunnels on this line"
                                note="Tunnels move to another foreign line if this one fails."
                            >
                                {(vpn?.tunnels ?? []).length === 0 ? (
                                    <p className="text-text-tertiary text-xs">
                                        No tunnels configured.
                                    </p>
                                ) : (
                                    <div className="space-y-1.5">
                                        {vpn!.tunnels.map((t) => (
                                            <div
                                                key={t.if_name}
                                                data-tunnel={t.if_name}
                                                className="border-border flex items-center gap-2.5 rounded-md border p-2.5"
                                            >
                                                <span
                                                    aria-hidden
                                                    className={cn(
                                                        "h-2 w-2 shrink-0 rounded-full",
                                                        t.verdict === "up" || t.verdict === "degraded"
                                                            ? "bg-status-success"
                                                            : "bg-status-danger",
                                                    )}
                                                />
                                                <div className="min-w-0">
                                                    <p className="truncate text-[13px] font-medium">
                                                        {t.name}
                                                    </p>
                                                    <p className="text-text-tertiary text-xs">
                                                        {t.in_pool ? "Carrying traffic" : "Standby"}
                                                    </p>
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </Block>
                        )}

                        <Block title="Address" note="Both addresses are assigned automatically.">
                            <AddressPath up={up} address={primary} />
                            {extra.length > 0 && (
                                <div className="space-y-1">
                                    {extra.map((a) => (
                                        <CopyableText
                                            key={a}
                                            text={a}
                                            className="text-text-tertiary font-mono text-xs"
                                        />
                                    ))}
                                </div>
                            )}
                        </Block>

                        <Block title="Connection">
                            <Row
                                k="Plugged into"
                                v={iface ? attachmentLabel(iface.source) : "unknown"}
                            />
                            <Row
                                k="Speed"
                                v={
                                    hasSpeed(iface?.speed_mbit) ? (
                                        <span className="tabular-nums">
                                            {speedLabel(iface!.speed_mbit)}
                                        </span>
                                    ) : (
                                        "not reported"
                                    )
                                }
                            />
                        </Block>

                        <Block title="Data used" note="Since the last restart.">
                            <div className="flex gap-6">
                                <div>
                                    <p className="text-text-tertiary text-[11px]">downloaded</p>
                                    <p className="text-lg font-medium tabular-nums">
                                        {bytesLabel(up.rx_bytes)}
                                    </p>
                                </div>
                                <div>
                                    <p className="text-text-tertiary text-[11px]">uploaded</p>
                                    <p className="text-lg font-medium tabular-nums">
                                        {bytesLabel(up.tx_bytes)}
                                    </p>
                                </div>
                            </div>
                        </Block>

                        <Block
                            title="Reachable from outside"
                            action={
                                portmap.data?.enabled ? (
                                    <span className="text-text-tertiary text-xs">
                                        <RefreshCw className="mr-1 inline h-3 w-3" />
                                        checked every 30 s
                                    </span>
                                ) : undefined
                            }
                        >
                            {!portmap.data?.enabled ? (
                                <p className="text-text-tertiary text-xs">
                                    Port mapping is turned off, so nothing is asked of the upstream
                                    router.
                                </p>
                            ) : !pm ? (
                                <p className="text-text-tertiary text-xs">Not checked yet.</p>
                            ) : (
                                <>
                                    <div className="flex flex-wrap items-center gap-2 text-[13px]">
                                        <ReachPill verdict={pm.verdict} suspended={pm.suspended} />
                                        {pm.external_ip && (
                                            <span className="text-text-secondary">
                                                public address{" "}
                                                <span className="font-mono">{pm.external_ip}</span>
                                            </span>
                                        )}
                                    </div>
                                    {pm.leases.length > 0 && (
                                        <div className="divide-border-subtle divide-y">
                                            {pm.leases.map((l) => (
                                                <Row
                                                    key={`${l.proto}-${l.internal_port}`}
                                                    k={`Port ${l.internal_port}`}
                                                    v={
                                                        <span className="text-text-secondary text-xs">
                                                            opened on the upstream router · renews
                                                            itself
                                                        </span>
                                                    }
                                                />
                                            ))}
                                        </div>
                                    )}
                                </>
                            )}
                        </Block>

                        <Block title="Port forwards">
                            {mine.length === 0 ? (
                                <p className="text-text-tertiary text-xs">
                                    None set up for {lineLabel}.
                                </p>
                            ) : (
                                <div className="divide-border-subtle divide-y">
                                    {mine.map((f) => (
                                        <Row
                                            key={f.id}
                                            k={`Port ${f.dport}`}
                                            v={
                                                <span className="text-xs">
                                                    {f.comment || "forwarded"}{" "}
                                                    <span className="text-text-tertiary font-mono">
                                                        {f.to_addr}
                                                    </span>
                                                </span>
                                            }
                                        />
                                    ))}
                                </div>
                            )}
                        </Block>

                        <Block title="Recent events">
                            {mineEvents.length === 0 ? (
                                <p className="text-text-tertiary text-xs">
                                    Nothing recorded for this line yet.
                                </p>
                            ) : (
                                <div className="divide-border-subtle divide-y">
                                    {mineEvents.map((e, i) => (
                                        <div
                                            key={`${e.type}-${e.timestamp}-${i}`}
                                            className="flex items-baseline gap-2.5 py-1.5 text-[13px]"
                                        >
                                            <span className="text-text-tertiary shrink-0 font-mono text-[11px]">
                                                {shortTime(e.timestamp)}
                                            </span>
                                            <span className="text-text-secondary">
                                                {eventSentence(e.type)}
                                            </span>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </Block>

                        <section className="border-border-subtle border-t pt-3.5">
                            <button
                                type="button"
                                aria-expanded={techOpen}
                                onClick={() => setTechOpen((v) => !v)}
                                className="border-border flex w-full items-center justify-between gap-2 rounded-md border px-3 py-2.5 text-[13px]"
                            >
                                <span>Technical details</span>
                                <ChevronDown
                                    className={cn(
                                        "text-text-tertiary h-3.5 w-3.5 transition-transform duration-150",
                                        techOpen && "rotate-180",
                                    )}
                                />
                            </button>
                            {techOpen && (
                                <div className="border-border space-y-0 rounded-b-md border border-t-0 px-3 pt-1 pb-2">
                                    <Row
                                        k="Interface"
                                        v={<span className="font-mono text-xs">{up.if_name}</span>}
                                    />
                                    <Row k="Slot" v={<span className="font-mono text-xs">{up.slot}</span>} />
                                    {iface?.perm_mac && (
                                        <Row
                                            k="MAC"
                                            v={
                                                <span className="font-mono text-xs">
                                                    {iface.perm_mac}
                                                </span>
                                            }
                                        />
                                    )}
                                    {iface?.driver && (
                                        <Row
                                            k="Driver"
                                            v={
                                                <span className="font-mono text-xs">
                                                    {iface.driver}
                                                </span>
                                            }
                                        />
                                    )}
                                    {iface?.mtu ? (
                                        <Row
                                            k="MTU"
                                            v={<span className="tabular-nums">{iface.mtu}</span>}
                                        />
                                    ) : null}
                                    {up.routes && up.routes.length > 0 && (
                                        <div className="pt-2">
                                            <p className="text-text-tertiary mb-1 text-xs">Routes</p>
                                            <pre className="bg-surface-2 overflow-x-auto rounded-md p-2 font-mono text-xs break-all whitespace-pre-wrap select-all">
                                                {up.routes.join("\n")}
                                            </pre>
                                        </div>
                                    )}
                                </div>
                            )}
                        </section>
                    </div>
                </SheetContent>
            </Sheet>

            <ConnectionChecksDialog
                open={checksOpen}
                onOpenChange={setChecksOpen}
                slot={up.slot}
                lineLabel={lineLabel}
                siblings={checkSiblings}
            />
        </>
    )
}

const VERDICT_WORD: Record<string, string> = {
    "up": "online",
    "degraded": "slow",
    "no-internet": "no internet",
    "no-gateway": "no gateway",
    "no-carrier": "no carrier",
    "forced-up": "forced up",
    "forced-down": "forced down",
    "": "waiting",
}

function VerdictPill({ verdict }: { verdict: string }) {
    const tone =
        verdict === "up"
            ? "bg-status-success-soft text-status-success"
            : verdict === "degraded" || verdict === "forced-up"
              ? "bg-status-warning-soft text-status-warning"
              : verdict === ""
                ? "bg-surface-3 text-text-tertiary"
                : "bg-status-danger-soft text-status-danger"
    return (
        <span
            data-verdict={verdict}
            className={cn("rounded-full px-2.5 py-0.5 text-xs font-medium whitespace-nowrap", tone)}
        >
            {VERDICT_WORD[verdict] ?? verdict}
        </span>
    )
}

function ReachPill({ verdict, suspended }: { verdict: string; suspended: boolean }) {
    if (suspended) {
        return (
            <span className="bg-surface-3 text-text-tertiary rounded-full px-2.5 py-0.5 text-xs font-medium">
                paused
            </span>
        )
    }
    const yes = verdict === "ok" || verdict === "public_direct"
    const word = yes ? "yes" : verdict === "nested_nat" ? "no" : verdict.replace(/_/g, " ")
    return (
        <span
            className={cn(
                "rounded-full px-2.5 py-0.5 text-xs font-medium",
                yes
                    ? "bg-status-success-soft text-status-success"
                    : "bg-status-warning-soft text-status-warning",
            )}
        >
            {word}
        </span>
    )
}

/** Event types as outcomes, because a type name is not a sentence. */
const EVENT_WORD: Record<string, string> = {
    "wan.up": "Back to normal",
    "wan.down": "Lost internet",
    "wan.failover": "Switched to another line",
    "wan.failover_restored": "Back on its own line",
    "wan.failover_lost": "Nothing left to carry it",
    "wan.degraded": "Connection quality changed",
    "wan.force_state": "Override changed",
    "wan.gateway_changed": "Upstream router changed",
    "portmap.acquired": "A port was opened upstream",
    "portmap.lost": "A port closed upstream",
    "portmap.denied": "The upstream router refused",
}

function eventSentence(type: string): string {
    return EVENT_WORD[type] ?? type
}

function shortTime(ts: string): string {
    const d = new Date(ts)
    if (Number.isNaN(d.getTime())) return ""
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}
