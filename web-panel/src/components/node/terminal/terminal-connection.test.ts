import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { buildTerminalWebSocketUrl, TerminalConnection, type TerminalConnectionState } from "./terminal-connection"

class FakeWebSocket {
    static readonly CONNECTING = 0
    static readonly OPEN = 1
    static readonly CLOSING = 2
    static readonly CLOSED = 3
    static instances: FakeWebSocket[] = []

    readonly url: string
    readyState = FakeWebSocket.CONNECTING
    binaryType = "blob"
    onopen: (() => void) | null = null
    onclose: (() => void) | null = null
    onerror: (() => void) | null = null
    onmessage: ((event: { data: unknown }) => void) | null = null
    send = vi.fn((_data: string | Uint8Array) => {
        if (this.readyState !== FakeWebSocket.OPEN) throw new Error("Socket is not open")
    })
    close = vi.fn((_code?: number, _reason?: string) => {
        this.readyState = FakeWebSocket.CLOSED
        this.onclose?.()
    })

    constructor(url: string) {
        this.url = url
        FakeWebSocket.instances.push(this)
    }

    open(): void {
        this.readyState = FakeWebSocket.OPEN
        this.onopen?.()
    }

    message(data: unknown): void {
        this.onmessage?.({ data })
    }

    networkClose(): void {
        this.readyState = FakeWebSocket.CLOSED
        this.onclose?.()
    }
}

function setup() {
    const states: TerminalConnectionState[] = []
    const onData = vi.fn()
    const connection = new TerminalConnection({
        url: "wss://panel.example/prefix/api/v1/nodes/6/terminal/ws",
        onState: (state) => states.push(state),
        onData,
    })
    connection.start()
    const socket = FakeWebSocket.instances.at(-1)!
    return { connection, socket, states, onData }
}

beforeEach(() => {
    vi.useFakeTimers()
    FakeWebSocket.instances = []
    vi.stubGlobal("WebSocket", FakeWebSocket)
})

afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.unstubAllGlobals()
})

