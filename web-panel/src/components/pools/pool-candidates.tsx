import { useDeferredValue, useState } from "react"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Archive, Ban, CircleCheck, CircleDashed, CircleX, Copy, EyeOff, Loader2, Search } from "lucide-react"
import { toast } from "@/lib/toast"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { protocolColors } from "@/components/node/protocol-badge"
import { poolAPI, type CandidateFilter, type OutboundPool, type PoolCandidate } from "@/lib/api/outbound-pools"
import { cn, copyToClipboard } from "@/lib/utils"
import { fullDate } from "./pool-display"

const PAGE = 50
const filters: { value: Exclude<CandidateFilter, "">, label: string, active: string }[] = [
    { value: "live", label: "In sources", active: "border-foreground/30 bg-foreground/5 text-foreground" },
    { value: "passed", label: "Passed", active: "border-emerald-500/40 bg-emerald-500/10 text-emerald-500" },
    { value: "failed", label: "Failed", active: "border-red-500/40 bg-red-500/10 text-red-400" },
    { value: "untested", label: "Untested", active: "border-sky-500/40 bg-sky-500/10 text-sky-400" },
    { value: "unsupported", label: "Unsupported", active: "border-amber-500/40 bg-amber-500/10 text-amber-500" },
    { value: "excluded", label: "Excluded", active: "border-foreground/30 bg-foreground/5 text-foreground" },
    { value: "missing", label: "Stale", active: "border-foreground/30 bg-foreground/5 text-foreground" },
]

// "Required HTTPS probes failed (0/2)" reads fine in a tooltip, not in a 180px column.
function reason(candidate: PoolCandidate) {
    const text = candidate.last_error ?? ""
    const short = text.replace(/^Required HTTPS probes failed/, "Probes failed").replace(/^Unsupported or /, "")
    return short.charAt(0).toUpperCase() + short.slice(1)
}

function StatusIcon({ candidate }: { candidate: PoolCandidate }) {
    const cls = "size-4 shrink-0"
    if (candidate.excluded) return <EyeOff className={cn(cls, "text-muted-foreground")} aria-label="Excluded" />
    if (!candidate.present) return <Archive className={cn(cls, "text-muted-foreground/60")} aria-label="No longer in sources" />
    switch (candidate.status) {
        case "passed": return <CircleCheck className={cn(cls, "text-emerald-500")} aria-label="Passed" />
        case "untested": return <CircleDashed className={cn(cls, "text-muted-foreground")} aria-label="Untested" />
        case "unsupported": return <Ban className={cn(cls, "text-amber-500/80")} aria-label="Unsupported" />
        default: return <CircleX className={cn(cls, "text-red-400")} aria-label="Failed" />
    }
}

function Result({ candidate }: { candidate: PoolCandidate }) {
    if (candidate.status === "passed" && candidate.latency_ms > 0) return <span className="font-mono text-emerald-500 tabular-nums">{candidate.latency_ms.toLocaleString()} ms</span>
    if (candidate.status === "untested") return <span className="text-muted-foreground">Not tested yet</span>
    if (!candidate.last_error) return <span className="text-muted-foreground capitalize">{candidate.status}</span>
    return <span className={cn("truncate", candidate.status === "unsupported" ? "text-amber-500/90" : "text-red-400/90")} title={candidate.last_error}>
        {reason(candidate)}
        {candidate.failures > 1 && <span className="text-muted-foreground tabular-nums"> · {candidate.failures}×</span>}
    </span>
}

const GRID = "grid items-center gap-x-3 grid-cols-[16px_minmax(0,1fr)_auto] md:grid-cols-[16px_minmax(0,1fr)_64px_minmax(0,200px)_auto]"

