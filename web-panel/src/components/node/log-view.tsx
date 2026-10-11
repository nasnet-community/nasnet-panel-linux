import type { ReactNode } from "react"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { LEVEL_COLORS, LEVEL_LABELS, formatTimestamp, type LogLevel } from "./log-format"

function highlightSearch(text: string, query: string): ReactNode {
    if (!query) return text
    const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
    const parts = text.split(new RegExp(`(${escaped})`, "gi"))
    return parts.map((part, i) =>
        part.toLowerCase() === query.toLowerCase() ? (
            <mark key={i} className="bg-yellow-500/40 text-yellow-200 rounded-sm px-0.5">
                {part}
            </mark>
        ) : (
            part
        )
    )
}

interface LogLineProps {
    /** epoch ms; leave out to hide the time column */
    time?: number
    /** leave out for a line with no level: it shows without a badge */
    level?: string
    message: string
    query: string
}

/** One row of the black log pane. */
export function LogLine({ time, level, message, query }: LogLineProps) {
    return (
        <div className="flex items-start gap-2 py-0.5 px-1 -mx-1 rounded hover:bg-white/5 leading-tight">
            {time !== undefined && (
                <span className="text-zinc-500 shrink-0 select-none tabular-nums">
                    {formatTimestamp(time)}
                </span>
            )}
            {level !== undefined && (
                <Badge
                    variant="outline"
                    className={cn(
                        "text-[11px] px-1.5 py-0 h-4 shrink-0 font-semibold uppercase leading-none",
                        LEVEL_COLORS[level as LogLevel] || LEVEL_COLORS.info,
                    )}
                >
                    {LEVEL_LABELS[level as LogLevel] || level}
                </Badge>
            )}
            <span className="break-all whitespace-pre-wrap min-w-0">
                {highlightSearch(message, query)}
            </span>
        </div>
    )
}
