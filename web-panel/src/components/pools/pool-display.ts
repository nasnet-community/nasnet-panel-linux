import type { OutboundPool, PoolRun } from "@/lib/api/outbound-pools"

export type PoolTone = "live" | "good" | "error" | "muted"

export function poolStatus(pool: OutboundPool): { label: string, tone: PoolTone } {
    if (pool.active_run_id) return { label: pool.last_run?.state === "applying" ? "Publishing" : pool.last_run?.state === "fetching" ? "Fetching sources" : pool.last_run?.state === "cancelling" ? "Cancelling" : "Scanning", tone: "live" }
    if (pool.last_run?.state === "cancelled") return { label: "Cancelled", tone: "muted" }
    if (pool.last_run?.error) return { label: "Needs attention", tone: "error" }
    if (!pool.enabled) return { label: "Paused", tone: "muted" }
    if (pool.active_count) return { label: "Ready", tone: "good" }
    if (pool.last_run) return { label: "On fallback", tone: "error" }
    return { label: "Awaiting scan", tone: "muted" }
}
export function intervalLabel(seconds: number) {
    if (seconds % 86400 === 0) return `${seconds / 86400}d`
    if (seconds % 3600 === 0) return `${seconds / 3600}h`
    return `${Math.round(seconds / 60)}m`
}
export function relativeTime(value?: string | null, now = Date.now()) {
    if (!value) return "Not yet"
    const delta = new Date(value).getTime() - now
    const minutes = Math.max(1, Math.round(Math.abs(delta) / 60000))
    const text = minutes >= 1440 ? `${Math.floor(minutes / 1440)}d` : minutes >= 60 ? `${Math.floor(minutes / 60)}h` : `${minutes}m`
    if (Math.abs(delta) < 60000) return delta > 0 ? "Soon" : "Just now"
    return delta > 0 ? `In ${text}` : `${text} ago`
}
export function sourceHost(url?: string) {
    try { return url ? new URL(url).hostname : "Pasted config list" } catch { return "Subscription" }
}
export const fullDate = (value?: string | null) => value ? new Date(value).toLocaleString() : "Never"

/** Wall time of a run: "42s", "6m 34s", "1h 3m". Empty while it has no end. */
export function runDuration(run: Pick<PoolRun, "started_at" | "finished_at">) {
    if (!run.finished_at) return ""
    const seconds = Math.max(0, Math.round((new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()) / 1000))
    if (seconds < 60) return `${seconds}s`
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}

/** One line for a run error; the full text stays behind a disclosure. */
export function errorHeadline(error: string) {
    if (error.includes("updated agent")) return "Agent update required"
    if (error.includes("config validation failed")) return "Xray rejected the published config"
    const head = error.split(": ")[0].trim()
    const line = head.length > 80 ? `${head.slice(0, 77)}…` : head
    return line.charAt(0).toUpperCase() + line.slice(1)
}

export type RunOutcome = "running" | "failed" | "cancelled" | "empty" | "healthy"
export function runOutcome(run: PoolRun): RunOutcome {
    if (["fetching", "scanning", "applying", "cancelling", "queued"].includes(run.state)) return "running"
    if (run.state === "cancelled") return "cancelled"
    if (run.error) return "failed"
    return run.healthy > 0 ? "healthy" : "empty"
}

/**
 * Members the last scan did not pass but the pool kept, because they have not
 * yet failed failure_threshold scans in a row. Zero when every member passed.
 */
export function retainedMembers(pool: OutboundPool) {
    const run = pool.last_run
    if (!run || pool.active_run_id || run.error || run.state === "cancelled") return 0
    return Math.max(0, pool.active_count - run.healthy)
}
