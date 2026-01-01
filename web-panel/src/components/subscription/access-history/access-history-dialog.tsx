import { useEffect, useMemo, useState } from "react"
import type { DateRange } from "react-day-picker"
import {
    Area,
    AreaChart,
    CartesianGrid,
    ResponsiveContainer,
    Tooltip,
    XAxis,
    YAxis,
} from "recharts"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import { HiOutlineRefresh } from "react-icons/hi"
import { AlertCircle, Download, Eye, EyeOff } from "lucide-react"
import { useSubscriptionAccessHistory } from "@/lib/queries"
import { useChartPalette, tooltipContentStyle } from "@/lib/design/palette"
import { DateRangePicker } from "./date-range-picker"
import { AccessHistorySearchPanel } from "./search-panel"
import { cn } from "@/lib/utils"
import type { AccessHistoryGranularity } from "@/lib/types"

interface AccessHistoryDialogProps {
    subId: number | undefined
    open: boolean
    onOpenChange: (open: boolean) => void
}

type RangePreset = "24h" | "7d" | "14d" | "30d" | "custom"

const PRESETS: { key: RangePreset; label: string; ms: number | null }[] = [
    { key: "24h", label: "24h", ms: 24 * 60 * 60 * 1000 },
    { key: "7d", label: "7d", ms: 7 * 24 * 60 * 60 * 1000 },
    { key: "14d", label: "14d", ms: 14 * 24 * 60 * 60 * 1000 },
    { key: "30d", label: "30d", ms: 30 * 24 * 60 * 60 * 1000 },
    { key: "custom", label: "Custom", ms: null },
]

function startOfDay(d: Date): Date {
    const x = new Date(d)
    x.setHours(0, 0, 0, 0)
    return x
}

function endOfDay(d: Date): Date {
    const x = new Date(d)
    x.setHours(23, 59, 59, 999)
    return x
}

function formatBucket(iso: string, granularity: AccessHistoryGranularity): string {
    const d = new Date(iso)
    if (granularity === "day") {
        return d.toLocaleDateString([], { month: "short", day: "numeric" })
    }
    return d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
}

function formatLargeNumber(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
    return n.toLocaleString()
}