describe("terminal connection", () => {
    it("waits for shell readiness and sends typed text as binary, without queued input", () => {
        const { connection, socket, states } = setup()
        expect(socket.binaryType).toBe("arraybuffer")
        expect(connection.sendInput("before open")).toBe(false)
        socket.open()
        expect(states.map((state) => state.status)).toEqual(["connecting", "starting"])
        expect(connection.sendInput("before shell ready")).toBe(false)
        socket.message('{"pong":true}')
        expect(connection.sendInput("still not ready")).toBe(false)
        socket.message('{"ready":true}')
        expect(socket.send).not.toHaveBeenCalled()
        expect(connection.sendInput('{"close":true} café\r')).toBe(true)
        expect(socket.send).toHaveBeenCalledExactlyOnceWith(new TextEncoder().encode('{"close":true} café\r'))
        expect(states.at(-1)).toEqual({ status: "ready" })
    })

    it("supports legacy output readiness and delivers bytes unchanged", () => {
        const { connection, socket, states, onData } = setup()
        socket.open()
        socket.message(new ArrayBuffer(0))
        socket.message("")
        expect(states.at(-1)?.status).toBe("starting")
        const output = new Uint8Array([0x1b, 0x5b, 0x31, 0x6d, 0xc3, 0xa9])
        socket.message(output.buffer)
        expect(states.at(-1)?.status).toBe("ready")
        expect(onData).toHaveBeenCalledExactlyOnceWith(output)
        expect(connection.sendInput(new Uint8Array([0xff, 0x00, 0x03]))).toBe(true)
        expect(socket.send).toHaveBeenCalledExactlyOnceWith(new Uint8Array([0xff, 0x00, 0x03]))
    })

    it("accepts legacy plain text without leaking protocol messages into output", () => {
        const { socket, states, onData } = setup()
        socket.open()
        socket.message("root@kit:~# ")
        socket.message('{"ready":true}')
        socket.message('{"pong":true}')
        expect(states.filter((state) => state.status === "ready")).toHaveLength(1)
        expect(onData).toHaveBeenCalledExactlyOnceWith("root@kit:~# ")
    })

    it("never reconnects or replays dropped input after connection loss", () => {
        const { connection, socket, states } = setup()
        connection.start()
        socket.open()
        socket.message('{"ready":true}')
        connection.sendInput("accepted\r")
        socket.networkClose()
        expect(connection.sendInput("dangerous stale input\r")).toBe(false)
        connection.start()
        vi.advanceTimersByTime(600_000)
        expect(FakeWebSocket.instances).toHaveLength(1)
        expect(socket.send).toHaveBeenCalledTimes(1)
        expect(states.at(-1)).toMatchObject({ status: "disconnected", message: expect.stringContaining("Connection lost") })
        expect(vi.getTimerCount()).toBe(0)
    })

    it.each([
        ['{"exit_code":0}', { status: "exited", exitCode: 0 }],
        ['{"exit_code":137}', { status: "exited", exitCode: 137 }],
        ['{"error":"Agent unavailable"}', { status: "error", message: "Agent unavailable" }],
    ])("preserves final shell state for %s even when close arrives", (payload, expected) => {
        const { connection, socket, states, onData } = setup()
        socket.open()
        const staleClose = socket.onclose!
        const staleMessage = socket.onmessage!
        socket.message(payload)
        staleClose()
        staleMessage({ data: "late output" })
        expect(states.at(-1)).toMatchObject(expected)
        expect(socket.close).toHaveBeenCalledOnce()
        expect(connection.sendInput("\r")).toBe(false)
        expect(onData).not.toHaveBeenCalled()
        expect(vi.getTimerCount()).toBe(0)
    })

    it("keeps a quiet shell alive with matching application pongs", () => {
        const { connection, socket, states } = setup()
        socket.open()
        socket.message('{"ready":true}')
        for (let ping = 0; ping < 8; ping += 1) {
            vi.advanceTimersByTime(20_000)
            expect(socket.send).toHaveBeenLastCalledWith('{"ping":true}')
            socket.message('{"pong":true}')
        }
        expect(states.at(-1)?.status).toBe("ready")
        expect(socket.close).not.toHaveBeenCalled()
        connection.dispose()
        expect(vi.getTimerCount()).toBe(0)
    })

    it("closes a silent transport at a bounded timeout without retrying", () => {
        const { socket, states } = setup()
        socket.open()
        socket.message('{"ready":true}')
        vi.advanceTimersByTime(44_999)
        expect(states.at(-1)?.status).toBe("ready")
        vi.advanceTimersByTime(1)
        expect(states.at(-1)).toMatchObject({ status: "disconnected", message: expect.stringContaining("stopped responding") })
        expect(socket.close).toHaveBeenCalledOnce()
        expect(socket.send.mock.calls.map(([data]) => data)).toEqual(['{"ping":true}', '{"ping":true}'])
        expect(vi.getTimerCount()).toBe(0)
    })

    it("refreshes liveness on shell output as well as pongs", () => {
        const { connection, socket, states } = setup()
        socket.open()
        socket.message('{"ready":true}')
        vi.advanceTimersByTime(40_000)
        socket.message(new TextEncoder().encode("output").buffer)
        vi.advanceTimersByTime(40_000)
        expect(states.at(-1)?.status).toBe("ready")
        connection.dispose()
    })

    it.each([false, true])("bounds a stalled handshake (socket open: %s)", (open) => {
        const { socket, states } = setup()
        if (open) {
            socket.open()
            socket.message('{"pong":true}')
        }
        vi.advanceTimersByTime(30_000)
        expect(states.at(-1)).toMatchObject({ status: "error", message: expect.stringContaining(open ? "shell did not become ready" : "connection timed out") })
        expect(socket.close).toHaveBeenCalledOnce()
        expect(vi.getTimerCount()).toBe(0)
    })

    it("sends only the latest cached dimensions on open, then resizes while starting and ready", () => {
        const { connection, socket } = setup()
        connection.resize(80, 24)
        connection.resize(120, 35)
        expect(socket.send).not.toHaveBeenCalled()
        socket.open()
        connection.resize(120, 35)
        connection.resize(130, 40)
        socket.message('{"ready":true}')
        connection.resize(140, 45)
        connection.resize(0, 0)
        connection.resize(Number.NaN, 30)
        expect(socket.send.mock.calls.map(([data]) => JSON.parse(data as string))).toEqual([
            { resize: { cols: 120, rows: 35 } },
            { resize: { cols: 130, rows: 40 } },
            { resize: { cols: 140, rows: 45 } },
        ])
    })

    it("silently disposes a connecting socket and suppresses stale handlers", () => {
        const { connection, socket, states, onData } = setup()
        const callbacks = { open: socket.onopen!, message: socket.onmessage!, error: socket.onerror!, close: socket.onclose! }
        connection.dispose()
        connection.dispose()
        connection.disconnect()
        connection.start()
        callbacks.open()
        callbacks.message({ data: '{"ready":true}' })
        callbacks.message({ data: "late output" })
        callbacks.error()
        callbacks.close()
        vi.advanceTimersByTime(100_000)
        expect(states).toEqual([{ status: "connecting" }])
        expect(onData).not.toHaveBeenCalled()
        expect(FakeWebSocket.instances).toHaveLength(1)
        expect(socket.close).toHaveBeenCalledOnce()
        expect([socket.onopen, socket.onmessage, socket.onerror, socket.onclose]).toEqual([null, null, null, null])
        expect(vi.getTimerCount()).toBe(0)
    })

    it("reports deliberate session end once and clears timers", () => {
        const { connection, socket, states } = setup()
        socket.open()
        socket.message('{"ready":true}')
        connection.disconnect()
        connection.disconnect()
        connection.start()
        expect(states.at(-1)).toEqual({ status: "disconnected", message: "Session ended" })
        expect(states.filter((state) => state.status === "disconnected")).toHaveLength(1)
        expect(socket.close).toHaveBeenCalledOnce()
        expect(connection.sendInput("\r")).toBe(false)
        expect(vi.getTimerCount()).toBe(0)
    })

    it("fails safely when a send throws and does not retry uncertain input", () => {
        const { connection, socket, states } = setup()
        socket.open()
        socket.message('{"ready":true}')
        socket.send.mockImplementationOnce(() => { throw new Error("broken pipe") })
        expect(connection.sendInput("possibly delivered\r")).toBe(false)
        expect(states.at(-1)).toMatchObject({ status: "disconnected", message: expect.stringContaining("may not have reached") })
        expect(connection.sendInput("never delivered\r")).toBe(false)
        expect(socket.send).toHaveBeenCalledOnce()
    })
})

