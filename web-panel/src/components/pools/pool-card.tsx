import { useState } from "react"
import { ArrowRight, ChevronDown, Loader2, MoreHorizontal, Pause, Play, RefreshCw, Settings2, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import type { OutboundPool } from "@/lib/api/outbound-pools"
import type { Outbound } from "@/lib/types"
import { cn } from "@/lib/utils"
import { poolStatus, retainedMembers, type PoolTone } from "./pool-display"
import { PoolHealth, RetainedNote, RunError, RunProgress } from "./pool-health"
import { PoolMembers } from "./pool-members"
import { PoolCandidates } from "./pool-candidates"
import { PoolSources, strategyLabel } from "./pool-sources"
import { PoolHistory, usePoolRuns } from "./pool-history"

const tones: Record<PoolTone, string> = {
    live: "text-sky-400 bg-sky-500/10 border-sky-500/20",
    good: "text-emerald-400 bg-emerald-500/10 border-emerald-500/20",
    error: "text-amber-500 bg-amber-500/10 border-amber-500/20",
    muted: "text-muted-foreground bg-muted border-border",
}
type Tab = "members" | "candidates" | "sources" | "history"

interface PoolCardProps {
    nodeId: number
    pool: OutboundPool
    members: Outbound[]
    defaultOpen: boolean
    pending: boolean
    onRun: (refresh: boolean) => void
    onCancel: () => void
    onEdit: () => void
    onToggleSchedule: () => void
    onDelete: () => void
}

export function PoolCard({ nodeId, pool, members, defaultOpen, pending, onRun, onCancel, onEdit, onToggleSchedule, onDelete }: PoolCardProps) {
    const [tab, setTab] = useState<Tab>("members")
    const [open, setOpen] = useState(defaultOpen)
    const runs = usePoolRuns(nodeId, pool)
    const status = poolStatus(pool)
    const run = pool.last_run
    const busy = !!pool.active_run_id
    const failed = !busy && !!run?.error && run.state !== "cancelled"
    const notes = busy || failed || retainedMembers(pool) > 0
    const tabs: { id: Tab, label: string, count?: number }[] = [
        { id: "members", label: "Members", count: pool.active_count },
        { id: "candidates", label: "Candidates", count: pool.candidate_count },
        { id: "sources", label: "Sources", count: pool.sources.length },
        { id: "history", label: "History" },
    ]
    const select = (id: Tab) => { setTab(id); setOpen(true) }

    return <Card className="overflow-hidden py-0 gap-0">
        {/* Phones stack the actions under the name, stretched to the card width. */}
        <div className="flex flex-col gap-3 px-4 sm:flex-row sm:items-start sm:gap-4 sm:px-5 pt-4 pb-3.5">
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 flex-wrap">
                    <h3 className="font-semibold tracking-tight text-base">{pool.name}</h3>
                    <span className={cn("inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[12px] font-medium", tones[status.tone])}>
                        {busy ? <Loader2 className="size-3 animate-spin" /> : <span className="size-1.5 rounded-full bg-current" />}{status.label}
                    </span>
                </div>
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground mt-1.5 min-w-0">
                    <span className="truncate">balancer <span className="font-mono text-foreground/80">{pool.balancer.tag}</span>{pool.balancer.strategy && ` · ${strategyLabel(pool.balancer.strategy).toLowerCase()}`}</span>
                    <ArrowRight className="size-3 shrink-0" />
                    <span className="truncate">fallback <span className="font-mono text-foreground/80">{pool.balancer.fallback_tag}</span></span>
                </p>
            </div>
            <div className="flex items-center gap-2">
                {busy ? <Button variant="outline" size="sm" className="max-sm:flex-1" disabled={pending || run?.state === "cancelling"} onClick={onCancel}>{run?.state === "cancelling" ? "Cancelling…" : "Cancel run"}</Button> : <>
                    <Button variant="outline" size="sm" className="max-sm:flex-1" disabled={pending || !pool.candidate_count} onClick={() => onRun(false)} title="Test the saved configs again without fetching sources">Rescan</Button>
                    <Button size="sm" className="max-sm:flex-1" disabled={pending} onClick={() => onRun(true)}><RefreshCw className="size-3.5 mr-1.5" />{failed ? "Retry scan" : "Refresh & scan"}</Button>
                </>}
                <DropdownMenu>
                    <DropdownMenuTrigger asChild><Button size="icon" variant="ghost" className="size-8" aria-label={`Actions for ${pool.name}`}><MoreHorizontal className="size-4" /></Button></DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                        <DropdownMenuItem disabled={busy || pending} onSelect={onEdit}><Settings2 className="size-4 mr-2" />Edit subscription pool</DropdownMenuItem>
                        <DropdownMenuItem disabled={busy || pending} onSelect={onToggleSchedule}>{pool.enabled ? <Pause className="size-4 mr-2" /> : <Play className="size-4 mr-2" />}{pool.enabled ? "Pause schedule" : "Resume schedule"}</DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem disabled={busy || pending} className="text-destructive" onSelect={onDelete}><Trash2 className="size-4 mr-2" />Delete subscription pool</DropdownMenuItem>
                    </DropdownMenuContent>
                </DropdownMenu>
            </div>
        </div>

        <PoolHealth pool={pool} runs={runs.data ?? []} runsLoading={runs.isLoading} />

        {notes && <div className="space-y-3 px-4 sm:px-5 py-3">
            {busy && <RunProgress pool={pool} />}
            {failed && run && <RunError pool={pool} run={run} />}
            {!busy && !failed && <RetainedNote pool={pool} />}
        </div>}

        <div className={cn("flex items-center gap-1 px-2 sm:px-3", notes && "border-t", open && "border-b")}>
            <div role="tablist" aria-label={`${pool.name} details`} className="flex min-w-0 flex-1 gap-1 overflow-x-auto">
                {tabs.map(t => {
                    const on = open && tab === t.id
                    return <button key={t.id} type="button" role="tab" aria-selected={on} onClick={() => on ? setOpen(false) : select(t.id)}
                        className={cn("relative flex shrink-0 items-center gap-1.5 px-2.5 py-3 text-xs transition-colors",
                            on ? "text-foreground font-medium after:absolute after:inset-x-2.5 after:bottom-0 after:h-0.5 after:rounded-full after:bg-foreground" : "text-muted-foreground hover:text-foreground")}>
                        {t.label}
                        {t.count !== undefined && <span className={cn("rounded px-1 font-mono text-[11px] tabular-nums", on ? "bg-foreground/10" : "bg-muted")}>{t.count.toLocaleString()}</span>}
                    </button>
                })}
            </div>
            <Button variant="ghost" size="icon" className="size-8 shrink-0" aria-expanded={open} aria-label={open ? "Hide details" : "Show details"} onClick={() => setOpen(!open)}>
                <ChevronDown className={cn("size-4 transition-transform", open && "rotate-180")} />
            </Button>
        </div>

        {open && <div role="tabpanel">
            {tab === "members" && <PoolMembers pool={pool} members={members} />}
            {tab === "candidates" && <PoolCandidates nodeId={nodeId} pool={pool} />}
            {tab === "sources" && <PoolSources pool={pool} onEdit={onEdit} editDisabled={busy || pending} />}
            {tab === "history" && <PoolHistory runs={runs.data ?? []} loading={runs.isLoading} error={runs.error} />}
        </div>}
    </Card>
}
