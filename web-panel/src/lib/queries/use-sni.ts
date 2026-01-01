import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    listSNIs,
    getSNI,
    createSNI,
    createSNIWithPaths,
    updateSNI,
    deleteSNI,
    validateSNICertificate,
    renewSNICert,
    getSNIUsage,
    issueSNICertHTTP01,
    startSNIDNS01,
    completeSNIDNS01,
} from "@/lib/admin-api"
import { queryKeys } from "./keys"
import { toast } from "sonner"

// ==================== Queries ====================

export function useSNIs() {
    return useQuery({
        queryKey: queryKeys.sniList(),
        queryFn: async () => {
            const res = await listSNIs()
            if (!res.success) throw new Error(res.error || "Failed to fetch domains")
            return res.data || []
        },
        staleTime: 5 * 60 * 1000,
    })
}

export function useSNI(id: number) {
    return useQuery({
        queryKey: queryKeys.sniDetails(id),
        queryFn: async () => {
            const res = await getSNI(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch domain")
            return res.data!
        },
        enabled: id > 0,
    })
}

// ==================== Mutations ====================

export function useCreateSNI() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (data: { name: string; domain: string; certificate: string; private_key: string; alpn?: string }) => {
            const res = await createSNI(data)
            if (!res.success) throw new Error(res.error || "Failed to create domain")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Domain added successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.sni })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useCreateSNIWithPaths() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (data: { name: string; domain: string; cert_path: string; key_path: string; alpn?: string }) => {
            const res = await createSNIWithPaths(data)
            if (!res.success) throw new Error(res.error || "Failed to create domain")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Domain added successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.sni })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useUpdateSNI() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, data }: { id: number; data: { name?: string; domain?: string; certificate?: string; private_key?: string; alpn?: string } }) => {
            const res = await updateSNI(id, data)
            if (!res.success) throw new Error(res.error || "Failed to update domain")
        },
        onSuccess: (_, { id }) => {
            toast.success("Domain updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.sniDetails(id) })
            queryClient.invalidateQueries({ queryKey: queryKeys.sni })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useDeleteSNI() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteSNI(id)
            if (!res.success) throw new Error(res.error || "Failed to delete domain")
        },
        onSuccess: () => {
            toast.success("Domain deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.sni })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useValidateSNICert() {
    return useMutation({
        mutationFn: async (data: { certificate: string; private_key?: string; domain?: string }) => {
            const res = await validateSNICertificate(data)
            if (!res.success) throw new Error(res.error || "Invalid certificate")
            return res.data!
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Reports how many inbounds reference this domain's certificate. Used to warn
// before delete and show a "used by" count.
export function useSNIUsage(id: number, enabled = true) {
    return useQuery({
        queryKey: [...queryKeys.sni, 'usage', id],
        queryFn: async () => {
            const res = await getSNIUsage(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch usage")
            return res.data?.used_by ?? 0
        },
        enabled: enabled && id > 0,
        staleTime: 60 * 1000,
    })
}

export function useIssueSNIHTTP01() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (data: { name: string; domain: string }) => {
            const res = await issueSNICertHTTP01(data)
            if (!res.success) throw new Error(res.error || "Failed to issue certificate")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Certificate issued")
            queryClient.invalidateQueries({ queryKey: queryKeys.sni })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useStartSNIDNS01() {
    return useMutation({
        mutationFn: async (domain: string) => {
            const res = await startSNIDNS01(domain)
            if (!res.success) throw new Error(res.error || "Failed to start DNS challenge")
            return res.data!
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useCompleteSNIDNS01() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (data: { name: string; domain: string }) => {
            const res = await completeSNIDNS01(data)
            if (!res.success) throw new Error(res.error || "Failed to complete DNS challenge")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Certificate issued via DNS-01")
            queryClient.invalidateQueries({ queryKey: queryKeys.sni })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useRenewSNICert() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await renewSNICert(id)
            if (!res.success) throw new Error(res.error || "Failed to renew certificate")
        },
        onSuccess: () => {
            toast.success("Certificate renewed successfully")
            queryClient.invalidateQueries({ queryKey: queryKeys.sni })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}
