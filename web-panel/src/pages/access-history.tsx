import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useSearchParams } from "react-router"
import type { DateRange } from "react-day-picker"
import { Search, Clock, AlertTriangle, Database, RefreshCw } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { PageHeader } from "@/components/shared/page-header"
import { useGlobalAccessHistorySearch } from "@/lib/queries"
import { useNodes } from "@/lib/queries/use-nodes"
import { cn } from "@/lib/utils"
import type { AccessHistorySearchKind } from "@/lib/types"
import { FilterRail, type FilterRailValue, type RangePreset, PRESETS } from "@/components/access-history/filter-rail"
import { ChipBar, type ChipBarValue } from "@/components/access-history/chip-bar"
import { ResultSearch } from "@/components/access-history/result-search"
import { ResultOverview } from "@/components/access-history/result-overview"

function startOfDay(d: Date): Date { const x = new Date(d); x.setHours(0, 0, 0, 0); return x }
function endOfDay(d: Date): Date { const x = new Date(d); x.setHours(23, 59, 59, 999); return x }

function deriveWindow(preset: RangePreset, custom?: DateRange): { from: Date; to: Date } | null {
    const now = new Date()
    if (preset !== "custom") {
        const ms = PRESETS.find(p => p.key === preset)?.ms ?? 0
        return { from: new Date(now.getTime() - ms), to: now }
    }
    if (custom?.from && custom?.to) return { from: startOfDay(custom.from), to: endOfDay(custom.to) }
    return null
}

function formatWindow(w: { from: Date; to: Date } | null): string {
    if (!w) return "—"
    const fmt = (d: Date) => d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
    return `${fmt(w.from)} → ${fmt(w.to)}`
}

function formatAge(d: Date): string {
    const seconds = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000))
    if (seconds < 60) return `${seconds}s ago`
    const minutes = Math.floor(seconds / 60)
    if (minutes < 60) return `${minutes}m ago`
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return `${hours}h ago`
    const days = Math.floor(hours / 24)
    return `${days}d ago`
}

function oldestDate(values: string[]): Date | null {
    if (values.length === 0) return null
    let oldest = new Date(values[0])
    for (let i = 1; i < values.length; i++) {
        const d = new Date(values[i])
        if (d < oldest) oldest = d
    }
    return oldest
}

const DEFAULT_PRESET: RangePreset = "7d"
const DEFAULT_LIMIT = 1000

