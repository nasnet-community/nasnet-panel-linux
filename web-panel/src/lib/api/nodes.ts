import { api, type ApiResponse } from "@/lib/api"
import { buildQueryString } from "./utils"
import type {
    Node,
    NodeStats,
    NodeHostInfo,
    NodeDataPoint,
    NodeDailyTraffic,
    NodeUptimeEvent,
    SSHStatus,
    InboundUsers,
    Inbound,
    StarlinkStatus,
    StarlinkObstructionMap,
    StarlinkDataPoint,
} from "@/lib/types"

// ==================== Node CRUD ====================

export async function listNodes(): Promise<ApiResponse<Node[]>> {
    return api.get<Node[]>("/api/v1/nodes")
}

export async function getNode(id: number): Promise<ApiResponse<Node>> {
    return api.get<Node>(`/api/v1/nodes/${id}`)
}

export async function createNode(node: Partial<Node>): Promise<ApiResponse<Node>> {
    return api.post<Node>("/api/v1/nodes", node)
}

export async function updateNode(id: number, node: Partial<Node>): Promise<ApiResponse<Node>> {
    return api.put<Node>(`/api/v1/nodes/${id}`, node)
}

export async function deleteNode(id: number, force: boolean = false): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/nodes/${id}${force ? '?force=true' : ''}`)
}

// ==================== Node Migration ====================

export async function migrateNode(id: number, targetNodeId: number, targetInboundId: number): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/nodes/${id}/migrate`, {
        target_node_id: targetNodeId,
        target_inbound_id: targetInboundId,
    })
}

export interface InboundMigrationResult {
    migrated_accounts: number
    skipped_accounts: number
    failed_accounts: number
    updated_plans: number
    skipped_plans: number
    source_deactivated: boolean
    protocol_warning?: string
}

export async function migrateInbound(inboundId: number, targetInboundId: number): Promise<ApiResponse<InboundMigrationResult>> {
    return api.post<InboundMigrationResult>(`/api/v1/inbounds/${inboundId}/migrate`, {
        target_inbound_id: targetInboundId,
    })
}

// ==================== Node Health & Stats ====================

export async function checkNodeHealth(id: number): Promise<ApiResponse<{ healthy: boolean; latency: number; message: string }>> {
    return api.get<{ healthy: boolean; latency: number; message: string }>(`/api/v1/nodes/${id}/health`)
}

export async function getNodeStats(id: number): Promise<ApiResponse<NodeStats>> {
    return api.get<NodeStats>(`/api/v1/nodes/${id}/stats`)
}

export interface NodeStatsBulkEntry {
    stats?: NodeStats
    error?: string
}

export type NodeStatsBulkMap = Record<string, NodeStatsBulkEntry>

export async function getNodesStatsBulk(ids?: number[]): Promise<ApiResponse<NodeStatsBulkMap>> {
    const suffix = ids && ids.length > 0 ? `?ids=${ids.join(",")}` : ""
    return api.get<NodeStatsBulkMap>(`/api/v1/nodes/stats/bulk${suffix}`)
}

export async function getNodeHostInfo(id: number): Promise<ApiResponse<NodeHostInfo>> {
    return api.get<NodeHostInfo>(`/api/v1/nodes/${id}/info`)
}

export async function getNodeRealtimeUsers(id: number): Promise<ApiResponse<InboundUsers[]>> {
    return api.get<InboundUsers[]>(`/api/v1/nodes/${id}/users`)
}

// ==================== Agent Management ====================

export async function startXrayProcess(nodeId: number): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>(`/api/v1/nodes/${nodeId}/agent/start`)
}

export async function stopXrayProcess(nodeId: number): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>(`/api/v1/nodes/${nodeId}/agent/stop`)
}

export async function restartXrayProcess(nodeId: number): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>(`/api/v1/nodes/${nodeId}/agent/restart`)
}

export async function pushNodeConfig(nodeId: number): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>(`/api/v1/nodes/${nodeId}/agent/push-config`)
}

// ==================== Xray Config Diff ====================

export interface XrayConfigDiff {
    running: string
    generated: string
    running_hash: string
    generated_hash: string
    differs: boolean
    running_error?: string
}

