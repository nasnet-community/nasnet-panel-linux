import { api, type ApiResponse } from "@/lib/api"
import { getApiBaseUrl } from "@/lib/config"

const BASE = "/api/v1/mtproto"

// ---- Types (mirror internal/mtprotonode/handler.go) ----

export type MtprotoJobState = "running" | "ok" | "failed"
export type MtprotoStepState = "running" | "ok" | "failed"

export interface MtprotoStep {
    name: string
    state: MtprotoStepState
    detail?: string
}

export interface MtprotoJob {
    id: string
    kind: string
    state: MtprotoJobState
    steps: MtprotoStep[]
    error?: string
    node_id?: number
    started_at: string
    finished_at?: string
}

export interface MtprotoConn {
    name: string
    up: boolean
    since?: string
    rtt_ms: number
    streams: number
    error?: string
    /** the node's clock minus the panel's, from the last handshake */
    skew_ms?: number
}

export interface MtprotoLink {
    up: number
    conns: MtprotoConn[] | null
    streams: number
    relayed: number
    dial_failures: number
    violations: number
    bytes_to_dc: number
    bytes_from_dc: number
}

export interface MtprotoAgentStatus {
    agent_version: string
    /** SHA-256 of the running binary: dev builds share a version */
    agent_build?: string
    telemt_version: string
    state_rev: string
    uptime_sec: number
    rss_bytes: number
    warm: boolean
    boot_id?: string
    /** held: why the agent keeps the proxy stopped */
    telemt: { running: boolean; restarts: number; uptime_sec: number; last_exit?: string; crash_loop?: boolean; held?: string }
    /** restored: times the rules went missing and were put back */
    capture: { rules: boolean; active: number; rejected: number; restored?: number }
    disguise: { domain: string; ok: boolean; checked_at?: string; err?: string }
}

export interface MtprotoNode {
    node_id: number
    agent_id: string
    hub_port: number
    capture_port: number
    ssh_port: number
    ssh_user: string
    host_key: string
    disguise: string
    max_streams: number
    /** the operator stopped the proxy; it stays off until started */
    proxy_stopped?: boolean
    arch: string
    agent_version: string
    telemt_version: string
    last_seen_at?: string
    /** when the link keys last changed; absent means the ones from the install */
    keys_rotated_at?: string
    running: boolean
    link?: MtprotoLink
    agent?: MtprotoAgentStatus
    healthy: boolean
    /** the panel's last periodic check of the cover site */
    disguise_check?: DisguiseResult & { checked_at: string }
    desired_rev?: string
    applied_rev?: string
    last_push_error?: string
    /** why the tunnel's cover site isn't in use yet; users still update */
    cover_pending?: string
    /** when the node was installed */
    installed_at?: string
    /** users the panel last handed this node; absent until the first push */
    users?: number
    /** users who moved traffic in the last usage poll; absent while unknown */
    active_users?: number
    /** bytes per second to and from Telegram, over the last status interval */
    throughput?: MtprotoThroughput
    /** what this panel installs; agent_build is the binary it would push here */
    latest?: { agent: string; telemt: string; agent_build?: string }
    /** the node runs something other than what this panel installs */
    updates?: { agent: boolean; telemt: boolean }
}

export interface MtprotoThroughput {
    /** to Telegram (what users send) */
    up_bps: number
    /** from Telegram (what users receive) */
    down_bps: number
}

/** One MTProto node as the nodes list shows it. */
export interface MtprotoNodeSummary {
    node_id: number
    running: boolean
    healthy: boolean
    /** link connections up, of link_conns */
    link_up: number
    link_conns: number
    /** average round trip of the connections that are up; absent with none */
    rtt_ms?: number
    streams: number
    max_streams: number
    proxy_running: boolean
    /** the operator stopped it */
    proxy_stopped?: boolean
    /** why the agent keeps the proxy stopped */
    proxy_held?: string
    crash_loop?: boolean
    users?: number
    active_users?: number
    up_bps: number
    down_bps: number
    agent_version: string
    latest_agent: string
    /** the node runs another agent build than this panel installs */
    agent_update?: boolean
}