export default function AccessHistoryPage() {
    const [searchParams, setSearchParams] = useSearchParams()
    const nodes = useNodes().data ?? []
    const searchInputRef = useRef<HTMLInputElement>(null)

    const initialPreset = (searchParams.get("preset") as RangePreset) || DEFAULT_PRESET
    const initialQuery = searchParams.get("q") ?? ""
    const initialKinds = (searchParams.get("kinds")?.split(",").filter(Boolean) as AccessHistorySearchKind[]) ?? ["domain", "rejected_domain"]
    const initialIPs = searchParams.get("ips") === "1"
    const initialLimit = Number(searchParams.get("limit") ?? String(DEFAULT_LIMIT))
    const initialNodeIDs = searchParams.get("node_ids")?.split(",").map(Number).filter(n => Number.isFinite(n) && n > 0) ?? []
    const initialSubIDs = searchParams.get("subscription_ids")?.split(",").map(Number).filter(n => Number.isFinite(n) && n > 0) ?? []
    const initialEmails = searchParams.get("emails")?.split(",").filter(Boolean) ?? []

    const [rail, setRail] = useState<FilterRailValue>({
        preset: initialPreset,
        customRange: undefined,
        subscriptions: initialSubIDs.map(id => ({ id, label: `Sub #${id}` })),
        emails: initialEmails,
        nodeIDs: initialNodeIDs,
    })
    const [chip, setChip] = useState<ChipBarValue>({
        kinds: initialKinds,
        includeIPs: initialIPs,
        limit: Number.isFinite(initialLimit) && initialLimit > 0 ? initialLimit : DEFAULT_LIMIT,
    })
    const [raw, setRaw] = useState(initialQuery)
    const [debounced, setDebounced] = useState(initialQuery)

    useEffect(() => {
        const id = window.setTimeout(() => setDebounced(raw.trim()), 350)
        return () => window.clearTimeout(id)
    }, [raw])

    // "/" focuses the search input from anywhere on the page; Esc clears it.
    useEffect(() => {
        const handler = (e: KeyboardEvent) => {
            const target = e.target as HTMLElement | null
            const inField = target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)
            if (e.key === "/" && !inField) {
                e.preventDefault()
                searchInputRef.current?.focus()
            } else if (e.key === "Escape" && target === searchInputRef.current) {
                setRaw("")
            }
        }
        window.addEventListener("keydown", handler)
        return () => window.removeEventListener("keydown", handler)
    }, [])

    const win = useMemo(() => deriveWindow(rail.preset, rail.customRange), [rail.preset, rail.customRange])

    useEffect(() => {
        const next = new URLSearchParams()
        if (rail.preset !== DEFAULT_PRESET) next.set("preset", rail.preset)
        if (debounced) next.set("q", debounced)
        if (chip.kinds.length > 0 && (chip.kinds.length !== 2 || !(chip.kinds.includes("domain") && chip.kinds.includes("rejected_domain")))) {
            next.set("kinds", chip.kinds.join(","))
        }
        if (chip.includeIPs) next.set("ips", "1")
        if (chip.limit !== DEFAULT_LIMIT) next.set("limit", String(chip.limit))
        if (rail.nodeIDs.length > 0) next.set("node_ids", rail.nodeIDs.join(","))
        if (rail.subscriptions.length > 0) next.set("subscription_ids", rail.subscriptions.map(s => s.id).join(","))
        if (rail.emails.length > 0) next.set("emails", rail.emails.join(","))
        setSearchParams(next, { replace: true })
    }, [rail, chip, debounced, setSearchParams])

    const params = useMemo(() => {
        if (!win) return null
        return {
            from: win.from.toISOString(),
            to: win.to.toISOString(),
            q: debounced,
            kinds: chip.kinds.length > 0 ? chip.kinds : undefined,
            node_ids: rail.nodeIDs.length > 0 ? rail.nodeIDs : undefined,
            subscription_ids: rail.subscriptions.length > 0 ? rail.subscriptions.map(s => s.id) : undefined,
            emails: rail.emails.length > 0 ? rail.emails : undefined,
            include_ips: chip.includeIPs,
            limit: chip.limit,
        }
    }, [win, debounced, chip, rail])

    const hasQuery = debounced.length >= 2
    const enableGlobal = !!params && hasQuery

    const { data, isFetching, isError, error } = useGlobalAccessHistorySearch(
        params ?? { from: "", to: "", q: "" },
        enableGlobal,
    )

    const clearAll = useCallback(() => {
        setRail({ preset: DEFAULT_PRESET, customRange: undefined, subscriptions: [], emails: [], nodeIDs: [] })
        setChip({ kinds: ["domain", "rejected_domain"], includeIPs: false, limit: DEFAULT_LIMIT })
        setRaw("")
    }, [])

    const overviewMode = rail.subscriptions.length === 1 && !hasQuery && !!win
    const retentionDays = data?.retention_days ?? 30
    const oldestSync = useMemo(() => oldestDate(Object.values(data?.last_synced_at ?? {})), [data?.last_synced_at])

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            <PageHeader
                title="Access History"
                description="Forensic search across persisted hourly summaries."
                actions={
                    <div className="flex items-center gap-2">
                        <Badge variant="secondary" className="h-7 gap-1.5 px-2.5 text-xs">
                            <Database className="w-3 h-3 text-muted-foreground" />
                            <span className="font-mono tabular-nums">{retentionDays}d</span>
                            <span className="text-muted-foreground">retained</span>
                        </Badge>
                        {oldestSync && (
                            <Badge
                                variant="secondary"
                                className="h-7 gap-1.5 px-2.5 text-xs"
                                title={oldestSync.toLocaleString()}
                            >
                                <RefreshCw className="w-3 h-3 text-muted-foreground" />
                                <span className="text-muted-foreground">synced</span>
                                <span className="font-mono tabular-nums">{formatAge(oldestSync)}</span>
                            </Badge>
                        )}
                        <Badge variant="outline" className="h-7 gap-1.5 px-2.5 text-xs">
                            <Clock className="w-3 h-3 text-muted-foreground" />
                            <span className="font-mono">{formatWindow(win)}</span>
                        </Badge>
                    </div>
                }
            />

            <div className="grid grid-cols-1 lg:grid-cols-[280px_1fr] gap-4 lg:gap-5">
                <div className="lg:sticky lg:top-4 lg:self-start">
                    <FilterRail value={rail} onChange={setRail} onClearAll={clearAll} retentionDays={retentionDays} />
                </div>

                <div className="space-y-4 min-w-0">
                    <Card className="p-3 md:p-4 space-y-3">
                        <div className="relative">
                            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
                            <Input
                                ref={searchInputRef}
                                value={raw}
                                onChange={e => setRaw(e.target.value)}
                                placeholder="Search domain, IP, or rejected host (min 2 characters)…"
                                className="pl-9 pr-16 h-10 text-sm"
                            />
                            <kbd className="absolute right-3 top-1/2 -translate-y-1/2 pointer-events-none px-1.5 py-0.5 rounded border bg-muted text-[10px] font-mono text-muted-foreground">
                                /
                            </kbd>
                        </div>
                        <ChipBar value={chip} onChange={setChip} />
                    </Card>

                    {!hasQuery && rail.subscriptions.length === 0 && <EmptyHint />}

                    {overviewMode && (
                        <>
                            <ScopeStrip subLabel={rail.subscriptions[0].label} subId={rail.subscriptions[0].id} />
                            <ResultOverview
                                subscriptionId={rail.subscriptions[0].id}
                                from={win!.from.toISOString()}
                                to={win!.to.toISOString()}
                                nodeIDs={rail.nodeIDs.length > 0 ? rail.nodeIDs : undefined}
                                includeIPs={chip.includeIPs}
                            />
                        </>
                    )}

                    {!overviewMode && hasQuery && (
                        <>
                            {isFetching && !data && (
                                <div className="space-y-3 animate-in fade-in duration-300">
                                    <Skeleton className="h-10 w-full rounded-lg" />
                                    <Skeleton className="h-9 w-80 rounded-md" />
                                    <Skeleton className="h-[400px] w-full rounded-lg" />
                                </div>
                            )}
                            {isError && (
                                <Card className="p-4 flex items-center gap-2.5 border-red-500/30 bg-red-500/5">
                                    <AlertTriangle className="w-4 h-4 text-red-400 shrink-0" />
                                    <span className="text-sm text-red-300">Search failed: {error instanceof Error ? error.message : String(error)}</span>
                                </Card>
                            )}
                            {data && (
                                <>
                                    <ResultSummaryBar
                                        hits={data.hits?.length ?? 0}
                                        subs={data.by_subscription?.length ?? 0}
                                        truncated={data.truncated}
                                        onRaiseLimit={() => setChip({ ...chip, limit: Math.min(chip.limit * 2, 2000) })}
                                    />
                                    <ResultSearch
                                        hits={data.hits ?? []}
                                        bySubscription={data.by_subscription ?? []}
                                        byValue={data.by_value ?? []}
                                        nodes={nodes}
                                        onPivotToSubscription={(id, label) => setRail({ ...rail, subscriptions: [{ id, label }] })}
                                    />
                                </>
                            )}
                        </>
                    )}

                    {!overviewMode && !hasQuery && rail.subscriptions.length > 1 && (
                        <Card className="p-10 text-center space-y-2">
                            <p className="text-sm font-medium">Multiple subscriptions selected</p>
                            <p className="text-xs text-muted-foreground">
                                Add a query to search across them, or remove all but one to view its Overview.
                            </p>
                        </Card>
                    )}
                </div>
            </div>
        </div>
    )
}

