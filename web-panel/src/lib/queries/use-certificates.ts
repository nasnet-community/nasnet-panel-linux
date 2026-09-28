import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { ApiResponse } from "@/lib/api"
import type { RotateResult } from "@/lib/types"
import {
    deleteCertificate,
    getCertificateDetails,
    getCertificateOverview,
    initializeCA,
    listCertificates,
    purgeCerts,
    revokeCertificate,
    revokeOrphanCerts,
    rotateExpiringCerts,
    rotateNodeCert,
    setCertStrictMode,
} from "@/lib/api/certificates"
import { queryKeys } from "./keys"

// List all certificates (inbound TLS form uses this)
export function useCertificates() {
    return useQuery({
        queryKey: queryKeys.certificates,
        queryFn: async () => {
            const res = await listCertificates()
            if (!res.success) throw new Error(res.error || "Failed to fetch certificates")
            return res.data?.certificates || []
        },
        staleTime: 5 * 60 * 1000,
    })
}

export function useCertOverview(refetchMs = 30_000) {
    return useQuery({
        queryKey: queryKeys.certOverview(),
        queryFn: async () => {
            const res = await getCertificateOverview()
            if (!res.success || !res.data) throw new Error(res.error || "Failed to load certificates")
            return res.data
        },
        refetchInterval: refetchMs,
        staleTime: 10_000,
    })
}

/** Re-reads file-based domain certs on their nodes, bypassing the 6 h cache. */
export function useRefreshDomainExpiry() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async () => {
            const res = await getCertificateOverview(true)
            if (!res.success || !res.data) throw new Error(res.error || "Failed to re-check domain files")
            return res.data
        },
        onSuccess: (data) => qc.setQueryData(queryKeys.certOverview(), data),
    })
}

export function useCertificateDetails(id: number | null) {
    return useQuery({
        queryKey: queryKeys.certDetails(id ?? 0),
        queryFn: async () => {
            const res = await getCertificateDetails(id!)
            if (!res.success || !res.data) throw new Error(res.error || "Failed to load certificate")
            return res.data
        },
        enabled: !!id,
    })
}

function useCertMutation<TVars, TData>(fn: (vars: TVars) => Promise<ApiResponse<TData>>) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (vars: TVars) => {
            const res = await fn(vars)
            if (!res.success) throw new Error(res.error || "Request failed")
            return res.data as TData
        },
        onSuccess: () => { void qc.invalidateQueries({ queryKey: queryKeys.certificates }) },
    })
}

export const useInitializeCA = () => useCertMutation<void, { id: number; common_name: string }>(() => initializeCA())
export const useRotateNodeCert = () => useCertMutation((nodeId: number) => rotateNodeCert(nodeId))
export const useRotateExpiring = () => useCertMutation<void, RotateResult[]>(() => rotateExpiringCerts())
export const useRevokeOrphans = () => useCertMutation<void, { revoked: number }>(() => revokeOrphanCerts())
export const usePurgeCerts = () => useCertMutation<void, { purged: number }>(() => purgeCerts())
export const useSetStrictMode = () => useCertMutation((enabled: boolean) => setCertStrictMode(enabled))
export const useRevokeCert = () => useCertMutation(({ id, confirm }: { id: number; confirm?: string }) => revokeCertificate(id, confirm))
export const useDeleteCert = () => useCertMutation(({ id, confirm }: { id: number; confirm?: string }) => deleteCertificate(id, confirm))
