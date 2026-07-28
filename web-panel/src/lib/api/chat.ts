import { api, type ApiResponse } from "@/lib/api"

export interface ChatMessage {
    id: number
    subscription_id: number
    sender_type: "user" | "admin"
    admin_id?: number
    content: string
    is_read: boolean
    is_pinned: boolean
    reply_to_message_id?: number | null
    edited_at?: string | null
    created_at: string
}

export type ChatSearchHit = ChatMessage

export interface ChatReaction {
    id: number
    message_id: number
    reactor: "user" | "admin"
    admin_id?: number | null
    emoji: string
    created_at: string
}

export interface ConversationSummary {
    subscription_id: number
    subscription_label: string
    subscription_status: string
    user_id?: number
    username?: string
    first_name?: string
    last_message_content: string
    last_message_at: string
    last_sender_type: "user" | "admin"
    unread_count: number
}

interface PaginatedResponse<T> {
    success: boolean
    data: T
    meta: {
        page: number
        per_page: number
        total: number
        total_pages: number
    }
}

// Sub panel (public)
export async function getSubChatMessages(uuid: string, page = 1, limit = 50): Promise<PaginatedResponse<ChatMessage[]>> {
    return api.getRaw(`/api/v1/public/sub/${uuid}/chat?page=${page}&limit=${limit}`)
}

export async function sendSubChatMessage(uuid: string, content: string, replyToMessageId?: number): Promise<ApiResponse<ChatMessage>> {
    return api.post(`/api/v1/public/sub/${uuid}/chat`, { content, reply_to_message_id: replyToMessageId })
}

export async function editSubChatMessage(uuid: string, messageId: number, content: string): Promise<ApiResponse<ChatMessage>> {
    return api.patch(`/api/v1/public/sub/${uuid}/chat/${messageId}`, { content })
}

export async function deleteSubChatMessage(uuid: string, messageId: number): Promise<ApiResponse<void>> {
    return api.delete(`/api/v1/public/sub/${uuid}/chat/${messageId}`)
}

export async function markSubChatRead(uuid: string): Promise<ApiResponse<void>> {
    return api.put(`/api/v1/public/sub/${uuid}/chat/read`)
}

export async function listSubMessageReactions(uuid: string, messageId: number): Promise<ApiResponse<ChatReaction[]>> {
    return api.get(`/api/v1/public/sub/${uuid}/chat/messages/${messageId}/reactions`)
}

export async function addSubMessageReaction(uuid: string, messageId: number, emoji: string): Promise<ApiResponse<void>> {
    return api.post(`/api/v1/public/sub/${uuid}/chat/messages/${messageId}/reactions`, { emoji })
}

export async function removeSubMessageReaction(uuid: string, messageId: number, emoji: string): Promise<ApiResponse<void>> {
    return api.delete(`/api/v1/public/sub/${uuid}/chat/messages/${messageId}/reactions/${encodeURIComponent(emoji)}`)
}

export async function replaceSubMessageReaction(uuid: string, messageId: number, oldEmoji: string, newEmoji: string): Promise<ApiResponse<void>> {
    return api.patch(`/api/v1/public/sub/${uuid}/chat/messages/${messageId}/reactions/${encodeURIComponent(oldEmoji)}`, { new_emoji: newEmoji })
}

// Admin
export interface ChatListFilters {
    page?: number
    limit?: number
    search?: string
    status?: string
    unread?: boolean
    mine?: boolean
    pinned?: boolean
    sort?: "recent" | "unread" | "oldest"
}

export async function getAdminChats(filters: ChatListFilters = {}): Promise<PaginatedResponse<ConversationSummary[]>> {
    const params = new URLSearchParams()
    params.set("page", String(filters.page ?? 1))
    params.set("limit", String(filters.limit ?? 20))
    if (filters.search) params.set("search", filters.search)
    if (filters.status) params.set("status", filters.status)
    if (filters.unread) params.set("unread", "true")
    if (filters.sort) params.set("sort", filters.sort)
    if (filters.mine) params.set("mine", "true")
    if (filters.pinned) params.set("pinned", "true")
    return api.getRaw(`/api/v1/admin/chats?${params}`)
}

export async function getAdminChatMessages(subscriptionId: number, page = 1, limit = 50): Promise<PaginatedResponse<ChatMessage[]>> {
    return api.getRaw(`/api/v1/admin/chats/${subscriptionId}/messages?page=${page}&limit=${limit}`)
}

export async function sendAdminChatMessage(subscriptionId: number, content: string, replyToMessageId?: number): Promise<ApiResponse<ChatMessage>> {
    return api.post(`/api/v1/admin/chats/${subscriptionId}/messages`, { content, reply_to_message_id: replyToMessageId })
}

export async function listAdminMessageReactions(subscriptionId: number, messageId: number): Promise<ApiResponse<ChatReaction[]>> {
    return api.get(`/api/v1/admin/chats/${subscriptionId}/messages/${messageId}/reactions`)
}

export async function addAdminMessageReaction(subscriptionId: number, messageId: number, emoji: string): Promise<ApiResponse<void>> {
    return api.post(`/api/v1/admin/chats/${subscriptionId}/messages/${messageId}/reactions`, { emoji })
}

export async function removeAdminMessageReaction(subscriptionId: number, messageId: number, emoji: string): Promise<ApiResponse<void>> {
    return api.delete(`/api/v1/admin/chats/${subscriptionId}/messages/${messageId}/reactions/${encodeURIComponent(emoji)}`)
}

export async function replaceAdminMessageReaction(subscriptionId: number, messageId: number, oldEmoji: string, newEmoji: string): Promise<ApiResponse<void>> {
    return api.patch(`/api/v1/admin/chats/${subscriptionId}/messages/${messageId}/reactions/${encodeURIComponent(oldEmoji)}`, { new_emoji: newEmoji })
}

export async function markChatRead(subscriptionId: number): Promise<ApiResponse<void>> {
    return api.put(`/api/v1/admin/chats/${subscriptionId}/read`)
}

export async function getUnreadChatCount(): Promise<ApiResponse<{ count: number }>> {
    return api.get(`/api/v1/admin/chats/unread-count`)
}

export async function pinMessage(subscriptionId: number, messageId: number): Promise<ApiResponse<void>> {
    return api.put(`/api/v1/admin/chats/${subscriptionId}/messages/${messageId}/pin`)
}

export async function unpinMessage(subscriptionId: number, messageId: number): Promise<ApiResponse<void>> {
    return api.delete(`/api/v1/admin/chats/${subscriptionId}/messages/${messageId}/pin`)
}

export async function getPinnedMessages(subscriptionId: number): Promise<ApiResponse<ChatMessage[]>> {
    return api.get(`/api/v1/admin/chats/${subscriptionId}/pinned`)
}

export async function searchAdminChatMessages(
    subscriptionId: number,
    q: string,
    page = 1,
    limit = 25,
): Promise<PaginatedResponse<ChatSearchHit[]>> {
    const params = new URLSearchParams({ q, page: String(page), limit: String(limit) })
    return api.getRaw(`/api/v1/admin/chats/${subscriptionId}/search?${params}`)
}

export async function editAdminChatMessage(
    subscriptionId: number,
    messageId: number,
    content: string,
): Promise<ApiResponse<ChatMessage>> {
    return api.patch(`/api/v1/admin/chats/${subscriptionId}/messages/${messageId}`, { content })
}

export async function deleteAdminChatMessage(
    subscriptionId: number,
    messageId: number,
): Promise<ApiResponse<void>> {
    return api.delete(`/api/v1/admin/chats/${subscriptionId}/messages/${messageId}`)
}
