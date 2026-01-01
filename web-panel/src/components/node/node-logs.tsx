import { useEffect, useState, useRef, useCallback, useMemo } from "react"
import { getApiBaseUrl } from '@/lib/config'
import { ReconnectingSSE } from '@/lib/realtime'
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import {
    HiOutlineTerminal,
    HiOutlinePause,
    HiOutlinePlay,
    HiOutlineDownload,
    HiOutlineSearch,
    HiOutlineArrowsExpand,
    HiOutlineX,
} from "react-icons/hi"
import { cn } from "@/lib/utils"
import type { LogEntry } from "@/lib/types"

interface NodeLogsProps {
    nodeId: number
    isOnline: boolean
}

type ConnectionStatus = "connecting" | "connected" | "disconnected" | "error"
type LogLevel = "info" | "warning" | "error" | "debug"

const LEVEL_COLORS: Record<LogLevel, string> = {
    info: "bg-blue-500/15 text-blue-400 border-blue-500/25",
    warning: "bg-yellow-500/15 text-yellow-400 border-yellow-500/25",
    error: "bg-red-500/15 text-red-400 border-red-500/25",
    debug: "bg-zinc-500/15 text-zinc-400 border-zinc-500/25",
}

const LEVEL_LABELS: Record<LogLevel, string> = {
    info: "Info",
    warning: "Warn",
    error: "Error",
    debug: "Debug",
}

const LINE_COUNT_OPTIONS = [
    { value: "50", label: "50 lines" },
    { value: "100", label: "100 lines" },
    { value: "200", label: "200 lines" },
    { value: "500", label: "500 lines" },
    { value: "1000", label: "1,000 lines" },
    { value: "2000", label: "2,000 lines" },
]

const TIME_FILTER_OPTIONS = [
    { value: "0", label: "All time" },
    { value: "60000", label: "Last 1m" },
    { value: "300000", label: "Last 5m" },
    { value: "900000", label: "Last 15m" },
    { value: "1800000", label: "Last 30m" },
    { value: "3600000", label: "Last 1h" },
]