export function AccessHistoryDialog({ subId, open, onOpenChange }: AccessHistoryDialogProps) {
    const [preset, setPreset] = useState<RangePreset>("24h")
    const [customRange, setCustomRange] = useState<DateRange | undefined>(undefined)
    const [granularity, setGranularity] = useState<AccessHistoryGranularity | "auto">("auto")
    const [includeIPs, setIncludeIPs] = useState(false)

    // Resolve the active window from preset / custom inputs.
    const window = useMemo(() => {
        const now = new Date()
        if (preset !== "custom") {
            const ms = PRESETS.find(p => p.key === preset)?.ms ?? 0
            return { from: new Date(now.getTime() - ms), to: now }
        }
        if (customRange?.from && customRange?.to) {
            return { from: startOfDay(customRange.from), to: endOfDay(customRange.to) }
        }
        return null
    }, [preset, customRange])

    const params = useMemo(() => {
        if (!window) return null
        return {
            from: window.from.toISOString(),
            to: window.to.toISOString(),
            granularity: granularity === "auto" ? undefined : granularity,
            include_ips: includeIPs,
            top_n: 20,
        }
    }, [window, granularity, includeIPs])

    const { data, isLoading, isFetching, isError, error, refetch } = useSubscriptionAccessHistory(
        subId,
        params ?? { from: "", to: "" },
        open && !!params,
    )

    // Reset granularity to auto when preset changes — explicit override only sticks
    // for the lifetime of the chosen window.
    useEffect(() => {
        setGranularity("auto")
    }, [preset, customRange])

    const series = data?.series ?? []
    const topDomains = data?.top_domains ?? []
    const topRejected = data?.top_rejected ?? []
    const topIPs = data?.top_source_ips ?? []
    const totals = data?.totals
    const resolvedGranularity = data?.granularity ?? "hour"
    const retentionDays = data?.retention_days ?? 30

    const onExport = () => {
        if (!data || !subId) return
        downloadAccessHistoryCSV(data, subId)
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-4xl p-0 gap-0 max-h-[92vh] overflow-hidden flex flex-col">
                <DialogHeader className="px-5 pt-4 pb-3 border-b shrink-0">
                    <DialogTitle className="text-sm font-semibold">Access History</DialogTitle>
                    <DialogDescription className="text-xs text-muted-foreground">
                        Hourly summaries persisted on the hub. Retained for {retentionDays} day{retentionDays === 1 ? "" : "s"}.
                    </DialogDescription>
                </DialogHeader>

                <div className="flex-1 min-h-0 overflow-y-auto">
                    {/* Toolbar */}
                    <div className="px-5 py-3 border-b bg-muted/20 flex flex-wrap items-center gap-2">
                        <div className="flex items-center gap-1 bg-background rounded-md p-0.5 ring-1 ring-border">
                            {PRESETS.map(p => (
                                <button
                                    key={p.key}
                                    type="button"
                                    onClick={() => setPreset(p.key)}
                                    aria-pressed={preset === p.key}
                                    className={cn(
                                        "px-2.5 py-1 rounded text-[11px] font-bold transition-colors",
                                        preset === p.key ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground"
                                    )}
                                >
                                    {p.label}
                                </button>
                            ))}
                        </div>

                        {preset === "custom" && (
                            <DateRangePicker
                                value={customRange}
                                onChange={setCustomRange}
                                maxDate={new Date()}
                                minDate={new Date(Date.now() - retentionDays * 24 * 60 * 60 * 1000)}
                            />
                        )}

                        <Select value={granularity} onValueChange={v => setGranularity(v as AccessHistoryGranularity | "auto")}>
                            <SelectTrigger className="h-8 w-[100px] text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="auto">Auto</SelectItem>
                                <SelectItem value="hour">Hourly</SelectItem>
                                <SelectItem value="day">Daily</SelectItem>
                            </SelectContent>
                        </Select>

                        <Button
                            variant="outline"
                            size="sm"
                            className="h-8 gap-1.5 text-xs"
                            onClick={() => setIncludeIPs(v => !v)}
                            aria-pressed={includeIPs}
                        >
                            {includeIPs ? <Eye className="w-3.5 h-3.5" /> : <EyeOff className="w-3.5 h-3.5" />}
                            Source IPs
                        </Button>

                        <div className="grow" />

                        <Button
                            variant="outline"
                            size="sm"
                            className="h-8 gap-1.5 text-xs"
                            onClick={onExport}
                            disabled={!data}
                            title="Download CSV"
                        >
                            <Download className="w-3.5 h-3.5" />
                            CSV
                        </Button>

                        <Button
                            variant="outline"
                            size="sm"
                            className="h-8"
                            onClick={() => refetch()}
                            disabled={isFetching}
                        >
                            <HiOutlineRefresh className={cn("w-3.5 h-3.5", isFetching && "animate-spin")} />
                        </Button>
                    </div>

                    {/* Body */}
                    <div className="px-5 py-4 space-y-4">
                        {!params ? (
                            <EmptyMessage text="Select a custom date range to load history." />
                        ) : isError ? (
                            <ErrorState message={error instanceof Error ? error.message : "Failed to load access history"} onRetry={() => refetch()} />
                        ) : isLoading ? (
                            <LoadingState />
                        ) : !data || (totals && totals.hour_buckets === 0) ? (
                            <EmptyMessage text="No activity in the selected range." />
                        ) : (
                            <>
                                <AccessHistorySearchPanel
                                    subId={subId}
                                    from={params!.from}
                                    to={params!.to}
                                    includeIPs={includeIPs}
                                />
                                <TotalsRow totals={totals!} />
                                <ChartCard series={series} granularity={resolvedGranularity} />
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <DomainTable title="Top Domains" rows={topDomains} accent="emerald" />
                                    <DomainTable title="Rejected Domains" rows={topRejected} accent="red" />
                                </div>
                                {includeIPs && (
                                    <IPTable rows={topIPs} />
                                )}
                            </>
                        )}
                    </div>
                </div>
            </DialogContent>
        </Dialog>
    )
}

// ─── Sub-components ─────────────────────────────────────────────────

