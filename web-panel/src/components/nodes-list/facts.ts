import type { Node, NodeDigest, NodeStats } from "@/lib/types"
import type { MtprotoNodeSummary } from "@/lib/api/mtproto"
import { formatBytes, formatSpeed } from "@/lib/utils"
import { isLiveReading } from "@/components/node/overview/overview-helpers"
import { proxyState, type ProxyState } from "@/components/mtproto/overview/mtproto-health"
import { isLiteNode, isLocalNode } from "@/lib/node-kind"

// What the Nodes list knows about one node, merged from the node row, its
// live (or last known) stats and the panel's digest. Pure: no hooks, no JSX.

export type NodeStatus = "online" | "down" | "maintenance" | "pending"

export type IssueTone = "crit" | "warn" | "info"
export type IssueKind = "xray" | "memory" | "disk" | "cpu" | "drift" | "agent" | "proxy" | "link"

export interface NodeIssue {
    kind: IssueKind
    tone: IssueTone
    text: string
    hint?: string
}

export interface NodeView {
    node: Node
    status: NodeStatus
    /** Live readings for an online node, the last known ones otherwise. */
    stats?: NodeStats
    digest?: NodeDigest
    /** An MTProto node's readings; it has no stats sweep. */
    mtproto?: MtprotoNodeSummary
    /** The built-in node: this router, no agent. */
    local: boolean
    lite: boolean
    /** Online, but the newest reading is older than the live window. */
    stale: boolean
    issues: NodeIssue[]
    /** 0 down, 1 never connected, 2 needs a look, 3 healthy, 4 maintenance. */
    group: number
    agentDue: boolean
}

/** The node Overview tab's warning and critical levels. */
export const THRESHOLDS = {
    cpu: [85, 95],
    memory: [90, 97],
    disk: [85, 95],
} as const

export type Tone = "" | "warn" | "crit"

export function usageTone(kind: keyof typeof THRESHOLDS, pct: number | undefined): Tone {
    if (pct === undefined) return ""
    const [warn, crit] = THRESHOLDS[kind]
    return pct >= crit ? "crit" : pct >= warn ? "warn" : ""
}

/** CPU must stay past its warning level this long before it counts. */
const CPU_HOT_MINUTES = 5

export function isPending(node: Node): boolean {
    return !node.is_online && new Date(node.last_check).getFullYear() < 2000
}

export function statusOf(node: Node): NodeStatus {
    if (node.maintenance_mode) return "maintenance"
    if (isPending(node)) return "pending"
    return node.is_online ? "online" : "down"
}

function liteIssues(s: MtprotoNodeSummary | undefined): NodeIssue[] {
    const out: NodeIssue[] = []
    if (!s) return out
    if (s.link_conns > 0 && s.link_up === 0) {
        out.push({ kind: "link", tone: "crit", text: "Link down", hint: "The panel can't reach the MTProto agent." })
    } else if (s.link_up < s.link_conns) {
        out.push({ kind: "link", tone: "warn", text: `Link ${s.link_up} of ${s.link_conns} up` })
    }
    const state = liteProxy(s)
    if (state === "crashing" || state === "stopped") {
        out.push({ kind: "proxy", tone: "crit", text: state === "crashing" ? "Proxy crashing" : "Proxy not running", hint: "Users on this node can't connect until the proxy runs." })
    } else if (state === "held") {
        out.push({ kind: "proxy", tone: "warn", text: "Proxy held", hint: s.proxy_held })
    }
    if (s.agent_update) out.push({ kind: "agent", tone: "info", text: "Agent update ready" })
    return out
}

