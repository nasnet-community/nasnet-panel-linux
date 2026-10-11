// Level colours and the time format both log tabs use, so a full node's log
// and an MTProto node's log read the same.

export type LogLevel = "info" | "warning" | "error" | "debug"

export const LEVEL_COLORS: Record<LogLevel, string> = {
    info: "bg-blue-500/15 text-blue-400 border-blue-500/25",
    warning: "bg-yellow-500/15 text-yellow-400 border-yellow-500/25",
    error: "bg-red-500/15 text-red-400 border-red-500/25",
    debug: "bg-zinc-500/15 text-zinc-400 border-zinc-500/25",
}

export const LEVEL_LABELS: Record<LogLevel, string> = {
    info: "Info",
    warning: "Warn",
    error: "Error",
    debug: "Debug",
}

export function formatTimestamp(ms: number): string {
    const d = new Date(ms)
    return d.toLocaleTimeString("en-US", { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" })
}
