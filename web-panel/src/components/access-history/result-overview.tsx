import { useEffect, useMemo, useState, type ReactNode } from "react"
import { AlertCircle, Search } from "lucide-react"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useSubscriptionAccessHistory } from "@/lib/queries"
import { useChartPalette } from "@/lib/design/palette"
import { cn } from "@/lib/utils"
import type { AccessHistoryDestinationSplit, AccessHistoryDomainCount, AccessHistoryResponse } from "@/lib/types"
import { FieldLabel, Segmented } from "./bits"
import { HitsChart, RejectionStrip, type HourPoint } from "./charts"
import { ProfileSummary } from "./summary-card"
import { fillBuckets, fmtCompact, fmtInt, fmtShare, isIP, tzName } from "./format"

interface Props {
    subscriptionId: number
    label: string
    from: Date
    to: Date
    nodeIDs?: number[]
    includeIPs: boolean
    nodeNames: Map<number, string>
    actions: ReactNode
    onSearchValue: (value: string) => void
    onRetention?: (days: number) => void
}

/** Older hubs send only the mixed top list; split it here so the page still works. */
function splitOf(split: AccessHistoryDestinationSplit | undefined, mixed: AccessHistoryDomainCount[] | null): AccessHistoryDestinationSplit {
    if (split) return split
    const list = mixed ?? []
    const domains = list.filter(d => !isIP(d.domain))
    const ips = list.filter(d => isIP(d.domain))
    return { domains, ips, domain_count: domains.length, ip_count: ips.length }
}

export function ResultOverview({ subscriptionId, label, from, to, nodeIDs, includeIPs, nodeNames, actions, onSearchValue, onRetention }: Props) {
    const params = useMemo(() => ({
        from: from.toISOString(),
        to: to.toISOString(),
        node_ids: nodeIDs && nodeIDs.length > 0 ? nodeIDs : undefined,
        include_ips: includeIPs,
        top_n: 20,
    }), [from, to, nodeIDs, includeIPs])

    const { data, isLoading, isError, error } = useSubscriptionAccessHistory(subscriptionId, params, true)
    useEffect(() => {
        if (data?.retention_days) onRetention?.(data.retention_days)
    }, [data?.retention_days, onRetention])

    if (isLoading && !data) {
        return (
            <div className="space-y-4">
                <Skeleton className="h-[150px] rounded-xl" />
                <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
                    {[0, 1, 2, 3].map(i => <Skeleton key={i} className="h-[104px] rounded-xl" />)}
                </div>
                <Skeleton className="h-[320px] rounded-xl" />
            </div>
        )
    }
    if (isError) {
        return (
            <Card className="flex-row items-center gap-2.5 p-5 text-sm text-red-500 dark:text-red-400">
                <AlertCircle className="size-4 shrink-0" />
                <span>Couldn't load this subscription's history: {error instanceof Error ? error.message : String(error)}</span>
            </Card>
        )
    }
    if (!data) return null
    return <Profile data={data} {...{ subscriptionId, label, from, to, includeIPs, nodeNames, actions, onSearchValue }} />
}