function issuesOf(node: Node, stats: NodeStats | undefined, digest: NodeDigest | undefined, agentDue: boolean, mtproto: MtprotoNodeSummary | undefined): NodeIssue[] {
    const out: NodeIssue[] = []
    if (!node.is_online || node.maintenance_mode) return out
    if (isLiteNode(node)) return liteIssues(mtproto)
    if (node.xray_stopped || stats?.xray_status === "stopped") {
        out.push({ kind: "xray", tone: "crit", text: "Xray not running", hint: "Users on this node can't connect until Xray starts." })
    }
    const mem = usageTone("memory", stats?.memory_percent)
    if (mem) out.push({ kind: "memory", tone: mem, text: `Memory ${Math.round(stats!.memory_percent)}%` })
    const disk = usageTone("disk", stats?.disk_percent)
    if (disk) out.push({ kind: "disk", tone: disk, text: `Disk ${Math.round(stats!.disk_percent)}% full` })
    // The run is up to a couple of minutes old; the live reading must agree.
    const hot = hotMinutes(digest?.cpu_recent)
    const cpuNow = stats?.cpu_percent ?? digest?.cpu_recent[digest.cpu_recent.length - 1] ?? 0
    if (hot >= CPU_HOT_MINUTES && cpuNow >= THRESHOLDS.cpu[0]) {
        const now = cpuNow
        out.push({ kind: "cpu", tone: "warn", text: `CPU ${Math.round(now)}% for ${hot}${hot >= 10 ? "+" : ""} min` })
    }
    if (digest?.config_state === "drifted") {
        out.push({ kind: "drift", tone: "warn", text: "Config out of sync", hint: "The node runs a different config than the panel last pushed. Push config to fix it." })
    }
    if (agentDue) out.push({ kind: "agent", tone: "info", text: "Agent update ready" })
    return out
}

/** Minutes, counted back from the newest, that CPU stayed past its warning level. */
function hotMinutes(recent: number[] | undefined): number {
    if (!recent?.length) return 0
    let n = 0
    for (let i = recent.length - 1; i >= 0 && recent[i] >= THRESHOLDS.cpu[0]; i--) n++
    return n
}

export const problemsOf = (v: NodeView) => v.issues.filter((i) => i.tone !== "info")

export function worstTone(v: NodeView): Tone {
    const p = problemsOf(v)
    return p.some((i) => i.tone === "crit") ? "crit" : p.length ? "warn" : ""
}

export function buildView(node: Node, live: NodeStats | undefined, digest: NodeDigest | undefined, agentDue: boolean, now: number, mtproto?: MtprotoNodeSummary): NodeView {
    const status = statusOf(node)
    const lite = isLiteNode(node)
    const stats = lite ? undefined : node.is_online ? live : (digest?.last ?? live)
    const issues = issuesOf(node, stats, digest, agentDue, mtproto)
    const problems = issues.some((i) => i.tone !== "info")
    const group = status === "down" ? 0 : status === "pending" ? 1 : status === "maintenance" ? 4 : problems ? 2 : 3
    return {
        node,
        status,
        stats,
        digest,
        mtproto,
        local: isLocalNode(node),
        lite,
        stale: status === "online" && !!stats && !isLiveReading(stats, now),
        issues,
        group,
        agentDue: agentDue && !lite,
    }
}

/** Users online now; an MTProto node counts its own. */
export function usersOnline(v: NodeView): number | undefined {
    if (v.lite) return v.mtproto ? v.mtproto.active_users ?? 0 : undefined
    return v.stats ? v.stats.online_users ?? 0 : undefined
}

/** Out of how many: active accounts, or an MTProto node's users. */
export function usersOf(v: NodeView): number | undefined {
    return v.lite ? v.mtproto?.users : v.stats?.active_accounts
}

/** The current down and up rate, bytes/s. */
export function speedOf(v: NodeView): { down: number; up: number } | undefined {
    if (v.lite) return v.mtproto ? { down: v.mtproto.down_bps, up: v.mtproto.up_bps } : undefined
    return v.stats ? { down: v.stats.down_speed ?? 0, up: v.stats.up_speed ?? 0 } : undefined
}

export function agentVersionOf(v: NodeView): string {
    return v.lite ? v.mtproto?.agent_version ?? "" : v.stats?.agent_version || v.node.agent_version || ""
}

export function liteProxy(s: MtprotoNodeSummary): ProxyState {
    return proxyState({ running: s.proxy_running, crash_loop: s.crash_loop, held: s.proxy_held }, s.users, s.link_up > 0, s.proxy_stopped)
}

