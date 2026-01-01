import { backoffDelay, type BackoffOptions } from "./backoff"

export type WSStatus = "idle" | "connecting" | "connected" | "reconnecting" | "closed"

export interface WSClientOptions {
    url: string
    protocols?: string | string[]
    binaryType?: BinaryType
    // Handlers
    onOpen?: () => void
    onMessage?: (ev: MessageEvent) => void
    onStatus?: (status: WSStatus, info?: { attempt: number; delayMs?: number; error?: unknown }) => void
    // Liveness: if >0, the wrapper sends a keepalive frame every `pingIntervalMs`
    // and treats silence beyond `pongTimeoutMs` as a dead connection. The
    // WebSocket spec doesn't expose raw ping frames from the browser, so we
    // send a minimal JSON probe that the server should echo or simply accept.
    pingIntervalMs?: number
    pongTimeoutMs?: number
    pingPayload?: () => string | ArrayBufferLike
    // Treat any received message as liveness evidence (default true). Set to
    // false only if the server echoes pongs explicitly and you want to match
    // on those alone.
    livenessFromAnyMessage?: boolean
    // Backoff for reconnects
    backoff?: BackoffOptions
    maxAttempts?: number
    // Buffer outgoing sends while disconnected and flush on reconnect.
    bufferWhenDisconnected?: boolean
    maxBufferedMessages?: number
}

// ReconnectingWebSocket: backoff+jitter reconnect, JSON ping keepalive,
// optional send-while-disconnected buffer, status transitions. No history
// replay on reconnect — session-resume callers pass a token in the URL.
export class ReconnectingWebSocket {
    private opts: WSClientOptions
    private ws: WebSocket | null = null
    private attempt = 0
    private retryTimer: ReturnType<typeof setTimeout> | null = null
    private pingTimer: ReturnType<typeof setInterval> | null = null
    private pongDeadline: number | null = null
    private closed = false
    private status: WSStatus = "idle"
    private sendBuffer: Array<string | ArrayBufferLike | Blob | ArrayBufferView> = []

    constructor(opts: WSClientOptions) {
        this.opts = opts
    }

    start(): void {
        if (this.closed) return
        this.connect()
    }

    close(code?: number, reason?: string): void {
        this.closed = true
        this.stopTimers()
        if (this.ws) {
            try {
                this.ws.close(code, reason)
            } catch {
                // noop
            }
            this.ws = null
        }
        this.setStatus("closed")
    }

    getStatus(): WSStatus {
        return this.status
    }

    // send queues or forwards a frame. Returns true if written directly,
    // false if buffered or dropped.
    send(data: string | ArrayBufferLike | Blob | ArrayBufferView): boolean {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(data as any)
            return true
        }
        if (this.opts.bufferWhenDisconnected) {
            const cap = this.opts.maxBufferedMessages ?? 256
            if (this.sendBuffer.length < cap) {
                this.sendBuffer.push(data)
            }
        }
        return false
    }

    private setStatus(s: WSStatus, info?: { attempt: number; delayMs?: number; error?: unknown }): void {
        this.status = s
        this.opts.onStatus?.(s, info)
    }

    private stopTimers(): void {
        if (this.retryTimer) {
            clearTimeout(this.retryTimer)
            this.retryTimer = null
        }
        if (this.pingTimer) {
            clearInterval(this.pingTimer)
            this.pingTimer = null
        }
        this.pongDeadline = null
    }

    private connect(): void {
        if (this.closed) return

        this.setStatus(this.attempt === 0 ? "connecting" : "reconnecting", { attempt: this.attempt })

        const ws = new WebSocket(this.opts.url, this.opts.protocols)
        if (this.opts.binaryType) {
            ws.binaryType = this.opts.binaryType
        }
        this.ws = ws

        ws.onopen = () => {
            this.attempt = 0
            this.setStatus("connected", { attempt: 0 })
            this.opts.onOpen?.()

            // Flush any queued sends.
            if (this.sendBuffer.length > 0) {
                const pending = this.sendBuffer.splice(0)
                for (const frame of pending) {
                    if (ws.readyState === WebSocket.OPEN) {
                        ws.send(frame as any)
                    }
                }
            }

            this.startKeepalive()
        }

        ws.onmessage = (ev) => {
            if (this.opts.livenessFromAnyMessage !== false) {
                // Any message proves the tunnel is live.
                this.pongDeadline = null
            }
            this.opts.onMessage?.(ev)
        }

        ws.onerror = (ev) => {
            // Let onclose handle the reconnect. Surface the error for UI.
            this.setStatus(this.status, { attempt: this.attempt, error: ev })
        }

        ws.onclose = (ev) => {
            this.stopTimers()
            if (this.ws === ws) this.ws = null
            if (this.closed) return

            const max = this.opts.maxAttempts ?? 0
            if (max > 0 && this.attempt >= max) {
                this.setStatus("closed", { attempt: this.attempt, error: ev })
                return
            }

            this.attempt++
            const delay = backoffDelay(this.attempt - 1, this.opts.backoff)
            this.setStatus("reconnecting", { attempt: this.attempt, delayMs: delay, error: ev })
            this.retryTimer = setTimeout(() => {
                this.retryTimer = null
                this.connect()
            }, delay)
        }
    }

    private startKeepalive(): void {
        const interval = this.opts.pingIntervalMs ?? 0
        if (interval <= 0) return

        const pongTimeout = this.opts.pongTimeoutMs ?? Math.max(interval, 10_000)
        const makePayload = this.opts.pingPayload ?? (() => '{"type":"ping"}')

        this.pingTimer = setInterval(() => {
            if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
            try {
                this.ws.send(makePayload() as any)
            } catch {
                return
            }
            // Start pong window if not already ticking.
            if (this.pongDeadline == null) {
                this.pongDeadline = Date.now() + pongTimeout
            }
            // Check: if we set a deadline last tick and nothing reset it, the
            // tunnel is dead.
            if (Date.now() > (this.pongDeadline ?? 0)) {
                try {
                    this.ws.close(4000, "pong timeout")
                } catch {
                    // noop
                }
            }
        }, interval)
    }
}