export type MtprotoEventKind =
    | "link_up" | "link_down"
    | "proxy_up" | "proxy_down" | "proxy_crash_loop"
    | "cover_ok" | "cover_failed"
    | "applied" | "push_failed"
    | "refused" | "egress_failed"
    | "job_ok" | "job_failed"

/** One thing that happened on a node, newest first in the feed. */
export interface MtprotoEvent {
    at: string
    kind: MtprotoEventKind | string
    level: "info" | "warn" | "error"
    /** free text: a reason, an error, a version */
    detail?: string
    /** how many times it happened in this burst (refused, egress_failed) */
    count?: number
    /** job_ok / job_failed: install | repair | update | rotate */
    subject?: string
}

export interface MtprotoEvents {
    /** the feed is kept in memory: nothing before this */
    since: string
    events: MtprotoEvent[]
}

/** What the Settings tab may change. */
export interface MtprotoSettingsBody {
    max_streams?: number
    ssh_port?: number
    ssh_user?: string
}

/** The stream limit the agent's memory budget was measured at. */
export const MAX_STREAM_LIMIT = 5000
export const MIN_STREAM_LIMIT = 100

export interface DisguiseResult {
    ok: boolean
    tls13: boolean
    x25519: boolean
    h2: boolean
    cert_ok: boolean
    addrs?: string[]
    domestic?: boolean
    latency_ms: number
    err?: string
}

export interface MtprotoAllowlist {
    prefixes: number
    placeholder: boolean
    last_error?: string
    refreshed_at?: string
    /** where the daily refresh fetches from; empty when it's turned off */
    url?: string
}

export interface MtprotoLogin {
    host?: string
    ssh_port?: number
    ssh_user?: string
    ssh_password?: string
    ssh_key?: string
    ssh_passphrase?: string
    host_key?: string
}

export interface InstallMtprotoBody extends MtprotoLogin {
    name: string
    host: string
    host_key: string
    disguise: string
    max_streams?: number
    hub_port?: number
    capture_port?: number
    allow_unverified_disguise?: boolean
}

// ---- Calls ----

export function probeMtprotoHost(body: { host: string; ssh_port?: number; ssh_user?: string }): Promise<ApiResponse<{ fingerprint: string }>> {
    return api.post<{ fingerprint: string }>(`${BASE}/probe`, body, 30000)
}

/** The node signs in and tests the cover site before the job starts, so this can take a while. */
export function installMtprotoNode(body: InstallMtprotoBody): Promise<ApiResponse<{ job_id: string }>> {
    return api.post<{ job_id: string }>(`${BASE}/nodes`, body, 60_000)
}

export function getMtprotoJob(jobId: string): Promise<ApiResponse<MtprotoJob>> {
    return api.get<MtprotoJob>(`${BASE}/jobs/${encodeURIComponent(jobId)}`)
}

export function getMtprotoNode(nodeId: number): Promise<ApiResponse<MtprotoNode>> {
    return api.get<MtprotoNode>(`${BASE}/nodes/${nodeId}`)
}

export function listMtprotoNodes(): Promise<ApiResponse<MtprotoNodeSummary[]>> {
    return api.get<MtprotoNodeSummary[]>(`${BASE}/nodes`)
}

export function getMtprotoEvents(nodeId: number, limit = 50): Promise<ApiResponse<MtprotoEvents>> {
    return api.get<MtprotoEvents>(`${BASE}/nodes/${nodeId}/events?limit=${limit}`)
}

export function updateMtprotoSettings(nodeId: number, body: MtprotoSettingsBody): Promise<ApiResponse<MtprotoNode>> {
    return api.patch<MtprotoNode>(`${BASE}/nodes/${nodeId}`, body)
}

export function repairMtprotoNode(nodeId: number, body: MtprotoLogin & { reset?: boolean }): Promise<ApiResponse<{ job_id: string }>> {
    return api.post<{ job_id: string }>(`${BASE}/nodes/${nodeId}/repair`, body)
}

export function removeMtprotoNode(nodeId: number, body: MtprotoLogin & { force?: boolean }): Promise<ApiResponse<{ job_id: string }>> {
    return api.post<{ job_id: string }>(`${BASE}/nodes/${nodeId}/remove`, body)
}

