import { useState, useRef, useEffect, useCallback, useMemo } from "react"
import { Link } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import {
    useAdminChats,
    useAdminChatMessagesInfinite,
    useSendAdminChatMessage,
    usePinnedMessages,
} from "@/lib/queries/use-chat"
import { useChatSocket } from "@/lib/hooks/use-chat-socket"
import { usePageTitle } from "@/hooks/use-page-title"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { cn } from "@/lib/utils"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Send, ArrowLeft, Search, MessageSquare, Pin, ExternalLink } from "lucide-react"
import { formatDistanceToNow } from "date-fns"
import { toast } from "sonner"
import { queryKeys } from "@/lib/queries/keys"
import type { ConversationSummary, ChatMessage } from "@/lib/api/chat"
import { ConversationSearch } from "@/components/chats/conversation-search"
import { InboxTabs, type InboxTab } from "@/components/chats/inbox-tabs"
import { VirtualizedThread } from "@/components/chats/virtualized-thread"
import { ReplyPreview } from "@/components/chats/reply-preview"
import { useAuthStore } from "@/store/auth-store"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import { useNavigate } from "react-router"
import type { Subscription } from "@/lib/types"

// ─── Conversation list item ──────────────────────────────────────────────────

interface ConversationItemProps {
    conv: ConversationSummary
    isSelected: boolean
    isOnline: boolean
    onClick: () => void
}

function ConversationItem({ conv, isSelected, isOnline, onClick }: ConversationItemProps) {
    const hasUnread = conv.unread_count > 0
    const displayName = conv.subscription_label || conv.username || `#${conv.subscription_id}`
    const relativeTime = conv.last_message_at
        ? formatDistanceToNow(new Date(conv.last_message_at), { addSuffix: true })
        : ""

    return (
        <button
            onClick={onClick}
            role="option"
            aria-selected={isSelected}
            tabIndex={isSelected ? 0 : -1}
            className={cn(
                "w-full text-left px-4 py-3 border-b border-border transition-colors hover:bg-muted/50",
                isSelected && "bg-muted",
                hasUnread && !isSelected && "bg-primary/5"
            )}
        >
            <div className="flex items-start justify-between gap-2">
                <span
                    className={cn(
                        "text-sm truncate flex-1",
                        hasUnread ? "font-semibold" : "font-medium"
                    )}
                >
                    {isOnline && (
                        <span className="inline-block h-1.5 w-1.5 rounded-full bg-green-500 mr-1.5 align-middle" />
                    )}
                    {displayName}
                </span>
                <div className="flex items-center gap-1.5 shrink-0">
                    {conv.last_message_at && (
                        <span className="text-[10px] text-muted-foreground whitespace-nowrap">
                            {relativeTime}
                        </span>
                    )}
                    {hasUnread && (
                        <Badge className="h-5 min-w-5 px-1 text-[10px] rounded-full flex items-center justify-center">
                            {conv.unread_count}
                        </Badge>
                    )}
                </div>
            </div>
            {conv.last_message_content && (
                <p
                    className={cn(
                        "text-xs text-muted-foreground truncate mt-0.5",
                        hasUnread && "text-foreground/70"
                    )}
                >
                    {conv.last_sender_type === "admin" && (
                        <span className="text-primary/70 mr-1">You:</span>
                    )}
                    {conv.last_message_content}
                </p>
            )}
        </button>
    )
}

// ─── Conversation list panel ─────────────────────────────────────────────────

interface ConversationListPanelProps {
    conversations: ConversationSummary[]
    isLoading: boolean
    selectedSubId: number | null
    search: string
    onSearchChange: (v: string) => void
    onSelect: (id: number) => void
    onlineSubIds: Set<number>
    sortBy: "recent" | "unread" | "oldest"
    onSortChange: (v: "recent" | "unread" | "oldest") => void
    statusFilter: string
    onStatusChange: (v: string) => void
    chatPage: number
    totalPages: number
    onPageChange: (page: number) => void
}

