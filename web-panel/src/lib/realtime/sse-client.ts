import { backoffDelay, type BackoffOptions } from "./backoff"

export type SSEStatus = "idle" | "connecting" | "connected" | "reconnecting" | "closed"

export interface SSEClientOptions {
    url: string
    withCredentials?: boolean
    // If set, the wrapper listens for named events; otherwise only the default
    // message event is dispatched.
    events?: string[]
    // Called every time the underlying EventSource produces a message event.
    // `type` is the event name ("message" for unnamed events).
    onMessage?: (type: string, ev: MessageEvent) => void
    // Status changes (connecting/connected/reconnecting/closed/idle).
    onStatus?: (status: SSEStatus, info?: { attempt: number; delayMs?: number; error?: unknown }) => void
    // Backoff policy for reconnects. Defaults to 500ms base / 30s max.
    backoff?: BackoffOptions
    // Maximum number of reconnect attempts. 0 or omitted = unlimited.
    maxAttempts?: number
}

// ReconnectingSSE wraps EventSource with backoff+jitter, capped attempts,
// status transitions for UI badges, and clean .close() teardown. Native
// lastEventId resume still works.
export class ReconnectingSSE {
    private opts: SSEClientOptions
    private es: EventSource | null = null
    private attempt = 0
    private retryTimer: ReturnType<typeof setTimeout> | null = null
    private closed = false
    private status: SSEStatus = "idle"

    constructor(opts: SSEClientOptions) {
        this.opts = opts
    }

    start(): void {
        if (this.closed) return
        this.connect()
    }

    close(): void {
        this.closed = true
        this.setStatus("closed")
        if (this.retryTimer) {
            clearTimeout(this.retryTimer)
            this.retryTimer = null
        }
        if (this.es) {
            this.es.close()
            this.es = null
        }
    }

    getStatus(): SSEStatus {
        return this.status
    }

    private setStatus(s: SSEStatus, info?: { attempt: number; delayMs?: number; error?: unknown }): void {
        this.status = s
        this.opts.onStatus?.(s, info)
    }

    private connect(): void {
        if (this.closed) return

        this.setStatus(this.attempt === 0 ? "connecting" : "reconnecting", { attempt: this.attempt })

        const es = new EventSource(this.opts.url, {
            withCredentials: this.opts.withCredentials ?? false,
        })
        this.es = es

        es.onopen = () => {
            this.attempt = 0
            this.setStatus("connected", { attempt: 0 })
        }

        const handle = (type: string) => (ev: MessageEvent) => {
            this.opts.onMessage?.(type, ev)
        }

        if (this.opts.events && this.opts.events.length > 0) {
            for (const name of this.opts.events) {
                es.addEventListener(name, handle(name) as EventListener)
            }
        } else {
            es.onmessage = handle("message")
        }

        es.onerror = (ev) => {
            // EventSource reaches CLOSED on fatal errors. Either way we bin it
            // and take ownership of the retry.
            if (this.es === es) {
                this.es = null
            }
            es.close()

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
}
