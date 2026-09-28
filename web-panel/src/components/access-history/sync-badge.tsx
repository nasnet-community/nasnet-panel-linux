import { AlertTriangle, RefreshCw } from "lucide-react"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import type { Node } from "@/lib/types"
import { fmtAgo, fmtDayTime } from "./format"

/** A node counts as behind once its last hourly summary is this old. */
const STALE_MS = 2 * 60 * 60 * 1000

export type SyncState = "ok" | "behind" | "never"

export interface NodeSync {
    node: Node
    state: SyncState
    last?: string
}

/** Access-log freshness for the nodes that collect access logs. */
export function nodeSyncs(nodes: Node[], now: number = Date.now()): NodeSync[] {
    return nodes
        .filter(n => n.enable_access_log)
        .map(n => {
            const last = n.last_access_log_synced_at
            if (!last) return { node: n, state: "never" as const }
            return { node: n, last, state: now - new Date(last).getTime() > STALE_MS ? "behind" as const : "ok" as const }
        })
}

/** Short note for a node whose data stops early, e.g. "no data since Sep 9". */
export function staleNotes(syncs: NodeSync[]): Map<number, string> {
    const out = new Map<number, string>()
    for (const s of syncs) {
        if (s.state === "never") out.set(s.node.id, "never synced")
        if (s.state === "behind" && s.last) {
            out.set(s.node.id, `no data since ${new Date(s.last).toLocaleDateString([], { month: "short", day: "numeric" })}`)
        }
    }
    return out
}

export function syncDotClass(state: SyncState | undefined, online: boolean): string {
    if (!online) return "bg-red-500"
    if (state === "behind" || state === "never") return "bg-amber-500"
    return "bg-emerald-500"
}

export function syncTitle(s: NodeSync | undefined, online: boolean): string {
    const base = online ? "Online" : "Offline"
    if (!s) return `${base}, access log off`
    if (s.state === "never") return `${base}, never synced`
    return `${base}, last synced ${fmtDayTime(s.last!)}`
}

export function SyncBadge({ syncs }: { syncs: NodeSync[] }) {
    if (syncs.length === 0) return null
    const behind = syncs.filter(s => s.state !== "ok")
    const newestOk = syncs.filter(s => s.state === "ok").map(s => s.last!).sort().pop()
    const label = behind.length === 0
        ? <>Synced <span className="font-mono">{newestOk ? fmtAgo(newestOk) : ""}</span></>
        : behind.length === 1 ? `${behind[0].node.name} behind` : `${behind.length} nodes behind`
    return (
        <Popover>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    className={cn(
                        "inline-flex h-7 items-center gap-1.5 rounded-full px-2.5 text-xs transition-colors",
                        behind.length > 0
                            ? "border border-amber-500/40 bg-amber-500/10 text-amber-700 hover:bg-amber-500/15 dark:text-amber-300"
                            : "bg-secondary text-secondary-foreground hover:bg-secondary/80",
                    )}
                >
                    {behind.length > 0 ? <AlertTriangle className="size-3.5" /> : <RefreshCw className="size-3 text-muted-foreground" />}
                    {label}
                </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-[300px] space-y-2.5 p-4">
                <p className="text-sm font-semibold">How fresh the data is</p>
                <div className="grid grid-cols-[10px_auto_minmax(0,1fr)] items-center gap-x-2 gap-y-2 text-sm">
                    {syncs.map(s => (
                        <span key={s.node.id} className="contents">
                            <span className={cn("size-[7px] rounded-full", syncDotClass(s.state, s.node.is_online))} />
                            <span>{s.node.name}</span>
                            <span className="truncate text-muted-foreground">
                                {s.state === "never" ? "Never synced" : s.state === "ok" ? `Synced ${fmtAgo(s.last!)}` : `Last synced ${fmtDayTime(s.last!)}`}
                            </span>
                        </span>
                    ))}
                </div>
                {behind.length > 0 && (
                    <p className="border-t border-border pt-2 text-[13px] text-muted-foreground">
                        {behind.length === 1
                            ? `Nothing from ${behind[0].node.name} after its last sync is in these results.`
                            : "Nothing from these nodes after their last sync is in these results."}
                    </p>
                )}
            </PopoverContent>
        </Popover>
    )
}