function TotalsRow({ totals }: { totals: NonNullable<ReturnType<typeof useSubscriptionAccessHistory>["data"]>["totals"] }) {
    const items = [
        { label: "Accepted", value: totals.accepted_count, color: "text-emerald-400" },
        { label: "Rejected", value: totals.rejected_count, color: "text-red-400" },
        { label: "TCP", value: totals.tcp_count, color: "text-blue-400" },
        { label: "UDP", value: totals.udp_count, color: "text-amber-400" },
    ]
    return (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            {items.map(it => (
                <Card key={it.label} className="p-3">
                    <p className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-bold">{it.label}</p>
                    <p className={cn("text-xl font-bold tabular-nums mt-0.5", it.color)}>{formatLargeNumber(it.value)}</p>
                </Card>
            ))}
        </div>
    )
}

function ChartCard({
    series,
    granularity,
}: {
    series: NonNullable<ReturnType<typeof useSubscriptionAccessHistory>["data"]>["series"]
    granularity: AccessHistoryGranularity
}) {
    const c = useChartPalette()
    const data = (series ?? []).map(b => ({
        time: formatBucket(b.bucket, granularity),
        accepted: b.accepted_count,
        rejected: b.rejected_count,
    }))
    return (
        <Card className="p-4">
            <p className="text-xs uppercase font-bold text-muted-foreground/70 tracking-wider mb-3">
                Requests over time · {granularity === "day" ? "daily" : "hourly"}
            </p>
            <div className="h-[260px] w-full">
                <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={data} margin={{ top: 5, right: 5, left: 0, bottom: 5 }}>
                        <defs>
                            <linearGradient id="ah-acc" x1="0" y1="0" x2="0" y2="1">
                                <stop offset="0%" stopColor={c.success} stopOpacity={0.4} />
                                <stop offset="100%" stopColor={c.success} stopOpacity={0} />
                            </linearGradient>
                            <linearGradient id="ah-rej" x1="0" y1="0" x2="0" y2="1">
                                <stop offset="0%" stopColor={c.danger} stopOpacity={0.35} />
                                <stop offset="100%" stopColor={c.danger} stopOpacity={0} />
                            </linearGradient>
                        </defs>
                        <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                        <XAxis dataKey="time" tick={{ fontSize: 10, fill: c.axis }} axisLine={false} tickLine={false} interval={Math.max(Math.floor(data.length / 8), 0)} />
                        <YAxis tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} orientation="right" tickFormatter={formatLargeNumber} width={50} />
                        <Tooltip
                            contentStyle={{ ...tooltipContentStyle(c), fontSize: 12 }}
                            labelStyle={{ color: c.tooltipLabel, fontSize: 11 }}
                        />
                        <Area type="monotone" dataKey="accepted" stroke={c.success} strokeWidth={2} fill="url(#ah-acc)" dot={false} />
                        <Area type="monotone" dataKey="rejected" stroke={c.danger} strokeWidth={2} fill="url(#ah-rej)" dot={false} />
                    </AreaChart>
                </ResponsiveContainer>
            </div>
        </Card>
    )
}