function formatTimestamp(ms: number): string {
    const d = new Date(ms)
    return d.toLocaleTimeString("en-US", { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" })
}

function highlightSearch(text: string, query: string): React.ReactNode {
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

export function NodeLogs({ nodeId, isOnline }: NodeLogsProps) {
    const [logs, setLogs] = useState<LogEntry[]>([])
    const [status, setStatus] = useState<ConnectionStatus>("disconnected")
    const [isPaused, setIsPaused] = useState(false)
    const [isFullscreen, setIsFullscreen] = useState(false)
    const [tailCount, setTailCount] = useState("100")
    const [showTimestamps, setShowTimestamps] = useState(true)
    const [levelFilter, setLevelFilter] = useState("all")
    const [searchQuery, setSearchQuery] = useState("")
    const [timeFilter, setTimeFilter] = useState("0")

    const sseRef = useRef<ReconnectingSSE | null>(null)
    const logsEndRef = useRef<HTMLDivElement>(null)
    const bufferRef = useRef<LogEntry[]>([])
    const isPausedRef = useRef(isPaused)
    const maxLines = parseInt(tailCount)

    // Append logs, respecting max line count
    const appendLogs = useCallback(
        (newEntries: LogEntry[]) => {
            setLogs((prev) => {
                const combined = [...prev, ...newEntries]
                if (combined.length > maxLines) {
                    return combined.slice(combined.length - maxLines)
                }
                return combined
            })
        },
        [maxLines]
    )

    // Auto-scroll when not paused
    useEffect(() => {
        if (!isPaused && logsEndRef.current) {
            const viewport = logsEndRef.current.closest("[data-radix-scroll-area-viewport]")
            if (viewport) {
                viewport.scrollTop = viewport.scrollHeight
            } else {
                logsEndRef.current.scrollIntoView({ behavior: "auto", block: "nearest" })
            }
        }
    }, [logs, isPaused])

    // Keep isPausedRef in sync
    useEffect(() => {
        isPausedRef.current = isPaused
    }, [isPaused])

    // SSE connection management via ReconnectingSSE wrapper: exponential
    // backoff + jitter on errors, status transitions surfaced to the UI.
    useEffect(() => {
        if (!isOnline) {
            setStatus("disconnected")
            return
        }

        const url = `${getApiBaseUrl()}/api/v1/nodes/${nodeId}/logs/stream?tail=${tailCount}`
        const sse = new ReconnectingSSE({
            url,
            withCredentials: true,
            events: ["log", "error"],
            onStatus: (s) => {
                switch (s) {
                    case "connecting":
                    case "reconnecting":
                        setStatus("connecting")
                        break
                    case "connected":
                        setStatus("connected")
                        break
                    case "closed":
                        setStatus("disconnected")
                        break
                }
            },
            onMessage: (type, event) => {
                if (type === "log") {
                    let entry: LogEntry
                    try {
                        entry = JSON.parse(event.data)
                    } catch {
                        entry = {
                            timestamp: Date.now(),
                            level: "info",
                            log_type: "all",
                            message: event.data,
                            source: "",
                        }
                    }
                    if (isPausedRef.current) {
                        bufferRef.current.push(entry)
                    } else {
                        appendLogs([entry])
                    }
                } else if (type === "error") {
                    appendLogs([{
                        timestamp: Date.now(),
                        level: "error",
                        log_type: "error",
                        message: `--- ${event.data || "Stream error"} ---`,
                        source: "system",
                    }])
                }
            },
        })
        sseRef.current = sse
        sse.start()

        return () => {
            sse.close()
            if (sseRef.current === sse) sseRef.current = null
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [nodeId, isOnline, tailCount])

    // Flush buffer on resume
    const togglePause = useCallback(() => {
        setIsPaused((prev) => {
            if (prev) {
                // Resuming — flush buffer
                const buffered = bufferRef.current.splice(0)
                if (buffered.length > 0) {
                    appendLogs(buffered)
                }
            }
            return !prev
        })
    }, [appendLogs])

    // When tailCount changes, clear logs (connection will reconnect via useEffect)
    useEffect(() => {
        setLogs([])
        bufferRef.current = []
    }, [tailCount])

    // Filtered logs
    const filteredLogs = useMemo(() => {
        let result = logs

        // Level filter
        if (levelFilter !== "all") {
            result = result.filter((l) => l.level === levelFilter)
        }

        // Time filter
        const timeMs = parseInt(timeFilter)
        if (timeMs > 0) {
            const cutoff = Date.now() - timeMs
            result = result.filter((l) => l.timestamp >= cutoff)
        }

        // Search filter
        if (searchQuery) {
            const q = searchQuery.toLowerCase()
            result = result.filter((l) => l.message.toLowerCase().includes(q))
        }

        return result
    }, [logs, levelFilter, timeFilter, searchQuery])

    // Download handler
    const handleDownload = useCallback(() => {
        const lines = filteredLogs.map((l) => {
            const ts = formatTimestamp(l.timestamp)
            return `${ts}  [${LEVEL_LABELS[l.level as LogLevel] || l.level}]  ${l.message}`
        })
        const blob = new Blob([lines.join("\n")], { type: "text/plain" })
        const url = URL.createObjectURL(blob)
        const a = document.createElement("a")
        a.href = url
        a.download = `node-${nodeId}-logs-${new Date().toISOString().slice(0, 19).replace(/:/g, "-")}.log`
        a.click()
        URL.revokeObjectURL(url)
    }, [filteredLogs, nodeId])

    const toggleFullscreen = () => setIsFullscreen(!isFullscreen)

    return (
        <Card
            className={cn(
                "flex flex-col bg-card/50 backdrop-blur-sm border-white/5 transition-all duration-200",
                isFullscreen
                    ? "fixed inset-0 z-50 h-screen rounded-none bg-background/95 backdrop-blur-xl"
                    : "h-[600px]"
            )}
        >
            <CardHeader className="border-b bg-muted/10 py-3 space-y-3">
                {/* Title row */}
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                        <CardTitle className="flex items-center gap-2 text-base">
                            <HiOutlineTerminal className="w-5 h-5" />
                            Live Logs
                        </CardTitle>
                        <Badge
                            variant="outline"
                            className={cn(
                                "font-mono text-xs capitalize",
                                status === "connected"
                                    ? "bg-green-500/10 text-green-500 border-green-500/20"
                                    : status === "connecting"
                                      ? "bg-yellow-500/10 text-yellow-500 border-yellow-500/20"
                                      : "bg-red-500/10 text-red-500 border-red-500/20"
                            )}
                        >
                            <span
                                className={cn(
                                    "w-1.5 h-1.5 rounded-full mr-1.5",
                                    status === "connected"
                                        ? "bg-green-500 animate-pulse"
                                        : status === "connecting"
                                          ? "bg-yellow-500 animate-pulse"
                                          : "bg-red-500"
                                )}
                            />
                            {status}
                        </Badge>
                        {isPaused && (
                            <Badge variant="outline" className="bg-yellow-500/10 text-yellow-500 border-yellow-500/20 text-xs">
                                {bufferRef.current.length > 0 ? `Paused (${bufferRef.current.length} buffered)` : "Paused"}
                            </Badge>
                        )}
                    </div>
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={toggleFullscreen}
                        className="h-8 w-8 p-0"
                        title={isFullscreen ? "Exit Fullscreen" : "Fullscreen"}
                    >
                        {isFullscreen ? <HiOutlineX className="w-4 h-4" /> : <HiOutlineArrowsExpand className="w-4 h-4" />}
                    </Button>
                </div>

                {/* Controls row */}
                <div className="flex flex-wrap items-center gap-3 text-sm">
                    {/* Line count */}
                    <div className="flex items-center gap-1.5">
                        <Label className="text-xs text-muted-foreground whitespace-nowrap">Lines:</Label>
                        <Select value={tailCount} onValueChange={setTailCount}>
                            <SelectTrigger className="h-7 w-[100px] text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {LINE_COUNT_OPTIONS.map((o) => (
                                    <SelectItem key={o.value} value={o.value}>
                                        {o.label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Time filter */}
                    <div className="flex items-center gap-1.5">
                        <Label className="text-xs text-muted-foreground whitespace-nowrap">Since:</Label>
                        <Select value={timeFilter} onValueChange={setTimeFilter}>
                            <SelectTrigger className="h-7 w-[110px] text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {TIME_FILTER_OPTIONS.map((o) => (
                                    <SelectItem key={o.value} value={o.value}>
                                        {o.label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Timestamps toggle */}
                    <div className="flex items-center gap-1.5">
                        <Switch id="timestamps" checked={showTimestamps} onCheckedChange={setShowTimestamps} className="scale-75" />
                        <Label htmlFor="timestamps" className="text-xs text-muted-foreground cursor-pointer">
                            Timestamps
                        </Label>
                    </div>

                    {/* Level filter */}
                    <div className="flex items-center gap-1.5">
                        <Label className="text-xs text-muted-foreground whitespace-nowrap">Level:</Label>
                        <Select value={levelFilter} onValueChange={setLevelFilter}>
                            <SelectTrigger className="h-7 w-[90px] text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="all">All</SelectItem>
                                <SelectItem value="info">Info</SelectItem>
                                <SelectItem value="warning">Warning</SelectItem>
                                <SelectItem value="error">Error</SelectItem>
                                <SelectItem value="debug">Debug</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Search */}
                    <div className="relative flex-1 min-w-[150px]">
                        <HiOutlineSearch className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
                        <Input
                            placeholder="Search logs..."
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            className="h-7 pl-7 text-xs"
                        />
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1.5">
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={togglePause}
                            className="h-7 gap-1.5 text-xs px-2.5"
                        >
                            {isPaused ? (
                                <>
                                    <HiOutlinePlay className="w-3.5 h-3.5" /> Resume
                                </>
                            ) : (
                                <>
                                    <HiOutlinePause className="w-3.5 h-3.5" /> Pause
                                </>
                            )}
                        </Button>
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={handleDownload}
                            className="h-7 gap-1.5 text-xs px-2.5"
                            disabled={filteredLogs.length === 0}
                        >
                            <HiOutlineDownload className="w-3.5 h-3.5" /> Download
                        </Button>
                    </div>
                </div>
            </CardHeader>

            <CardContent className="p-0 flex-1 bg-black/95 font-mono text-xs text-green-400 overflow-hidden relative">
                <ScrollArea className="h-full w-full">
                    <div className="p-3 space-y-0">
                        {filteredLogs.length === 0 && status === "connected" && (
                            <div className="flex flex-col items-center justify-center text-muted-foreground opacity-50 space-y-2 py-12">
                                <div className="animate-pulse">
                                    {searchQuery || levelFilter !== "all" || timeFilter !== "0"
                                        ? "No logs match filters"
                                        : "Waiting for logs..."}
                                </div>
                            </div>
                        )}
                        {filteredLogs.length === 0 && status === "disconnected" && (
                            <p className="text-gray-500 italic py-12 text-center">Node offline — logs unavailable.</p>
                        )}
                        {filteredLogs.map((log, i) => {
                            const level = log.level as LogLevel
                            const colorClass = LEVEL_COLORS[level] || LEVEL_COLORS.info
                            return (
                                <div
                                    key={i}
                                    className="flex items-start gap-2 py-0.5 px-1 -mx-1 rounded hover:bg-white/5 leading-tight"
                                >
                                    {showTimestamps && (
                                        <span className="text-zinc-500 shrink-0 select-none tabular-nums">
                                            {formatTimestamp(log.timestamp)}
                                        </span>
                                    )}
                                    <Badge
                                        variant="outline"
                                        className={cn("text-[10px] px-1.5 py-0 h-4 shrink-0 font-semibold uppercase leading-none", colorClass)}
                                    >
                                        {LEVEL_LABELS[level] || log.level}
                                    </Badge>
                                    <span className="break-all whitespace-pre-wrap min-w-0">
                                        {highlightSearch(log.message, searchQuery)}
                                    </span>
                                </div>
                            )
                        })}
                        <div ref={logsEndRef} />
                    </div>
                </ScrollArea>
                {isPaused && (
                    <div className="absolute bottom-4 left-1/2 -translate-x-1/2 bg-yellow-500/90 text-black px-4 py-1.5 rounded-full text-xs font-bold shadow-lg">
                        PAUSED {bufferRef.current.length > 0 && `\u00B7 ${bufferRef.current.length} buffered`}
                    </div>
                )}
            </CardContent>
        </Card>
    )
}
