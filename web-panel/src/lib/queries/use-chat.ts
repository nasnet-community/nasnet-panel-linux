import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { queryKeys } from "./keys"
import {
    getSubChatMessages,
    sendSubChatMessage,
    getAdminChats,
    getAdminChatMessages,
    sendAdminChatMessage,
    markChatRead,
    getUnreadChatCount,
    pinMessage,
    unpinMessage,
    getPinnedMessages,
    searchAdminChatMessages,
    editAdminChatMessage,
    deleteAdminChatMessage,
    editSubChatMessage,
    deleteSubChatMessage,
    markSubChatRead,
    listSubMessageReactions,
    addSubMessageReaction,
    removeSubMessageReaction,
    replaceSubMessageReaction,
    listAdminMessageReactions,
    addAdminMessageReaction,
    removeAdminMessageReaction,
    replaceAdminMessageReaction,
    type ChatMessage,
    type ChatListFilters,
    type ChatReaction,
} from "@/lib/api/chat"

// Sub panel hooks
export function useSubChat(uuid: string, page = 1) {
    return useQuery({
        queryKey: queryKeys.subChat(uuid),
        queryFn: async () => {
            const res = await getSubChatMessages(uuid, page)
            if (!res.success) throw new Error("Failed to fetch messages")
            return { messages: res.data ?? [], meta: res.meta }
        },
        refetchInterval: false,
    })
}

export function useSendSubChatMessage(uuid: string) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (content: string) => {
            const res = await sendSubChatMessage(uuid, content)
            if (!res.success) throw new Error(res.error || "Failed to send message")
            return res.data!
        },
        onError: (err: Error) => {
            toast.error(err.message)
        },
        onSettled: () => {
            queryClient.invalidateQueries({ queryKey: queryKeys.subChat(uuid) })
        },
    })
}

// Admin hooks
export function useAdminChats(filters: ChatListFilters = {}) {
    return useQuery({
        queryKey: queryKeys.chatList(filters),
        queryFn: async () => {
            const res = await getAdminChats(filters)
            if (!res.success) throw new Error("Failed to fetch conversations")
            return { conversations: res.data ?? [], meta: res.meta }
        },
    })
}

export function useAdminChatMessages(subscriptionId: number | null, page = 1) {
    return useQuery({
        queryKey: queryKeys.chatMessages(subscriptionId ?? 0),
        queryFn: async () => {
            if (!subscriptionId) return { messages: [], meta: { page: 1, per_page: 50, total: 0, total_pages: 0 } }
            const res = await getAdminChatMessages(subscriptionId, page)
            if (!res.success) throw new Error("Failed to fetch messages")
            return { messages: res.data ?? [], meta: res.meta }
        },
        enabled: !!subscriptionId,
    })
}

export function useSendAdminChatMessage(subscriptionId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (args: string | { content: string; replyToMessageId?: number }) => {
            const content = typeof args === "string" ? args : args.content
            const replyToMessageId = typeof args === "string" ? undefined : args.replyToMessageId
            const res = await sendAdminChatMessage(subscriptionId, content, replyToMessageId)
            if (!res.success) throw new Error(res.error || "Failed to send message")
            return res.data!
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.chats })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useMarkChatRead(subscriptionId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async () => {
            const res = await markChatRead(subscriptionId)
            if (!res.success) throw new Error(res.error || "Failed to mark as read")
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chats })
            queryClient.invalidateQueries({ queryKey: queryKeys.chatUnreadCount() })
        },
    })
}

export function useUnreadChatCount() {
    return useQuery({
        queryKey: queryKeys.chatUnreadCount(),
        queryFn: async () => {
            const res = await getUnreadChatCount()
            if (!res.success) return 0
            return res.data?.count ?? 0
        },
        refetchOnWindowFocus: false,
        staleTime: 30_000,
    })
}

export function useAdminChatMessagesInfinite(subscriptionId: number | null) {
    return useInfiniteQuery({
        queryKey: queryKeys.chatMessages(subscriptionId ?? 0),
        queryFn: async ({ pageParam = 1 }) => {
            if (!subscriptionId) return { messages: [] as ChatMessage[], meta: { page: 1, per_page: 50, total: 0, total_pages: 0 } }
            const res = await getAdminChatMessages(subscriptionId, pageParam)
            if (!res.success) throw new Error("Failed to fetch messages")
            return { messages: (res.data ?? []) as ChatMessage[], meta: res.meta }
        },
        initialPageParam: 1,
        getNextPageParam: (lastPage) => {
            if (lastPage.meta.page < lastPage.meta.total_pages) return lastPage.meta.page + 1
            return undefined
        },
        enabled: !!subscriptionId,
    })
}

export function useSubChatInfinite(uuid: string) {
    return useInfiniteQuery({
        queryKey: queryKeys.subChat(uuid),
        queryFn: async ({ pageParam = 1 }) => {
            const res = await getSubChatMessages(uuid, pageParam)
            if (!res.success) throw new Error("Failed to fetch messages")
            return { messages: (res.data ?? []) as ChatMessage[], meta: res.meta }
        },
        initialPageParam: 1,
        getNextPageParam: (lastPage) => {
            if (lastPage.meta.page < lastPage.meta.total_pages) return lastPage.meta.page + 1
            return undefined
        },
    })
}

