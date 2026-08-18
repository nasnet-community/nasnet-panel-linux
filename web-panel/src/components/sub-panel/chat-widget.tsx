import { useState, useRef, useEffect, useMemo, useCallback } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { motion, AnimatePresence } from "framer-motion"
import { MessageCircle, X, Send, Maximize2, Minimize2, Bell, BellOff } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
    useSubChatInfinite,
    useMarkSubChatRead,
} from "@/lib/queries/use-chat"
import { useChatSocket } from "@/lib/hooks/use-chat-socket"
import { useDraft } from "@/lib/hooks/use-draft"
import { useOfflineQueue } from "@/lib/hooks/use-offline-queue"
import { useChatNotifications } from "@/lib/hooks/use-notification"
import { queryKeys } from "@/lib/queries/keys"
import { ChatMessageBubble, type ReplyTo } from "./chat-message"
import { WidgetMessageToolbar } from "./widget-message-toolbar"
import { ReactionsBar } from "@/components/chats/reactions-bar"
import { ReplyPreview } from "@/components/chats/reply-preview"
import { cn } from "@/lib/utils"
import { toast } from "sonner"
import type { ChatMessage } from "@/lib/api/chat"

let audioCtx: AudioContext | null = null
function playNotificationSound() {
    try {
        if (!audioCtx) audioCtx = new AudioContext()
        const osc = audioCtx.createOscillator()
        const gain = audioCtx.createGain()
        osc.connect(gain)
        gain.connect(audioCtx.destination)
        osc.frequency.setValueAtTime(587.33, audioCtx.currentTime)
        osc.frequency.setValueAtTime(783.99, audioCtx.currentTime + 0.1)
        gain.gain.setValueAtTime(0.3, audioCtx.currentTime)
        gain.gain.exponentialRampToValueAtTime(0.001, audioCtx.currentTime + 0.3)
        osc.start(audioCtx.currentTime)
        osc.stop(audioCtx.currentTime + 0.3)
    } catch {
        /* */
    }
}

interface ChatWidgetProps {
    uuid: string
}

const EXPAND_KEY = "chat:widget:expanded"

