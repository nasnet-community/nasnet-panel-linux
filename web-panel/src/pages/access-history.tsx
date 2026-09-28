import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Link, useSearchParams } from "react-router"
import type { DateRange } from "react-day-picker"
import { AlertTriangle, Database, ExternalLink, Search, SlidersHorizontal } from "lucide-react"
import { toast } from "@/lib/toast"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { PageHeader } from "@/components/shared/page-header"
import { SubscriptionDetailsSheet } from "@/components/subscription/subscription-details-sheet"
import { useGlobalAccessHistorySearch } from "@/lib/queries"
import { useNodes } from "@/lib/queries/use-nodes"
import { useSubscription } from "@/lib/queries/use-subscriptions"
import { getSubscription } from "@/lib/api/subscriptions"
import { subscriptionLabel } from "@/lib/subscription-label"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import type { AccessHistoryMatch, AccessHistorySearchKind } from "@/lib/types"
import {
    FilterRail, PRESETS, DEFAULT_PRESET, EMPTY_RAIL, activeFilterCount,
    type FilterRailValue, type RangePreset,
} from "@/components/access-history/filter-rail"
import { SearchBar } from "@/components/access-history/search-bar"
import { SearchSummary } from "@/components/access-history/summary-card"
import { SubscriptionsTable, ValuesTable, RecordsTable } from "@/components/access-history/result-search"
import { Timeline } from "@/components/access-history/timeline"
import { ResultOverview } from "@/components/access-history/result-overview"
import { EmptyState, useRecent } from "@/components/access-history/empty-state"
import { SyncBadge, nodeSyncs, staleNotes } from "@/components/access-history/sync-badge"
import { Pill } from "@/components/access-history/bits"
import type { HourPoint } from "@/components/access-history/charts"
import { fillHours, fmtInt } from "@/components/access-history/format"

type Tab = "subscriptions" | "values" | "timeline" | "records"

const DEFAULT_KINDS: AccessHistorySearchKind[] = ["domain", "rejected_domain"]
const DEFAULT_LIMIT = 1000
const MAX_LIMIT = 2000
const TABS: Tab[] = ["subscriptions", "values", "timeline", "records"]

const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`
function parseYmd(s: string | null): Date | undefined {
    if (!s || !/^\d{4}-\d{2}-\d{2}$/.test(s)) return undefined
    const [y, m, d] = s.split("-").map(Number)
    return new Date(y, m - 1, d)
}

function deriveWindow(preset: RangePreset, custom?: DateRange): { from: Date; to: Date } | null {
    const now = new Date()
    if (preset !== "custom") {
        const ms = PRESETS.find(p => p.key === preset)?.ms ?? PRESETS[1].ms
        return { from: new Date(now.getTime() - ms), to: now }
    }
    if (!custom?.from || !custom?.to) return null
    const from = new Date(custom.from)
    from.setHours(0, 0, 0, 0)
    const to = new Date(custom.to)
    to.setHours(23, 59, 59, 999)
    // The hub rejects windows that end in the future.
    return { from, to: to > now ? now : to }
}

const isPlaceholderLabel = (s: { id: number; label: string }) => s.label === `Sub #${s.id}`

