import type { LogLevel } from "@/components/node/log-format"

export interface AgentLogLine {
    raw: string
    /** epoch ms, when the line starts with a timestamp */
    time?: number
    level?: LogLevel
    message: string
}

const LEVELS: Record<string, LogLevel> = {
    info: "info",
    warn: "warning",
    warning: "warning",
    error: "error",
    debug: "debug",
    trace: "debug",
}

// telemt prints colour codes when it thinks it has a terminal
const ANSI = new RegExp(`${String.fromCharCode(27)}\\[[0-9;]*[A-Za-z]`, "g")
// "2026-10-02T10:15:57.148698Z  INFO telemt::maestro: ..." (telemt's tracing format)
const STAMPED = /^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?)\s+(.*)$/
const LEVEL_WORD = /^\[?(info|warn|warning|error|debug|trace)\]?:?(?:\s+|$)(.*)$/i
// "time=... level=INFO msg=..." (Go's slog / logfmt)
const KV_TIME = /(?:^|\s)time="?([^\s"]+)"?/
const KV_LEVEL = /(?:^|\s)level="?(\w+)"?/i
const KV_MSG = /(?:^|\s)msg=("(?:[^"\\]|\\.)*"|\S+)/

function parseTime(s: string): number | undefined {
    // Safari won't read more than milliseconds
    const ms = Date.parse(s.replace(" ", "T").replace(/(\.\d{3})\d+/, "$1"))
    return Number.isNaN(ms) ? undefined : ms
}

function unquote(s: string): string {
    if (!s.startsWith("\"")) return s
    try {
        return JSON.parse(s) as string
    } catch {
        return s.slice(1, -1)
    }
}

/** Splits an agent or proxy log line into time, level and message; anything else stays raw. */
export function parseLogLine(input: string): AgentLogLine {
    const raw = input.replace(ANSI, "")

    if (/^(?:time=\S+\s+)?level=/i.test(raw)) {
        const level = LEVELS[(KV_LEVEL.exec(raw)?.[1] ?? "").toLowerCase()]
        const t = KV_TIME.exec(raw)?.[1]
        const msg = KV_MSG.exec(raw)
        // keep the attributes after msg, they carry the detail
        const rest = raw
            .replace(KV_TIME, "")
            .replace(KV_LEVEL, "")
            .replace(KV_MSG, "")
            .trim()
        const message = [msg ? unquote(msg[1]) : "", rest].filter(Boolean).join(" ")
        return { raw, time: t ? parseTime(t) : undefined, level, message: message || raw }
    }

    const stamped = STAMPED.exec(raw)
    if (!stamped) return { raw, message: raw }
    const time = parseTime(stamped[1])
    if (time === undefined) return { raw, message: raw }
    const lw = LEVEL_WORD.exec(stamped[2])
    if (!lw) return { raw, time, message: stamped[2] }
    return { raw, time, level: LEVELS[lw[1].toLowerCase()], message: lw[2] }
}

const pad = (n: number) => String(n).padStart(2, "0")

/** "<node-name>-mtproto-agent-2026-10-02.log", safe for any file system. */
export function logFileName(nodeName: string, at: Date): string {
    const name = nodeName.trim().replace(/[^\p{L}\p{N}._-]+/gu, "-").replace(/-+/g, "-").replace(/^-|-$/g, "") || "node"
    return `${name}-mtproto-agent-${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}.log`
}