export function ChatWidget({ uuid }: ChatWidgetProps) {
    const [isOpen, setIsOpen] = useState(false)
    const [expanded, setExpanded] = useState<boolean>(() => {
        try {
            return localStorage.getItem(EXPAND_KEY) === "1"
        } catch {
            return false
        }
    })
    useEffect(() => {
        try {
            localStorage.setItem(EXPAND_KEY, expanded ? "1" : "0")
        } catch {
            /* */
        }
    }, [expanded])

    const [input, setInput, clearDraft] = useDraft(uuid)
    const [adminTyping, setAdminTyping] = useState(false)
    const [adminOnline, setAdminOnline] = useState(false)
    const [unreadCount, setUnreadCount] = useState(0)
    const [replyTo, setReplyTo] = useState<ChatMessage | null>(null)
    const messagesEndRef = useRef<HTMLDivElement>(null)
    const inputRef = useRef<HTMLTextAreaElement>(null)
    const typingTimeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined)
    const lastTypingSentRef = useRef(0)
    const isOpenRef = useRef(false)
    const queryClient = useQueryClient()

    const offline = useOfflineQueue(uuid)
    const notif = useChatNotifications(uuid)
    const markRead = useMarkSubChatRead(uuid)

    const {
        data,
        isLoading,
        fetchNextPage,
        hasNextPage,
        isFetchingNextPage,
    } = useSubChatInfinite(uuid)

    const rawMessages = useMemo(() => data?.pages.flatMap((p) => p.messages) ?? [], [data])
    const messages = useMemo(() => [...rawMessages].reverse(), [rawMessages])
    const messageById = useMemo(() => {
        const m = new Map<number, ChatMessage>()
        for (const x of messages) m.set(x.id, x)
        return m
    }, [messages])

    useEffect(() => {
        isOpenRef.current = isOpen
    }, [isOpen])

    const ws = useChatSocket(`/api/v1/public/sub/${uuid}/chat/ws`, {
        onNewMessage: useCallback(() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.subChat(uuid) })
            if (!isOpenRef.current) {
                if (!notif.muted) playNotificationSound()
                notif.notify("New message", "Support replied — open chat to read.")
                setUnreadCount((c) => c + 1)
            }
        }, [queryClient, uuid, notif]),
        onMessageAck: useCallback(
            (nonce: string) => {
                if (nonce) offline.drop(nonce)
                queryClient.invalidateQueries({ queryKey: queryKeys.subChat(uuid) })
            },
            [offline, queryClient, uuid],
        ),
        onMessageEdited: useCallback(() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.subChat(uuid) })
        }, [queryClient, uuid]),
        onMessageDeleted: useCallback(() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.subChat(uuid) })
        }, [queryClient, uuid]),
        onReactionAdded: useCallback(
            (id: number) => {
                queryClient.invalidateQueries({ queryKey: queryKeys.chatReactions(id) })
            },
            [queryClient],
        ),
        onReactionRemoved: useCallback(
            (id: number) => {
                queryClient.invalidateQueries({ queryKey: queryKeys.chatReactions(id) })
            },
            [queryClient],
        ),
        onMessagesRead: useCallback(() => {
            // Admin marked the user's messages as read — refresh so ✓ flips to ✓✓.
            queryClient.invalidateQueries({ queryKey: queryKeys.subChat(uuid) })
        }, [queryClient, uuid]),
        onTyping: useCallback(() => {
            setAdminTyping(true)
            if (typingTimeoutRef.current) clearTimeout(typingTimeoutRef.current)
            typingTimeoutRef.current = setTimeout(() => setAdminTyping(false), 3000)
        }, []),
        onOnlineStatus: useCallback((isOnline: boolean) => {
            setAdminOnline(isOnline)
        }, []),
        onError: useCallback((message: string) => {
            toast.error(message)
        }, []),
    })

    // Replay queued messages once reconnected.
    useEffect(() => {
        if (ws.status !== "connected") return
        const q = offline.peek()
        if (q.length === 0) return
        for (const m of q) {
            ws.sendMessage(m.content, { nonce: m.nonce, replyToMessageId: m.replyToMessageId })
        }
    }, [ws.status, ws, offline])

    useEffect(() => {
        return () => {
            if (typingTimeoutRef.current) clearTimeout(typingTimeoutRef.current)
        }
    }, [])

    useEffect(() => {
        if (isOpen) {
            messagesEndRef.current?.scrollIntoView({ behavior: "smooth" })
        }
    }, [rawMessages, isOpen])

    useEffect(() => {
        if (isOpen) inputRef.current?.focus()
    }, [isOpen])

    // Mark admin messages as read on open / after new admin message arrives while open.
    useEffect(() => {
        if (!isOpen) return
        const t = setTimeout(() => {
            markRead.mutate()
        }, 500)
        return () => clearTimeout(t)
    }, [isOpen, rawMessages.length, markRead])

    const handleSend = useCallback(() => {
        const trimmed = input.trim()
        if (!trimmed) return
        const nonce = (typeof crypto !== "undefined" && "randomUUID" in crypto
            ? (crypto as Crypto).randomUUID()
            : `${Date.now()}-${Math.random().toString(36).slice(2)}`)
        const replyToMessageId = replyTo?.id
        if (ws.status !== "connected") {
            offline.enqueue({ nonce, content: trimmed, replyToMessageId })
            setInput("")
            clearDraft()
            setReplyTo(null)
            toast.message("Queued — will send when reconnected")
            return
        }
        ws.sendMessage(trimmed, { nonce, replyToMessageId })
        offline.enqueue({ nonce, content: trimmed, replyToMessageId })
        setInput("")
        clearDraft()
        setReplyTo(null)
    }, [input, ws, offline, clearDraft, setInput, replyTo])

    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent) => {
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
        [handleSend, replyTo],
    )

    const handleOpen = useCallback(() => {
        setIsOpen(true)
        setUnreadCount(0)
        // Browsers require Notification.requestPermission() to be invoked from
        // a real user-gesture handler. Calling it here (synchronously inside
        // the click handler) keeps Firefox happy. We only ask once.
        if (notif.permission === "default") {
            notif.requestPermission()
        }
    }, [notif])

    const handleInputChange = useCallback(
        (e: React.ChangeEvent<HTMLTextAreaElement>) => {
            setInput(e.target.value.slice(0, 2000))
            const now = Date.now()
            if (now - lastTypingSentRef.current > 2000) {
                lastTypingSentRef.current = now
                ws.sendTyping()
            }
        },
        [ws, setInput],
    )

    const jumpToMessage = useCallback((messageId: number) => {
        const node = document.getElementById(`widget-msg-${messageId}`)
        if (node) {
            node.scrollIntoView({ behavior: "smooth", block: "center" })
            node.classList.add("ring-2", "ring-primary")
            setTimeout(() => node.classList.remove("ring-2", "ring-primary"), 1500)
        }
    }, [])

    const trimmedLength = input.trim().length

    return (
        <>
            <AnimatePresence>
                {!isOpen && (
                    <motion.div
                        initial={{ scale: 0, opacity: 0 }}
                        animate={{ scale: 1, opacity: 1 }}
                        exit={{ scale: 0, opacity: 0 }}
                        className="fixed bottom-6 right-6 z-50"
                    >
                        <Button
                            size="lg"
                            className="h-14 w-14 rounded-full shadow-lg relative"
                            onClick={handleOpen}
                            aria-label={`Open support chat${unreadCount > 0 ? `, ${unreadCount} unread messages` : ""}`}
                        >
                            <MessageCircle className="h-6 w-6" />
                            {unreadCount > 0 && (
                                <span className="absolute -top-1 -right-1 h-5 min-w-5 px-1 rounded-full bg-destructive text-destructive-foreground text-xs font-medium flex items-center justify-center">
                                    {unreadCount > 99 ? "99+" : unreadCount}
                                </span>
                            )}
                        </Button>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {isOpen && (
                    <motion.div
                        initial={{ opacity: 0, y: 20, scale: 0.95 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 20, scale: 0.95 }}
                        transition={{ duration: 0.2 }}
                        className={cn(
                            "fixed z-[60] border bg-background shadow-2xl flex flex-col overflow-hidden overscroll-contain",
                            expanded
                                // Phone: edge-to-edge so the sub-panel sticky header
                                // doesn't peek through above the widget.
                                ? "inset-0 rounded-none sm:inset-auto sm:right-6 sm:bottom-6 sm:w-[min(900px,calc(100vw-3rem))] sm:h-[min(80dvh,calc(100dvh-3rem))] sm:rounded-2xl"
                                : "right-6 bottom-6 w-[360px] max-w-[calc(100vw-3rem)] h-[500px] max-h-[calc(100dvh-6rem)] rounded-2xl",
                        )}
                        role="dialog"
                        aria-modal="true"
                        aria-label="Support chat"
                    >
                        <div className="flex items-center justify-between gap-2 px-4 py-3 border-b bg-muted/30 shrink-0 pt-[max(env(safe-area-inset-top),0.75rem)]">
                            <div className="flex items-center gap-2 min-w-0 flex-1">
                                <h3 className="font-semibold text-sm truncate">Support Chat</h3>
                                {adminOnline && (
                                    <span className="hidden sm:flex items-center gap-1 text-xs text-green-600 dark:text-green-400 shrink-0">
                                        <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse" />
                                        online
                                    </span>
                                )}
                                {ws.status !== "connected" && (
                                    <span className="hidden sm:inline text-xs px-1.5 py-0.5 rounded-full border border-border text-muted-foreground shrink-0">
                                        {ws.status === "connecting" ? "Connecting..." : "Reconnecting..."}
                                    </span>
                                )}
                            </div>
                            <div className="flex items-center gap-0.5 shrink-0">
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-8 w-8"
                                    onClick={() => notif.setMuted((m) => !m)}
                                    aria-label={notif.muted ? "Unmute notifications" : "Mute notifications"}
                                    title={notif.muted ? "Unmute" : "Mute"}
                                >
                                    {notif.muted ? <BellOff className="h-4 w-4" /> : <Bell className="h-4 w-4" />}
                                </Button>
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-8 w-8"
                                    onClick={() => setExpanded((v) => !v)}
                                    aria-label={expanded ? "Shrink chat" : "Expand chat"}
                                    title={expanded ? "Shrink" : "Expand"}
                                >
                                    {expanded ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
                                </Button>
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-8 w-8"
                                    onClick={() => setIsOpen(false)}
                                    aria-label="Close chat"
                                >
                                    <X className="h-4 w-4" />
                                </Button>
                            </div>
                        </div>

                        <div
                            className="flex-1 overflow-y-auto overscroll-contain px-4 py-3 space-y-1"
                            role="log"
                            aria-live="polite"
                            aria-relevant="additions"
                            aria-label="Chat messages"
                        >
                            {hasNextPage && !isLoading && (
                                <div className="flex justify-center pb-1">
                                    <button
                                        onClick={() => fetchNextPage()}
                                        disabled={isFetchingNextPage}
                                        className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                                    >
                                        {isFetchingNextPage ? "Loading..." : "Load older messages"}
                                    </button>
                                </div>
                            )}
                            {isLoading ? (
                                <div className="space-y-3">
                                    {[1, 2, 3].map((i) => (
                                        <div
                                            key={i}
                                            className={`flex ${i % 2 ? "justify-end" : "justify-start"}`}
                                        >
                                            <div className="h-10 w-40 bg-muted animate-pulse rounded-2xl" />
                                        </div>
                                    ))}
                                </div>
                            ) : messages.length === 0 ? (
                                <div className="flex items-center justify-center h-full text-muted-foreground text-sm text-center px-4">
                                    Send a message and we'll get back to you.
                                </div>
                            ) : (
                                messages.map((msg) => {
                                    const mine = msg.sender_type === "user"
                                    let replyToBlock: ReplyTo | null = null
                                    if (msg.reply_to_message_id) {
                                        const orig = messageById.get(msg.reply_to_message_id)
                                        if (orig) {
                                            replyToBlock = {
                                                senderLabel:
                                                    orig.sender_type === "user" ? "you" : "Admin",
                                                content: orig.content,
                                                onJump: () => jumpToMessage(orig.id),
                                            }
                                        }
                                    }
                                    return (
                                        <div key={msg.id} id={`widget-msg-${msg.id}`}>
                                            <WidgetMessageToolbar
                                                uuid={uuid}
                                                message={msg}
                                                isMine={mine}
                                                onQuote={(m) => setReplyTo(m)}
                                            >
                                                <ChatMessageBubble
                                                    content={msg.content}
                                                    senderType={msg.sender_type}
                                                    createdAt={msg.created_at}
                                                    isOptimistic={!msg.subscription_id}
                                                    isPinned={msg.is_pinned}
                                                    editedAt={msg.edited_at ?? null}
                                                    isMine={mine}
                                                    isRead={mine ? msg.is_read : undefined}
                                                    replyTo={replyToBlock}
                                                />
                                            </WidgetMessageToolbar>
                                            {msg.subscription_id ? (
                                                <div className={cn("flex w-full", mine ? "justify-end pr-2" : "justify-start pl-2")}>
                                                    <ReactionsBar
                                                        messageId={msg.id}
                                                        scope={{ uuid }}
                                                        selfReactor="user"
                                                    />
                                                </div>
                                            ) : null}
                                        </div>
                                    )
                                })
                            )}
                            <div ref={messagesEndRef} />
                        </div>

                        {adminTyping && (
                            <div className="px-4 py-1 text-xs text-muted-foreground animate-pulse shrink-0">
                                Admin is typing...
                            </div>
                        )}

                        {replyTo && (
                            <ReplyPreview target={replyTo} onClear={() => setReplyTo(null)} selfReactor="user" />
                        )}

                        <div className="border-t px-3 py-2 flex items-end gap-2 shrink-0">
                            <textarea
                                ref={inputRef}
                                value={input}
                                onChange={handleInputChange}
                                onKeyDown={handleKeyDown}
                                placeholder="Type a message..."
                                rows={1}
                                className="flex-1 resize-none bg-transparent text-sm outline-none placeholder:text-muted-foreground max-h-24 overflow-y-auto py-2"
                            />
                            <Button
                                size="icon"
                                className="h-10 w-10 sm:h-9 sm:w-9 shrink-0"
                                disabled={trimmedLength === 0}
                                onClick={handleSend}
                                aria-label="Send message"
                            >
                                <Send className="h-4 w-4" />
                            </Button>
                        </div>
                        {input.length > 1800 && (
                            <div className="px-3 pb-1 text-xs text-muted-foreground text-right">
                                {input.length}/2000
                            </div>
                        )}
                    </motion.div>
                )}
            </AnimatePresence>
        </>
    )
}
