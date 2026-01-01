import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    listNodes,
    getNode,
    createNode,
    updateNode,
    deleteNode,
    checkNodeHealth,
    getNodeStats,
    getNodesStatsBulk,
    getNodesStatsHistoryBulk,
    getNodeHostInfo,
    getNodeStatsHistory,
    listNodeInbounds,
    listNodeOutbounds,
    listNodeRoutingRules,
    addNodeInbound,
    updateNodeInbound,
    deleteNodeInbound,
    discoverNodeInbounds,
    syncNodeInbounds,
    getNodeRealtimeUsers,
    getAccessLogs,
    getNodeDailyTraffic,
    getNodeUptimeEvents,
    migrateInbound,
    bulkRestartNodes,
    bulkPushNodeConfig,
    bulkCheckNodeHealth,
    bulkUpdateXrayVersion,
    getNodeXrayConfigDiff,
    toggleInboundDisabled,
    addNodeOutbound,
    updateNodeOutbound,
    deleteNodeOutbound,
    toggleOutboundDisabled,
    testOutbound,
    addNodeRoutingRule,
    updateNodeRoutingRule,
    deleteNodeRoutingRule,
    toggleRoutingRule,
    listBalancingRules,
    listReverseProxies,
    addReverseProxy,
    updateReverseProxy,
    deleteReverseProxy,
} from "@/lib/admin-api"
import type { NodeStatsBulkMap, NodeBulkActionResponse, XrayConfigDiff } from "@/lib/api/nodes"
import { ApiError } from "@/lib/api"
import { queryKeys } from "./keys"
import type { Node, Inbound, Outbound, RoutingRule, ReverseProxy, BalancingRule, OutboundTestResult, NodeDataPoint } from "@/lib/types"
import { toast } from "sonner"

// ==================== Queries ====================

// List all nodes
export function useNodes() {
    return useQuery({
        queryKey: queryKeys.nodeList(),
        queryFn: async () => {
            const res = await listNodes()
            if (!res.success) throw new Error(res.error || "Failed to fetch nodes")
            return res.data || []
        },
    })
}

