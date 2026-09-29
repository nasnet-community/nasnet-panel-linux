import { api, ApiError, type ApiResponse } from "@/lib/api"
import type { RealitySettings } from "@/lib/types"

// One server name handshaked against the dest. `reason` is a stable code from
// the backend (key_share, hello_retry_request, tls_version, …); `message`
// explains it in prose.
export interface RealityDestCheck {
    server_name: string
    ok: boolean
    reason?: string
    message?: string
    tls_version?: string
    key_share?: string
    cipher_suite?: string
    alpn?: string
    handshake_ms: number
    flight_bytes: number
    largest_record_bytes: number
    cert_subject?: string
    cert_issuer?: string
    cert_not_after?: string
    cert_error?: string
    warnings?: string[]
}

export interface RealityDestReport {
    ok: boolean
    dest: string
    /** Where the handshake ran. The node is the vantage that matters. */
    checked_from?: "node" | "hub"
    summary: string
    checks: RealityDestCheck[]
    warnings?: string[]
    checked_at: string
}

/** Error code the API returns when a save is blocked by the dest check. */
export const REALITY_DEST_UNSUPPORTED = "REALITY_DEST_UNSUPPORTED"

export type RealityDestInput = Pick<RealitySettings, "dest" | "xver" | "serverNames">

export function checkRealityDest(nodeId: number, input: RealityDestInput): Promise<ApiResponse<RealityDestReport>> {
    // A few handshakes at 8s each can legitimately outlast the default 30s.
    return api.post<RealityDestReport>(`/api/v1/nodes/${nodeId}/reality-dest/check`, input, 45000)
}

export function isRealityDestReport(value: unknown): value is RealityDestReport {
    return typeof value === "object" && value !== null
        && Array.isArray((value as { checks?: unknown }).checks)
        && typeof (value as { ok?: unknown }).ok === "boolean"
}

/** The report a save blocked by the dest check carries, or null for any other error. */
export function realityDestReportOf(err: unknown): RealityDestReport | null {
    if (!(err instanceof ApiError) || err.code !== REALITY_DEST_UNSUPPORTED) return null
    const data = (err.body as { data?: unknown } | undefined)?.data
    return isRealityDestReport(data) ? data : null
}

export function isRealityDestError(err: unknown): err is ApiError {
    return realityDestReportOf(err) !== null
}