function Profile({
    data, subscriptionId, label, from, to, includeIPs, nodeNames, actions, onSearchValue,
}: Omit<Props, "nodeIDs" | "onRetention"> & { data: AccessHistoryResponse }) {
    const c = useChartPalette()
    const totals = data.totals
    const windowHours = Math.max(1, Math.round((to.getTime() - from.getTime()) / 3_600_000))
    const daily = data.granularity === "day"
    // The hub leaves out empty buckets; fill them so idle time reads as zero.
    const points: HourPoint[] = useMemo(
        () => fillBuckets(from, to, daily ? "day" : "hour", (data.series ?? []).map(b => ({ bucket: b.bucket, v: b })), () => null)
            .map(({ t, v }) => ({ t, accepted: v?.accepted_count ?? 0, rejected: v?.rejected_count ?? 0 })),
        [data.series, from, to, daily],
    )
    const firstSeen = totals.first_seen ?? data.series?.find(b => b.accepted_count + b.rejected_count > 0)?.bucket
    const lastSeen = totals.last_seen ?? [...(data.series ?? [])].reverse().find(b => b.accepted_count + b.rejected_count > 0)?.bucket
    const activeHours = totals.active_hours ?? (daily ? undefined : points.filter(p => p.accepted + p.rejected > 0).length)
    const nodes = (data.nodes_queried ?? []).map(id => nodeNames.get(id) ?? `Node ${id}`).join(", ")
    const requests = totals.accepted_count + totals.rejected_count
    const traffic = totals.tcp_count + totals.udp_count

    const summary = (
        <ProfileSummary
            subId={subscriptionId}
            label={label}
            from={from}
            to={to}
            activeHours={activeHours}
            windowHours={windowHours}
            emails={data.emails_resolved}
            sourceIps={includeIPs ? data.source_ip_count ?? data.top_source_ips?.length : undefined}
            firstSeen={firstSeen}
            lastSeen={lastSeen}
            nodes={nodes}
            actions={actions}
        />
    )

    if (totals.hour_buckets === 0) {
        return (
            <div className="space-y-4">
                {summary}
                <Card className="items-center gap-1.5 p-10 text-center">
                    <p className="text-sm font-medium">No activity in this window</p>
                    <p className="text-[13px] text-muted-foreground">Try a wider range, or check that the subscription's nodes have access logs on.</p>
                </Card>
            </div>
        )
    }

    const tiles = [
        { label: "Accepted", value: totals.accepted_count, color: "text-emerald-600 dark:text-emerald-400", sub: `${fmtShare(totals.accepted_count, requests)} of requests` },
        { label: "Rejected", value: totals.rejected_count, color: "text-red-600 dark:text-red-400", sub: `${fmtShare(totals.rejected_count, requests)} of requests` },
        { label: "TCP", value: totals.tcp_count, color: "text-sky-600 dark:text-sky-400", sub: `${fmtShare(totals.tcp_count, traffic)} of traffic` },
        { label: "UDP", value: totals.udp_count, color: "text-amber-600 dark:text-amber-400", sub: `${fmtShare(totals.udp_count, traffic)} of traffic` },
    ]

    return (
        <div className="space-y-4">
            {summary}

            <div className="grid grid-cols-2 gap-3 md:grid-cols-4 md:gap-4">
                {tiles.map(t => (
                    <Card key={t.label} className="gap-1.5 px-4 py-4 transition-shadow duration-300 hover:shadow-lg md:px-5">
                        <FieldLabel>{t.label}</FieldLabel>
                        <span className={cn("font-mono text-2xl font-bold tracking-tight tabular-nums md:text-3xl", t.color)} title={fmtInt(t.value)}>{fmtCompact(t.value)}</span>
                        <span className="text-[13px] text-muted-foreground">{t.sub}</span>
                    </Card>
                ))}
            </div>

            <Card className="gap-3.5 p-4 sm:p-5">
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <div className="flex flex-wrap items-center gap-4">
                        <FieldLabel as="h3">Requests over time</FieldLabel>
                        <LegendDot color={c.success} label="Accepted" />
                        <LegendDot color={c.danger} label="Rejected" />
                    </div>
                    <span className="text-[13px] text-muted-foreground">{daily ? "Daily" : "Hourly"}, {tzName()}</span>
                </div>
                <HitsChart
                    points={points}
                    height={220}
                    bucketLabel={daily ? (t => new Date(t).toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" })) : undefined}
                />
                {totals.rejected_count > 0 && <RejectionStrip points={points} total={totals.rejected_count} />}
            </Card>

            <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
                <DestinationsCard
                    title="Top destinations"
                    split={splitOf(data.destinations, data.top_domains)}
                    countClass="text-emerald-600 dark:text-emerald-400"
                    label={label}
                    onSearchValue={onSearchValue}
                />
                <DestinationsCard
                    title="Rejected destinations"
                    subtitle="Connections the node blocked."
                    split={splitOf(data.rejected_destinations, data.top_rejected)}
                    countClass="text-red-600 dark:text-red-400"
                    label={label}
                    onSearchValue={onSearchValue}
                />
            </div>

            {includeIPs && (data.top_source_ips?.length ?? 0) > 0 && (
                <Card className="gap-0 overflow-hidden py-0">
                    <div className="flex items-center justify-between px-5 pb-3 pt-4">
                        <FieldLabel as="h3">Connected from</FieldLabel>
                        <span className="text-[13px] text-muted-foreground">
                            {fmtInt(data.source_ip_count ?? data.top_source_ips!.length)} source IPs, top {data.top_source_ips!.length}
                        </span>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 md:gap-x-px">
                        {data.top_source_ips!.map(r => (
                            <div key={r.ip} className="grid h-9 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-2.5 border-t border-border/50 px-5 transition-colors hover:bg-muted/30">
                                <span className="font-mono text-[13px]">{r.ip}</span>
                                <span className="text-right font-mono text-sm font-semibold tabular-nums">{fmtInt(r.count)}</span>
                            </div>
                        ))}
                    </div>
                </Card>
            )}
        </div>
    )
}

function DestinationsCard({
    title, subtitle, split, countClass, label, onSearchValue,
}: {
    title: string
    subtitle?: string
    split: AccessHistoryDestinationSplit
    countClass: string
    label: string
    onSearchValue: (value: string) => void
}) {
    const [kind, setKind] = useState<"domains" | "ips">(split.domain_count > 0 || split.ip_count === 0 ? "domains" : "ips")
    const rows = (kind === "domains" ? split.domains : split.ips) ?? []
    return (
        <Card className="gap-0 overflow-hidden py-0">
            <div className={cn("flex items-center justify-between gap-3 px-5 pt-4", subtitle ? "pb-1" : "pb-3")}>
                <FieldLabel as="h3">{title}</FieldLabel>
                <Segmented
                    ariaLabel={`${title} type`}
                    value={kind}
                    onChange={setKind}
                    options={[
                        { value: "domains", label: <>Domains <span className="font-mono text-muted-foreground">{fmtInt(split.domain_count)}</span></> },
                        { value: "ips", label: <>IPs <span className="font-mono text-muted-foreground">{fmtInt(split.ip_count)}</span></> },
                    ]}
                />
            </div>
            {subtitle && <p className="px-5 pb-3 text-[13px] text-muted-foreground">{subtitle}</p>}
            {rows.length === 0 ? (
                <p className="border-t border-border/50 px-5 py-4 text-sm text-muted-foreground">None in this window.</p>
            ) : (
                <div className="max-h-[364px] overflow-y-auto">
                    {rows.map(r => (
                        <button
                            key={r.domain}
                            type="button"
                            title={`Search for ${r.domain} within ${label}`}
                            onClick={() => onSearchValue(r.domain)}
                            className="group grid h-9 w-full grid-cols-[minmax(0,1fr)_auto_18px] items-center gap-x-2.5 border-t border-border/50 px-5 text-left transition-colors hover:bg-muted/30"
                        >
                            <span className="truncate font-mono text-[13px]">{r.domain}</span>
                            <span className={cn("font-mono text-sm font-semibold tabular-nums", countClass)}>{fmtInt(r.count)}</span>
                            <Search className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                        </button>
                    ))}
                </div>
            )}
        </Card>
    )
}

function LegendDot({ color, label }: { color: string; label: string }) {
    return (
        <span className="inline-flex items-center gap-1.5 text-[13px] text-muted-foreground">
            <span className="h-[3px] w-2.5 rounded-full" style={{ background: color }} />
            {label}
        </span>
    )
}
