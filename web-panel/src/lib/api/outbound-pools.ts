import { api, type ApiResponse } from "@/lib/api"
import type { BalancingRule } from "@/lib/types"

export interface PoolSource {
    id?: number
    name: string
    kind: "url" | "links"
    url: string
    links: string
    user_agent: string
    allow_private: boolean
    fetched_at?: string | null
    last_error?: string
    link_count?: number
}
export interface PoolSettings {
    name: string
    balancer_id: number
    enabled: boolean
    stop_when_full: boolean
    max_active: number
    refresh_seconds: number
    rescan_seconds: number
    concurrency: number
    max_delay_ms: number
    retries: number
    failure_threshold: number
    max_source_links: number
    max_source_bytes: number
    max_candidates: number
    test_urls: string[]
    sources: PoolSource[]
    balancer_tag?: string
    strategy?: string
    fallback_tag?: string
}
export interface PoolRun {
    id: number
    state: string
    /** True when the run fetched sources first; false for a rescan of saved configs. */
    refresh?: boolean
    raw_count: number
    unique_count: number
    duplicate_count: number
    tested: number
    healthy: number
    active: number
    error?: string
    started_at: string
    finished_at?: string
}
export interface OutboundPool extends PoolSettings {
    id: number
    node_id: number
    revision: number
    applied_revision: number
    active_run_id: number
    balancer: BalancingRule
    last_run?: PoolRun
    candidate_count: number
    active_count: number
    next_refresh_at: string
    next_scan_at: string
}
export interface PoolCandidate {
    id: number
    link: string
    remark: string
    protocol: string
    address: string
    status: string
    excluded: boolean
    present: boolean
    source_ids: number[]
    latency_ms: number
    /** Consecutive failed scans. */
    failures: number
    last_error?: string
    tested_at?: string
}
/** "live" and the status filters cover configs still in the sources; "missing" is the rest. */
export type CandidateFilter = "live" | "passed" | "failed" | "untested" | "unsupported" | "excluded" | "missing" | ""
export interface CandidatePage {
    items: PoolCandidate[]
    total: number
    /** Size of every filter for the same search. Absent on servers older than this panel. */
    counts?: Partial<Record<Exclude<CandidateFilter, "">, number>>
}
export function poolBase(nodeId: number) { return `/api/v1/nodes/${nodeId}/pools` }
export async function unwrapPool<T>(request: Promise<ApiResponse<T>>): Promise<T> {
    const result = await request
    if (!result.success) throw new Error(result.error || "Pool operation failed")
    return result.data as T
}
export const poolAPI = {
    list: (nodeId: number) => unwrapPool(api.get<OutboundPool[]>(poolBase(nodeId))),
    save: (nodeId: number, id: number | undefined, input: PoolSettings) => unwrapPool(id
        ? api.put<OutboundPool>(`${poolBase(nodeId)}/${id}`, input)
        : api.post<OutboundPool>(poolBase(nodeId), input)),
    run: (nodeId: number, id: number, refresh: boolean) => unwrapPool(api.post<PoolRun>(`${poolBase(nodeId)}/${id}/run`, { refresh })),
    cancel: (nodeId: number, id: number) => unwrapPool(api.post(`${poolBase(nodeId)}/${id}/cancel`)),
    remove: (nodeId: number, id: number) => unwrapPool(api.delete(`${poolBase(nodeId)}/${id}`)),
    candidates: (nodeId: number, id: number, offset: number, search = "", status: CandidateFilter = "") => unwrapPool(api.get<CandidatePage>(`${poolBase(nodeId)}/${id}/candidates?offset=${offset}&limit=50&q=${encodeURIComponent(search)}&status=${encodeURIComponent(status)}`)),
    runs: (nodeId: number, id: number) => unwrapPool(api.get<PoolRun[]>(`${poolBase(nodeId)}/${id}/runs`)),
    exclude: (nodeId: number, id: number, candidate: number, excluded: boolean) => unwrapPool(api.put(`${poolBase(nodeId)}/${id}/candidates/${candidate}`, { excluded })),
}
export const emptySource = (): PoolSource => ({ name: "", kind: "url", url: "", links: "", user_agent: "", allow_private: false })
export const defaultPool = (): PoolSettings => ({
    name: "", balancer_id: 0, enabled: true, stop_when_full: true, max_active: 30,
    refresh_seconds: 3600, rescan_seconds: 900, concurrency: 4, max_delay_ms: 5000,
    retries: 1, failure_threshold: 2, max_source_links: 100000,
    max_source_bytes: 64 * 1024 * 1024, max_candidates: 100000,
    test_urls: ["https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204"],
    sources: [emptySource()], balancer_tag: "", strategy: "leastping", fallback_tag: "",
})