function EmptyHint() {
    return (
        <Card className="p-10 lg:p-14 flex flex-col items-center justify-center gap-4 text-center">
            <div className="w-14 h-14 rounded-full bg-muted/50 flex items-center justify-center ring-4 ring-muted/20">
                <Search className="w-6 h-6 text-muted-foreground" />
            </div>
            <div className="space-y-1.5 max-w-md">
                <h3 className="text-base font-semibold">Search domain access across all subscriptions</h3>
                <p className="text-sm text-muted-foreground">
                    Type at least 2 characters, or pick a subscription on the left to view its hourly overview.
                </p>
            </div>
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <span>Press</span>
                <kbd className="px-1.5 py-0.5 rounded border bg-muted font-mono text-[10px]">/</kbd>
                <span>to focus search</span>
                <span className="mx-2">·</span>
                <kbd className="px-1.5 py-0.5 rounded border bg-muted font-mono text-[10px]">Esc</kbd>
                <span>to clear</span>
            </div>
        </Card>
    )
}

function ScopeStrip({ subLabel, subId }: { subLabel: string; subId: number }) {
    return (
        <div className="flex items-center gap-2 px-3 py-2 rounded-md border border-border bg-muted/30">
            <span className="text-[10px] uppercase tracking-wider font-semibold text-muted-foreground">Overview scope</span>
            <span className="text-sm font-medium">{subLabel}</span>
            <span className="text-xs text-muted-foreground font-mono">#{subId}</span>
        </div>
    )
}

function ResultSummaryBar({ hits, subs, truncated, onRaiseLimit }: { hits: number; subs: number; truncated: boolean; onRaiseLimit: () => void }) {
    return (
        <div className={cn(
            "flex items-center justify-between gap-3 px-3 py-2 rounded-md border",
            truncated ? "border-amber-500/30 bg-amber-500/5" : "border-border bg-muted/30",
        )}>
            <div className="flex items-center gap-3 text-xs">
                <span><span className="font-mono tabular-nums font-semibold text-foreground">{hits.toLocaleString()}</span> <span className="text-muted-foreground">hits</span></span>
                <span className="text-muted-foreground/40">·</span>
                <span><span className="font-mono tabular-nums font-semibold text-foreground">{subs.toLocaleString()}</span> <span className="text-muted-foreground">subscription{subs === 1 ? "" : "s"}</span></span>
                {truncated && (
                    <>
                        <span className="text-muted-foreground/40">·</span>
                        <span className="flex items-center gap-1 text-amber-300">
                            <AlertTriangle className="w-3 h-3" />
                            <span>truncated</span>
                        </span>
                    </>
                )}
            </div>
            {truncated && (
                <Button size="sm" variant="outline" className="h-7 text-xs" onClick={onRaiseLimit}>
                    Raise limit
                </Button>
            )}
        </div>
    )
}
