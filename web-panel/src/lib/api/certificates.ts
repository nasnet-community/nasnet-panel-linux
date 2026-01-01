import { api, type ApiResponse } from "@/lib/api"
import type { AgentCertificate, CertBundle, DNSChallengeResponse } from "@/lib/types"

export async function initializeCA(config?: { common_name?: string; organization?: string; valid_years?: number }): Promise<ApiResponse<{ id: number; common_name: string }>> {
    return api.post<{ id: number; common_name: string }>("/api/v1/certificates/ca", config || {})
}

export async function getCA(): Promise<ApiResponse<AgentCertificate & { certificate: string }>> {
    return api.get<AgentCertificate & { certificate: string }>("/api/v1/certificates/ca")
}

export async function hasCA(): Promise<boolean> {
    const result = await getCA()
    return result.success
}

export async function listCertificates(): Promise<ApiResponse<{ certificates: AgentCertificate[] }>> {
    return api.get<{ certificates: AgentCertificate[] }>("/api/v1/certificates")
}

export async function generateMasterCert(): Promise<ApiResponse<AgentCertificate>> {
    return api.post<AgentCertificate>("/api/v1/certificates/master")
}

export async function getMasterCert(): Promise<ApiResponse<AgentCertificate & { certificate: string; private_key: string }>> {
    return api.get<AgentCertificate & { certificate: string; private_key: string }>("/api/v1/certificates/master")
}

export async function generateAgentCert(nodeId: number): Promise<ApiResponse<AgentCertificate & { certificate: string; private_key: string }>> {
    return api.post<AgentCertificate & { certificate: string; private_key: string }>(`/api/v1/certificates/agent/${nodeId}`)
}

export async function getAgentCert(nodeId: number): Promise<ApiResponse<AgentCertificate>> {
    return api.get<AgentCertificate>(`/api/v1/certificates/agent/${nodeId}`)
}

export async function regenerateAgentCert(nodeId: number): Promise<ApiResponse<AgentCertificate & { certificate: string; private_key: string }>> {
    return api.post<AgentCertificate & { certificate: string; private_key: string }>(`/api/v1/certificates/agent/${nodeId}/regenerate`)
}

export async function getCertBundle(nodeId: number): Promise<ApiResponse<CertBundle>> {
    return api.get<CertBundle>(`/api/v1/certificates/agent/${nodeId}/bundle`)
}

export async function revokeCertificate(id: number): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/certificates/${id}/revoke`)
}

export async function renewCertificate(id: number): Promise<ApiResponse<AgentCertificate>> {
    return api.post<AgentCertificate>(`/api/v1/certificates/${id}/renew`)
}

export async function deleteCertificate(id: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/certificates/${id}`)
}

export async function getExpiringSoonCerts(days: number = 30): Promise<ApiResponse<{ certificates: AgentCertificate[]; days_threshold: number }>> {
    return api.get<{ certificates: AgentCertificate[]; days_threshold: number }>(`/api/v1/certificates/expiring?days=${days}`)
}

// Issue public certificate (HTTP-01)
export async function issuePublicCertificate(domain: string): Promise<ApiResponse<AgentCertificate>> {
    return api.post<AgentCertificate>("/api/v1/certificates/public", { domain })
}

// Start DNS-01 challenge
export async function startDNSChallenge(domain: string): Promise<ApiResponse<DNSChallengeResponse>> {
    return api.post<DNSChallengeResponse>("/api/v1/certificates/dns/start", { domain })
}

// Complete DNS-01 challenge
export async function completeDNSChallenge(domain: string): Promise<ApiResponse<AgentCertificate>> {
    return api.post<AgentCertificate>("/api/v1/certificates/dns/complete", { domain })
}

// Get certificate details including PEM content and private key
export async function getCertificateDetails(id: number): Promise<ApiResponse<AgentCertificate & { certificate: string; private_key?: string }>> {
    return api.get<AgentCertificate & { certificate: string; private_key?: string }>(`/api/v1/certificates/details/${id}`)
}

// Toggle auto-renew for a certificate
export async function toggleAutoRenew(id: number, enabled: boolean): Promise<ApiResponse<{ auto_renew: boolean }>> {
    return api.post<{ auto_renew: boolean }>(`/api/v1/certificates/${id}/auto-renew`, { enabled })
}