export function PoolCandidates({ nodeId, pool }: { nodeId: number, pool: OutboundPool }) {
    const [offset, setOffset] = useState(0)
    const [search, setSearch] = useState("")
    const [filter, setFilter] = useState<Exclude<CandidateFilter, "">>("live")
    const deferredSearch = useDeferredValue(search)
    const qc = useQueryClient()
    const busy = !!pool.active_run_id
    const query = useQuery({
        queryKey: ["pool-candidates", pool.id, offset, deferredSearch, filter, pool.last_run?.finished_at ?? ""],
        queryFn: () => poolAPI.candidates(nodeId, pool.id, offset, deferredSearch, filter),
        placeholderData: keepPreviousData,
        refetchInterval: busy ? 5000 : false,
    })
    const exclusion = useMutation({
        mutationFn: ({ id, excluded }: { id: number, excluded: boolean }) => poolAPI.exclude(nodeId, pool.id, id, excluded),
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ["pool-candidates", pool.id] }); toast.success("Updated. Membership changes apply after the next successful scan.") },
        onError: error => toast.error(error.message),
    })
    const data = query.data
    const counts = data?.counts
    const pick = (value: typeof filter) => { setFilter(value); setOffset(0) }

    return <div>
        <div className="space-y-3 px-4 sm:px-5 py-3.5 border-b">
            <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
                <Input aria-label="Search candidates" placeholder="Search address, name or protocol…" className="h-9 pl-8 text-sm" value={search} onChange={e => { setSearch(e.target.value); setOffset(0) }} />
                {query.isFetching && <Loader2 className="absolute right-3 top-1/2 -translate-y-1/2 size-3.5 animate-spin text-muted-foreground" />}
            </div>
            <div role="radiogroup" aria-label="Filter candidates" className="flex flex-wrap gap-1.5">
                {filters.map(f => {
                    const count = counts?.[f.value]
                    const on = filter === f.value
                    if (count === 0 && !on && f.value !== "live") return null
                    return <button key={f.value} type="button" role="radio" aria-checked={on} onClick={() => pick(f.value)}
                        className={cn("inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors",
                            on ? f.active : "border-border/60 text-muted-foreground hover:text-foreground hover:border-border")}>
                        {count !== undefined && <span className="font-semibold tabular-nums">{count.toLocaleString()}</span>}
                        {f.label}
                    </button>
                })}
            </div>
        </div>

        {query.isError && <p className="px-5 py-6 text-sm text-destructive">{query.error.message}</p>}
        {query.isLoading && <p className="flex items-center gap-2 px-5 py-8 text-xs text-muted-foreground"><Loader2 className="size-3.5 animate-spin" />Loading candidates…</p>}
        {data && !data.items.length && <p className="px-5 py-10 text-center text-sm text-muted-foreground">
            {search ? "No candidates match this search." : filter === "live" ? "Run a scan to collect and test configs." : "Nothing here."}
        </p>}
        {!!data?.items.length && <ul className={cn("divide-y transition-opacity", query.isPlaceholderData && "opacity-60")}>
            {data.items.map(candidate => <li key={candidate.id} className={cn(GRID, "group min-h-11 px-4 sm:px-5 py-1.5 text-xs hover:bg-muted/30 transition-colors")}>
                <StatusIcon candidate={candidate} />
                <div className="min-w-0">
                    <p className={cn("font-mono text-[14px] font-medium truncate", (!candidate.present || candidate.excluded) && "text-muted-foreground")} title={candidate.address}>{candidate.address || "Unrecognized config"}</p>
                    <p className="text-[12px] text-muted-foreground truncate" title={candidate.tested_at ? `Tested ${fullDate(candidate.tested_at)}` : undefined}>
                        {candidate.remark || "No name"}
                        {candidate.excluded && " · excluded"}
                        {!candidate.present && " · not in sources"}
                        {/* Narrow screens have no result column. */}
                        <span className="md:hidden"> · <Result candidate={candidate} /></span>
                    </p>
                </div>
                <div className="hidden md:block">
                    {candidate.protocol && <Badge variant="outline" className={cn("font-mono text-[11px] px-1.5 py-0 h-5", protocolColors[candidate.protocol.toLowerCase()] || "")}>{candidate.protocol.toUpperCase()}</Badge>}
                </div>
                <div className="hidden md:flex min-w-0"><Result candidate={candidate} /></div>
                <div className="flex items-center gap-0.5 md:opacity-50 md:group-hover:opacity-100 md:focus-within:opacity-100 transition-opacity">
                    <Button size="icon" variant="ghost" className="size-8" aria-label={`Copy config ${candidate.id}`} onClick={async () => { if (await copyToClipboard(candidate.link)) toast.success("Config copied") }}><Copy className="size-3.5" /></Button>
                    <Button size="sm" variant="ghost" className="h-8 px-2 text-xs" disabled={busy || exclusion.isPending} onClick={() => exclusion.mutate({ id: candidate.id, excluded: !candidate.excluded })}>{candidate.excluded ? "Include" : "Exclude"}</Button>
                </div>
            </li>)}
        </ul>}
        {data && data.total > PAGE && <div className="flex items-center justify-between gap-3 border-t px-4 sm:px-5 py-2.5 text-xs text-muted-foreground">
            <span className="tabular-nums">{(offset + 1).toLocaleString()}–{Math.min(offset + PAGE, data.total).toLocaleString()} of {data.total.toLocaleString()}</span>
            <div className="flex gap-1">
                <Button size="sm" variant="ghost" className="h-8" disabled={offset === 0} onClick={() => setOffset(n => Math.max(0, n - PAGE))}>Previous</Button>
                <Button size="sm" variant="ghost" className="h-8" disabled={offset + PAGE >= data.total} onClick={() => setOffset(n => n + PAGE)}>Next</Button>
            </div>
        </div>}
    </div>
}
