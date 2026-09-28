import { api, type ApiResponse } from "@/lib/api"
import type { AgentCertificate, CertDetails, CertOverview, RotateResult, RotatedCert } from "@/lib/types"

export async function initializeCA(config?: { common_name?: string; organization?: string; valid_years?: number }): Promise<ApiResponse<{ id: number; common_name: string }>> {
    return api.post<{ id: number; common_name: string }>("/api/v1/certificates/ca", config || {})
}

export async function getCA(): Promise<ApiResponse<AgentCertificate & { certificate: string }>> {
    return api.get<AgentCertificate & { certificate: string }>("/api/v1/certificates/ca")
}

export async function listCertificates(): Promise<ApiResponse<{ certificates: AgentCertificate[] }>> {
    return api.get<{ certificates: AgentCertificate[] }>("/api/v1/certificates")
}

export async function getCertificateOverview(refreshDomains = false): Promise<ApiResponse<CertOverview>> {
    return api.get<CertOverview>(`/api/v1/certificates/overview${refreshDomains ? "?refresh_domains=1" : ""}`)
}

export async function getCertificateDetails(id: number): Promise<ApiResponse<CertDetails>> {
    return api.get<CertDetails>(`/api/v1/certificates/details/${id}`)
}

export async function rotateNodeCert(nodeId: number): Promise<ApiResponse<RotatedCert>> {
    return api.post<RotatedCert>(`/api/v1/certificates/nodes/${nodeId}/rotate`, undefined, 45000)
}

export async function rotateExpiringCerts(): Promise<ApiResponse<RotateResult[]>> {
    return api.post<RotateResult[]>("/api/v1/certificates/rotate-expiring", undefined, 120000)
}

export async function revokeOrphanCerts(): Promise<ApiResponse<{ revoked: number }>> {
    return api.post<{ revoked: number }>("/api/v1/certificates/orphans/revoke")
}

export async function purgeCerts(): Promise<ApiResponse<{ purged: number }>> {
    return api.post<{ purged: number }>("/api/v1/certificates/purge")
}

export async function setCertStrictMode(enabled: boolean): Promise<ApiResponse<{ enabled: boolean }>> {
    return api.put<{ enabled: boolean }>("/api/v1/certificates/strict-mode", { enabled })
}

export async function revokeCertificate(id: number, confirm?: string): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/certificates/${id}/revoke`, confirm ? { confirm } : undefined)
}

export async function deleteCertificate(id: number, confirm?: string): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/certificates/${id}${confirm ? `?confirm=${encodeURIComponent(confirm)}` : ""}`)
}

export async function revealPrivateKey(id: number, confirm?: string): Promise<ApiResponse<{ private_key: string }>> {
    return api.post<{ private_key: string }>(`/api/v1/certificates/${id}/private-key`, { confirm: confirm ?? "" })
}
