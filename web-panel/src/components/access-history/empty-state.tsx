import { useCallback, useState } from "react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import type { AccessHistoryMatch, AccessHistorySearchKind } from "@/lib/types"
import { Kbd } from "./bits"
import { fmtAgo } from "./format"

// ─── Recent history (this browser only) ─────────────────────────────

export interface RecentSearch {
    q: string
    match: AccessHistoryMatch
    kinds: AccessHistorySearchKind[]
    range: string
    at: number
}

export interface RecentSubscription {
    id: number
    label: string
    at: number
}

interface RecentStore {
    searches: RecentSearch[]
    subs: RecentSubscription[]
}

const KEY = "access-history.recent"
const MAX = 5

function read(): RecentStore {
    try {
        const raw = localStorage.getItem(KEY)
        const v = raw ? JSON.parse(raw) : null
        return { searches: Array.isArray(v?.searches) ? v.searches : [], subs: Array.isArray(v?.subs) ? v.subs : [] }
    } catch {
        return { searches: [], subs: [] }
    }
}

function write(v: RecentStore) {
    try { localStorage.setItem(KEY, JSON.stringify(v)) } catch { /* storage off: history just isn't kept */ }
}

export function useRecent() {
    const [store, setStore] = useState<RecentStore>(read)
    const update = useCallback((fn: (s: RecentStore) => RecentStore) => {
        setStore(prev => {
            const next = fn(prev)
            write(next)
            return next
        })
    }, [])
    const pushSearch = useCallback((s: Omit<RecentSearch, "at">) => update(prev => ({
        ...prev,
        searches: [{ ...s, at: Date.now() }, ...prev.searches.filter(x => !(x.q === s.q && x.match === s.match))].slice(0, MAX),
    })), [update])
    const pushSub = useCallback((s: Omit<RecentSubscription, "at">) => update(prev => ({
        ...prev,
        subs: [{ ...s, at: Date.now() }, ...prev.subs.filter(x => x.id !== s.id)].slice(0, MAX),
    })), [update])
    const clear = useCallback(() => update(() => ({ searches: [], subs: [] })), [update])
    return { ...store, pushSearch, pushSub, clear }
}

// ─── Empty state ────────────────────────────────────────────────────

export interface ExampleSearch {
    q: string
    what: string
    tag: string
    match: AccessHistoryMatch
    kinds: AccessHistorySearchKind[]
}

const EXAMPLES: ExampleSearch[] = [
    { q: "instagram.com", what: "Who used Instagram, subdomains included", tag: "Domain", match: "domain", kinds: ["domain"] },
    { q: "telegram", what: "Anything with telegram in the name", tag: "Contains", match: "contains", kinds: ["domain", "rejected_domain"] },
    { q: "10.10.34.36", what: "Who was blocked from the filtered DNS address", tag: "Rejected", match: "domain", kinds: ["rejected_domain"] },
    { q: "203.0.113.7", what: "Who connected from this address", tag: "Source IP", match: "domain", kinds: ["source_ip"] },
]

const TAG_TONE: Record<string, string> = {
    Rejected: "border-red-500/35 bg-red-500/10 text-red-700 dark:text-red-300",
    "Source IP": "border-sky-500/35 bg-sky-500/10 text-sky-700 dark:text-sky-300",
}

const RANGE_LABEL: Record<string, string> = { "24h": "last 24 hours", "7d": "last 7 days", "14d": "last 14 days", "30d": "last 30 days", custom: "custom range" }

