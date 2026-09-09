export type TerminalConnectionState = {
    status: "connecting" | "starting" | "ready" | "disconnected" | "exited" | "error"
    message?: string
    exitCode?: number
}

type TerminalConnectionOptions = {
    url: string
    onState: (state: TerminalConnectionState) => void
    onData: (data: Uint8Array | string) => void
}

const HANDSHAKE_TIMEOUT_MS = 30_000
const PING_INTERVAL_MS = 20_000
const LIVENESS_TIMEOUT_MS = 45_000

export function buildTerminalWebSocketUrl(baseUrl: string, nodeId: number): string {
    let url: URL
    try {
        url = new URL(baseUrl || "/", window.location.origin)
        if (!["http:", "https:", "ws:", "wss:"].includes(url.protocol)) {
            throw new Error("Unsupported terminal URL protocol")
        }
    } catch {
        url = new URL(window.location.origin)
    }
    url.protocol = url.protocol === "https:" || url.protocol === "wss:" ? "wss:" : "ws:"
    url.pathname = `${url.pathname.replace(/\/+$/, "")}/api/v1/nodes/${nodeId}/terminal/ws`
    url.search = ""
    url.hash = ""
    return url.toString()
}

/** A single shell attempt. Retrying requires a new instance and never replays input. */
export class TerminalConnection {
    private readonly options: TerminalConnectionOptions
    private socket: WebSocket | null = null
    private status: TerminalConnectionState["status"] = "disconnected"
    private started = false
    private ended = false
    private disposed = false
    private dimensions: { cols: number; rows: number } | null = null
    private handshakeTimer: ReturnType<typeof setTimeout> | null = null
    private pingTimer: ReturnType<typeof setInterval> | null = null
    private livenessTimer: ReturnType<typeof setTimeout> | null = null

    constructor(options: TerminalConnectionOptions) {
        this.options = options
    }

    start(): void {
        if (this.started || this.ended || this.disposed) return
        this.started = true
        this.setState({ status: "connecting" })
        if (this.ended || this.disposed) return

        let socket: WebSocket
        try {
            socket = new WebSocket(this.options.url)
        } catch {
            this.finish({ status: "error", message: "Unable to connect to the terminal." })
            return
        }
        this.socket = socket
        socket.binaryType = "arraybuffer"
        this.handshakeTimer = setTimeout(() => {
            this.finish({
                status: "error",
                message: this.status === "connecting"
                    ? "The terminal connection timed out. Start a new shell to try again."
                    : "The shell did not become ready. Start a new shell to try again.",
            })
        }, HANDSHAKE_TIMEOUT_MS)

        socket.onopen = () => {
            if (!this.isCurrent(socket)) return
            this.setState({ status: "starting" })
            if (!this.isCurrent(socket)) return
            this.sendDimensions()
            if (!this.isCurrent(socket)) return
            this.refreshLiveness()
            this.pingTimer = setInterval(() => {
                if (this.isCurrent(socket)) this.sendControl({ ping: true })
            }, PING_INTERVAL_MS)
        }
        socket.onmessage = (event: MessageEvent<unknown>) => {
            if (!this.isCurrent(socket)) return
            this.refreshLiveness()
            this.receive(event.data)
        }
        socket.onerror = () => {
            if (!this.isCurrent(socket)) return
            this.finish({ status: "error", message: "The terminal connection failed. Start a new shell to try again." })
        }
        socket.onclose = () => {
            if (!this.isCurrent(socket)) return
            this.finish({ status: "disconnected", message: "Connection lost. Output is preserved. Start a new shell to reconnect." })
        }
    }

    sendInput(data: string | Uint8Array): boolean {
        const socket = this.socket
        if (this.status !== "ready" || !socket || !this.isCurrent(socket) || socket.readyState !== WebSocket.OPEN) return false
        // Text frames are reserved for JSON control messages, including when
        // the user pastes text that happens to look like a control message.
        const bytes = typeof data === "string" ? new TextEncoder().encode(data) : new Uint8Array(data)
        if (bytes.byteLength === 0) return false
        try {
            socket.send(bytes)
            return true
        } catch {
            this.finish({ status: "disconnected", message: "Connection lost. Your last input may not have reached the shell." })
            return false
        }
    }

