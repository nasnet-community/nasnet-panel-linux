import { useState, type ReactNode } from "react"
import { AlertTriangle, Check, Info } from "lucide-react"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { OutboundPool, PoolRun } from "@/lib/api/outbound-pools"
import { cn } from "@/lib/utils"
import { errorHeadline, fullDate, intervalLabel, relativeTime, retainedMembers, runDuration, runOutcome, type RunOutcome } from "./pool-display"

function Cell({ label, children, className }: { label: string, children: ReactNode, className?: string }) {
    return <div className={cn("min-w-0 px-4 py-3.5 sm:px-5", className)}>
        <p className="text-[12px] font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
        <div className="mt-1.5">{children}</div>
    </div>
}

// Slots when the limit is small enough to count, a plain meter past that.
function Capacity({ active, max }: { active: number, max: number }) {
    const fill = Math.min(active, max)
    if (max > 60) return <div className="mt-2 h-1.5 rounded-full bg-muted overflow-hidden"><div className="h-full rounded-full bg-emerald-500" style={{ width: `${(fill / max) * 100}%` }} /></div>
    return <div className="mt-2 flex gap-[2px]" aria-hidden>{Array.from({ length: max }, (_, i) => <span key={i} className={cn("h-1.5 flex-1 rounded-[1px]", i < fill ? "bg-emerald-500" : "bg-muted")} />)}</div>
}

const barTone: Record<RunOutcome, string> = {
    healthy: "bg-emerald-500/70 group-hover:bg-emerald-400",
    empty: "bg-muted-foreground/25 group-hover:bg-muted-foreground/50",
    failed: "bg-red-500/80 group-hover:bg-red-400",
    cancelled: "bg-muted-foreground/25 group-hover:bg-muted-foreground/50",
    running: "bg-sky-500 animate-pulse",
}

/** Passed configs per scan, oldest on the left. Failures show as short red bars. */
export function RunTrend({ runs, loading }: { runs: PoolRun[], loading?: boolean }) {
    const series = [...runs].reverse()
    const peak = Math.max(1, ...series.map(r => r.healthy))
    if (loading) return <div className="flex h-9 items-end gap-[3px]" aria-hidden>{Array.from({ length: 20 }, (_, i) => <span key={i} className="h-1/4 max-w-2.5 flex-1 rounded-[2px] bg-muted animate-pulse" />)}</div>
    if (!series.length) return <p className="text-sm text-muted-foreground">No scans yet</p>
    return <div className="flex h-9 items-end gap-[3px]" role="img" aria-label={`Passed configs over the last ${series.length} scans`}>
        {series.map(run => {
            const outcome = runOutcome(run)
            const height = outcome === "failed" ? 40 : outcome === "healthy" ? 18 + (run.healthy / peak) * 82 : 8
            return <Tooltip key={run.id}>
                <TooltipTrigger asChild>
                    <span className="group flex h-full max-w-2.5 flex-1 items-end cursor-default">
                        <span className={cn("w-full rounded-[2px] transition-colors", barTone[outcome])} style={{ height: `${height}%` }} />
                    </span>
                </TooltipTrigger>
                <TooltipContent className="text-xs">
                    <p className="font-medium">{relativeTime(run.started_at)} · {run.refresh === false ? "Rescan" : "Refresh"}{runDuration(run) && ` · ${runDuration(run)}`}</p>
                    <p className="text-muted-foreground mt-0.5 tabular-nums">
                        {outcome === "failed" ? errorHeadline(run.error ?? "") : outcome === "cancelled" ? "Cancelled" : `${run.healthy.toLocaleString()} of ${run.tested.toLocaleString()} passed · ${run.active} active`}
                    </p>
                </TooltipContent>
            </Tooltip>
        })}
    </div>
}

export function PoolHealth({ pool, runs, runsLoading }: { pool: OutboundPool, runs: PoolRun[], runsLoading: boolean }) {
    const busy = !!pool.active_run_id
    // Mid-run, last_run is the run in progress; the strip reports the one before it.
    const run = busy ? runs.find(r => r.id !== pool.active_run_id && runOutcome(r) !== "running") : pool.last_run
    const onFallback = !pool.active_count
    const schedule = pool.refresh_seconds === pool.rescan_seconds
        ? `every ${intervalLabel(pool.refresh_seconds)}`
        : `refresh ${intervalLabel(pool.refresh_seconds)} · rescan ${intervalLabel(pool.rescan_seconds)}`
    // gap-px over a border-coloured backdrop draws the dividers at any column count.
    return <div className="grid grid-cols-2 lg:grid-cols-[1.1fr_1.1fr_0.8fr_1.2fr] gap-px border-y bg-border [&>*]:bg-card">
        <Cell label="Members">
            <p className="text-xl font-semibold tabular-nums leading-none">{pool.active_count}<span className="text-sm font-normal text-muted-foreground"> / {pool.max_active}</span></p>
            <Capacity active={pool.active_count} max={pool.max_active} />
            <p className={cn("mt-1.5 text-xs truncate", onFallback ? "text-amber-500" : "text-muted-foreground")}>
                {onFallback ? <>On fallback <span className="font-mono">{pool.balancer.fallback_tag}</span></> : pool.active_count >= pool.max_active ? "Full" : `${(pool.max_active - pool.active_count).toLocaleString()} slots open`}
            </p>
        </Cell>
        <Cell label="Last scan">
            {!run ? <>
                <p className="text-xl font-semibold leading-none text-muted-foreground">Never</p>
                <p className="mt-2.5 text-xs text-muted-foreground">Run a scan to fill the subscription pool</p>
            </> : run.error && run.state !== "cancelled" ? <>
                <p className="text-xl font-semibold leading-none text-amber-500">Failed</p>
                <p className="mt-2.5 text-xs text-muted-foreground truncate" title={fullDate(run.started_at)}>{relativeTime(run.started_at)}</p>
            </> : <>
                <p className={cn("text-xl font-semibold tabular-nums leading-none", !run.healthy && run.state !== "cancelled" && "text-amber-500")}>{run.healthy.toLocaleString()}<span className="text-sm font-normal text-muted-foreground"> passed</span></p>
                <p className="mt-2.5 text-xs text-muted-foreground truncate tabular-nums" title={fullDate(run.finished_at || run.started_at)}>
                    of {run.tested.toLocaleString()} tested · {relativeTime(run.finished_at || run.started_at).toLowerCase()}
                </p>
            </>}
        </Cell>
        <Cell label="Next scan">
            <p className={cn("text-xl font-semibold leading-none", !pool.enabled && "text-muted-foreground")} title={pool.enabled ? fullDate(pool.next_scan_at) : undefined}>
                {busy ? "Running" : pool.enabled ? relativeTime(pool.next_scan_at).toLowerCase() : "Paused"}
            </p>
            <p className="mt-2.5 text-xs text-muted-foreground truncate">{pool.enabled ? schedule : "Schedule off"}</p>
        </Cell>
        <Cell label="Passed per scan">
            <RunTrend runs={runs} loading={runsLoading} />
            <p className="mt-1.5 text-xs text-muted-foreground">{runs.length ? `Last ${runs.length} ${runs.length === 1 ? "scan" : "scans"}` : "\u00a0"}</p>
        </Cell>
    </div>
}