// Get single node details
export function useNode(id: number) {
    return useQuery({
        queryKey: queryKeys.nodeDetails(id),
        queryFn: async () => {
            const res = await getNode(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch node")
            return res.data!
        },
        enabled: id > 0,
    })
}

// Get node stats (CPU, RAM, Disk, etc.)
export function useNodeStats(id: number, enabled = true) {
    return useQuery({
        queryKey: queryKeys.nodeStats(id),
        queryFn: async () => {
            const res = await getNodeStats(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch node stats")
            return res.data!
        },
        enabled: enabled && id > 0,
        staleTime: 15 * 1000,
    })
}

// Stats for many nodes in one request (no ids = all). Fans out each entry
// into the per-node nodeStats(id) cache so detail sheet / /nodes/:id
// renders instantly. SSE drives freshness; 60s poll is dropped-event fallback.
export function useNodesStatsBulk(ids?: number[], enabled = true) {
    const queryClient = useQueryClient()
    const stableIds = ids && ids.length > 0 ? [...ids].sort((a, b) => a - b) : undefined
    return useQuery({
        queryKey: queryKeys.nodeStatsBulk(stableIds),
        queryFn: async (): Promise<NodeStatsBulkMap> => {
            const res = await getNodesStatsBulk(stableIds)
            if (!res.success) throw new Error(res.error || "Failed to fetch bulk node stats")
            const data = res.data || {}
            for (const idStr in data) {
                const entry = data[idStr]
                if (entry?.stats) {
                    queryClient.setQueryData(queryKeys.nodeStats(Number(idStr)), entry.stats)
                }
            }
            return data
        },
        enabled,
        staleTime: 15 * 1000,
        refetchInterval: 60 * 1000,
    })
}

// Get node host info (static OS, CPU, etc.)
export function useNodeHostInfo(id: number, enabled = true) {
    return useQuery({
        queryKey: ["nodeHostInfo", id],
        queryFn: async () => {
            const res = await getNodeHostInfo(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch node host info")
            return res.data!
        },
        enabled: enabled && id > 0,
        staleTime: 24 * 60 * 60 * 1000, // Very long stale time (24h)
        refetchOnMount: false,
        refetchOnWindowFocus: false,
    })
}

// Pass `select` to derive a stable-reference value — React Query v5
// structural sharing keeps unchanged numeric series identity-equal so
// downstream memo'd charts don't re-render on refetch.
export function useNodeStatsHistory<TData = NodeDataPoint[]>(
    id: number,
    limit = 60,
    enabled = true,
    select?: (raw: NodeDataPoint[]) => TData,
) {
    return useQuery({
        queryKey: queryKeys.nodeStatsHistory(id),
        queryFn: async () => {
            const res = await getNodeStatsHistory(id, limit)
            if (!res.success) throw new Error(res.error || "Failed to fetch node history")
            return res.data || []
        },
        enabled: enabled && id > 0,
        select,
    })
}

// Convenience selector: returns only the CPU + memory series as plain
// numeric arrays suitable for a Sparkline. Re-renders only when the series
// actually changes.
export function useNodeSparkline(id: number, limit = 15, enabled = true) {
    return useNodeStatsHistory<{ cpu: number[]; memory: number[] }>(
        id,
        limit,
        enabled,
        sparklineSelect,
    )
}

function sparklineSelect(raw: NodeDataPoint[]): { cpu: number[]; memory: number[] } {
    const cpu = new Array(raw.length)
    const memory = new Array(raw.length)
    for (let i = 0; i < raw.length; i++) {
        cpu[i] = raw[i].cpu
        memory[i] = raw[i].memory
    }
    return { cpu, memory }
}

// Bulk per-node history — one HTTP call for every sparkline on the
// Nodes page instead of N per-card fetches. Parent page calls this
// once; CompactNodeCard reads its row out of the resulting map.
export function useNodesStatsHistoryBulk(ids: number[], limit = 15, enabled = true) {
    // Sort so cache key is stable across render orderings.
    const stableIds = ids.length > 0 ? [...ids].sort((a, b) => a - b) : []
    return useQuery({
        queryKey: queryKeys.nodeStatsHistoryBulk(stableIds, limit),
        queryFn: async () => {
            if (stableIds.length === 0) return {} as Record<string, NodeDataPoint[] | null>
            const res = await getNodesStatsHistoryBulk(stableIds, limit)
            if (!res.success) throw new Error(res.error || "Failed to fetch bulk node history")
            return res.data || {}
        },
        enabled: enabled && stableIds.length > 0,
        staleTime: 15 * 1000,
        // History has no SSE channel — poll remains authoritative, but
        // cadence is relaxed since sparklines are visual noise tolerant.
        refetchInterval: 60 * 1000,
    })
}

// Pure selector: bulk history map → per-node sparkline series.
// Zero-alloc empty fallback keeps consumer memo checks stable on no-data refetches.
const emptyNumberArray: number[] = []
export function sparklineFromBulk(
    bulk: Record<string, NodeDataPoint[] | null> | undefined,
    nodeID: number,
): { cpu: number[]; memory: number[] } {
    if (!bulk) return { cpu: emptyNumberArray, memory: emptyNumberArray }
    const raw = bulk[String(nodeID)]
    if (!raw || raw.length === 0) {
        return { cpu: emptyNumberArray, memory: emptyNumberArray }
    }
    return sparklineSelect(raw)
}

// Get node realtime users
export function useNodeRealtimeUsers(id: number, enabled = true) {
    return useQuery({
        queryKey: ["nodeRealtimeUsers", id],
        queryFn: async () => {
            const res = await getNodeRealtimeUsers(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch realtime users")
            return res.data || []
        },
        enabled: enabled && id > 0,
    })
}

// Get node inbounds
export function useNodeInbounds(nodeId: number) {
    return useQuery({
        queryKey: queryKeys.nodeInbounds(nodeId),
        queryFn: async () => {
            const res = await listNodeInbounds(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch inbounds")
            return res.data || []
        },
        enabled: nodeId > 0,
    })
}

// Get node outbounds
export function useNodeOutbounds(nodeId: number) {
    return useQuery({
        queryKey: queryKeys.nodeOutbounds(nodeId),
        queryFn: async () => {
            const res = await listNodeOutbounds(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch outbounds")
            return res.data || []
        },
        enabled: nodeId > 0,
    })
}

// Get node routing rules
export function useNodeRouting(nodeId: number) {
    return useQuery({
        queryKey: queryKeys.nodeRouting(nodeId),
        queryFn: async () => {
            const res = await listNodeRoutingRules(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch routing rules")
            return res.data || []
        },
        enabled: nodeId > 0,
    })
}

// Get node access logs
export function useAccessLogs(nodeId: number, email?: string, limit?: number, enabled = true) {
    return useQuery({
        queryKey: queryKeys.nodeAccessLogs(nodeId, email),
        queryFn: async () => {
            const res = await getAccessLogs(nodeId, email, limit)
            if (!res.success) throw new Error(res.error || "Failed to fetch access logs")
            return res.data || []
        },
        enabled: enabled && nodeId > 0,
        refetchInterval: 10_000,
    })
}

// Get node daily traffic history
export function useNodeDailyTraffic(nodeId: number, days = 30, enabled = true) {
    return useQuery({
        queryKey: ["nodeDailyTraffic", nodeId, days],
        queryFn: async () => {
            const res = await getNodeDailyTraffic(nodeId, days)
            if (!res.success) throw new Error(res.error || "Failed to fetch daily traffic")
            return res.data || []
        },
        enabled: enabled && nodeId > 0,
        staleTime: 5 * 60 * 1000, // 5 minutes
    })
}

// Get node uptime events
export function useNodeUptimeEvents(nodeId: number, hours = 168, enabled = true) {
    return useQuery({
        queryKey: ["nodeUptimeEvents", nodeId, hours],
        queryFn: async () => {
            const res = await getNodeUptimeEvents(nodeId, hours)
            if (!res.success) throw new Error(res.error || "Failed to fetch uptime events")
            return res.data || []
        },
        enabled: enabled && nodeId > 0,
        staleTime: 60 * 1000, // 1 minute
    })
}

// ==================== Mutations ====================

// Create a new node
export function useCreateNode() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (node: Partial<Node>) => {
            const res = await createNode(node)
            if (!res.success) throw new Error(res.error || "Failed to create node")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Node created successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Update a node
export function useUpdateNode() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, data }: { id: number; data: Partial<Node> }) => {
            const res = await updateNode(id, data)
            if (!res.success) throw new Error(res.error || "Failed to update node")
            return res.data!
        },
        onSuccess: (_, { id }) => {
            toast.success("Node updated successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeDetails(id) })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Delete a node
export function useDeleteNode() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, force }: { id: number; force?: boolean }) => {
            const res = await deleteNode(id, force)
            if (!res.success) {
                throw new ApiError(res.error || "Failed to delete node", res.code)
            }
        },
        onSuccess: () => {
            toast.success("Node deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeStatsBulkAll() })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeStatsHistoryBulkAll() })
        },
        onError: (error: Error | ApiError) => {
            // If it's a specific error we handle in UI, don't show toast
            if (error instanceof ApiError && error.code === "NODE_HAS_CHILDREN") {
                return
            }
            toast.error(error.message)
        },
    })
}

// Check node health
export function useCheckNodeHealth() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await checkNodeHealth(id)
            if (!res.success) throw new Error(res.error || "Health check failed")
            return res.data!
        },
        onSuccess: (data, id) => {
            if (data.healthy) {
                toast.success(data.message || "Node is healthy")
            } else {
                toast.error(data.message || "Node is unhealthy")
            }
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeDetails(id) })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeStats(id) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// ──────────────────── Xray Config Diff ────────────────────

// Live diff between the agent's running config and what the master would
// push if an operator clicked "Push Config". Disabled by default — the
// caller enables it when opening the diff dialog to avoid building the
// config on every node page view.
export function useNodeXrayConfigDiff(nodeId: number, enabled: boolean) {
    return useQuery<XrayConfigDiff>({
        queryKey: queryKeys.nodeXrayConfigDiff(nodeId),
        queryFn: async () => {
            const res = await getNodeXrayConfigDiff(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch config diff")
            return res.data!
        },
        enabled: enabled && nodeId > 0,
        staleTime: 5_000,
        refetchOnWindowFocus: false,
    })
}

// ──────────────────── Bulk Node Actions ────────────────────

// summariseBulk turns a bulk response into a success/error toast pair.
// Callers also get the raw data back so the UI can mark individual cards.
function summariseBulk(label: string, res: NodeBulkActionResponse) {
    const { total, succeeded, results } = res
    const failed = total - succeeded
    if (failed === 0) {
        toast.success(`${label}: ${succeeded}/${total} nodes`)
        return
    }
    // List up to 3 failures so the toast stays readable.
    const failedEntries = Object.entries(results).filter(([, r]) => !r.success)
    const preview = failedEntries.slice(0, 3).map(([id, r]) => `#${id}: ${r.error || "failed"}`).join("\n")
    const more = failedEntries.length > 3 ? `\n… +${failedEntries.length - 3} more` : ""
    toast.error(`${label}: ${succeeded}/${total} succeeded`, { description: preview + more })
}

export function useBulkRestartNodes() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (ids: number[]) => {
            const res = await bulkRestartNodes(ids)
            if (!res.success) throw new Error(res.error || "Bulk restart failed")
            return res.data!
        },
        onSuccess: (data) => {
            summariseBulk("Restart Xray", data)
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useBulkPushNodeConfig() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (ids: number[]) => {
            const res = await bulkPushNodeConfig(ids)
            if (!res.success) throw new Error(res.error || "Bulk push failed")
            return res.data!
        },
        onSuccess: (data) => {
            summariseBulk("Push config", data)
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useBulkCheckNodeHealth() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (ids: number[]) => {
            const res = await bulkCheckNodeHealth(ids)
            if (!res.success) throw new Error(res.error || "Bulk health check failed")
            return res.data!
        },
        onSuccess: (data) => {
            summariseBulk("Health check", data)
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

// useBulkUpdateXrayVersion fans out an xray-core version change across the
// selected nodes. Per-node operations are capped at 5 minutes server-side
// (the agent's UpdateXrayBinary RPC deadline plus dial/HostInfo overhead),
// so a large fleet update can take a while. The returned mutation surfaces
// the standard bulk-result envelope so callers can render per-node status.
export function useBulkUpdateXrayVersion() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (args: { ids: number[]; version: string }) => {
            const res = await bulkUpdateXrayVersion(args.ids, args.version)
            if (!res.success) throw new Error(res.error || "Bulk xray update failed")
            return res.data!
        },
        onSuccess: (data, args) => {
            summariseBulk(`Update Xray → v${args.version}`, data)
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

// Discover node inbounds
export function useDiscoverInbounds() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (nodeId: number) => {
            const res = await discoverNodeInbounds(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to discover inbounds")
            return res.data!
        },
        onSuccess: (data, nodeId) => {
            toast.success(`Discovered ${data.length} inbounds`)
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeInbounds(nodeId) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Sync node inbounds
export function useSyncInbounds() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (nodeId: number) => {
            const res = await syncNodeInbounds(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to sync inbounds")
        },
        onSuccess: (_, nodeId) => {
            toast.success("Inbounds synced successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeInbounds(nodeId) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Add inbound to node
export function useAddInbound() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ nodeId, inbound }: { nodeId: number; inbound: Partial<Inbound> }) => {
            const res = await addNodeInbound(nodeId, inbound)
            if (!res.success) throw new Error(res.error || "Failed to add inbound")
            return res.data!
        },
        onSuccess: (_, { nodeId }) => {
            toast.success("Inbound added")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeInbounds(nodeId) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Update inbound
export function useUpdateInbound() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ nodeId, inboundId, inbound }: { nodeId: number; inboundId: number; inbound: Partial<Inbound> }) => {
            const res = await updateNodeInbound(nodeId, inboundId, inbound)
            if (!res.success) throw new Error(res.error || "Failed to update inbound")
            return res.data!
        },
        onSuccess: (_, { nodeId }) => {
            toast.success("Inbound updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeInbounds(nodeId) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Delete inbound
export function useDeleteInbound() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ nodeId, inboundId }: { nodeId: number; inboundId: number }) => {
            const res = await deleteNodeInbound(nodeId, inboundId)
            if (!res.success) throw new Error(res.error || "Failed to delete inbound")
        },
        onSuccess: (_, { nodeId }) => {
            toast.success("Inbound deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeInbounds(nodeId) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useMigrateInbound() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ sourceInboundId, targetInboundId }: { sourceInboundId: number; targetInboundId: number }) => {
            const res = await migrateInbound(sourceInboundId, targetInboundId)
            if (!res.success) throw new Error(res.error || "Failed to migrate inbound")
            return res.data!
        },
        onSuccess: (data) => {
            const parts = [`Migrated ${data.migrated_accounts} accounts`]
            if (data.skipped_accounts > 0) parts.push(`${data.skipped_accounts} skipped`)
            if (data.failed_accounts > 0) parts.push(`${data.failed_accounts} failed`)
            parts.push(`${data.updated_plans} plans updated`)
            toast.success(parts.join(", "))
            // Migration shuffles accounts between nodes — refresh inbound
            // lists (both source + target), plus account caches.
            queryClient.invalidateQueries({ queryKey: [...queryKeys.nodes, 'inbounds'], exact: false })
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Toggle inbound disabled
export function useToggleInbound(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (inbound: Inbound) => {
            const res = await toggleInboundDisabled(inbound.id)
            if (!res.success) throw new Error(res.error || "Failed to toggle inbound")
            return { result: res.data!, prevDisabled: inbound.is_disabled }
        },
        onSuccess: ({ prevDisabled }) => {
            toast.success(prevDisabled ? "Inbound enabled" : "Inbound disabled")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeInbounds(nodeId) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// ==================== Outbounds ====================

export function useAddOutbound(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (outbound: Partial<Outbound>) => {
            const res = await addNodeOutbound(nodeId, outbound)
            if (!res.success) throw new Error(res.error || "Failed to add outbound")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Outbound created")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeOutbounds(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useUpdateOutbound(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ outboundId, outbound }: { outboundId: number; outbound: Partial<Outbound> }) => {
            const res = await updateNodeOutbound(nodeId, outboundId, outbound)
            if (!res.success) throw new Error(res.error || "Failed to update outbound")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Outbound updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeOutbounds(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useDeleteOutbound(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (outboundId: number) => {
            const res = await deleteNodeOutbound(nodeId, outboundId)
            if (!res.success) throw new Error(res.error || "Failed to delete outbound")
        },
        onSuccess: () => {
            toast.success("Outbound deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeOutbounds(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useToggleOutbound(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (outbound: Outbound) => {
            const res = await toggleOutboundDisabled(outbound.id)
            if (!res.success) throw new Error(res.error || "Failed to toggle outbound")
            return { result: res.data!, prevDisabled: outbound.is_disabled }
        },
        onSuccess: ({ prevDisabled }) => {
            toast.success(prevDisabled ? "Outbound enabled" : "Outbound disabled")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeOutbounds(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useTestOutbound() {
    return useMutation<OutboundTestResult, Error, number>({
        mutationFn: async (outboundId: number) => {
            const res = await testOutbound(outboundId)
            if (!res.success) throw new Error(res.error || "Test failed")
            return res.data!
        },
        // Caller decides toast (per-row context)
    })
}

// ==================== Routing Rules ====================

export function useAddRoutingRule(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (rule: Partial<RoutingRule>) => {
            const res = await addNodeRoutingRule(nodeId, rule)
            if (!res.success) throw new Error(res.error || "Failed to add routing rule")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Routing rule created")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeRouting(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useUpdateRoutingRule(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ ruleId, rule }: { ruleId: number; rule: Partial<RoutingRule> }) => {
            const res = await updateNodeRoutingRule(nodeId, ruleId, rule)
            if (!res.success) throw new Error(res.error || "Failed to update routing rule")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Routing rule updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeRouting(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useDeleteRoutingRule(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (ruleId: number) => {
            const res = await deleteNodeRoutingRule(nodeId, ruleId)
            if (!res.success) throw new Error(res.error || "Failed to delete routing rule")
        },
        onSuccess: () => {
            toast.success("Routing rule deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeRouting(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useToggleRoutingRule(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (rule: RoutingRule) => {
            const res = await toggleRoutingRule(rule.id)
            if (!res.success) throw new Error(res.error || "Failed to toggle rule")
            return { result: res.data!, prevEnabled: rule.enabled, tag: rule.rule_tag }
        },
        onSuccess: ({ prevEnabled, tag }) => {
            toast.success(`Rule "${tag}" ${prevEnabled ? "disabled" : "enabled"}`)
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeRouting(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

// ==================== Balancing Rules ====================

export function useBalancingRules(nodeId: number) {
    return useQuery({
        queryKey: queryKeys.nodeBalancingRules(nodeId),
        queryFn: async () => {
            const res = await listBalancingRules(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch balancing rules")
            return res.data || []
        },
        enabled: nodeId > 0,
    })
}

// ==================== Reverse Proxies ====================

export function useReverseProxies(nodeId: number) {
    return useQuery<ReverseProxy[]>({
        queryKey: queryKeys.nodeReverseProxies(nodeId),
        queryFn: async () => {
            const res = await listReverseProxies(nodeId)
            if (!res.success) return []
            return res.data || []
        },
        enabled: nodeId > 0,
    })
}

export function useAddReverseProxy(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (data: Partial<ReverseProxy>) => {
            const res = await addReverseProxy(nodeId, data)
            if (!res.success) throw new Error(res.error || "Failed to add reverse proxy")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Reverse proxy added")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeReverseProxies(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useUpdateReverseProxy(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, data }: { id: number; data: Partial<ReverseProxy> }) => {
            const res = await updateReverseProxy(id, data)
            if (!res.success) throw new Error(res.error || "Failed to update reverse proxy")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Reverse proxy updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeReverseProxies(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useDeleteReverseProxy(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteReverseProxy(id)
            if (!res.success) throw new Error(res.error || "Failed to delete reverse proxy")
        },
        onSuccess: () => {
            toast.success("Reverse proxy deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.nodeReverseProxies(nodeId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

// Suppress unused vars (BalancingRule type kept for downstream consumers)
export type { BalancingRule, ReverseProxy, RoutingRule, Outbound, OutboundTestResult }
