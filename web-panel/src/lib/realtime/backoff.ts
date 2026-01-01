// Exponential backoff with jitter. delay = clamp(base*2^n, max) + rand(0, delay/2).
// Mirrors agent heartbeat; avoids thundering-herd on server bounce.
export interface BackoffOptions {
    base?: number // first-attempt delay in ms (default 500)
    max?: number  // ceiling in ms (default 30000)
}

export function backoffDelay(attempt: number, opts: BackoffOptions = {}): number {
    const base = opts.base ?? 500
    const max = opts.max ?? 30_000
    const exp = Math.min(attempt, 10) // avoid overflow
    const raw = Math.min(base * Math.pow(2, exp), max)
    const jitter = Math.random() * (raw / 2)
    return Math.floor(raw / 2 + jitter)
}
