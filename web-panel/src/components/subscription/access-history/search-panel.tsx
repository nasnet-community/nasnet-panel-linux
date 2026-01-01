import { useEffect, useMemo, useState } from "react"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { AlertCircle, Search, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { useSubscriptionAccessSearch } from "@/lib/queries"
import type {
    AccessHistorySearchKind,
    AccessHistorySearchResponse,
} from "@/lib/types"

const KIND_LABEL: Record<AccessHistorySearchKind, string> = {
    domain: "Accepted",
    rejected_domain: "Rejected",
    source_ip: "Source IP",
}
const KIND_TONE: Record<AccessHistorySearchKind, string> = {
    domain: "text-emerald-400 bg-emerald-500/10",
    rejected_domain: "text-red-400 bg-red-500/10",
    source_ip: "text-sky-400 bg-sky-500/10",
}

interface Props {
    subId: number | undefined
    from: string // ISO
    to: string // ISO
    includeIPs: boolean
    className?: string
}

export function AccessHistorySearchPanel({ subId, from, to, includeIPs, className }: Props) {
    const [raw, setRaw] = useState("")
    const [debounced, setDebounced] = useState("")
    const [kindFilter, setKindFilter] = useState<AccessHistorySearchKind | "all">("all")

    // Debounce typing — 350 ms keeps the UI snappy without spamming the API.
    useEffect(() => {
        const id = window.setTimeout(() => setDebounced(raw.trim()), 350)
        return () => window.clearTimeout(id)
    }, [raw])

    const kinds: AccessHistorySearchKind[] | undefined = useMemo(() => {
        if (kindFilter === "all") return undefined
        if (kindFilter === "source_ip" && !includeIPs) return undefined
        return [kindFilter]
    }, [kindFilter, includeIPs])

    const { data, isFetching, isError, error } = useSubscriptionAccessSearch(
        subId,
        {
            from,
            to,
            q: debounced,
            kinds,
            limit: 500,
            include_ips: includeIPs,
        },
        true,
    )

    const showResults = debounced.length >= 2

    return (
        <Card className={cn("p-3 space-y-3", className)}>
            <div className="flex flex-wrap items-center gap-2">
                <div className="relative flex-1 min-w-[200px]">
                    <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground/70" />
                    <input
                        type="search"
                        value={raw}
                        onChange={e => setRaw(e.target.value)}
                        placeholder="Search domain or IP across this window…"
                        className="w-full h-8 pl-7 pr-7 text-xs bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-ring"
                        spellCheck={false}
                        autoComplete="off"
                    />
                    {raw && (
                        <button
                            type="button"
                            onClick={() => setRaw("")}
                            className="absolute right-1.5 top-1/2 -translate-y-1/2 p-0.5 text-muted-foreground/70 hover:text-foreground"
                            aria-label="Clear"
                        >
                            <X className="w-3.5 h-3.5" />
                        </button>
                    )}
                </div>
                <KindToggle
                    value={kindFilter}
                    onChange={setKindFilter}
                    showSourceIP={includeIPs}
                />
            </div>

            {!showResults ? (
                <p className="text-[11px] text-muted-foreground/70">
                    Type at least 2 characters. Matches are limited to the top-100 domains/IPs the
                    agent persisted per hour, so very long-tail keys may not appear.
                </p>
            ) : isError ? (
                <div className="flex items-center gap-2 text-xs text-red-400">
                    <AlertCircle className="w-3.5 h-3.5" />
                    {error instanceof Error ? error.message : "Search failed"}
                </div>
            ) : isFetching && !data ? (
                <SearchSkeleton />
            ) : !data ? null : (
                <SearchResults data={data} />
            )}
        </Card>
    )
}

function KindToggle({
    value,
    onChange,
    showSourceIP,
}: {
    value: AccessHistorySearchKind | "all"
    onChange: (v: AccessHistorySearchKind | "all") => void
    showSourceIP: boolean
}) {
    const buttons: { v: AccessHistorySearchKind | "all"; label: string }[] = [
        { v: "all", label: "All" },
        { v: "domain", label: "Accepted" },
        { v: "rejected_domain", label: "Rejected" },
    ]
    if (showSourceIP) buttons.push({ v: "source_ip", label: "IPs" })
    return (
        <div className="flex items-center gap-0.5 bg-background rounded-md p-0.5 ring-1 ring-border">
            {buttons.map(b => (
                <button
                    key={b.v}
                    type="button"
                    onClick={() => onChange(b.v)}
                    aria-pressed={value === b.v}
                    className={cn(
                        "px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider transition-colors",
                        value === b.v
                            ? "bg-foreground text-background"
                            : "text-muted-foreground hover:text-foreground",
                    )}
                >
                    {b.label}
                </button>
            ))}
        </div>
    )
}

function SearchSkeleton() {
    return (
        <div className="space-y-1.5">
            {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
            ))}
        </div>
    )
}

function SearchResults({ data }: { data: AccessHistorySearchResponse }) {
    const aggregates = data.aggregates ?? []
    const hits = data.hits ?? []
    const truncated = data.truncated
    const total = aggregates.reduce((acc, a) => acc + a.count, 0)
    const distinctValues = aggregates.length

    if (hits.length === 0) {
        return (
            <p className="text-[11px] text-muted-foreground/70">
                No matches in this window.
            </p>
        )
    }

    return (
        <div className="space-y-2">
            <div className="flex items-center gap-3 text-[11px] text-muted-foreground/80">
                <span>
                    <span className="font-bold text-foreground">{distinctValues}</span> match
                    {distinctValues === 1 ? "" : "es"}
                </span>
                <span>·</span>
                <span>
                    <span className="font-bold text-foreground">{total.toLocaleString()}</span> hits
                </span>
                {truncated && (
                    <>
                        <span>·</span>
                        <span className="text-amber-400">truncated — narrow query or range</span>
                    </>
                )}
            </div>

            <div className="overflow-hidden rounded-md ring-1 ring-border max-h-[280px] overflow-y-auto">
                <table className="w-full text-xs">
                    <thead className="bg-muted/30 text-[10px] uppercase tracking-wider text-muted-foreground/70 sticky top-0">
                        <tr>
                            <th className="text-left px-3 py-1.5 font-bold">Kind</th>
                            <th className="text-left px-3 py-1.5 font-bold">Value</th>
                            <th className="text-right px-3 py-1.5 font-bold">Count</th>
                            <th className="text-right px-3 py-1.5 font-bold">Hours</th>
                        </tr>
                    </thead>
                    <tbody className="font-mono">
                        {aggregates.map((a, i) => (
                            <tr
                                key={`${a.kind}:${a.value}:${i}`}
                                className="border-t border-border/30"
                            >
                                <td className="px-3 py-1.5">
                                    <span
                                        className={cn(
                                            "inline-block px-1.5 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider",
                                            KIND_TONE[a.kind],
                                        )}
                                    >
                                        {KIND_LABEL[a.kind]}
                                    </span>
                                </td>
                                <td className="px-3 py-1.5 truncate max-w-[280px]" title={a.value}>
                                    {a.value}
                                </td>
                                <td className="px-3 py-1.5 text-right tabular-nums">
                                    {a.count.toLocaleString()}
                                </td>
                                <td className="px-3 py-1.5 text-right tabular-nums">{a.hours}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    )
}
