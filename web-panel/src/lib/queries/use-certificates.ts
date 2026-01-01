import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    getCA,
    initializeCA,
    listCertificates,
    getMasterCert,
    generateMasterCert,
    getAgentCert,
    generateAgentCert,
    regenerateAgentCert,
    getCertBundle,
    revokeCertificate,
    getExpiringSoonCerts,
    issuePublicCertificate,
    startDNSChallenge,
    completeDNSChallenge,
    renewCertificate,
    deleteCertificate,
    getCertificateDetails,
    toggleAutoRenew,
} from "@/lib/admin-api"
import { queryKeys } from "./keys"
import { toast } from "sonner"

// ==================== Queries ====================

// Get CA certificate
export function useCA() {
    return useQuery({
        queryKey: queryKeys.ca(),
        queryFn: async () => {
            const res = await getCA()
            if (!res.success) return null
            return res.data
        },
        staleTime: 10 * 60 * 1000, // 10 minutes — CA rarely changes
    })
}

// Check if CA exists
export function useHasCA() {
    const { data, isLoading } = useCA()
    return {
        hasCA: !!data,
        isLoading,
    }
}

// List all certificates
export function useCertificates() {
    return useQuery({
        queryKey: queryKeys.certificates,
        queryFn: async () => {
            const res = await listCertificates()
            if (!res.success) throw new Error(res.error || "Failed to fetch certificates")
            return res.data?.certificates || []
        },
        staleTime: 5 * 60 * 1000, // 5 minutes — certificates don't change often
    })
}

// Get master certificate
export function useMasterCert() {
    return useQuery({
        queryKey: queryKeys.masterCert(),
        queryFn: async () => {
            const res = await getMasterCert()
            if (!res.success) return null
            return res.data
        },
        staleTime: 10 * 60 * 1000, // 10 minutes — master cert rarely changes
    })
}

// Get agent certificate for a node
export function useAgentCert(nodeId: number) {
    return useQuery({
        queryKey: queryKeys.agentCert(nodeId),
        queryFn: async () => {
            const res = await getAgentCert(nodeId)
            if (!res.success) return null
            return res.data
        },
        enabled: nodeId > 0,
    })
}

// Get certificate details by ID (includes PEM and private key)
export function useCertificateDetails(certId: number) {
    return useQuery({
        queryKey: [...queryKeys.certificates, 'details', certId] as const,
        queryFn: async () => {
            const res = await getCertificateDetails(certId)
            if (!res.success) return null
            return res.data
        },
        enabled: certId > 0,
    })
}

// Get certificate bundle for a node
export function useCertBundle(nodeId: number) {
    return useQuery({
        queryKey: queryKeys.certBundle(nodeId),
        queryFn: async () => {
            const res = await getCertBundle(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch certificate bundle")
            return res.data!
        },
        enabled: nodeId > 0,
    })
}

// Get expiring certificates
export function useExpiringSoonCerts(days = 30) {
    return useQuery({
        queryKey: [...queryKeys.certificates, 'expiring', days] as const,
        queryFn: async () => {
            const res = await getExpiringSoonCerts(days)
            if (!res.success) throw new Error(res.error || "Failed to fetch expiring certificates")
            return res.data!
        },
    })
}

// ==================== Mutations ====================

// Initialize CA
export function useInitializeCA() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (config?: { common_name?: string; organization?: string; valid_years?: number }) => {
            const res = await initializeCA(config)
            if (!res.success) throw new Error(res.error || "Failed to initialize CA")
            return res.data!
        },
        onSuccess: () => {
            toast.success("CA initialized")
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Generate master certificate
export function useGenerateMasterCert() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async () => {
            const res = await generateMasterCert()
            if (!res.success) throw new Error(res.error || "Failed to generate master certificate")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Master certificate generated")
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Generate agent certificate
export function useGenerateAgentCert() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (nodeId: number) => {
            const res = await generateAgentCert(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to generate agent certificate")
            return res.data!
        },
        onSuccess: (_, nodeId) => {
            toast.success("Agent certificate generated")
            queryClient.invalidateQueries({ queryKey: queryKeys.agentCert(nodeId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Regenerate agent certificate
export function useRegenerateAgentCert() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (nodeId: number) => {
            const res = await regenerateAgentCert(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to regenerate agent certificate")
            return res.data!
        },
        onSuccess: (_, nodeId) => {
            toast.success("Agent certificate regenerated")
            queryClient.invalidateQueries({ queryKey: queryKeys.agentCert(nodeId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Revoke certificate
export function useRevokeCertificate() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await revokeCertificate(id)
            if (!res.success) throw new Error(res.error || "Failed to revoke certificate")
        },
        onSuccess: () => {
            toast.success("Certificate revoked")
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Renew certificate
export function useRenewCertificate() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await renewCertificate(id)
            if (!res.success) throw new Error(res.error || "Failed to renew certificate")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Certificate renewed successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Delete certificate
export function useDeleteCertificate() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteCertificate(id)
            if (!res.success) throw new Error(res.error || "Failed to delete certificate")
        },
        onSuccess: () => {
            toast.success("Certificate deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Issue public certificate
export function useIssuePublicCertificate() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ domain }: { domain: string }) => {
            const res = await issuePublicCertificate(domain)
            if (!res.success) throw new Error(res.error || "Failed to issue public certificate")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Certificate issued successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Start DNS-01 challenge
export function useStartDNSChallenge() {
    return useMutation({
        mutationFn: async ({ domain }: { domain: string }) => {
            const res = await startDNSChallenge(domain)
            if (!res.success) throw new Error(res.error || "Failed to start DNS challenge")
            return res.data!
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Complete DNS-01 challenge
export function useCompleteDNSChallenge() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ domain }: { domain: string }) => {
            const res = await completeDNSChallenge(domain)
            if (!res.success) throw new Error(res.error || "Failed to complete DNS challenge")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Certificate issued successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Toggle auto-renew
export function useToggleAutoRenew() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, enabled }: { id: number; enabled: boolean }) => {
            const res = await toggleAutoRenew(id, enabled)
            if (!res.success) throw new Error(res.error || "Failed to toggle auto-renew")
            return res.data!
        },
        onSuccess: (_, { enabled }) => {
            toast.success(enabled ? "Auto-renew enabled" : "Auto-renew disabled")
            queryClient.invalidateQueries({ queryKey: queryKeys.certificates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}
