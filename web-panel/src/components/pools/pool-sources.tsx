import { AlertTriangle, FileText, Link2, Settings2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { OutboundPool } from "@/lib/api/outbound-pools"
import { fullDate, intervalLabel, relativeTime, sourceHost } from "./pool-display"

const strategies: Record<string, string> = { leastping: "Lowest latency", random: "Random", roundrobin: "Round robin", leastload: "Least load" }
export const strategyLabel = (strategy?: string) => strategies[strategy ?? ""] ?? strategy ?? ""

export function PoolSources({ pool, onEdit, editDisabled }: { pool: OutboundPool, onEdit: () => void, editDisabled: boolean }) {
    const settings: [string, string][] = [
        ["Scan mode", pool.stop_when_full ? `Stop at ${pool.max_active} healthy` : "Test every config"],
        ["Scan threads", pool.concurrency.toLocaleString()],
        ["Max latency", `${pool.max_delay_ms.toLocaleString()} ms`],
        ["Retries", String(pool.retries)],
        ["Drop member after", `${pool.failure_threshold} failed ${pool.failure_threshold === 1 ? "scan" : "scans"}`],
        ["Refresh · rescan", `${intervalLabel(pool.refresh_seconds)} · ${intervalLabel(pool.rescan_seconds)}`],
        ["Balancer strategy", strategyLabel(pool.balancer.strategy)],
        ["Probe URLs", pool.test_urls.map(sourceHost).join(", ")],
    ]
    return <div>
        <ul className="divide-y">
            {pool.sources.map((source, index) => {
                const Icon = source.kind === "url" ? Link2 : FileText
                return <li key={source.id ?? index} className="flex items-center gap-3 px-4 sm:px-5 py-3">
                    <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted/60"><Icon className="size-4 text-muted-foreground" /></span>
                    <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium truncate">{source.name || `Source ${index + 1}`}</p>
                        <p className="text-[12px] text-muted-foreground truncate">
                            <span className="font-mono">{source.kind === "url" ? sourceHost(source.url) : "Pasted list"}</span>
                            {" · "}<span className="tabular-nums">{(source.link_count ?? 0).toLocaleString()}</span> links
                        </p>
                        {source.last_error && <p className="mt-1 flex items-start gap-1.5 text-xs text-amber-500"><AlertTriangle className="size-3 mt-0.5 shrink-0" /><span className="break-words">{source.last_error}{source.fetched_at && " · using the previous copy"}</span></p>}
                    </div>
                    <span className="shrink-0 text-xs text-muted-foreground tabular-nums" title={fullDate(source.fetched_at)}>{source.kind === "url" ? source.fetched_at ? `fetched ${relativeTime(source.fetched_at).toLowerCase()}` : "not fetched" : ""}</span>
                </li>
            })}
        </ul>
        <div className="border-t bg-muted/20 px-4 sm:px-5 py-4">
            <div className="flex items-center justify-between gap-3">
                <p className="text-[12px] font-medium uppercase tracking-wide text-muted-foreground">Scan settings</p>
                <Button size="sm" variant="ghost" className="h-7 -mr-2 text-xs" disabled={editDisabled} onClick={onEdit}><Settings2 className="size-3.5 mr-1.5" />Edit</Button>
            </div>
            <dl className="mt-2 grid grid-cols-2 sm:grid-cols-4 gap-x-6 gap-y-3">
                {settings.map(([label, value]) => <div key={label} className="min-w-0">
                    <dt className="text-[12px] text-muted-foreground">{label}</dt>
                    <dd className="text-sm truncate tabular-nums" title={value}>{value}</dd>
                </div>)}
            </dl>
        </div>
    </div>
}
