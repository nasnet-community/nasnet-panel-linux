import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Ban, CircleCheck, CircleMinus, CircleX, Loader2 } from "lucide-react"
import { poolAPI, type OutboundPool, type PoolRun } from "@/lib/api/outbound-pools"
import { cn } from "@/lib/utils"
import { errorHeadline, fullDate, relativeTime, runDuration, runOutcome } from "./pool-display"

/** The last 20 runs. Keyed on the last run so a finished scan refetches without polling. */
export function usePoolRuns(nodeId: number, pool: OutboundPool) {
    return useQuery({
        queryKey: ["pool-runs", pool.id, pool.last_run?.id ?? 0, pool.last_run?.state ?? ""],
        queryFn: () => poolAPI.runs(nodeId, pool.id),
        refetchInterval: pool.active_run_id ? 5000 : false,
        staleTime: 30_000,
    })
}

function RunIcon({ run }: { run: PoolRun }) {
    const cls = "size-4 shrink-0"
    switch (runOutcome(run)) {
        case "running": return <Loader2 className={cn(cls, "text-sky-500 animate-spin")} aria-label="Running" />
        case "failed": return <CircleX className={cn(cls, "text-red-400")} aria-label="Failed" />
        case "cancelled": return <Ban className={cn(cls, "text-muted-foreground")} aria-label="Cancelled" />
        case "empty": return <CircleMinus className={cn(cls, "text-amber-500/80")} aria-label="None passed" />
        default: return <CircleCheck className={cn(cls, "text-emerald-500")} aria-label="Completed" />
    }
}

const GRID = "grid items-center gap-x-3 grid-cols-[16px_minmax(0,1fr)_64px_48px] sm:grid-cols-[16px_minmax(0,1fr)_76px_64px_64px_56px_64px]"
const head = "text-[12px] font-medium uppercase tracking-wide text-muted-foreground text-right"
const num = (n: number) => n.toLocaleString()

export function PoolHistory({ runs, loading, error }: { runs: PoolRun[], loading: boolean, error?: Error | null }) {
    const [open, setOpen] = useState<number | null>(null)
    if (error) return <p className="px-5 py-6 text-sm text-destructive">{error.message}</p>
    if (loading) return <p className="flex items-center gap-2 px-5 py-8 text-xs text-muted-foreground"><Loader2 className="size-3.5 animate-spin" />Loading scans…</p>
    if (!runs.length) return <p className="px-5 py-10 text-center text-sm text-muted-foreground">No scans yet.</p>
    return <div>
        <div className={cn(GRID, "h-9 px-4 sm:px-5 bg-muted/40")}>
            <span />
            <span className={cn(head, "text-left")}>Scan</span>
            <span className={cn(head, "hidden sm:block")} title="Unique configs after removing duplicates">Unique</span>
            <span className={head}>Tested</span>
            <span className={head}>Passed</span>
            <span className={cn(head, "hidden sm:block")}>Active</span>
            <span className={cn(head, "hidden sm:block")}>Took</span>
        </div>
        <ul className="divide-y">
            {runs.map(run => {
                const outcome = runOutcome(run)
                const failed = outcome === "failed"
                return <li key={run.id}>
                    <div className={cn(GRID, "min-h-11 px-4 sm:px-5 py-2 text-sm")}>
                        <RunIcon run={run} />
                        <div className="min-w-0">
                            <p className="truncate" title={fullDate(run.started_at)}>{relativeTime(run.started_at)}<span className="text-muted-foreground"> · {run.refresh === false ? "Rescan" : "Refresh"}</span></p>
                            {failed && <p className="flex items-center gap-2 text-[12px] text-red-400 min-w-0">
                                <span className="truncate">{errorHeadline(run.error ?? "")}</span>
                                <button type="button" aria-expanded={open === run.id} onClick={() => setOpen(open === run.id ? null : run.id)} className="shrink-0 text-muted-foreground hover:text-foreground hover:underline underline-offset-2">{open === run.id ? "Hide" : "Details"}</button>
                            </p>}
                            {outcome === "cancelled" && <p className="text-[12px] text-muted-foreground">Cancelled. Members unchanged.</p>}
                        </div>
                        <span className="hidden sm:block text-right font-mono text-xs tabular-nums text-muted-foreground" title={`${num(run.raw_count)} links, ${num(run.duplicate_count)} duplicates removed`}>{num(run.unique_count)}</span>
                        <span className="text-right font-mono text-xs tabular-nums text-muted-foreground">{num(run.tested)}</span>
                        <span className={cn("text-right font-mono text-xs tabular-nums", run.healthy > 0 ? "text-emerald-500" : "text-muted-foreground")}>{num(run.healthy)}</span>
                        <span className="hidden sm:block text-right font-mono text-xs tabular-nums">{num(run.active)}</span>
                        <span className="hidden sm:block text-right font-mono text-xs tabular-nums text-muted-foreground">{runDuration(run) || "—"}</span>
                    </div>
                    {failed && open === run.id && <pre className="mx-4 sm:mx-5 mb-3 max-h-48 overflow-auto whitespace-pre-wrap [overflow-wrap:anywhere] rounded-md bg-muted/40 p-2.5 font-mono text-[12px] leading-relaxed text-muted-foreground">{run.error}</pre>}
                </li>
            })}
        </ul>
    </div>
}