// ─── Order ──────────────────────────────────────────────────────────

export type SortKey = "status" | "name" | "users" | "today" | "now" | "cpu" | "memory" | "disk" | "uptime" | "agent"
export type SortDir = "asc" | "desc"
export interface Sort { key: SortKey; dir: SortDir }

export const DEFAULT_SORT: Sort = { key: "status", dir: "asc" }

/** Names sort up; numbers start with the biggest. */
export const firstDir = (key: SortKey): SortDir => (key === "status" || key === "name" || key === "agent" ? "asc" : "desc")

const byName = (a: NodeView, b: NodeView) => a.node.name.localeCompare(b.node.name)

function sortValue(v: NodeView, key: SortKey): number | string {
    switch (key) {
        case "users": return usersOnline(v) ?? -1
        case "today": return v.digest ? v.digest.today.uplink + v.digest.today.downlink : -1
        case "now": {
            const sp = v.status === "online" ? speedOf(v) : undefined
            return sp ? sp.up + sp.down : -1
        }
        case "cpu": return v.stats?.cpu_percent ?? -1
        case "memory": return v.stats?.memory_percent ?? -1
        case "disk": return v.stats?.disk_percent ?? -1
        case "uptime": return v.digest?.uptime.days.length ? v.digest.uptime.pct : -1
        case "agent": return agentVersionOf(v)
        default: return v.node.name.toLowerCase()
    }
}

/** Problems first, then this router, then by name: a card moves only when its group changes. */
export function orderViews(views: NodeView[], sort: Sort = DEFAULT_SORT): NodeView[] {
    const out = [...views]
    if (sort.key === "status") {
        out.sort((a, b) => (a.group - b.group) * (sort.dir === "asc" ? 1 : -1) || Number(b.local) - Number(a.local) || byName(a, b))
        return out
    }
    const sign = sort.dir === "asc" ? 1 : -1
    out.sort((a, b) => {
        const x = sortValue(a, sort.key), y = sortValue(b, sort.key)
        const c = typeof x === "string" ? x.localeCompare(String(y), undefined, { numeric: true }) : x - (y as number)
        return c * sign || byName(a, b)
    })
    return out
}

// ─── Formatting ─────────────────────────────────────────────────────

/** "2d 19h", "4h", "33m". */
export function shortDuration(ms: number): string {
    const m = Math.max(0, Math.floor(ms / 60000))
    if (m < 60) return `${m}m`
    const h = Math.floor(m / 60)
    if (h < 24) return m % 60 ? `${h}h ${m % 60}m` : `${h}h`
    const d = Math.floor(h / 24)
    return h % 24 ? `${d}d ${h % 24}h` : `${d}d`
}

export function clockTime(t: number | string): string {
    return new Date(t).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" })
}

/** "47.7 GB" → ["47.7", "GB"]; three digits at most, so "534 KB/s", not "534.4". */
export function splitBytes(bytes: number, rate = false): [string, string] {
    const s = rate ? formatSpeed(bytes) : formatBytes(bytes, 1)
    const i = s.indexOf(" ")
    if (i < 0) return [s, ""]
    const n = parseFloat(s)
    return [n >= 100 ? String(Math.round(n)) : s.slice(0, i), s.slice(i + 1)]
}

/** "↓41.4 ↑6.4 GB", or "↓1 GB ↑287 MB" when the units differ. */
export function downUp(down: number, up: number, rate = false): string {
    const [dv, du] = splitBytes(down, rate)
    const [uv, uu] = splitBytes(up, rate)
    return du === uu ? `↓${dv} ↑${uv} ${du}` : `↓${dv} ${du} ↑${uv} ${uu}`
}

export function uptimeText(pct: number): string {
    if (pct >= 99.995) return "100%"
    return pct >= 99.9 ? `${pct.toFixed(2)}%` : `${pct.toFixed(1)}%`
}

export const outagesText = (n: number) => `${n} outage${n === 1 ? "" : "s"}`
