import { useRef, useMemo, useEffect } from "react"
import { useVirtualizer } from "@tanstack/react-virtual"
import { ChatMessageBubble, type ReplyTo } from "@/components/sub-panel/chat-message"
import { MessageToolbar } from "./message-toolbar"
import { DateSeparator } from "./date-separator"
import { ReactionsBar } from "./reactions-bar"
import type { ChatMessage } from "@/lib/api/chat"
import { motion } from "framer-motion"
import { cn } from "@/lib/utils"

type Row =
    | { kind: "sep"; iso: string; key: string }
    | { kind: "msg"; m: ChatMessage; key: string }

interface Props {
    messages: ChatMessage[]
    subscriptionId: number
    selfAdminId?: number
    onQuote?: (m: ChatMessage) => void
    onJump?: (messageId: number) => void
}

export function VirtualizedThread({ messages, subscriptionId, selfAdminId, onQuote, onJump }: Props) {
    const parentRef = useRef<HTMLDivElement>(null)

    const messageById = useMemo(() => {
        const m = new Map<number, ChatMessage>()
        for (const x of messages) m.set(x.id, x)
        return m
    }, [messages])

    const rows = useMemo<Row[]>(() => {
        const out: Row[] = []
        let lastDay = ""
        for (const m of messages) {
            const day = m.created_at.slice(0, 10)
            if (day !== lastDay) {
                out.push({ kind: "sep", iso: m.created_at, key: `sep-${day}` })
                lastDay = day
            }
            out.push({ kind: "msg", m, key: `m-${m.id}` })
        }
        return out
    }, [messages])

    const v = useVirtualizer({
        count: rows.length,
        getScrollElement: () => parentRef.current,
        estimateSize: (i) => (rows[i].kind === "sep" ? 24 : 52),
        overscan: 8,
    })

    const userScrolledUpRef = useRef(false)
    useEffect(() => {
        if (!parentRef.current) return
        if (userScrolledUpRef.current) return
        parentRef.current.scrollTop = parentRef.current.scrollHeight
    }, [messages.length])

    const onScroll = () => {
        const el = parentRef.current
        if (!el) return
        userScrolledUpRef.current = el.scrollHeight - el.scrollTop - el.clientHeight > 200
    }

    return (
        <div
            ref={parentRef}
            onScroll={onScroll}
            className="flex-1 overflow-y-auto overscroll-contain p-3 sm:p-4"
            role="log"
            aria-live="polite"
            aria-relevant="additions"
            aria-atomic="false"
        >
            <div style={{ height: v.getTotalSize(), position: "relative" }}>
                {v.getVirtualItems().map((vi) => {
                    const row = rows[vi.index]
                    return (
                        <div
                            key={row.key}
                            data-index={vi.index}
                            ref={v.measureElement}
                            style={{
                                position: "absolute",
                                top: 0,
                                left: 0,
                                width: "100%",
                                transform: `translateY(${vi.start}px)`,
                            }}
                        >
                            {row.kind === "sep" ? (
                                <DateSeparator iso={row.iso} />
                            ) : (
                                (() => {
                                    const m = row.m
                                    const mine = m.sender_type === "admin"
                                    let replyToBlock: ReplyTo | null = null
                                    if (m.reply_to_message_id) {
                                        const orig = messageById.get(m.reply_to_message_id)
                                        if (orig) {
                                            replyToBlock = {
                                                senderLabel: orig.sender_type === "admin" ? "Admin" : "User",
                                                content: orig.content,
                                                onJump: () => onJump?.(orig.id),
                                            }
                                        }
                                    }
                                    return (
                                        <motion.div
                                            initial={{ opacity: 0, y: 4 }}
                                            animate={{ opacity: 1, y: 0 }}
                                            transition={{ duration: 0.15 }}
                                            className="my-0.5"
                                            id={`msg-${m.id}`}
                                        >
                                            <MessageToolbar
                                                message={m}
                                                subscriptionId={subscriptionId}
                                                isMine={mine}
                                                onQuote={onQuote}
                                            >
                                                <ChatMessageBubble
                                                    content={m.content}
                                                    senderType={m.sender_type}
                                                    createdAt={m.created_at}
                                                    isPinned={m.is_pinned}
                                                    editedAt={m.edited_at ?? null}
                                                    isMine={mine}
                                                    isRead={mine ? m.is_read : undefined}
                                                    replyTo={replyToBlock}
                                                />
                                            </MessageToolbar>
                                            <div className={cn("flex w-full", mine ? "justify-end pr-2" : "justify-start pl-2")}>
                                                <ReactionsBar
                                                    messageId={m.id}
                                                    scope={{ subscriptionId }}
                                                    selfReactor="admin"
                                                    selfAdminId={selfAdminId}
                                                />
                                            </div>
                                        </motion.div>
                                    )
                                })()
                            )}
                        </div>
                    )
                })}
            </div>
        </div>
    )
}