function ConversationListPanel({
    conversations,
    isLoading,
    selectedSubId,
    search,
    onSearchChange,
    onSelect,
    onlineSubIds,
    sortBy,
    onSortChange,
    statusFilter,
    onStatusChange,
    chatPage,
    totalPages,
    onPageChange,
}: ConversationListPanelProps) {
    return (
        <div className="flex flex-1 flex-col overflow-hidden">
            {/* Search bar */}
            <div className="p-3 border-b border-border">
                <div className="relative">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
                    <Input
                        className="pl-9"
                        placeholder="Search conversations..."
                        value={search}
                        onChange={(e) => onSearchChange(e.target.value)}
                        aria-label="Search conversations"
                    />
                </div>
            </div>

            {/* Filter / sort controls */}
            <div className="flex items-center gap-1 px-2 py-1.5 border-b border-border flex-wrap sm:gap-1.5 sm:px-3 sm:py-2">
                <select
                    value={sortBy}
                    onChange={(e) => onSortChange(e.target.value as "recent" | "unread" | "oldest")}
                    className="text-xs border border-input rounded-md px-2 py-1 bg-background text-foreground"
                    aria-label="Sort conversations"
                >
                    <option value="recent">Recent</option>
                    <option value="unread">Unread first</option>
                    <option value="oldest">Oldest</option>
                </select>
                <select
                    value={statusFilter}
                    onChange={(e) => onStatusChange(e.target.value)}
                    className="text-xs border border-input rounded-md px-2 py-1 bg-background text-foreground"
                    aria-label="Filter by status"
                >
                    <option value="">All status</option>
                    <option value="active">Active</option>
                    <option value="expired">Expired</option>
                    <option value="suspended">Suspended</option>
                </select>
            </div>

            {/* List */}
            <div
                className="flex-1 overflow-y-auto"
                role="listbox"
                aria-label="Conversations"
                tabIndex={0}
                onKeyDown={(e) => {
                    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return
                    e.preventDefault()
                    const idx = conversations.findIndex((c) => c.subscription_id === selectedSubId)
                    const next = e.key === "ArrowDown"
                        ? Math.min(conversations.length - 1, idx + 1)
                        : Math.max(0, idx - 1)
                    const target = conversations[next]
                    if (target) onSelect(target.subscription_id)
                }}
            >
                {isLoading ? (
                    <div className="flex flex-col gap-0">
                        {Array.from({ length: 6 }).map((_, i) => (
                            <div key={i} className="px-4 py-3 border-b border-border animate-pulse">
                                <div className="flex justify-between mb-1.5">
                                    <div className="h-3.5 bg-muted rounded w-32" />
                                    <div className="h-3 bg-muted rounded w-12" />
                                </div>
                                <div className="h-3 bg-muted rounded w-48" />
                            </div>
                        ))}
                    </div>
                ) : conversations.length === 0 ? (
                    <div className="flex flex-col items-center justify-center h-full gap-2 text-muted-foreground p-8">
                        <MessageSquare className="h-8 w-8 opacity-40" />
                        <p className="text-sm text-center">
                            {search ? "No conversations match your search" : "No conversations yet"}
                        </p>
                    </div>
                ) : (
                    conversations.map((conv) => (
                        <ConversationItem
                            key={conv.subscription_id}
                            conv={conv}
                            isSelected={selectedSubId === conv.subscription_id}
                            isOnline={onlineSubIds.has(conv.subscription_id)}
                            onClick={() => onSelect(conv.subscription_id)}
                        />
                    ))
                )}
            </div>

            {/* Pagination controls */}
            {totalPages > 1 && (
                <div className="flex items-center justify-between px-3 py-2 border-t border-border shrink-0">
                    <Button
                        variant="outline"
                        size="sm"
                        className="text-xs h-7"
                        disabled={chatPage <= 1}
                        onClick={() => onPageChange(chatPage - 1)}
                    >
                        Prev
                    </Button>
                    <span className="text-xs text-muted-foreground">
                        {chatPage} / {totalPages}
                    </span>
                    <Button
                        variant="outline"
                        size="sm"
                        className="text-xs h-7"
                        disabled={chatPage >= totalPages}
                        onClick={() => onPageChange(chatPage + 1)}
                    >
                        Next
                    </Button>
                </div>
            )}
        </div>
    )
}

// ─── Chat view panel ─────────────────────────────────────────────────────────

interface ChatViewPanelProps {
    conv: ConversationSummary | undefined
    subscriptionId: number
    isMobile: boolean
    onBack: () => void
    onUserOnlineChange: (subscriptionId: number, isOnline: boolean) => void
}

