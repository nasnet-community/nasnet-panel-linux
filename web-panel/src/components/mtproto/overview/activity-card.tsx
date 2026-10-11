import { Activity, Ban, CheckCircle2, Globe, Link2, Link2Off, Loader2, Play, RefreshCw, Settings2, Square, TriangleAlert, Wrench, XCircle } from "lucide-react"
import { useMtprotoEvents } from "@/lib/queries"
import type { MtprotoEvent } from "@/lib/api/mtproto"
import { cn, formatRelativeTime } from "@/lib/utils"
import { T } from "@/components/node/node-ui"
import { OverviewCard } from "@/components/node/overview/overview-card"

const JOB: Record<string, string> = { install: "Install", repair: "Repair", update: "Update", rotate: "Key rotation", "link-port": "Link port change", address: "Address change" }

/** How a feed entry reads to a person. */
export function eventLabel(e: MtprotoEvent): string {
    const job = JOB[e.subject ?? ""] ?? "Job"
    switch (e.kind) {
        case "link_up": return "Link connected"
        case "link_down": return "Link lost"
        case "proxy_up": return "Proxy started"
        case "proxy_down": return "Proxy stopped"
        case "proxy_crash_loop": return "Proxy keeps crashing"
        case "cover_ok": return "Cover site working"
        case "cover_failed": return "Cover site check failed"
        case "applied": return "Settings applied"
        case "push_failed": return "Settings didn't reach the agent"
        case "refused": return "Refused destinations outside Telegram"
        case "egress_failed": return "Couldn't reach Telegram"
        case "job_ok": return `${job} finished`
        case "job_failed": return `${job} failed`
        default: return e.kind.replace(/_/g, " ")
    }
}

function EventIcon({ e }: { e: MtprotoEvent }) {
    const cls = cn("size-3.5", e.level === "error" ? "text-red-500" : e.level === "warn" ? "text-amber-500" : "text-muted-foreground")
    switch (e.kind) {
        case "link_up": return <Link2 className={cls} />
        case "link_down": return <Link2Off className={cls} />
        case "proxy_up": return <Play className={cls} />
        case "proxy_down": return <Square className={cls} />
        case "proxy_crash_loop": return <TriangleAlert className={cls} />
        case "cover_ok": case "cover_failed": return <Globe className={cls} />
        case "applied": return <Settings2 className={cls} />
        case "push_failed": return <XCircle className={cls} />
        case "refused": return <Ban className={cls} />
        case "egress_failed": return <TriangleAlert className={cls} />
        case "job_ok": return e.subject === "update" ? <RefreshCw className={cls} /> : <CheckCircle2 className={cls} />
        case "job_failed": return <Wrench className={cls} />
        default: return <Activity className={cls} />
    }
}

/** What happened on the node since the panel started, newest first. */
export function ActivityCard({ nodeId, className }: { nodeId: number; className?: string }) {
    const q = useMtprotoEvents(nodeId, 50)
    const events = q.data?.events ?? []
    return (
        <OverviewCard glow="purple" title="Activity" aside={q.data?.since ? <span className={T.meta}>since {formatRelativeTime(q.data.since)}</span> : undefined} className={className}>
            {q.isLoading ? (
                <div className="flex min-h-[140px] items-center justify-center"><Loader2 className="h-5 w-5 animate-spin text-muted-foreground/50" /></div>
            ) : q.isError ? (
                <p className={cn(T.meta, "text-red-500")}>{(q.error as Error).message}</p>
            ) : events.length === 0 ? (
                <p className={cn(T.meta, "py-6 text-center")}>Nothing has happened since the panel started.</p>
            ) : (
                <ol className="-mx-1 max-h-[340px] overflow-y-auto pr-1" aria-label="Recent activity">
                    {events.map((e, i) => (
                        <li key={`${e.at}-${e.kind}-${i}`} className="flex items-start gap-2.5 rounded-lg px-1 py-1.5">
                            <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full bg-muted/40"><EventIcon e={e} /></span>
                            <div className="min-w-0 flex-1">
                                <div className="flex items-baseline justify-between gap-2">
                                    <p className="truncate text-xs font-medium leading-5">
                                        {eventLabel(e)}
                                        {(e.count ?? 0) > 1 && <span className="ml-1.5 rounded bg-muted px-1 text-[11px] tabular-nums text-muted-foreground">×{e.count}</span>}
                                    </p>
                                    <time className={cn(T.meta, "shrink-0 tabular-nums")} dateTime={e.at} title={new Date(e.at).toLocaleString()}>{formatRelativeTime(e.at)}</time>
                                </div>
                                {e.detail && <p className="break-words text-[12px] leading-4 text-muted-foreground">{e.detail}</p>}
                            </div>
                        </li>
                    ))}
                </ol>
            )}
        </OverviewCard>
    )
}