export function updateMtprotoNode(nodeId: number, kind: "agent" | "telemt"): Promise<ApiResponse<{ job_id: string }>> {
    return api.post<{ job_id: string }>(`${BASE}/nodes/${nodeId}/update`, { kind })
}

/** Moves the link to another port on the node, as a job; a port the panel can't reach changes nothing. */
/** Points the panel at the node's new address; the link must come up there. */
export function changeMtprotoAddress(nodeId: number, address: string): Promise<ApiResponse<{ job_id: string }>> {
    return api.post<{ job_id: string }>(`${BASE}/nodes/${nodeId}/address`, { address })
}

export function changeMtprotoLinkPort(nodeId: number, port: number): Promise<ApiResponse<{ job_id: string }>> {
    return api.post<{ job_id: string }>(`${BASE}/nodes/${nodeId}/link-port`, { port })
}

export function rotateMtprotoKeys(nodeId: number): Promise<ApiResponse<{ job_id: string }>> {
    return api.post<{ job_id: string }>(`${BASE}/nodes/${nodeId}/rotate`, {})
}

export function restartMtprotoTelemt(nodeId: number): Promise<ApiResponse<{ restarted: boolean }>> {
    return api.post<{ restarted: boolean }>(`${BASE}/nodes/${nodeId}/restart`, {})
}

/** Stop keeps the proxy off until started, across restarts; the node's view comes back. */
export function setMtprotoProxyStopped(nodeId: number, stopped: boolean): Promise<ApiResponse<MtprotoNode>> {
    return api.post<MtprotoNode>(`${BASE}/nodes/${nodeId}/${stopped ? "stop" : "start"}`, {})
}

export function getMtprotoLogs(nodeId: number, lines: number): Promise<ApiResponse<{ lines: string[] | null }>> {
    return api.get<{ lines: string[] | null }>(`${BASE}/nodes/${nodeId}/logs?lines=${lines}`)
}

export function checkMtprotoDisguise(nodeId: number, domain: string): Promise<ApiResponse<DisguiseResult>> {
    return api.post<DisguiseResult>(`${BASE}/nodes/${nodeId}/disguise-check`, { domain })
}

export function getMtprotoAllowlist(): Promise<ApiResponse<MtprotoAllowlist>> {
    return api.get<MtprotoAllowlist>(`${BASE}/allowlist`)
}

export function refreshMtprotoAllowlist(): Promise<ApiResponse<MtprotoAllowlist>> {
    return api.post<MtprotoAllowlist>(`${BASE}/allowlist/refresh`, {})
}

// ---- SSE progress stream ----

export interface MtprotoJobCallbacks {
    onProgress: (job: MtprotoJob) => void
    onDone: (job: MtprotoJob) => void
    onError: (e: Error) => void
}

/** Follows a job until it ends. Frames are `event: progress` with the job as JSON. */
export async function streamMtprotoJob(jobId: string, cbs: MtprotoJobCallbacks, signal?: AbortSignal): Promise<void> {
    const res = await fetch(`${getApiBaseUrl()}${BASE}/jobs/${encodeURIComponent(jobId)}/stream`, {
        credentials: "include",
        signal,
    })
    if (!res.ok || !res.body) {
        throw new Error(`progress stream failed: ${res.status} ${res.statusText}`)
    }
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
    let buffer = ""
    while (true) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += value
        let idx: number
        while ((idx = buffer.indexOf("\n\n")) !== -1) {
            const frame = buffer.slice(0, idx)
            buffer = buffer.slice(idx + 2)
            parseMtprotoFrame(frame, cbs)
        }
    }
}

export function parseMtprotoFrame(frame: string, cbs: MtprotoJobCallbacks): void {
    let eventName = "message"
    let data = ""
    for (const line of frame.split("\n")) {
        if (line.startsWith("event:")) eventName = line.slice(6).trim()
        else if (line.startsWith("data:")) data += (data ? "\n" : "") + line.slice(5).replace(/^ /, "")
    }
    if (!data) return
    if (eventName === "error") {
        cbs.onError(new Error(data))
        return
    }
    try {
        const job = JSON.parse(data) as MtprotoJob
        cbs.onProgress(job)
        if (job.state !== "running") cbs.onDone(job)
    } catch (e) {
        cbs.onError(e instanceof Error ? e : new Error(String(e)))
    }
}