    resize(cols: number, rows: number): void {
        if (this.ended || this.disposed || !Number.isFinite(cols) || !Number.isFinite(rows) || cols < 1 || rows < 1) return
        const next = { cols: Math.floor(cols), rows: Math.floor(rows) }
        if (this.dimensions?.cols === next.cols && this.dimensions.rows === next.rows) return
        this.dimensions = next
        this.sendDimensions()
    }

    disconnect(): void {
        if (this.ended || this.disposed) return
        this.finish({ status: "disconnected", message: "Session ended" })
    }

    dispose(): void {
        if (this.disposed) return
        this.disposed = true
        this.ended = true
        this.release()
    }

    private isCurrent(socket: WebSocket): boolean {
        return !this.disposed && !this.ended && this.socket === socket
    }

    private setState(state: TerminalConnectionState): void {
        if (this.disposed) return
        this.status = state.status
        this.options.onState(state)
    }

    private markReady(): void {
        if (this.status === "ready" || this.ended || this.disposed) return
        if (this.handshakeTimer !== null) clearTimeout(this.handshakeTimer)
        this.handshakeTimer = null
        this.setState({ status: "ready" })
    }

    private receive(data: unknown): void {
        if (typeof data === "string") {
            let message: unknown
            try {
                message = JSON.parse(data)
            } catch {
                // Older agents can emit plain text instead of binary output.
            }
            if (message !== null && typeof message === "object") {
                const control = message as Record<string, unknown>
                if (typeof control.error === "string") {
                    this.finish({ status: "error", message: control.error || "The shell reported an error." })
                    return
                }
                if (typeof control.exit_code === "number") {
                    this.finish({ status: "exited", exitCode: control.exit_code, message: `Shell exited with code ${control.exit_code}.` })
                    return
                }
                if (control.ready === true) {
                    this.markReady()
                    return
                }
                if (control.pong === true) return
            }
            if (data.length > 0) {
                this.markReady()
                if (!this.ended && !this.disposed) this.options.onData(data)
            }
        } else if (data instanceof ArrayBuffer && data.byteLength > 0) {
            this.markReady()
            if (!this.ended && !this.disposed) this.options.onData(new Uint8Array(data))
        }
    }

    private sendDimensions(): void {
        if (this.dimensions) this.sendControl({ resize: this.dimensions })
    }

    private sendControl(message: object): void {
        const socket = this.socket
        if (!socket || !this.isCurrent(socket) || socket.readyState !== WebSocket.OPEN) return
        try {
            socket.send(JSON.stringify(message))
        } catch {
            this.finish({ status: "disconnected", message: "Connection lost. Output is preserved. Start a new shell to reconnect." })
        }
    }

    private refreshLiveness(): void {
        if (this.livenessTimer !== null) clearTimeout(this.livenessTimer)
        this.livenessTimer = setTimeout(() => {
            this.finish({ status: "disconnected", message: "The terminal stopped responding. Output is preserved. Start a new shell to reconnect." })
        }, LIVENESS_TIMEOUT_MS)
    }

    private finish(state: TerminalConnectionState): void {
        if (this.ended || this.disposed) return
        this.ended = true
        this.release()
        this.setState(state)
    }

    private release(): void {
        if (this.handshakeTimer !== null) clearTimeout(this.handshakeTimer)
        if (this.pingTimer !== null) clearInterval(this.pingTimer)
        if (this.livenessTimer !== null) clearTimeout(this.livenessTimer)
        this.handshakeTimer = null
        this.pingTimer = null
        this.livenessTimer = null

        const socket = this.socket
        this.socket = null
        if (!socket) return
        socket.onopen = null
        socket.onmessage = null
        socket.onerror = null
        socket.onclose = null
        if (socket.readyState === WebSocket.CONNECTING || socket.readyState === WebSocket.OPEN) {
            try {
                socket.close(1000, "Terminal session ended")
            } catch {
                // A connecting or already closing socket may reject close().
            }
        }
    }
}
