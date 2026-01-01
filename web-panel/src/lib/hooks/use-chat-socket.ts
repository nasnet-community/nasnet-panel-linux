import { useEffect, useRef, useCallback, useState } from "react"
import { getApiBaseUrl } from "@/lib/config"

export type ChatSocketStatus = "connecting" | "connected" | "disconnected"

interface UseChatSocketOptions {
    onNewMessage?: (msg: any) => void
    onMessageAck?: (nonce: string, msg: any) => void
    onMessageEdited?: (messageId: number, content: string, editedAt: string) => void
    onMessageDeleted?: (messageId: number) => void
    onReactionAdded?: (messageId: number, reactor: "user" | "admin", emoji: string) => void
    onReactionRemoved?: (messageId: number, reactor: "user" | "admin", emoji: string) => void
    onAdminMessagesRead?: () => void
    onTyping?: (senderType: "user" | "admin") => void
    onOnlineStatus?: (isOnline: boolean, senderType: "user" | "admin") => void
    onMessagesRead?: () => void
    onError?: (message: string) => void
}

export interface SendMessageOpts {
    nonce?: string
    replyToMessageId?: number
}

interface UseChatSocketReturn {
    status: ChatSocketStatus
    sendMessage: (content: string, opts?: SendMessageOpts) => void
    sendTyping: () => void
    sendMarkRead: () => void
}

export function useChatSocket(
    url: string | null,
    options: UseChatSocketOptions = {},
): UseChatSocketReturn {
    const [status, setStatus] = useState<ChatSocketStatus>("disconnected")
    const wsRef = useRef<WebSocket | null>(null)
    const attemptRef = useRef(0)
    const reconnectTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined)
    const optionsRef = useRef(options)

    useEffect(() => { optionsRef.current = options })

    const connect = useCallback(() => {
        if (!url) return
        if (reconnectTimerRef.current) {
            clearTimeout(reconnectTimerRef.current)
            reconnectTimerRef.current = undefined
        }
        const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
        const apiUrl = getApiBaseUrl()
        let host: string
        try {
            host = apiUrl && apiUrl.startsWith("http") ? new URL(apiUrl).host : window.location.host
        } catch {
            host = window.location.host
        }
        const basePath = getApiBaseUrl()
        const wsUrl = `${protocol}//${host}${basePath}${url}`

        setStatus("connecting")
        const ws = new WebSocket(wsUrl)
        wsRef.current = ws

        ws.onopen = () => {
            attemptRef.current = 0
            setStatus("connected")
        }

        ws.onmessage = (ev) => {
            try {
                const msg = JSON.parse(ev.data)
                const opts = optionsRef.current
                switch (msg.type) {
                    case "new_message": opts.onNewMessage?.(msg.message); break
                    case "message_ack": opts.onMessageAck?.(msg.nonce ?? "", msg.message); break
                    case "message_edited": opts.onMessageEdited?.(msg.message_id, msg.content ?? "", msg.edited_at ?? ""); break
                    case "message_deleted": opts.onMessageDeleted?.(msg.message_id); break
                    case "reaction_added": opts.onReactionAdded?.(msg.message_id, msg.reactor, msg.emoji); break
                    case "reaction_removed": opts.onReactionRemoved?.(msg.message_id, msg.reactor, msg.emoji); break
                    case "admin_messages_read": opts.onAdminMessagesRead?.(); break
                    case "typing": opts.onTyping?.(msg.sender_type); break
                    case "online_status": opts.onOnlineStatus?.(msg.is_online, msg.sender_type); break
                    case "messages_read": opts.onMessagesRead?.(); break
                    case "error": opts.onError?.(msg.message ?? msg.error); break
                }
            } catch { /* malformed frame */ }
        }

        ws.onclose = (ev) => {
            wsRef.current = null
            setStatus("disconnected")
            // Reset back-off on a clean close so the next reconnect starts fast.
            // Without this a transient upgrade rejection can ratchet attempts to
            // the 30 s cap across remounts of the same hook instance.
            if (ev.wasClean) attemptRef.current = 0
            const base = Math.min(1000 * 2 ** attemptRef.current, 30000)
            const jitter = Math.floor(Math.random() * 1000)
            attemptRef.current++
            reconnectTimerRef.current = setTimeout(connect, base + jitter)
        }

        ws.onerror = () => { /* onclose follows */ }
    }, [url])

    useEffect(() => {
        connect()
        return () => {
            if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
            if (wsRef.current) {
                wsRef.current.close()
                wsRef.current = null
            }
        }
    }, [connect])

    const sendMessage = useCallback((content: string, opts?: SendMessageOpts) => {
        if (wsRef.current?.readyState === WebSocket.OPEN) {
            wsRef.current.send(JSON.stringify({
                type: "send_message",
                content,
                nonce: opts?.nonce,
                reply_to_message_id: opts?.replyToMessageId,
            }))
        }
    }, [])

    const sendTyping = useCallback(() => {
        if (wsRef.current?.readyState === WebSocket.OPEN) {
            wsRef.current.send(JSON.stringify({ type: "typing" }))
        }
    }, [])

    const sendMarkRead = useCallback(() => {
        if (wsRef.current?.readyState === WebSocket.OPEN) {
            wsRef.current.send(JSON.stringify({ type: "mark_read" }))
        }
    }, [])

    return { status, sendMessage, sendTyping, sendMarkRead }
}