export default function AccessHistoryPage() {
    const [searchParams, setSearchParams] = useSearchParams()
    const nodes = useNodes().data ?? []
    const syncs = useMemo(() => nodeSyncs(nodes), [nodes])
    const stale = useMemo(() => staleNotes(syncs), [syncs])
    const nodeNames = useMemo(() => new Map(nodes.map(n => [n.id, n.name])), [nodes])
    const recent = useRecent()
    const { openDetailsSheet } = useSubscriptionsStore()
    const searchInputRef = useRef<HTMLInputElement>(null)
    const [sheetOpen, setSheetOpen] = useState(false)

    const [rail, setRail] = useState<FilterRailValue>(() => {
        const preset = (searchParams.get("preset") as RangePreset) || DEFAULT_PRESET
        const from = parseYmd(searchParams.get("from"))
        const to = parseYmd(searchParams.get("to"))
        const ids = searchParams.get("subscription_ids")?.split(",").map(Number).filter(n => Number.isFinite(n) && n > 0) ?? []
        return {
            preset: preset === "custom" && !(from && to) ? DEFAULT_PRESET : preset,
            customRange: from && to ? { from, to } : undefined,
            subscriptions: ids.map(id => ({ id, label: `Sub #${id}` })),
            emails: searchParams.get("emails")?.split(",").filter(Boolean) ?? [],
            nodeIDs: searchParams.get("node_ids")?.split(",").map(Number).filter(n => Number.isFinite(n) && n > 0) ?? [],
        }
    })
    const [kinds, setKinds] = useState<AccessHistorySearchKind[]>(() => {
        const k = searchParams.get("kinds")?.split(",").filter(Boolean) as AccessHistorySearchKind[] | undefined
        return k && k.length > 0 ? k : DEFAULT_KINDS
    })
    const [match, setMatch] = useState<AccessHistoryMatch>(() => (searchParams.get("match") === "domain" ? "domain" : "contains"))
    const [limit, setLimit] = useState(() => {
        const n = Number(searchParams.get("limit"))
        return Number.isFinite(n) && n > 0 ? Math.min(n, MAX_LIMIT) : DEFAULT_LIMIT
    })
    const [tab, setTab] = useState<Tab>(() => {
        const t = searchParams.get("tab") as Tab
        return TABS.includes(t) ? t : "subscriptions"
    })
    const [raw, setRaw] = useState(searchParams.get("q") ?? "")
    const [debounced, setDebounced] = useState(raw.trim())

    useEffect(() => {
        const id = window.setTimeout(() => setDebounced(raw.trim()), 350)
        return () => window.clearTimeout(id)
    }, [raw])

    // "/" focuses the search from anywhere on the page; Esc clears it.
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
        if (rail.preset === "custom" && rail.customRange?.from && rail.customRange?.to) {
            next.set("from", ymd(rail.customRange.from))
            next.set("to", ymd(rail.customRange.to))
        }
        if (debounced) next.set("q", debounced)
        if (match !== "contains") next.set("match", match)
        if (kinds.join(",") !== DEFAULT_KINDS.join(",")) next.set("kinds", kinds.join(","))
        if (limit !== DEFAULT_LIMIT) next.set("limit", String(limit))
        if (tab !== "subscriptions") next.set("tab", tab)
        if (rail.nodeIDs.length > 0) next.set("node_ids", rail.nodeIDs.join(","))
        if (rail.subscriptions.length > 0) next.set("subscription_ids", rail.subscriptions.map(s => s.id).join(","))
        if (rail.emails.length > 0) next.set("emails", rail.emails.join(","))
        setSearchParams(next, { replace: true })
    }, [rail, kinds, match, limit, tab, debounced, setSearchParams])

    // Mirrors the hub: domain mode drops a leading "*." or "." before the 2-char minimum.
    const effective = match === "domain" ? debounced.replace(/^\*\./, "").replace(/^\.+|\.+$/g, "") : debounced
    const hasQuery = effective.length >= 2
    const single = rail.subscriptions.length === 1 && rail.emails.length === 0 ? rail.subscriptions[0] : undefined
    const profileMode = !!single && !hasQuery && !!win

    // Links carry only ids; fetch the one subscription on screen for its name.
    const { data: singleSub } = useSubscription(single?.id ?? 0)
    useEffect(() => {
        if (!singleSub || !single || !isPlaceholderLabel(single)) return
        const label = subscriptionLabel(singleSub)
        if (label) setRail(r => ({ ...r, subscriptions: r.subscriptions.map(s => (s.id === singleSub.id ? { ...s, label } : s)) }))
    }, [singleSub, single])

    const params = useMemo(() => (win ? {
        from: win.from.toISOString(),
        to: win.to.toISOString(),
        q: debounced,
        match,
        kinds: kinds.length > 0 ? kinds : undefined,
        node_ids: rail.nodeIDs.length > 0 ? rail.nodeIDs : undefined,
        subscription_ids: rail.subscriptions.length > 0 ? rail.subscriptions.map(s => s.id) : undefined,
        emails: rail.emails.length > 0 ? rail.emails : undefined,
        include_ips: kinds.includes("source_ip"),
        limit,
    } : null), [win, debounced, match, kinds, rail, limit])

    const { data, isFetching, isPlaceholderData, isError, error } = useGlobalAccessHistorySearch(
        params ?? { from: "", to: "", q: "" },
        !!params && hasQuery,
    )

    const { pushSearch, pushSub } = recent
    useEffect(() => {
        if (data && !isPlaceholderData && data.query) pushSearch({ q: data.query, match: data.match ?? "contains", kinds, range: rail.preset })
        // Record once per answered search, not on every filter tweak.
    }, [data?.query, data?.match, isPlaceholderData])
    useEffect(() => {
        if (profileMode && single && !isPlaceholderLabel(single)) pushSub(single)
    }, [profileMode, single, pushSub])

    const points: HourPoint[] = useMemo(() => {
        if (!data || !win) return []
        const buckets = (data.series ?? []).map(b => ({ bucket: b.bucket, v: b }))
        return fillHours(win.from, win.to, buckets, () => null).map(({ t, v }) => ({
            t,
            accepted: v?.accepted ?? 0,
            rejected: v?.rejected ?? 0,
            sourceIp: v?.source_ip ?? 0,
            nodes: v?.nodes,
        }))
    }, [data, win])

    // Known only once the hub has answered; the badge waits for it.
    const [retentionDays, setRetentionDays] = useState<number>()
    useEffect(() => {
        if (data?.retention_days) setRetentionDays(data.retention_days)
    }, [data?.retention_days])
    const windowHours = win ? Math.max(1, Math.round((win.to.getTime() - win.from.getTime()) / 3_600_000)) : 0

    // ─── Actions ────────────────────────────────────────────────────

    const openProfile = useCallback((id: number, label: string) => {
        setRail(r => ({ ...r, subscriptions: [{ id, label }], emails: [] }))
        setRaw("")
        setDebounced("")
        setTab("subscriptions")
    }, [])
    const searchWithin = useCallback((id: number, label: string) => {
        setRail(r => ({ ...r, subscriptions: [{ id, label }], emails: [] }))
        setTab("values")
    }, [])
    const searchValue = useCallback((value: string) => {
        setRaw(value)
        setMatch("domain")
        setTab("subscriptions")
    }, [])
    const openSubscription = useCallback(async (id: number) => {
        const res = await getSubscription(id)
        if (res.success && res.data) openDetailsSheet(res.data)
        else toast.error(res.error || "Couldn't open the subscription")
    }, [openDetailsSheet])
    const zoomToDay = useCallback((t: number) => {
        const day = new Date(t)
        setRail(r => ({ ...r, preset: "custom", customRange: { from: day, to: day } }))
    }, [])

    const filterCount = activeFilterCount(rail)
    const filtersButton = (
        <Button variant="outline" className="h-9 gap-2 lg:hidden" onClick={() => setSheetOpen(true)}>
            <SlidersHorizontal className="size-4" />
            Filters
            {filterCount > 0 && (
                <span className="flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-primary px-1 font-mono text-[11px] font-bold text-primary-foreground">{filterCount}</span>
            )}
        </Button>
    )

    // ─── Main column ────────────────────────────────────────────────

    let body: React.ReactNode
    if (hasQuery) {
        const scope = (
            <>
                {rail.subscriptions.length === 0 && rail.emails.length === 0 && <Pill>All subscriptions</Pill>}
                {rail.subscriptions.slice(0, 3).map(s => (
                    <Pill key={s.id}><span className="font-mono font-normal text-muted-foreground">#{s.id}</span>{s.label}</Pill>
                ))}
                {rail.subscriptions.length > 3 && <Pill>+{rail.subscriptions.length - 3} more</Pill>}
                {rail.emails.map(e => <Pill key={e} className="font-mono font-normal">{e}</Pill>)}
                {rail.nodeIDs.map(id => <Pill key={id}>{nodeNames.get(id) ?? `Node ${id}`}</Pill>)}
                {single && (
                    <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={() => { setRaw(""); setDebounced("") }}>
                        Back to {single.label}'s profile
                    </Button>
                )}
            </>
        )
        if (!data && isFetching) {
            body = (
                <div className="space-y-4">
                    <Skeleton className="h-[210px] rounded-xl" />
                    <Skeleton className="h-10 w-96 rounded-lg" />
                    <Skeleton className="h-[420px] rounded-xl" />
                </div>
            )
        } else if (isError) {
            body = (
                <Card className="flex-row items-center gap-2.5 border-red-500/30 bg-red-500/5 p-4">
                    <AlertTriangle className="size-4 shrink-0 text-red-500" />
                    <span className="text-sm text-red-600 dark:text-red-300">Search failed: {error instanceof Error ? error.message : String(error)}</span>
                </Card>
            )
        } else if (data && win) {
            const subs = data.subscriptions ?? []
            body = (
                <div className="space-y-4">
                    <SearchSummary
                        scope={scope}
                        from={win.from}
                        to={win.to}
                        subscriptions={data.totals?.subscriptions ?? 0}
                        values={data.totals?.values ?? 0}
                        hits={data.totals?.hits ?? 0}
                        firstSeen={data.totals?.first_seen}
                        lastSeen={data.totals?.last_seen}
                        points={points}
                        approximate={!!data.approximate}
                        updating={isFetching && isPlaceholderData}
                        onPickDay={rail.preset === "custom" && rail.customRange?.from?.getTime() === rail.customRange?.to?.getTime() ? undefined : zoomToDay}
                    />
                    <Tabs value={tab} onValueChange={v => setTab(v as Tab)} className="gap-4">
                        <div className="-mr-4 overflow-x-auto pr-4 sm:mr-0 sm:pr-0">
                            <TabsList className="h-10">
                                <TabsTrigger value="subscriptions" className="gap-1.5">Subscriptions <span className="font-mono text-xs text-muted-foreground">{fmtInt(data.totals?.subscriptions ?? 0)}</span></TabsTrigger>
                                <TabsTrigger value="values" className="gap-1.5">Values <span className="font-mono text-xs text-muted-foreground">{fmtInt(data.totals?.values ?? 0)}</span></TabsTrigger>
                                <TabsTrigger value="timeline">Timeline</TabsTrigger>
                                <TabsTrigger value="records" className="gap-1.5">Records <span className="font-mono text-xs text-muted-foreground">{fmtInt(data.totals?.records ?? 0)}</span></TabsTrigger>
                            </TabsList>
                        </div>
                        <TabsContent value="subscriptions">
                            <SubscriptionsTable
                                rows={subs}
                                windowHours={windowHours}
                                nodeNames={nodeNames}
                                onOpenProfile={openProfile}
                                onSearchWithin={searchWithin}
                                onOpenSubscription={openSubscription}
                            />
                        </TabsContent>
                        <TabsContent value="values">
                            <ValuesTable rows={data.by_value ?? []} total={data.totals?.values ?? 0} onSearchValue={searchValue} />
                        </TabsContent>
                        <TabsContent value="timeline">
                            <Timeline points={points} nodes={nodes} staleNodes={stale} />
                        </TabsContent>
                        <TabsContent value="records">
                            <RecordsTable
                                rows={data.hits ?? []}
                                total={data.totals?.records ?? 0}
                                truncated={data.truncated}
                                limit={limit}
                                nodeNames={nodeNames}
                                onRaiseLimit={() => setLimit(MAX_LIMIT)}
                            />
                        </TabsContent>
                    </Tabs>
                </div>
            )
        }
    } else if (profileMode && single && win) {
        body = (
            <ResultOverview
                subscriptionId={single.id}
                label={single.label}
                from={win.from}
                to={win.to}
                nodeIDs={rail.nodeIDs.length > 0 ? rail.nodeIDs : undefined}
                includeIPs={kinds.includes("source_ip")}
                nodeNames={nodeNames}
                onSearchValue={searchValue}
                onRetention={setRetentionDays}
                actions={
                    <>
                        <Button size="sm" variant="outline" className="h-[30px] text-xs" onClick={() => searchInputRef.current?.focus()}>
                            <Search className="size-3.5" />Search within
                        </Button>
                        <Button size="sm" variant="ghost" className="h-[30px] text-xs" onClick={() => openSubscription(single.id)}>
                            Subscription<ExternalLink className="size-3" />
                        </Button>
                        {singleSub?.user_id ? (
                            <Button size="sm" variant="ghost" className="h-[30px] text-xs" asChild>
                                <Link to={`/users/${singleSub.user_id}`}>User<ExternalLink className="size-3" /></Link>
                            </Button>
                        ) : null}
                    </>
                }
            />
        )
    } else if (rail.subscriptions.length > 0 || rail.emails.length > 0) {
        body = (
            <Card className="items-center gap-1.5 p-10 text-center">
                <p className="text-sm font-medium">{fmtInt(rail.subscriptions.length + rail.emails.length)} picked</p>
                <p className="text-[13px] text-muted-foreground">Type a search to look across them, or keep just one subscription to see its profile.</p>
            </Card>
        )
    } else {
        body = (
            <EmptyState
                recent={recent}
                onExample={e => { setRaw(e.q); setMatch(e.match); setKinds(e.kinds) }}
                onRecentSearch={s => { setRaw(s.q); setMatch(s.match); setKinds(s.kinds.length > 0 ? s.kinds : DEFAULT_KINDS) }}
                onRecentSub={s => openProfile(s.id, s.label)}
            />
        )
    }

    const placeholder = single
        ? `Search inside ${single.label}'s history: a domain, IP or part of a name`
        : "Search a domain, IP or part of a name"

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            <PageHeader
                title="Access History"
                description="Find who reached a domain or IP, or review one subscription's traffic."
                actions={
                    <div className="flex flex-wrap items-center gap-2">
                        {retentionDays !== undefined && (
                            <Badge variant="secondary" className="h-7 gap-1.5 px-2.5 text-xs">
                                <Database className="size-3 text-muted-foreground" />
                                <span className="font-mono font-semibold">{retentionDays} days</span>
                                <span className="text-muted-foreground">kept</span>
                            </Badge>
                        )}
                        <SyncBadge syncs={syncs} />
                    </div>
                }
            />

            <div className="grid grid-cols-1 gap-4 lg:grid-cols-[280px_minmax(0,1fr)] lg:gap-5">
                <div className="hidden lg:sticky lg:top-4 lg:block lg:self-start">
                    <FilterRail value={rail} onChange={setRail} nodes={nodes} syncs={syncs} retentionDays={retentionDays ?? 30} />
                </div>
                <div className="min-w-0 space-y-4">
                    <SearchBar
                        inputRef={searchInputRef}
                        value={raw}
                        onChange={setRaw}
                        placeholder={placeholder}
                        kinds={kinds}
                        onKinds={setKinds}
                        match={match}
                        onMatch={setMatch}
                        filtersButton={filtersButton}
                    />
                    {body}
                </div>
            </div>

            <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
                <SheetContent side="bottom" className="max-h-[90dvh] gap-0 rounded-t-2xl px-0 pb-0">
                    <SheetHeader className="flex-row items-center justify-between px-5 pb-2 pt-4">
                        <SheetTitle className="text-[17px]">Filters</SheetTitle>
                        <SheetDescription className="sr-only">Range, who and nodes</SheetDescription>
                        {filterCount > 0 && (
                            <Button variant="ghost" size="sm" className="mr-8 text-muted-foreground" onClick={() => setRail(EMPTY_RAIL)}>Clear all</Button>
                        )}
                    </SheetHeader>
                    <div className="overflow-y-auto px-5 pb-5 pt-1">
                        <FilterRail value={rail} onChange={setRail} nodes={nodes} syncs={syncs} retentionDays={retentionDays ?? 30} touch />
                    </div>
                    <div className="border-t border-border px-5 pb-[calc(20px+env(safe-area-inset-bottom,0px))] pt-3">
                        <Button className="h-12 w-full text-[15px]" onClick={() => setSheetOpen(false)}>
                            {hasQuery && data ? `Show ${fmtInt(data.totals?.subscriptions ?? 0)} subscriptions` : "Show results"}
                        </Button>
                    </div>
                </SheetContent>
            </Sheet>

            <SubscriptionDetailsSheet />
        </div>
    )
}