export function usePinMessage() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ subscriptionId, messageId }: { subscriptionId: number; messageId: number }) => {
            const res = await pinMessage(subscriptionId, messageId)
            if (!res.success) throw new Error(res.error || "Failed to pin message")
        },
        onSuccess: (_, { subscriptionId }) => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.chatPinned(subscriptionId) })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useUnpinMessage() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ subscriptionId, messageId }: { subscriptionId: number; messageId: number }) => {
            const res = await unpinMessage(subscriptionId, messageId)
            if (!res.success) throw new Error(res.error || "Failed to unpin message")
        },
        onSuccess: (_, { subscriptionId }) => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.chatPinned(subscriptionId) })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function usePinnedMessages(subscriptionId: number | null) {
    return useQuery({
        queryKey: queryKeys.chatPinned(subscriptionId ?? 0),
        queryFn: async () => {
            if (!subscriptionId) return []
            const res = await getPinnedMessages(subscriptionId)
            if (!res.success) return []
            return res.data ?? []
        },
        enabled: !!subscriptionId,
    })
}

export function useSearchMessages(subscriptionId: number | null, q: string) {
    return useQuery({
        queryKey: queryKeys.chatSearch(subscriptionId ?? 0, q.trim()),
        queryFn: async () => {
            if (!subscriptionId || q.trim().length < 2) {
                return { hits: [], meta: { page: 1, per_page: 0, total: 0, total_pages: 0 } }
            }
            const res = await searchAdminChatMessages(subscriptionId, q.trim())
            if (!res.success) throw new Error("Search failed")
            return { hits: res.data ?? [], meta: res.meta }
        },
        enabled: !!subscriptionId && q.trim().length >= 2,
    })
}

export function useEditMessage(subscriptionId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ messageId, content }: { messageId: number; content: string }) => {
            const res = await editAdminChatMessage(subscriptionId, messageId, content)
            if (!res.success) throw new Error(res.error || "Failed to edit")
            return res.data!
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            qc.invalidateQueries({ queryKey: queryKeys.chatPinned(subscriptionId) })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useDeleteMessage(subscriptionId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (messageId: number) => {
            const res = await deleteAdminChatMessage(subscriptionId, messageId)
            if (!res.success) throw new Error(res.error || "Failed to delete")
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            qc.invalidateQueries({ queryKey: queryKeys.chatPinned(subscriptionId) })
            qc.invalidateQueries({ queryKey: queryKeys.chats })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

// ─── User-side mutations (widget) ────────────────────────────────────────────

export function useEditSubChatMessage(uuid: string) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ messageId, content }: { messageId: number; content: string }) => {
            const res = await editSubChatMessage(uuid, messageId, content)
            if (!res.success) throw new Error(res.error || "Failed to edit")
            return res.data!
        },
        onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.subChat(uuid) }),
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useDeleteSubChatMessage(uuid: string) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (messageId: number) => {
            const res = await deleteSubChatMessage(uuid, messageId)
            if (!res.success) throw new Error(res.error || "Failed to delete")
        },
        onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.subChat(uuid) }),
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useMarkSubChatRead(uuid: string) {
    return useMutation({
        mutationFn: async () => {
            const res = await markSubChatRead(uuid)
            if (!res.success) throw new Error(res.error || "Failed")
        },
    })
}

// ─── Reactions (shared user/admin scopes) ────────────────────────────────────

export interface ReactionScope {
    uuid?: string
    subscriptionId?: number
}

export function useMessageReactions(scope: ReactionScope, messageId: number, enabled = true) {
    return useQuery<ChatReaction[]>({
        queryKey: queryKeys.chatReactions(messageId),
        queryFn: async () => {
            if (scope.uuid) {
                const res = await listSubMessageReactions(scope.uuid, messageId)
                if (!res.success) return []
                return res.data ?? []
            }
            if (scope.subscriptionId != null) {
                const res = await listAdminMessageReactions(scope.subscriptionId, messageId)
                if (!res.success) return []
                return res.data ?? []
            }
            return []
        },
        enabled,
    })
}

export function useAddReaction(scope: ReactionScope) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ messageId, emoji }: { messageId: number; emoji: string }) => {
            if (scope.uuid) {
                const res = await addSubMessageReaction(scope.uuid, messageId, emoji)
                if (!res.success) throw new Error(res.error || "Failed")
            } else if (scope.subscriptionId != null) {
                const res = await addAdminMessageReaction(scope.subscriptionId, messageId, emoji)
                if (!res.success) throw new Error(res.error || "Failed")
            }
        },
        onSuccess: (_, { messageId }) => {
            qc.invalidateQueries({ queryKey: queryKeys.chatReactions(messageId) })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useRemoveReaction(scope: ReactionScope) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ messageId, emoji }: { messageId: number; emoji: string }) => {
            if (scope.uuid) {
                const res = await removeSubMessageReaction(scope.uuid, messageId, emoji)
                if (!res.success) throw new Error(res.error || "Failed")
            } else if (scope.subscriptionId != null) {
                const res = await removeAdminMessageReaction(scope.subscriptionId, messageId, emoji)
                if (!res.success) throw new Error(res.error || "Failed")
            }
        },
        onSuccess: (_, { messageId }) => {
            qc.invalidateQueries({ queryKey: queryKeys.chatReactions(messageId) })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useReplaceReaction(scope: ReactionScope) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ messageId, oldEmoji, newEmoji }: { messageId: number; oldEmoji: string; newEmoji: string }) => {
            if (scope.uuid) {
                const res = await replaceSubMessageReaction(scope.uuid, messageId, oldEmoji, newEmoji)
                if (!res.success) throw new Error(res.error || "Failed")
            } else if (scope.subscriptionId != null) {
                const res = await replaceAdminMessageReaction(scope.subscriptionId, messageId, oldEmoji, newEmoji)
                if (!res.success) throw new Error(res.error || "Failed")
            }
        },
        onSuccess: (_, { messageId }) => {
            qc.invalidateQueries({ queryKey: queryKeys.chatReactions(messageId) })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}
