import { api, type ApiResponse } from "@/lib/api"
import type { SNI, ValidateCertResponse, DNSChallengeResponse } from "@/lib/types"

export async function listSNIs(): Promise<ApiResponse<SNI[]>> {
    return api.get<SNI[]>("/api/v1/sni")
}

export async function getSNI(id: number): Promise<ApiResponse<SNI>> {
    return api.get<SNI>(`/api/v1/sni/${id}`)
}

export async function createSNI(data: {
    name: string
    domain: string
    certificate: string
    private_key: string
    alpn?: string
}): Promise<ApiResponse<SNI>> {
    return api.post<SNI>("/api/v1/sni", data)
}

export async function createSNIWithPaths(data: {
    name: string
    domain: string
    cert_path: string
    key_path: string
    alpn?: string
}): Promise<ApiResponse<SNI>> {
    return api.post<SNI>("/api/v1/sni/paths", data)
}

export async function updateSNI(id: number, data: {
    name?: string
    domain?: string
    certificate?: string
    private_key?: string
    alpn?: string
}): Promise<ApiResponse<void>> {
    return api.put<void>(`/api/v1/sni/${id}`, data)
}

export async function deleteSNI(id: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/sni/${id}`)
}

export async function validateSNICertificate(data: {
    certificate: string
    private_key?: string
    domain?: string
}): Promise<ApiResponse<ValidateCertResponse>> {
    return api.post<ValidateCertResponse>("/api/v1/sni/validate", data)
}

export async function renewSNICert(id: number): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/sni/${id}/renew`)
}

export async function getSNIUsage(id: number): Promise<ApiResponse<{ used_by: number }>> {
    return api.get<{ used_by: number }>(`/api/v1/sni/${id}/usage`)
}

// --- ACME (Let's Encrypt) issuance ---

export async function issueSNICertHTTP01(data: { name: string; domain: string }): Promise<ApiResponse<SNI>> {
    return api.post<SNI>("/api/v1/sni/acme/http01", data)
}

export async function startSNIDNS01(domain: string): Promise<ApiResponse<DNSChallengeResponse>> {
    return api.post<DNSChallengeResponse>("/api/v1/sni/acme/dns01/start", { domain })
}

export async function completeSNIDNS01(data: { name: string; domain: string }): Promise<ApiResponse<SNI>> {
    return api.post<SNI>("/api/v1/sni/acme/dns01/complete", data)
}