function DomainTable({
    title,
    rows,
    accent,
}: {
    title: string
    rows: { domain: string; count: number }[]
    accent: "emerald" | "red"
}) {
    if (!rows || rows.length === 0) {
        return (
            <Card className="p-4">
                <p className="text-xs uppercase font-bold text-muted-foreground/70 tracking-wider mb-2">{title}</p>
                <p className="text-xs text-muted-foreground">None</p>
            </Card>
        )
    }
    const accentClass = accent === "emerald" ? "text-emerald-400" : "text-red-400"
    return (
        <Card className="p-0 overflow-hidden">
            <div className="px-4 pt-3 pb-2">
                <p className="text-xs uppercase font-bold text-muted-foreground/70 tracking-wider">{title}</p>
            </div>
            <div className="max-h-[260px] overflow-y-auto">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Domain</TableHead>
                            <TableHead className="text-right">Hits</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map(r => (
                            <TableRow key={r.domain}>
                                <TableCell className="font-mono text-xs truncate max-w-[260px]">{r.domain}</TableCell>
                                <TableCell className={cn("text-right font-mono font-semibold tabular-nums", accentClass)}>
                                    {formatLargeNumber(r.count)}
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </Card>
    )
}

function IPTable({ rows }: { rows: { ip: string; count: number }[] }) {
    if (!rows || rows.length === 0) {
        return (
            <Card className="p-4">
                <p className="text-xs uppercase font-bold text-muted-foreground/70 tracking-wider mb-2">Source IPs</p>
                <p className="text-xs text-muted-foreground">No source IPs in window.</p>
            </Card>
        )
    }
    return (
        <Card className="p-0 overflow-hidden">
            <div className="px-4 pt-3 pb-2">
                <p className="text-xs uppercase font-bold text-muted-foreground/70 tracking-wider">Top Source IPs</p>
            </div>
            <div className="max-h-[260px] overflow-y-auto">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>IP</TableHead>
                            <TableHead className="text-right">Connections</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map(r => (
                            <TableRow key={r.ip}>
                                <TableCell className="font-mono text-xs">{r.ip}</TableCell>
                                <TableCell className="text-right font-mono font-semibold tabular-nums">{formatLargeNumber(r.count)}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </Card>
    )
}

function LoadingState() {
    return (
        <div className="space-y-4">
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                {[...Array(4)].map((_, i) => <Skeleton key={i} className="h-[68px] rounded-md" />)}
            </div>
            <Skeleton className="h-[280px] rounded-md" />
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <Skeleton className="h-[260px] rounded-md" />
                <Skeleton className="h-[260px] rounded-md" />
            </div>
        </div>
    )
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
    return (
        <div className="flex flex-col items-center justify-center py-12 px-4 text-center bg-red-500/5 rounded-md border-2 border-dashed border-red-500/20">
            <AlertCircle className="w-10 h-10 text-red-400/70 mb-3" />
            <p className="text-sm font-medium text-red-300">Failed to load access history</p>
            <p className="text-xs text-muted-foreground mt-1 max-w-md">{message}</p>
            <Button size="sm" variant="outline" className="mt-4 h-8 text-xs" onClick={onRetry}>Retry</Button>
        </div>
    )
}

function EmptyMessage({ text }: { text: string }) {
    return (
        <div className="flex items-center justify-center py-16 text-sm text-muted-foreground border-2 border-dashed border-white/5 rounded-md">
            {text}
        </div>
    )
}

// downloadAccessHistoryCSV: one CSV with sections (time series, top
// domains, rejected, optional source IPs). Client-side — response is
// small and there's no need to round-trip a render-only artefact.
function downloadAccessHistoryCSV(
    data: NonNullable<ReturnType<typeof useSubscriptionAccessHistory>["data"]>,
    subId: number,
): void {
    const rows: string[][] = []
    rows.push(["# Access history", "subscription", String(subId)])
    rows.push(["# from", data.from])
    rows.push(["# to", data.to])
    rows.push(["# granularity", data.granularity])
    rows.push([])
    rows.push(["[series]"])
    rows.push(["bucket", "accepted", "rejected", "tcp", "udp"])
    for (const b of data.series ?? []) {
        rows.push([
            b.bucket,
            String(b.accepted_count),
            String(b.rejected_count),
            String(b.tcp_count),
            String(b.udp_count),
        ])
    }
    rows.push([])
    rows.push(["[top_domains]"])
    rows.push(["domain", "count"])
    for (const d of data.top_domains ?? []) {
        rows.push([d.domain, String(d.count)])
    }
    rows.push([])
    rows.push(["[top_rejected]"])
    rows.push(["domain", "count"])
    for (const d of data.top_rejected ?? []) {
        rows.push([d.domain, String(d.count)])
    }
    if (data.top_source_ips && data.top_source_ips.length > 0) {
        rows.push([])
        rows.push(["[top_source_ips]"])
        rows.push(["ip", "count"])
        for (const r of data.top_source_ips) {
            rows.push([r.ip, String(r.count)])
        }
    }

    const csv = rows.map(r => r.map(csvCell).join(",")).join("\n")
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = `access-history-sub-${subId}-${data.from.slice(0, 10)}-to-${data.to.slice(0, 10)}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
}

function csvCell(v: string): string {
    if (/[",\n]/.test(v)) return `"${v.replace(/"/g, '""')}"`
    return v
}
