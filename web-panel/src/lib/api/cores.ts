import { api, type ApiResponse } from "@/lib/api"
import type {
    CoreInbound,
    CoreInboundInput,
    CoreUser,
    CoreUserInput,
    CoreUserUpdateInput,
    ClientArtifact,
    CoreStatus,
    InboundMutationResult,
    UserMutationResult,
    DeleteResult,
    NodeProtocolState,
    NodeProtocolToggleResult,
} from "@/lib/types/cores"

const BASE = "/api/v1/cores"

// Inbounds -------------------------------------------------------------------

export async function listCoreInbounds(
    nodeId: number,
): Promise<ApiResponse<{ inbounds: CoreInbound[] | null }>> {
    return api.get<{ inbounds: CoreInbound[] | null }>(`${BASE}/nodes/${nodeId}/inbounds`)
}

// listAllCoreInbounds returns every core inbound across all nodes for the
// cross-node "All Tunnels" overview (admin-only).
export async function listAllCoreInbounds(): Promise<
    ApiResponse<{ inbounds: CoreInbound[] | null }>
> {
    return api.get<{ inbounds: CoreInbound[] | null }>(`${BASE}/inbounds`)
}

export async function createCoreInbound(
    body: CoreInboundInput,
): Promise<ApiResponse<InboundMutationResult>> {
    return api.post<InboundMutationResult>(`${BASE}/inbounds`, body)
}

export async function updateCoreInbound(
    id: number,
    body: CoreInboundInput,
): Promise<ApiResponse<InboundMutationResult>> {
    return api.put<InboundMutationResult>(`${BASE}/inbounds/${id}`, body)
}

export async function deleteCoreInbound(id: number): Promise<ApiResponse<DeleteResult>> {
    return api.delete<DeleteResult>(`${BASE}/inbounds/${id}`)
}

export async function getCoreInboundStatus(id: number): Promise<ApiResponse<CoreStatus>> {
    return api.get<CoreStatus>(`${BASE}/inbounds/${id}/status`)
}

// Users ----------------------------------------------------------------------

export async function listCoreUsers(
    inboundId: number,
): Promise<ApiResponse<{ users: CoreUser[] | null }>> {
    return api.get<{ users: CoreUser[] | null }>(`${BASE}/inbounds/${inboundId}/users`)
}

export async function createCoreUser(
    inboundId: number,
    body: CoreUserInput,
): Promise<ApiResponse<UserMutationResult>> {
    return api.post<UserMutationResult>(`${BASE}/inbounds/${inboundId}/users`, body)
}

export async function updateCoreUser(
    id: number,
    body: CoreUserUpdateInput,
): Promise<ApiResponse<UserMutationResult>> {
    return api.put<UserMutationResult>(`${BASE}/users/${id}`, body)
}

export async function toggleCoreUser(id: number): Promise<ApiResponse<UserMutationResult>> {
    return api.post<UserMutationResult>(`${BASE}/users/${id}/toggle`)
}

export async function deleteCoreUser(id: number): Promise<ApiResponse<DeleteResult>> {
    return api.delete<DeleteResult>(`${BASE}/users/${id}`)
}

// Per-node protocol enable/disable -------------------------------------------

export async function listNodeProtocols(
    nodeId: number,
): Promise<ApiResponse<{ protocols: NodeProtocolState[] | null }>> {
    return api.get<{ protocols: NodeProtocolState[] | null }>(`${BASE}/nodes/${nodeId}/protocols`)
}

export async function toggleNodeProtocol(
    nodeId: number,
    protocol: string,
): Promise<ApiResponse<NodeProtocolToggleResult>> {
    return api.post<NodeProtocolToggleResult>(
        `${BASE}/nodes/${nodeId}/protocols/${protocol}/toggle`,
    )
}

// Client config --------------------------------------------------------------

export async function getCoreUserConfig(
    id: number,
    host?: string,
): Promise<ApiResponse<ClientArtifact>> {
    const q = host ? `?host=${encodeURIComponent(host)}` : ""
    return api.get<ClientArtifact>(`${BASE}/users/${id}/config${q}`)
}