export function EmptyState({
    recent,
    onExample,
    onRecentSearch,
    onRecentSub,
}: {
    recent: ReturnType<typeof useRecent>
    onExample: (e: ExampleSearch) => void
    onRecentSearch: (s: RecentSearch) => void
    onRecentSub: (s: RecentSubscription) => void
}) {
    const hasRecent = recent.searches.length > 0 || recent.subs.length > 0
    return (
        <Card className={cn("grid gap-8 p-5 sm:p-6", hasRecent && "lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]")}>
            <div className="space-y-3.5">
                <h2 className="text-[15px] font-semibold">Try a search</h2>
                <div className="space-y-2">
                    {EXAMPLES.map(e => (
                        <button
                            key={e.q}
                            type="button"
                            onClick={() => onExample(e)}
                            className={cn(
                                "grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3.5 gap-y-1 rounded-[10px] border border-border px-3.5 py-[11px] text-left transition-colors hover:border-input hover:bg-muted/30",
                                !hasRecent && "sm:grid-cols-[150px_minmax(0,1fr)_auto]",
                            )}
                        >
                            <span className="justify-self-start whitespace-nowrap rounded-[5px] bg-muted px-[7px] py-0.5 font-mono text-sm">{e.q}</span>
                            <span className={cn("text-sm text-foreground/85", hasRecent ? "order-last col-span-2" : "max-sm:order-last max-sm:col-span-2")}>{e.what}</span>
                            <span className={cn("whitespace-nowrap rounded-full border px-2 py-0.5 text-[12px] font-medium", TAG_TONE[e.tag] ?? "border-border text-foreground/80")}>{e.tag}</span>
                        </button>
                    ))}
                </div>
                <div className="space-y-1 pt-1 text-sm text-muted-foreground">
                    <p><span className="font-medium text-foreground/85">Contains</span> finds the text anywhere: google also finds googleapis.com.</p>
                    <p><span className="font-medium text-foreground/85">Domain</span> finds the name and its subdomains: google.com finds mail.google.com, not googleapis.com.</p>
                </div>
                <p className="hidden items-center gap-1.5 text-[13px] text-muted-foreground sm:flex">
                    Press <Kbd>/</Kbd> to search from anywhere on this page and <Kbd>Esc</Kbd> to clear.
                </p>
            </div>

            {hasRecent && (
                <div className="space-y-3.5">
                    <div className="flex items-center justify-between">
                        <h2 className="text-[15px] font-semibold">Recent</h2>
                        <button type="button" onClick={recent.clear} className="text-[13px] text-muted-foreground hover:text-foreground">Clear history</button>
                    </div>
                    {recent.searches.length > 0 && (
                        <div>
                            <p className="pb-1.5 text-[13px] text-muted-foreground">Searches</p>
                            {recent.searches.map(s => (
                                <button
                                    key={`${s.q}-${s.match}`}
                                    type="button"
                                    onClick={() => onRecentSearch(s)}
                                    className="-mx-2 grid w-[calc(100%+16px)] grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 rounded-lg px-2 py-[9px] text-left transition-colors hover:bg-muted/40"
                                >
                                    <span className="flex min-w-0 flex-col">
                                        <span className="truncate font-mono text-sm">{s.q}</span>
                                        <span className="text-[13px] text-muted-foreground">
                                            {s.match === "domain" ? "Domain" : "Contains"}, {RANGE_LABEL[s.range] ?? s.range}
                                        </span>
                                    </span>
                                    <span className="text-[13px] text-muted-foreground">{fmtAgo(new Date(s.at))}</span>
                                </button>
                            ))}
                        </div>
                    )}
                    {recent.subs.length > 0 && (
                        <div className="space-y-2">
                            <p className="text-[13px] text-muted-foreground">Subscriptions</p>
                            <div className="flex flex-wrap gap-1.5">
                                {recent.subs.map(s => (
                                    <button
                                        key={s.id}
                                        type="button"
                                        onClick={() => onRecentSub(s)}
                                        className="inline-flex h-7 items-center gap-1.5 rounded-full bg-muted px-2.5 text-[13px] transition-colors hover:bg-muted/70"
                                    >
                                        <span className="font-mono text-muted-foreground">#{s.id}</span>
                                        {s.label}
                                    </button>
                                ))}
                            </div>
                        </div>
                    )}
                    <p className="text-[12px] text-muted-foreground">Kept in this browser only.</p>
                </div>
            )}
        </Card>
    )
}