export async function getNodeXrayConfigDiff(id: number): Promise<ApiResponse<XrayConfigDiff>> {
    return api.get<XrayConfigDiff>(`/api/v1/nodes/${id}/xray-config/diff`)
}

// ==================== Bulk Node Actions ====================

export interface NodeBulkActionResult {
    success: boolean
    error?: string
}

export interface NodeBulkActionResponse {
    total: number
    succeeded: number
    results: Record<string, NodeBulkActionResult>
}

export async function bulkRestartNodes(ids: number[]): Promise<ApiResponse<NodeBulkActionResponse>> {
    return api.post<NodeBulkActionResponse>(`/api/v1/nodes/bulk/restart`, { ids })
}

export async function bulkPushNodeConfig(ids: number[]): Promise<ApiResponse<NodeBulkActionResponse>> {
    return api.post<NodeBulkActionResponse>(`/api/v1/nodes/bulk/push-config`, { ids })
}

export async function bulkCheckNodeHealth(ids: number[]): Promise<ApiResponse<NodeBulkActionResponse>> {
    return api.post<NodeBulkActionResponse>(`/api/v1/nodes/bulk/health-check`, { ids })
}

export interface LogEntry {
    timestamp: number
    level: string
    log_type: string
    message: string
    source: string
}

export async function getNodeRecentLogs(nodeId: number, lines: number = 50): Promise<ApiResponse<LogEntry[]>> {
    return api.get<LogEntry[]>(`/api/v1/nodes/${nodeId}/logs/tail?lines=${lines}`)
}

// ==================== Xray Version Management ====================

export interface XrayRelease {
    version: string
    tag: string
    name: string
    published_at: string
}

export async function getXrayReleases(): Promise<ApiResponse<XrayRelease[]>> {
    return api.get<XrayRelease[]>("/api/v1/nodes/xray/releases")
}

export async function updateXrayVersion(nodeId: number, version: string): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>(`/api/v1/nodes/${nodeId}/agent/xray-version`, { version })
}

// bulkUpdateXrayVersion fans out an xray-core version update across the
// given nodes. Reuses the standard bulk-action response envelope so callers
// can render per-node success/failure the same way as Restart / Push Config.
export async function bulkUpdateXrayVersion(ids: number[], version: string): Promise<ApiResponse<NodeBulkActionResponse>> {
    return api.post<NodeBulkActionResponse>(`/api/v1/nodes/bulk/xray-version`, { ids, version })
}

// ==================== Geofile Management ====================

export interface UpdateGeoFilesRequest {
    region: string
    custom_geoip_url?: string
    custom_geosite_url?: string
}

export async function updateGeoFiles(nodeId: number, data: UpdateGeoFilesRequest): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>(`/api/v1/nodes/${nodeId}/agent/geofiles`, data)
}

// ==================== SSH Management ====================

export async function getNodeSSHStatus(id: number): Promise<ApiResponse<SSHStatus>> {
    return api.get<SSHStatus>(`/api/v1/nodes/${id}/ssh`)
}

export async function updateNodeSSHConfig(id: number, enabled: boolean, port: number): Promise<ApiResponse<{ message: string }>> {
    return api.put<{ message: string }>(`/api/v1/nodes/${id}/ssh`, {
        enabled,
        port,
    })
}

export async function clearNodeSSHLogs(id: number): Promise<ApiResponse<{ message: string }>> {
    return api.delete<{ message: string }>(`/api/v1/nodes/${id}/ssh/logs`)
}

export async function restartNodeSSH(id: number): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>(`/api/v1/nodes/${id}/ssh/restart`)
}

// ==================== Node Inbounds ====================

export async function listNodeInbounds(nodeId: number): Promise<ApiResponse<Inbound[]>> {
    return api.get<Inbound[]>(`/api/v1/nodes/${nodeId}/inbounds`)
}

export async function addNodeInbound(nodeId: number, inbound: Partial<Inbound>): Promise<ApiResponse<Inbound>> {
    return api.post<Inbound>(`/api/v1/nodes/${nodeId}/inbounds`, inbound)
}