function ChatViewPanel({ conv, subscriptionId, isMobile, onBack, onUserOnlineChange }: ChatViewPanelProps) {
    const [input, setInput] = useState("")
    const [userTyping, setUserTyping] = useState(false)
    const [isUserOnline, setIsUserOnline] = useState(false)
    const [replyTo, setReplyTo] = useState<ChatMessage | null>(null)
    const inputRef = useRef<HTMLTextAreaElement>(null)
    const typingTimeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined)
    const lastTypingSentRef = useRef(0)
    const queryClient = useQueryClient()
    const me = useAuthStore((s) => s.user)
    const openDetailsSheet = useSubscriptionsStore((s) => s.openDetailsSheet)
    const navigate = useNavigate()

    const handleViewSubscription = useCallback(() => {
        openDetailsSheet({ id: subscriptionId } as Subscription)
        navigate("/subscriptions")
    }, [openDetailsSheet, subscriptionId, navigate])

    const {
        data,
        isLoading: messagesLoading,
        fetchNextPage,
        hasNextPage,
        isFetchingNextPage,
    } = useAdminChatMessagesInfinite(subscriptionId)
    const sendMessage = useSendAdminChatMessage(subscriptionId)
    const { data: pinnedMessages } = usePinnedMessages(subscriptionId)

    // Flat message list from infinite query pages (API returns newest-first)
    const rawMessages = useMemo(() => data?.pages.flatMap(p => p.messages) ?? [], [data])
    // Reverse for chronological display
    const messages = useMemo(() => [...rawMessages].reverse(), [rawMessages])

    // WebSocket connection
    const ws = useChatSocket(`/api/v1/admin/chats/${subscriptionId}/ws`, {
        onNewMessage: useCallback(() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.chats })
            queryClient.invalidateQueries({ queryKey: queryKeys.chatUnreadCount() })
        }, [queryClient, subscriptionId]),
        onMessageAck: useCallback(() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.chats })
        }, [queryClient, subscriptionId]),
        onMessageEdited: useCallback(() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.chatPinned(subscriptionId) })
        }, [queryClient, subscriptionId]),
        onMessageDeleted: useCallback(() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.chatPinned(subscriptionId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.chats })
        }, [queryClient, subscriptionId]),
        onAdminMessagesRead: useCallback(() => {
            // User has seen our messages; refresh so ✓ flips to ✓✓.
            queryClient.invalidateQueries({ queryKey: queryKeys.chatMessages(subscriptionId) })
        }, [queryClient, subscriptionId]),
        onReactionAdded: useCallback((id: number) => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatReactions(id) })
        }, [queryClient]),
        onReactionRemoved: useCallback((id: number) => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chatReactions(id) })
        }, [queryClient]),
        onTyping: useCallback((senderType: "user" | "admin") => {
            if (senderType === "user") {
                setUserTyping(true)
                if (typingTimeoutRef.current) clearTimeout(typingTimeoutRef.current)
                typingTimeoutRef.current = setTimeout(() => setUserTyping(false), 3000)
            }
        }, []),
        onOnlineStatus: useCallback((isOnline: boolean, senderType: "user" | "admin") => {
            if (senderType === "user") {
                setIsUserOnline(isOnline)
                onUserOnlineChange(subscriptionId, isOnline)
            }
        }, [subscriptionId, onUserOnlineChange]),
        onMessagesRead: useCallback(() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.chats })
            queryClient.invalidateQueries({ queryKey: queryKeys.chatUnreadCount() })
        }, [queryClient]),
        onError: useCallback((message: string) => {
            toast.error(message)
        }, []),
    })

    // Mark as read via WS — single fire per subscription change.
    // Depend only on subscriptionId + the stable sendMarkRead callback so re-renders
    // (e.g. WS messages arriving) don't repeatedly clear the 500 ms timer.
    const markReadFiredRef = useRef<number | null>(null)
    const { sendMarkRead } = ws
    useEffect(() => {
        if (markReadFiredRef.current === subscriptionId) return
        const t = setTimeout(() => {
            sendMarkRead()
            markReadFiredRef.current = subscriptionId
        }, 500)
        return () => clearTimeout(t)
    }, [subscriptionId, sendMarkRead])

    // Cleanup typing timeout on unmount
    useEffect(() => {
        return () => {
            if (typingTimeoutRef.current) clearTimeout(typingTimeoutRef.current)
        }
    }, [])

    const handleSend = useCallback(() => {
        const trimmed = input.trim()
        if (!trimmed || sendMessage.isPending) return
        sendMessage.mutate({ content: trimmed, replyToMessageId: replyTo?.id })
        setInput("")
        setReplyTo(null)
        inputRef.current?.focus()
    }, [input, sendMessage, replyTo])

    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
            if (e.key === "Enter" && !e.shiftKey && !(e.nativeEvent as any).isComposing) {
                e.preventDefault()
                handleSend()
                return
            }
            if (e.key === "Escape" && replyTo) {
                e.preventDefault()
                setReplyTo(null)
            }
        },
        [handleSend, replyTo]
    )

    const handleInputChange = useCallback(
        (e: React.ChangeEvent<HTMLTextAreaElement>) => {
            setInput(e.target.value.slice(0, 2000))
            const now = Date.now()
            if (now - lastTypingSentRef.current > 2000) {
                lastTypingSentRef.current = now
                ws.sendTyping()
            }
        },
        [ws]
    )

    const jumpToMessage = useCallback((messageId: number) => {
        const node = document.getElementById(`msg-${messageId}`)
        if (node) {
            node.scrollIntoView({ behavior: "smooth", block: "center" })
            node.classList.add("ring-2", "ring-primary")
            setTimeout(() => node.classList.remove("ring-2", "ring-primary"), 1500)
        }
    }, [])

    const displayName = conv
        ? conv.subscription_label || conv.username || `#${subscriptionId}`
        : `Subscription #${subscriptionId}`

    const statusVariant =
        conv?.subscription_status === "active"
            ? "default"
            : conv?.subscription_status === "expired"
            ? "danger"
            : "secondary"

    const trimmedLength = input.trim().length

    return (
        <div className="flex flex-col flex-1 overflow-hidden min-w-0">
            {/* Header */}
            <div className="flex items-center gap-2 px-3 py-2.5 border-b border-border shrink-0 sm:px-4 sm:py-3 sm:gap-3">
                {isMobile && (
                    <Button variant="ghost" size="icon" className="shrink-0 h-8 w-8" onClick={onBack} aria-label="Back to conversations">
                        <ArrowLeft className="h-4 w-4" />
                    </Button>
                )}
                <div className="flex flex-col min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 sm:gap-2">
                        <span className="font-semibold text-sm truncate">{displayName}</span>
                        {isUserOnline && (
                            <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse shrink-0" title="User is online" />
                        )}
                        {conv?.subscription_status && (
                            <Badge variant={statusVariant} className="text-[10px] px-1.5 py-0 shrink-0">
                                {conv.subscription_status}
                            </Badge>
                        )}
                        {ws.status !== "connected" && (
                            <span className="text-[10px] px-1.5 py-0.5 rounded-full border border-border text-muted-foreground">
                                {ws.status === "connecting" ? "Connecting..." : "Reconnecting..."}
                            </span>
                        )}
                    </div>
                    {conv?.username && conv.subscription_label && (
                        conv.user_id ? (
                            <Link
                                to={`/users/${conv.user_id}`}
                                className="text-xs text-muted-foreground truncate hover:text-primary transition-colors w-fit"
                            >
                                @{conv.username}
                            </Link>
                        ) : (
                            <span className="text-xs text-muted-foreground truncate">
                                @{conv.username}
                            </span>
                        )
                    )}
                </div>
                <ConversationSearch subscriptionId={subscriptionId} onJump={jumpToMessage} />
                <button
                    type="button"
                    onClick={handleViewSubscription}
                    className="shrink-0 text-muted-foreground hover:text-foreground transition-colors p-2 rounded-md hover:bg-muted h-9 w-9 flex items-center justify-center"
                    title="View subscription"
                    aria-label="View subscription"
                >
                    <ExternalLink className="h-4 w-4" />
                </button>
            </div>

            {/* Pinned messages banner */}
            {pinnedMessages && pinnedMessages.length > 0 && (
                <div className="px-3 py-1.5 border-b border-border bg-muted/30 shrink-0 sm:px-4 sm:py-2">
                    <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Pin className="h-3 w-3 shrink-0" />
                        <span className="truncate flex-1 min-w-0">
                            <span className="font-medium">Pinned:</span>{" "}
                            {pinnedMessages[0].content}
                        </span>
                        {pinnedMessages.length > 1 && (
                            <span className="shrink-0 text-[10px] px-1.5 py-0.5 rounded-full bg-primary/15 text-foreground">
                                +{pinnedMessages.length - 1}
                            </span>
                        )}
                    </div>
                </div>
            )}

            {/* Messages */}
            {hasNextPage && !messagesLoading && (
                <div className="flex justify-center py-1 border-b border-border/50 shrink-0">
                    <button
                        onClick={() => fetchNextPage()}
                        disabled={isFetchingNextPage}
                        className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                    >
                        {isFetchingNextPage ? "Loading..." : "Load older messages"}
                    </button>
                </div>
            )}
            {messagesLoading ? (
                <div className="flex-1 overflow-y-auto p-3 sm:p-4 space-y-2 sm:space-y-3">
                    {Array.from({ length: 5 }).map((_, i) => (
                        <div
                            key={i}
                            className={cn(
                                "flex",
                                i % 2 === 0 ? "justify-end" : "justify-start"
                            )}
                        >
                            <div
                                className={cn(
                                    "h-9 rounded-2xl animate-pulse bg-muted",
                                    i % 2 === 0 ? "w-48" : "w-56"
                                )}
                            />
                        </div>
                    ))}
                </div>
            ) : messages.length === 0 ? (
                <div className="flex-1 flex flex-col items-center justify-center gap-2 text-muted-foreground p-3 sm:p-4">
                    <MessageSquare className="h-8 w-8 opacity-40" />
                    <p className="text-sm">No messages yet. Start the conversation!</p>
                </div>
            ) : (
                <VirtualizedThread
                    messages={messages}
                    subscriptionId={subscriptionId}
                    selfAdminId={me?.id}
                    onQuote={(m) => setReplyTo(m)}
                    onJump={jumpToMessage}
                />
            )}
            {userTyping && (
                <div className="px-4 py-1 text-xs text-muted-foreground animate-pulse shrink-0">
                    User is typing...
                </div>
            )}
            {replyTo && (
                <ReplyPreview target={replyTo} onClear={() => setReplyTo(null)} selfReactor="admin" />
            )}

            {/* Input */}
            <div className="px-3 pt-2 pb-[max(env(safe-area-inset-bottom),0.75rem)] border-t border-border shrink-0 sm:px-4 sm:pt-3 sm:pb-[max(env(safe-area-inset-bottom),1rem)]">
                <div className="flex items-end gap-2">
                    <textarea
                        ref={inputRef}
                        className={cn(
                            "flex-1 resize-none rounded-md border border-input bg-background px-3 py-2 text-sm",
                            "placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2",
                            "focus-visible:ring-ring min-h-[40px] max-h-32 leading-5"
                        )}
                        placeholder={isMobile ? "Type a message..." : "Type a message... (Enter to send, Shift+Enter for newline)"}
                        rows={1}
                        value={input}
                        onChange={handleInputChange}
                        onKeyDown={handleKeyDown}
                    />
                    <Button
                        size="icon"
                        onClick={handleSend}
                        disabled={trimmedLength === 0 || sendMessage.isPending}
                        className="shrink-0 h-10 w-10 sm:h-9 sm:w-9"
                        aria-label="Send message"
                    >
                        <Send className="h-4 w-4" />
                    </Button>
                </div>
                {input.length > 1800 && (
                    <div className="text-[10px] text-muted-foreground text-right mt-1">
                        {input.length}/2000
                    </div>
                )}
            </div>
        </div>
    )
}

