import { StrictMode } from "react"
import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { useChatSocket } from "@/lib/hooks/use-chat-socket"

vi.mock("@/lib/config", () => ({ getApiBaseUrl: () => "/api" }))

class MockWebSocket {
    static readonly OPEN = 1
    static instances: MockWebSocket[] = []

    readyState = 0
    onopen: (() => void) | null = null
    onmessage: ((event: { data: string }) => void) | null = null
    onclose: ((event: { wasClean: boolean }) => void) | null = null
    onerror: (() => void) | null = null
    send = vi.fn()
    close = vi.fn(() => {
        this.readyState = 2
        // Browser close events arrive after effect cleanup has returned.
        setTimeout(() => this.serverClose(true), 100)
    })

    constructor(readonly url: string) {
        MockWebSocket.instances.push(this)
    }

    open() {
        this.readyState = MockWebSocket.OPEN
        this.onopen?.()
    }

    message(message: unknown) {
        this.onmessage?.({ data: JSON.stringify(message) })
    }

    serverClose(wasClean = false) {
        this.readyState = 3
        this.onclose?.({ wasClean })
    }
}

describe("useChatSocket lifecycle", () => {
    beforeEach(() => {
        vi.useFakeTimers()
        vi.spyOn(Math, "random").mockReturnValue(0)
        MockWebSocket.instances = []
        vi.stubGlobal("WebSocket", MockWebSocket)
    })

    afterEach(() => {
        cleanup()
        vi.clearAllTimers()
        vi.useRealTimers()
        vi.unstubAllGlobals()
        vi.restoreAllMocks()
    })

    it("does not reconnect or deliver queued messages after unmount", () => {
        const onNewMessage = vi.fn()
        const { unmount } = renderHook(() => useChatSocket("/chat/1", { onNewMessage }))
        const socket = MockWebSocket.instances[0]
        act(() => socket.open())

        unmount()
        act(() => {
            socket.message({ type: "new_message", message: { id: 1 } })
            vi.advanceTimersByTime(60000)
        })

        expect(socket.close).toHaveBeenCalledOnce()
        expect(onNewMessage).not.toHaveBeenCalled()
        expect(MockWebSocket.instances).toHaveLength(1)
        expect(vi.getTimerCount()).toBe(0)
    })

    it("cancels a pending reconnect when unmounted", () => {
        const { unmount } = renderHook(() => useChatSocket("/chat/1"))
        act(() => MockWebSocket.instances[0].serverClose())
        expect(vi.getTimerCount()).toBe(1)

        unmount()
        act(() => vi.advanceTimersByTime(60000))

        expect(MockWebSocket.instances).toHaveLength(1)
        expect(vi.getTimerCount()).toBe(0)
    })

    it("reconnects after a remote close and resets backoff after connecting", () => {
        const { result } = renderHook(() => useChatSocket("/chat/1"))
        expect(result.current.status).toBe("connecting")
        act(() => MockWebSocket.instances[0].serverClose())
        expect(result.current.status).toBe("disconnected")

        act(() => vi.advanceTimersByTime(999))
        expect(MockWebSocket.instances).toHaveLength(1)
        act(() => vi.advanceTimersByTime(1))
        expect(MockWebSocket.instances).toHaveLength(2)
        expect(result.current.status).toBe("connecting")

        act(() => MockWebSocket.instances[1].serverClose())
        act(() => vi.advanceTimersByTime(1999))
        expect(MockWebSocket.instances).toHaveLength(2)
        act(() => vi.advanceTimersByTime(1))
        expect(MockWebSocket.instances).toHaveLength(3)

        act(() => MockWebSocket.instances[2].open())
        expect(result.current.status).toBe("connected")
        act(() => MockWebSocket.instances[2].serverClose())
        act(() => vi.advanceTimersByTime(1000))
        expect(MockWebSocket.instances).toHaveLength(4)
    })

    it("ignores old callbacks after changing URLs and keeps the new socket usable", () => {
        const onNewMessage = vi.fn()
        const { result, rerender } = renderHook(({ url }) => useChatSocket(url, { onNewMessage }), {
            initialProps: { url: "/chat/1" },
        })
        const oldSocket = MockWebSocket.instances[0]
        act(() => oldSocket.open())

        rerender({ url: "/chat/2" })
        const newSocket = MockWebSocket.instances[1]
        expect(newSocket.url).toContain("/api/chat/2")
        act(() => {
            oldSocket.open()
            oldSocket.message({ type: "new_message", message: { id: 1 } })
        })
        expect(result.current.status).toBe("connecting")
        expect(onNewMessage).not.toHaveBeenCalled()

        act(() => newSocket.open())
        act(() => vi.advanceTimersByTime(60000))
        expect(result.current.status).toBe("connected")
        expect(MockWebSocket.instances).toHaveLength(2)
        act(() => result.current.sendMessage("hello", { nonce: "nonce", replyToMessageId: 3 }))
        expect(newSocket.send).toHaveBeenCalledWith(JSON.stringify({
            type: "send_message", content: "hello", nonce: "nonce", reply_to_message_id: 3,
        }))
        expect(oldSocket.send).not.toHaveBeenCalled()
    })

    it("stays disconnected after the URL is cleared", () => {
        const { result, rerender } = renderHook(({ url }: { url: string | null }) => useChatSocket(url), {
            initialProps: { url: "/chat/1" as string | null },
        })
        act(() => MockWebSocket.instances[0].open())

        rerender({ url: null })
        act(() => vi.advanceTimersByTime(60000))

        expect(result.current.status).toBe("disconnected")
        expect(MockWebSocket.instances).toHaveLength(1)
    })

    it("retires the first effect's socket during StrictMode replay", () => {
        const onNewMessage = vi.fn()
        const { result } = renderHook(() => useChatSocket("/chat/1", { onNewMessage }), {
            wrapper: StrictMode,
        })
        expect(MockWebSocket.instances).toHaveLength(2)
        const [retiredSocket, activeSocket] = MockWebSocket.instances
        expect(retiredSocket.close).toHaveBeenCalledOnce()

        act(() => {
            activeSocket.open()
            retiredSocket.message({ type: "new_message", message: { id: 1 } })
            vi.advanceTimersByTime(60000)
            result.current.sendTyping()
            result.current.sendMarkRead()
        })

        expect(result.current.status).toBe("connected")
        expect(onNewMessage).not.toHaveBeenCalled()
        expect(MockWebSocket.instances).toHaveLength(2)
        expect(activeSocket.send.mock.calls).toEqual([
            [JSON.stringify({ type: "typing" })],
            [JSON.stringify({ type: "mark_read" })],
        ])
    })

    it("uses updated message callbacks without reconnecting", () => {
        const initialCallback = vi.fn()
        const updatedCallback = vi.fn()
        const { rerender } = renderHook(({ onNewMessage }) => useChatSocket("/chat/1", { onNewMessage }), {
            initialProps: { onNewMessage: initialCallback },
        })
        rerender({ onNewMessage: updatedCallback })
        const message = { id: 1 }
        act(() => MockWebSocket.instances[0].message({ type: "new_message", message }))

        expect(initialCallback).not.toHaveBeenCalled()
        expect(updatedCallback).toHaveBeenCalledWith(message)
        expect(MockWebSocket.instances).toHaveLength(1)
    })
})
