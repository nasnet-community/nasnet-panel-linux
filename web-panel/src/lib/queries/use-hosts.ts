import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    listAllHosts,
    createHost,
    updateHost,
    deleteHost,
    duplicateHost,
    bulkUpdateHosts,
    listHostTags,
    listHostTemplates,
    createHostTemplate,
    updateHostTemplate as updateHostTemplateApi,
    deleteHostTemplate as deleteHostTemplateApi,
    applyHostTemplate,
    type ListHostsParams,
} from "@/lib/admin-api"
import type { Host, HostTemplate } from "@/lib/types"
import { queryKeys } from "./keys"
import { toast } from "sonner"

// ==================== Types ====================

export interface UseHostListParams {
    search?: string
    nodeId?: number
    inboundId?: number
    planId?: number
    disabled?: boolean
    hostType?: "server" | "info"
    tag?: string
    sortBy?: string
    sortOrder?: "asc" | "desc"
    page: number
    perPage: number
}

// ==================== Queries ====================

export function useHostList(params: UseHostListParams) {
    return useQuery({
        queryKey: queryKeys.hostList(params),
        queryFn: async () => {
            const apiParams: ListHostsParams = {
                offset: (params.page - 1) * params.perPage,
                limit: params.perPage,
            }
            if (params.search) apiParams.search = params.search
            if (params.nodeId) apiParams.node_id = params.nodeId
            if (params.inboundId) apiParams.inbound_id = params.inboundId
            if (params.planId) apiParams.plan_id = params.planId
            if (params.disabled !== undefined) apiParams.disabled = params.disabled
            if (params.hostType) apiParams.host_type = params.hostType
            if (params.tag) apiParams.tag = params.tag
            if (params.sortBy) apiParams.sort_by = params.sortBy
            if (params.sortOrder) apiParams.sort_order = params.sortOrder

            const res = await listAllHosts(apiParams)
            if (!res.success) throw new Error(res.error || "Failed to fetch hosts")
            return {
                hosts: Array.isArray(res.data) ? res.data : [],
                total: res.meta?.total ?? 0,
            }
        },
    })
}

export function useHostTags() {
    return useQuery({
        queryKey: queryKeys.hostTags(),
        queryFn: async () => {
            const res = await listHostTags()
            if (!res.success) throw new Error(res.error || "Failed to fetch host tags")
            return res.data ?? []
        },
    })
}

export function useHostTemplates() {
    return useQuery({
        queryKey: queryKeys.hostTemplateList(),
        queryFn: async () => {
            const res = await listHostTemplates()
            if (!res.success) throw new Error(res.error || "Failed to fetch host templates")
            return res.data ?? []
        },
    })
}

// ==================== Mutations ====================

export function useCreateHostMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (data: Partial<Host> & ({ inbound_id: number } | { plan_id: number })) => {
            const res = await createHost(data)
            if (!res.success) throw new Error(res.error || "Failed to create host")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Host created")
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useUpdateHostMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, data }: { id: number; data: Partial<Host> }) => {
            const res = await updateHost(id, data)
            if (!res.success) throw new Error(res.error || "Failed to update host")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Host updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useDeleteHostMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteHost(id)
            if (!res.success) throw new Error(res.error || "Failed to delete host")
        },
        onSuccess: () => {
            toast.success("Host deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useDuplicateHostMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await duplicateHost(id)
            if (!res.success) throw new Error(res.error || "Failed to duplicate host")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Host duplicated")
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useToggleHostMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, isDisabled }: { id: number; isDisabled: boolean }) => {
            const res = await updateHost(id, { is_disabled: !isDisabled })
            if (!res.success) throw new Error(res.error || "Failed to toggle host")
            return res.data!
        },
        onSuccess: (_data, variables) => {
            toast.success(variables.isDisabled ? "Host enabled" : "Host disabled")
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useBulkUpdateHostsMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ ids, fields }: { ids: number[]; fields: Partial<Host> }) => {
            const res = await bulkUpdateHosts(ids, fields)
            if (!res.success) throw new Error(res.error || "Failed to bulk update hosts")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success(`${data.updated} host(s) updated`)
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// ==================== Template Mutations ====================

export function useCreateHostTemplateMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (data: Partial<HostTemplate>) => {
            const res = await createHostTemplate(data)
            if (!res.success) throw new Error(res.error || "Failed to create template")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Template created")
            queryClient.invalidateQueries({ queryKey: queryKeys.hostTemplates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useUpdateHostTemplateMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, data }: { id: number; data: Partial<HostTemplate> }) => {
            const res = await updateHostTemplateApi(id, data)
            if (!res.success) throw new Error(res.error || "Failed to update template")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Template updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.hostTemplates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useDeleteHostTemplateMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteHostTemplateApi(id)
            if (!res.success) throw new Error(res.error || "Failed to delete template")
        },
        onSuccess: () => {
            toast.success("Template deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.hostTemplates })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useApplyHostTemplateMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ templateId, hostIds }: { templateId: number; hostIds: number[] }) => {
            const res = await applyHostTemplate(templateId, hostIds)
            if (!res.success) throw new Error(res.error || "Failed to apply template")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success(`Template applied to ${data.updated} host(s)`)
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}