describe("terminal WebSocket URL", () => {
    it.each([
        ["", "/api/v1/nodes/6/terminal/ws"],
        ["/", "/api/v1/nodes/6/terminal/ws"],
        ["/v8k3m2q", "/v8k3m2q/api/v1/nodes/6/terminal/ws"],
        ["v8k3m2q/", "/v8k3m2q/api/v1/nodes/6/terminal/ws"],
        ["/nested/prefix///?ignored=1#fragment", "/nested/prefix/api/v1/nodes/6/terminal/ws"],
    ])("resolves relative base %s against the page origin", (base, path) => {
        const origin = window.location.origin.replace(/^http/, "ws")
        expect(buildTerminalWebSocketUrl(base, 6)).toBe(`${origin}${path}`)
    })

    it.each([
        ["https://ftp.example:41062/v8k3m2q/", "wss://ftp.example:41062/v8k3m2q/api/v1/nodes/6/terminal/ws"],
        ["http://localhost:9761/panel", "ws://localhost:9761/panel/api/v1/nodes/6/terminal/ws"],
        ["https://api.example", "wss://api.example/api/v1/nodes/6/terminal/ws"],
    ])("uses the configured API host, protocol, and prefix for %s", (base, expected) => {
        expect(buildTerminalWebSocketUrl(base, 6)).toBe(expected)
    })

    it.each(["https://[invalid", "javascript:alert(1)"])("falls back to page origin for invalid base %s", (base) => {
        expect(buildTerminalWebSocketUrl(base, 6)).toBe(`${window.location.origin.replace(/^http/, "ws")}/api/v1/nodes/6/terminal/ws`)
    })
})
