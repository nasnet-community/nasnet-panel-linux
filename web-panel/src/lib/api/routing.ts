import { api, type ApiResponse } from "@/lib/api"
import type {
    RoutingSettings,
    DNSSettings,
    FakeDNSPool,
    RoutingRule,
    BalancingRule,
    ReverseProxy,
} from "@/lib/types"

// ==================== Routing Settings ====================

export async function getNodeRoutingSettings(nodeId: number): Promise<ApiResponse<RoutingSettings>> {
    return api.get<RoutingSettings>(`/api/v1/nodes/${nodeId}/routing-settings`)
}

export async function updateNodeRoutingSettings(nodeId: number, settings: RoutingSettings): Promise<ApiResponse<RoutingSettings>> {
    return api.put<RoutingSettings>(`/api/v1/nodes/${nodeId}/routing-settings`, settings)
}

// ==================== DNS Settings ====================

export async function getNodeDNSSettings(nodeId: number): Promise<ApiResponse<DNSSettings | null>> {
    return api.get<DNSSettings | null>(`/api/v1/nodes/${nodeId}/dns-settings`)
}

export async function updateNodeDNSSettings(nodeId: number, settings: DNSSettings): Promise<ApiResponse<DNSSettings>> {
    return api.put<DNSSettings>(`/api/v1/nodes/${nodeId}/dns-settings`, settings)
}

export async function deleteNodeDNSSettings(nodeId: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/nodes/${nodeId}/dns-settings`)
}

// ==================== FakeDNS Settings ====================

export async function getNodeFakeDNSSettings(nodeId: number): Promise<ApiResponse<FakeDNSPool[] | null>> {
    return api.get<FakeDNSPool[] | null>(`/api/v1/nodes/${nodeId}/fakedns-settings`)
}

export async function updateNodeFakeDNSSettings(nodeId: number, pools: FakeDNSPool[]): Promise<ApiResponse<FakeDNSPool[]>> {
    return api.put<FakeDNSPool[]>(`/api/v1/nodes/${nodeId}/fakedns-settings`, pools)
}

export async function deleteNodeFakeDNSSettings(nodeId: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/nodes/${nodeId}/fakedns-settings`)
}

// ==================== Routing Rules ====================

export async function listNodeRoutingRules(nodeId: number): Promise<ApiResponse<RoutingRule[]>> {
    return api.get<RoutingRule[]>(`/api/v1/nodes/${nodeId}/routing`)
}

export async function addNodeRoutingRule(nodeId: number, rule: Partial<RoutingRule>, skipPush = false): Promise<ApiResponse<RoutingRule>> {
    const qs = skipPush ? "?skip_push=true" : ""
    return api.post<RoutingRule>(`/api/v1/nodes/${nodeId}/routing${qs}`, rule)
}

export async function updateNodeRoutingRule(nodeId: number, ruleId: number, rule: Partial<RoutingRule>, skipPush = false): Promise<ApiResponse<RoutingRule>> {
    const qs = skipPush ? "?skip_push=true" : ""
    return api.put<RoutingRule>(`/api/v1/routing/${ruleId}${qs}`, rule)
}

export async function deleteNodeRoutingRule(nodeId: number, ruleId: number, skipPush = false): Promise<ApiResponse<void>> {
    const qs = skipPush ? "?skip_push=true" : ""
    return api.delete<void>(`/api/v1/routing/${ruleId}${qs}`)
}

export async function moveRoutingRule(ruleId: number, moveUp: boolean): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/routing/${ruleId}/move`, { move_up: moveUp })
}

export async function toggleRoutingRule(ruleId: number): Promise<ApiResponse<RoutingRule>> {
    return api.post<RoutingRule>(`/api/v1/routing/${ruleId}/toggle`)
}

export async function reorderRoutingRules(nodeId: number, ruleIds: number[]): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/nodes/${nodeId}/routing/reorder`, { rule_ids: ruleIds })
}

// ==================== Balancing Rules ====================

export async function listBalancingRules(nodeId: number): Promise<ApiResponse<BalancingRule[]>> {
    return api.get<BalancingRule[]>(`/api/v1/nodes/${nodeId}/balancing`)
}

export async function addBalancingRule(nodeId: number, rule: Partial<BalancingRule>): Promise<ApiResponse<BalancingRule>> {
    return api.post<BalancingRule>(`/api/v1/nodes/${nodeId}/balancing`, rule)
}

export async function updateBalancingRule(ruleId: number, rule: Partial<BalancingRule>): Promise<ApiResponse<BalancingRule>> {
    return api.put<BalancingRule>(`/api/v1/balancing/${ruleId}`, rule)
}

export async function deleteBalancingRule(ruleId: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/balancing/${ruleId}`)
}

// ==================== Reverse Proxies ====================

export async function listReverseProxies(nodeId: number): Promise<ApiResponse<ReverseProxy[]>> {
    return api.get<ReverseProxy[]>(`/api/v1/nodes/${nodeId}/reverse-proxies`)
}

export async function getReverseProxy(id: number): Promise<ApiResponse<ReverseProxy>> {
    return api.get<ReverseProxy>(`/api/v1/reverse-proxies/${id}`)
}

export async function addReverseProxy(nodeId: number, data: Partial<ReverseProxy>): Promise<ApiResponse<ReverseProxy>> {
    return api.post<ReverseProxy>(`/api/v1/nodes/${nodeId}/reverse-proxies`, data)
}

export async function updateReverseProxy(id: number, data: Partial<ReverseProxy>): Promise<ApiResponse<ReverseProxy>> {
    return api.put<ReverseProxy>(`/api/v1/reverse-proxies/${id}`, data)
}

export async function deleteReverseProxy(id: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/reverse-proxies/${id}`)
}