// ─── Empty chat placeholder ───────────────────────────────────────────────────

function EmptyChatPlaceholder() {
    return (
        <div className="flex flex-col flex-1 items-center justify-center gap-3 text-muted-foreground">
            <MessageSquare className="h-12 w-12 opacity-30" />
            <p className="text-sm font-medium">Select a conversation</p>
            <p className="text-xs opacity-70">Choose a conversation from the list to start chatting</p>
        </div>
    )
}

// ─── Main page ────────────────────────────────────────────────────────────────

export default function ChatsPage() {
    usePageTitle("Chats")
    const isMobile = useIsMobile()

    const [selectedSubId, setSelectedSubId] = useState<number | null>(null)
    const [search, setSearch] = useState("")
    const [debouncedSearch, setDebouncedSearch] = useState("")

    // Filter / sort state
    const [sortBy, setSortBy] = useState<"recent" | "unread" | "oldest">("recent")
    const [statusFilter, setStatusFilter] = useState("")
    const [tab, setTab] = useState<InboxTab>("all")
    const [chatPage, setChatPage] = useState(1)

    // Online subscription IDs reported by per-conversation WebSocket connections
    const [onlineSubIds, setOnlineSubIds] = useState<Set<number>>(new Set())

    // Callback for ChatViewPanel to report user online status changes
    const handleUserOnlineChange = useCallback((subscriptionId: number, isOnline: boolean) => {
        setOnlineSubIds((prev) => {
            const next = new Set(prev)
            if (isOnline) {
                next.add(subscriptionId)
            } else {
                next.delete(subscriptionId)
            }
            return next
        })
    }, [])

    // Debounce search
    useEffect(() => {
        const timer = setTimeout(() => setDebouncedSearch(search), 300)
        return () => clearTimeout(timer)
    }, [search])

    // Reset page when filters change
    useEffect(() => {
        setChatPage(1)
    }, [debouncedSearch, sortBy, statusFilter, tab])

    const { data: chatsData, isLoading: chatsLoading } = useAdminChats({
        page: chatPage,
        search: debouncedSearch,
        sort: sortBy,
        status: statusFilter || undefined,
        unread: tab === "unread" || undefined,
        mine: tab === "mine" || undefined,
        pinned: tab === "pinned" || undefined,
    })
    const conversations = chatsData?.conversations ?? []
    const totalPages = chatsData?.meta?.total_pages ?? 1

    const selectedConv = conversations.find((c) => c.subscription_id === selectedSubId)

    const handleSelect = useCallback((id: number) => {
        setSelectedSubId(id)
    }, [])

    const handleBack = useCallback(() => {
        setSelectedSubId(null)
    }, [])

    const handleSortChange = useCallback((v: "recent" | "unread" | "oldest") => {
        setSortBy(v)
    }, [])

    const handleStatusChange = useCallback((v: string) => {
        setStatusFilter(v)
    }, [])

    const handlePageChange = useCallback((page: number) => {
        setChatPage(page)
    }, [])

    // Mobile: show either list or chat
    const showList = isMobile ? !selectedSubId : true
    const showChat = isMobile ? !!selectedSubId : true

    return (
        <div className="flex flex-col h-[calc(100dvh-4rem)] overflow-hidden overscroll-contain">
            {/* Page title bar (desktop only -- mobile header is inside panels) */}
            {!isMobile && (
                <div className="flex items-center px-6 py-4 border-b border-border shrink-0">
                    <h1 className="text-xl font-semibold">Chats</h1>
                </div>
            )}

            {isMobile && showList && (
                <div className="flex items-center px-4 py-3 border-b border-border shrink-0">
                    <h1 className="text-lg font-semibold">Chats</h1>
                </div>
            )}

            <div className="flex flex-1 overflow-hidden">
                {showList && (
                    <div className={cn(
                        "flex flex-col overflow-hidden border-r border-border",
                        isMobile ? "w-full" : "w-80 shrink-0",
                    )}>
                        <InboxTabs value={tab} onChange={setTab} />
                        <ConversationListPanel
                            conversations={conversations}
                            isLoading={chatsLoading}
                            selectedSubId={selectedSubId}
                            search={search}
                            onSearchChange={setSearch}
                            onSelect={handleSelect}
                            onlineSubIds={onlineSubIds}
                            sortBy={sortBy}
                            onSortChange={handleSortChange}
                            statusFilter={statusFilter}
                            onStatusChange={handleStatusChange}
                            chatPage={chatPage}
                            totalPages={totalPages}
                            onPageChange={handlePageChange}
                        />
                    </div>
                )}

                {showChat && (
                    selectedSubId ? (
                        <ChatViewPanel
                            key={selectedSubId}
                            conv={selectedConv}
                            subscriptionId={selectedSubId}
                            isMobile={isMobile}
                            onBack={handleBack}
                            onUserOnlineChange={handleUserOnlineChange}
                        />
                    ) : (
                        <EmptyChatPlaceholder />
                    )
                )}
            </div>
        </div>
    )
}
