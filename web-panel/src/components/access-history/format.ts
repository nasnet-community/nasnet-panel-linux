// Shared formatting for the access-history page. Times render in the
// browser's zone; UTC goes into tooltips for matching against other logs.

const HOUR = 60 * 60 * 1000
const DAY = 24 * HOUR

export const fmtInt = (n: number) => n.toLocaleString("en-US")

export function fmtCompact(n: number): string {
    const trim = (v: number) => v.toFixed(1).replace(/\.0$/, "")
    if (n >= 1_000_000) return `${trim(n / 1_000_000)}M`
    if (n >= 1_000) return `${trim(n / 1_000)}k`
    return n.toLocaleString("en-US")
}

export function fmtShare(part: number, total: number): string {
    if (total <= 0) return "0%"
    const pct = (part / total) * 100
    if (pct > 0 && pct < 0.1) return `${pct.toFixed(2)}%`
    if (pct < 10) return `${pct.toFixed(1).replace(/\.0$/, "")}%`
    // 99.92% must not read as all of it.
    if (part < total && pct > 99) return `${(Math.floor(pct * 10) / 10).toFixed(1)}%`
    return `${Math.round(pct)}%`
}

/** Short zone name of the viewer, e.g. "BST" or "GMT+3:30". */
export function tzName(at: Date = new Date()): string {
    const part = new Intl.DateTimeFormat(undefined, { timeZoneName: "short" })
        .formatToParts(at)
        .find(p => p.type === "timeZoneName")
    return part?.value ?? "local"
}

type TimeLike = string | number | Date

const toDate = (v: TimeLike) => (v instanceof Date ? v : new Date(v))

/** "Sat, Sep 19, 18:00" */
export function fmtDayTime(v: TimeLike): string {
    return toDate(v).toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false })
}

/** "Sep 26, 15:00" */
export function fmtShortTime(v: TimeLike): string {
    return toDate(v).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false })
}

/** "14:00 UTC, Sep 26" */
export function fmtUTC(v: TimeLike): string {
    const d = toDate(v)
    const hm = d.toLocaleTimeString([], { timeZone: "UTC", hour: "2-digit", minute: "2-digit", hour12: false })
    const md = d.toLocaleDateString([], { timeZone: "UTC", month: "short", day: "numeric" })
    return `${hm} UTC, ${md}`
}

/** "Sep 19, 17:10 to Sep 26, 17:10 BST" */
export function fmtWindow(from: Date, to: Date): string {
    return `${fmtShortTime(from)} to ${fmtShortTime(to)} ${tzName(to)}`
}

export function fmtAgo(v: TimeLike, now: number = Date.now()): string {
    const ms = Math.max(0, now - toDate(v).getTime())
    const min = Math.floor(ms / 60_000)
    if (min < 1) return "just now"
    if (min < 60) return `${min} min ago`
    const h = Math.floor(min / 60)
    if (h < 24) return `${h}h ago`
    const d = Math.floor(h / 24)
    if (d === 1) return "yesterday"
    return `${d} days ago`
}

const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/

/** Destinations are stored as whatever xray logged: a hostname or a raw IP. */
export function isIP(v: string): boolean {
    const s = v.replace(/^\[|\]$/g, "")
    return IPV4.test(s) || (s.includes(":") && /^[0-9a-f:.]+$/i.test(s))
}

export function startOfHour(ms: number): number {
    const d = new Date(ms)
    d.setMinutes(0, 0, 0)
    return d.getTime()
}

export function startOfLocalDay(ms: number): number {
    const d = new Date(ms)
    d.setHours(0, 0, 0, 0)
    return d.getTime()
}

/**
 * Every hour from `from` to `to`, with the sparse `buckets` dropped in.
 * Buckets are UTC hour starts; zones with a half-hour offset still line up
 * because the hours are stepped in absolute time.
 */
export function fillHours<T>(from: Date, to: Date, buckets: { bucket: string; v: T }[], empty: () => T): { t: number; v: T }[] {
    const byHour = new Map<number, T>()
    for (const b of buckets) byHour.set(new Date(b.bucket).getTime(), b.v)
    const first = Math.floor(from.getTime() / HOUR) * HOUR
    const out: { t: number; v: T }[] = []
    for (let t = first; t <= to.getTime(); t += HOUR) out.push({ t, v: byHour.get(t) ?? empty() })
    return out
}

/** Like fillHours for the per-subscription series, which comes hourly or in UTC days. */
export function fillBuckets<T>(from: Date, to: Date, step: "hour" | "day", buckets: { bucket: string; v: T }[], empty: () => T): { t: number; v: T }[] {
    if (step === "hour") return fillHours(from, to, buckets, empty)
    const byDay = new Map<number, T>()
    for (const b of buckets) byDay.set(new Date(b.bucket).getTime(), b.v)
    const out: { t: number; v: T }[] = []
    for (let t = Math.floor(from.getTime() / DAY) * DAY; t <= to.getTime(); t += DAY) out.push({ t, v: byDay.get(t) ?? empty() })
    return out
}

/** Local midnights inside the window, thinned so at most `max` remain. */
export function dayTicks(from: number, to: number, max = 8): number[] {
    const ticks: number[] = []
    let t = startOfLocalDay(from) + DAY
    while (t <= to) {
        ticks.push(t)
        const next = new Date(t)
        next.setDate(next.getDate() + 1)
        t = next.getTime()
    }
    if (ticks.length <= max) return ticks
    const step = Math.ceil(ticks.length / max)
    return ticks.filter((_, i) => i % step === 0)
}

/** "Sun 20" */
export function fmtDayTick(ms: number): string {
    const d = new Date(ms)
    return `${d.toLocaleDateString([], { weekday: "short" })} ${d.getDate()}`
}