/** Why "3 active" can sit next to "0 passed": members survive a few failed scans. */
export function RetainedNote({ pool }: { pool: OutboundPool }) {
    const kept = retainedMembers(pool)
    if (!kept) return null
    return <p className="flex items-start gap-2 text-xs text-muted-foreground">
        <Info className="size-3.5 mt-px shrink-0" />
        <span>{kept === pool.active_count ? kept === 1 ? "The member was" : "All members were" : `${kept} ${kept === 1 ? "member was" : "members were"}`} kept from earlier scans. A member leaves after {pool.failure_threshold} failed {pool.failure_threshold === 1 ? "scan" : "scans in a row"}.</span>
    </p>
}

const phases = [{ key: "fetching", label: "Fetch sources" }, { key: "scanning", label: "Scan" }, { key: "applying", label: "Publish" }]

export function RunProgress({ pool }: { pool: OutboundPool }) {
    const run = pool.last_run
    const state = run?.state ?? "queued"
    // A rescan skips the fetch; show it as done rather than pending.
    const current = state === "queued" ? -1 : Math.max(0, phases.findIndex(p => p.key === state))
    const progress = run?.unique_count ? Math.min(100, (run.tested / run.unique_count) * 100) : 0
    return <div aria-live="polite" className="space-y-2.5">
        <ol className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
            {phases.map((phase, index) => {
                const done = index < current || (state === "cancelling" && index === 0)
                const active = index === current && state !== "cancelling"
                return <li key={phase.key} className="flex items-center gap-2">
                    {index > 0 && <span className="h-px w-4 bg-border" aria-hidden />}
                    <span className={cn("inline-flex items-center gap-1.5", active ? "text-foreground font-medium" : done ? "text-muted-foreground" : "text-muted-foreground/50")}>
                        {done ? <Check className="size-3 text-emerald-500" /> : <span className={cn("size-1.5 rounded-full", active ? "bg-sky-500 animate-pulse" : "bg-muted-foreground/40")} />}
                        {phase.label}
                        {active && phase.key === "scanning" && run && <span className="font-normal text-muted-foreground tabular-nums">{run.tested.toLocaleString()} / {run.unique_count.toLocaleString()} · {run.healthy.toLocaleString()} passed</span>}
                    </span>
                </li>
            })}
            {state === "cancelling" && <li className="text-amber-500">Cancelling…</li>}
        </ol>
        <div role="progressbar" aria-label="Scan progress" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(progress)} className="h-1 rounded-full bg-muted overflow-hidden">
            <div className="h-full rounded-full bg-sky-500 transition-all" style={{ width: `${Math.max(2, progress)}%` }} />
        </div>
        <p className="text-[12px] text-muted-foreground">{pool.stop_when_full ? `Stops once ${pool.max_active} healthy configs are found.` : `Tests all ${(run?.unique_count ?? 0).toLocaleString()} configs before publishing.`}</p>
    </div>
}

export function RunError({ pool, run }: { pool: OutboundPool, run: PoolRun }) {
    const [open, setOpen] = useState(false)
    const error = run.error ?? ""
    return <div role="alert" className="rounded-lg border border-amber-500/25 bg-amber-500/5 px-3 py-2.5">
        <div className="flex items-start gap-2.5">
            <AlertTriangle className="size-4 shrink-0 text-amber-500 mt-px" />
            <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{errorHeadline(error)}</p>
                <p className="text-xs text-muted-foreground mt-0.5">{pool.active_count ? "The last applied members stay in use." : <>The balancer uses its fallback <span className="font-mono">{pool.balancer.fallback_tag}</span>.</>}</p>
            </div>
            <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="shrink-0 text-xs text-muted-foreground hover:text-foreground underline-offset-2 hover:underline">{open ? "Hide details" : "Details"}</button>
        </div>
        {open && <pre className="mt-2.5 max-h-48 overflow-auto whitespace-pre-wrap [overflow-wrap:anywhere] rounded-md bg-background/60 p-2.5 font-mono text-[12px] leading-relaxed text-muted-foreground">{error}</pre>}
    </div>
}
