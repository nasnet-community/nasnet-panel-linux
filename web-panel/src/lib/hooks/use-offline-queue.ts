import { useCallback, useEffect, useRef, useState } from "react"

export interface QueuedMessage {
    nonce: string
    content: string
    replyToMessageId?: number
}

/** Offline message queue at chat:queue:<key>. Replays with original nonce
 * on reconnect; server dedup makes replay safe. */
export function useOfflineQueue(key: string) {
    const storageKey = `chat:queue:${key}`
    const [queue, setQueue] = useState<QueuedMessage[]>(() => {
        if (typeof window === "undefined") return []
        try {
            const raw = window.localStorage.getItem(storageKey)
            return raw ? (JSON.parse(raw) as QueuedMessage[]) : []
        } catch {
            return []
        }
    })
    const queueRef = useRef(queue)
    queueRef.current = queue

    useEffect(() => {
        if (typeof window === "undefined") return
        try {
            window.localStorage.setItem(storageKey, JSON.stringify(queue))
        } catch {
            /* */
        }
    }, [queue, storageKey])

    const enqueue = useCallback((m: QueuedMessage) => {
        setQueue((q) => [...q, m])
    }, [])

    const drop = useCallback((nonce: string) => {
        setQueue((q) => q.filter((m) => m.nonce !== nonce))
    }, [])

    const peek = useCallback(() => queueRef.current, [])

    return { queue, enqueue, drop, peek }
}