export async function updateNodeInbound(nodeId: number, inboundId: number, inbound: Partial<Inbound>): Promise<ApiResponse<Inbound>> {
    return api.put<Inbound>(`/api/v1/inbounds/${inboundId}`, inbound)
}

export async function deleteNodeInbound(nodeId: number, inboundId: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/inbounds/${inboundId}`)
}

export async function discoverNodeInbounds(nodeId: number): Promise<ApiResponse<Inbound[]>> {
    return api.post<Inbound[]>(`/api/v1/nodes/${nodeId}/inbounds/discover`)
}

export async function syncNodeInbounds(nodeId: number): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/nodes/${nodeId}/inbounds/sync`)
}

// ==================== Node History ====================

export async function getNodeStatsHistory(id: number, limit: number = 60): Promise<ApiResponse<NodeDataPoint[]>> {
    return api.get<NodeDataPoint[]>(`/api/v1/nodes/${id}/stats/history?limit=${limit}`)
}

// Bulk per-node history — replaces per-card sparkline queries on the
// Nodes page. Response is keyed by stringified node id; a node with
// no history yet returns an empty slice.
export type NodeStatsHistoryBulkMap = Record<string, NodeDataPoint[] | null>

export async function getNodesStatsHistoryBulk(ids: number[], limit: number = 15): Promise<ApiResponse<NodeStatsHistoryBulkMap>> {
    if (!ids || ids.length === 0) {
        return { success: true, data: {} }
    }
    return api.get<NodeStatsHistoryBulkMap>(`/api/v1/nodes/stats/history/bulk?ids=${ids.join(",")}&limit=${limit}`)
}

export async function getNodeDailyTraffic(id: number, days: number = 30): Promise<ApiResponse<NodeDailyTraffic[]>> {
    return api.get<NodeDailyTraffic[]>(`/api/v1/nodes/${id}/traffic/daily?days=${days}`)
}

export async function getNodeUptimeEvents(id: number, hours: number = 168): Promise<ApiResponse<NodeUptimeEvent[]>> {
    return api.get<NodeUptimeEvent[]>(`/api/v1/nodes/${id}/uptime?hours=${hours}`)
}

// ==================== Xray Config Editor ====================

export async function getXrayConfig(nodeId: number): Promise<ApiResponse<string>> {
    return api.get<string>(`/api/v1/nodes/${nodeId}/xray-config`)
}

export async function updateXrayConfig(nodeId: number, content: string): Promise<ApiResponse<void>> {
    return api.put<void>(`/api/v1/nodes/${nodeId}/xray-config`, { content })
}

export interface ValidationResult {
    valid: boolean
    errors: string[]
    warnings: string[]
}

export async function validateXrayConfig(nodeId: number, content: string): Promise<ApiResponse<ValidationResult>> {
    return api.post<ValidationResult>(`/api/v1/nodes/${nodeId}/xray-config/validate`, { content })
}

// ==================== Xray Tools ====================

export async function generateVLESSKeys(): Promise<ApiResponse<any[]>> {
    return api.get<any[]>("/api/v1/nodes/tools/vless-keys")
}

export async function generateX25519Keys(): Promise<ApiResponse<{ privateKey: string; publicKey: string }>> {
    return api.get<{ privateKey: string; publicKey: string }>("/api/v1/nodes/tools/x25519-keys")
}

// ==================== Starlink Monitoring ====================

export async function getStarlinkStatus(nodeId: number): Promise<ApiResponse<StarlinkStatus>> {
    return api.get<StarlinkStatus>(`/api/v1/nodes/${nodeId}/starlink/status`)
}

export async function getStarlinkObstructionMap(nodeId: number): Promise<ApiResponse<StarlinkObstructionMap>> {
    return api.get<StarlinkObstructionMap>(`/api/v1/nodes/${nodeId}/starlink/obstruction-map`)
}

export async function getStarlinkHistory(nodeId: number, limit: number = 60, since?: string): Promise<ApiResponse<StarlinkDataPoint[]>> {
    return api.get<StarlinkDataPoint[]>(
        `/api/v1/nodes/${nodeId}/starlink/history${buildQueryString({ limit, since })}`
    )
}
