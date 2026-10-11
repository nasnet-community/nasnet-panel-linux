import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import {
    HiOutlineArrowDown,
    HiOutlineArrowsExpand,
    HiOutlineDownload,
    HiOutlinePause,
    HiOutlinePlay,
    HiOutlineSearch,
    HiOutlineTerminal,
    HiOutlineX,
} from "react-icons/hi"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { LogLine } from "@/components/node/log-view"
import type { LogLevel } from "@/components/node/log-format"
import { useMtprotoLogs, useMtprotoNode } from "@/lib/queries"
import { cn } from "@/lib/utils"
import { logFileName, parseLogLine, type AgentLogLine } from "./logs/agent-log"

const LINE_OPTIONS = [
    { value: "100", label: "100 lines" },
    { value: "200", label: "200 lines" },
    { value: "500", label: "500 lines" },
    { value: "1000", label: "1,000 lines" },
]

const SINCE_OPTIONS = [
    { value: "0", label: "All time" },
    { value: "60000", label: "Last 1m" },
    { value: "300000", label: "Last 5m" },
    { value: "900000", label: "Last 15m" },
    { value: "1800000", label: "Last 30m" },
    { value: "3600000", label: "Last 1h" },
]

const LEVEL_OPTIONS: { value: "all" | LogLevel; label: string }[] = [
    { value: "all", label: "All" },
    { value: "error", label: "Error" },
    { value: "warning", label: "Warn" },
    { value: "info", label: "Info" },
    { value: "debug", label: "Debug" },
]

const POLL_MS = 5_000
// this close to the bottom still counts as following
const PIN_SLACK_PX = 24

type Status = "loading" | "following" | "paused" | "link-down" | "error"

const STATUS: Record<Status, { label: string; badge: string; dot: string }> = {
    loading: { label: "Loading", badge: "bg-yellow-500/10 text-yellow-500 border-yellow-500/20", dot: "bg-yellow-500 animate-pulse" },
    following: { label: "Following", badge: "bg-green-500/10 text-green-500 border-green-500/20", dot: "bg-green-500 animate-pulse" },
    paused: { label: "Paused", badge: "bg-muted/40 text-muted-foreground border-border", dot: "bg-muted-foreground" },
    "link-down": { label: "Link down", badge: "bg-red-500/10 text-red-500 border-red-500/20", dot: "bg-red-500" },
    error: { label: "Unavailable", badge: "bg-red-500/10 text-red-500 border-red-500/20", dot: "bg-red-500" },
}

// phones get 44px controls, the desktop keeps the full node's compact row
const control = "h-11 sm:h-7 text-xs"
const trigger = "w-full min-w-0 text-xs data-[size=default]:h-11 sm:w-[120px] sm:data-[size=default]:h-9"

export interface MtprotoLogsProps {
    nodeId: number
    /** names the downloaded file */
    nodeName?: string
}

/** The agent's log, read from its memory over the link every few seconds. */
export function MtprotoLogs({ nodeId, nodeName }: MtprotoLogsProps) {
    const [lines, setLines] = useState("200")
    const [since, setSince] = useState("0")
    const [level, setLevel] = useState<"all" | LogLevel>("all")
    const [query, setQuery] = useState("")
    const [showTimestamps, setShowTimestamps] = useState(true)
    const [paused, setPaused] = useState(false)
    const [fullscreen, setFullscreen] = useState(false)
    const [pinned, setPinned] = useState(true)
    const paneRef = useRef<HTMLDivElement>(null)

    const q = useMtprotoLogs(nodeId, Number(lines), true)
    const mtproto = useMtprotoNode(nodeId)
    const { refetch } = q

    useEffect(() => {
        if (paused) return
        const timer = window.setInterval(() => {
            // a hidden tab doesn't need the traffic over the link
            if (document.visibilityState === "visible") void refetch({ cancelRefetch: false })
        }, POLL_MS)
        return () => window.clearInterval(timer)
    }, [paused, refetch])

    useEffect(() => {
        if (!fullscreen) return
        const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setFullscreen(false) }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [fullscreen])

    const parsed = useMemo(() => {
        const out: (AgentLogLine & { at?: number })[] = []
        for (const raw of q.data ?? []) {
            const line = parseLogLine(raw)
            // an unstamped line belongs to the stamped one above it
            out.push({ ...line, at: line.time ?? out[out.length - 1]?.at })
        }
        return out
    }, [q.data])

    const sinceMs = Number(since)
    const filtering = level !== "all" || sinceMs > 0 || query !== ""
    const visible = useMemo(() => {
        const needle = query.toLowerCase()
        // measured from when the log was read, not from now
        const cutoff = q.dataUpdatedAt - sinceMs
        return parsed.filter((l) =>
            (level === "all" || l.level === level)
            && (sinceMs === 0 || (l.at !== undefined && l.at >= cutoff))
            && (!needle || l.message.toLowerCase().includes(needle)),
        )
    }, [parsed, level, sinceMs, query, q.dataUpdatedAt])

    useLayoutEffect(() => {
        const pane = paneRef.current
        if (pinned && pane) pane.scrollTop = pane.scrollHeight
    }, [visible, pinned, showTimestamps, fullscreen])

    const onScroll = () => {
        const pane = paneRef.current
        if (pane) setPinned(pane.scrollHeight - pane.scrollTop - pane.clientHeight <= PIN_SLACK_PX)
    }

    const linkDown = !!mtproto.data && (mtproto.data.link?.up ?? 0) === 0
    const status: Status = q.isError ? (linkDown ? "link-down" : "error") : paused ? "paused" : q.isPending ? "loading" : "following"
    const errorText = (q.error as Error | null)?.message ?? "Could not read the agent's log"

    const download = () => {
        const blob = new Blob([visible.map((l) => l.raw).join("\n") + "\n"], { type: "text/plain" })
        const url = URL.createObjectURL(blob)
        const a = document.createElement("a")
        a.href = url
        a.download = logFileName(nodeName ?? `node-${nodeId}`, new Date())
        a.click()
        URL.revokeObjectURL(url)
    }

    return (
        <Card
            className={cn(
                "flex flex-col bg-card/50 backdrop-blur-sm border-white/5 transition-all duration-200",
                fullscreen
                    ? "fixed inset-0 z-50 h-dvh rounded-none bg-background/95 backdrop-blur-xl"
                    : "h-[calc(100dvh-6rem)] min-h-[560px] sm:h-[600px] sm:min-h-0",
            )}
        >
            <CardHeader className="border-b bg-muted/10 py-3 space-y-3 px-4 sm:px-6">
                <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                            <CardTitle className="flex items-center gap-2 text-base">
                                <HiOutlineTerminal className="w-5 h-5" />
                                Agent log
                            </CardTitle>
                            <Badge variant="outline" className={cn("font-mono text-xs", STATUS[status].badge)} data-testid="mtproto-log-status">
                                <span className={cn("w-1.5 h-1.5 rounded-full mr-1.5", STATUS[status].dot)} />
                                {STATUS[status].label}
                            </Badge>
                        </div>
                        <p className="text-xs text-muted-foreground">Kept in memory on the node, addresses hidden.</p>
                    </div>
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setFullscreen((f) => !f)}
                        className="size-11 shrink-0 p-0 sm:size-8"
                        title={fullscreen ? "Exit full screen" : "Full screen"}
                        aria-label={fullscreen ? "Exit full screen" : "Full screen"}
                    >
                        {fullscreen ? <HiOutlineX className="w-4 h-4" /> : <HiOutlineArrowsExpand className="w-4 h-4" />}
                    </Button>
                </div>

                <div className="grid grid-cols-2 gap-2 text-sm sm:flex sm:flex-wrap sm:items-center sm:gap-3">
                    <div className="flex min-w-0 items-center gap-1.5">
                        <Label className="text-xs text-muted-foreground whitespace-nowrap">Lines:</Label>
                        <Select value={lines} onValueChange={setLines}>
                            <SelectTrigger aria-label="Lines" className={cn(trigger, "sm:w-[112px]")}><SelectValue /></SelectTrigger>
                            <SelectContent>
                                {LINE_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
                            </SelectContent>
                        </Select>
                    </div>
                    <div className="flex min-w-0 items-center gap-1.5">
                        <Label className="text-xs text-muted-foreground whitespace-nowrap">Since:</Label>
                        <Select value={since} onValueChange={setSince}>
                            <SelectTrigger aria-label="Since" className={trigger}><SelectValue /></SelectTrigger>
                            <SelectContent>
                                {SINCE_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
                            </SelectContent>
                        </Select>
                    </div>
                    <div className="flex min-w-0 items-center gap-1.5">
                        <Label className="text-xs text-muted-foreground whitespace-nowrap">Level:</Label>
                        <Select value={level} onValueChange={(v) => setLevel(v as "all" | LogLevel)}>
                            <SelectTrigger aria-label="Level" className={cn(trigger, "sm:w-[100px]")}><SelectValue /></SelectTrigger>
                            <SelectContent>
                                {LEVEL_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
                            </SelectContent>
                        </Select>
                    </div>
                    <div className="flex min-h-11 items-center gap-1.5 sm:min-h-0">
                        <Switch id={`mtproto-log-ts-${nodeId}`} checked={showTimestamps} onCheckedChange={setShowTimestamps} className="scale-75" />
                        <Label htmlFor={`mtproto-log-ts-${nodeId}`} className="text-xs text-muted-foreground cursor-pointer">
                            Timestamps
                        </Label>
                    </div>
                    <div className="relative col-span-2 min-w-0 sm:min-w-[150px] sm:flex-1">
                        <HiOutlineSearch className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
                        <Input
                            aria-label="Search the log"
                            placeholder="Search logs..."
                            value={query}
                            onChange={(e) => setQuery(e.target.value)}
                            className={cn(control, "pl-7")}
                        />
                    </div>
                    <div className="col-span-2 grid grid-cols-2 gap-2 sm:flex sm:items-center sm:gap-1.5">
                        <Button variant="outline" size="sm" onClick={() => setPaused((p) => !p)} className={cn(control, "gap-1.5 px-2.5")}>
                            {paused
                                ? <><HiOutlinePlay className="w-3.5 h-3.5" /> Resume</>
                                : <><HiOutlinePause className="w-3.5 h-3.5" /> Pause</>}
                        </Button>
                        <Button variant="outline" size="sm" onClick={download} disabled={visible.length === 0} className={cn(control, "gap-1.5 px-2.5")}>
                            <HiOutlineDownload className="w-3.5 h-3.5" /> Download
                        </Button>
                    </div>
                </div>
            </CardHeader>

            <CardContent className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-black/95 p-0 font-mono text-xs text-green-400">
                {q.isError && q.data && (
                    <div role="alert" className="flex shrink-0 items-center justify-between gap-3 border-b border-red-500/20 bg-red-500/10 px-3 py-1.5 text-red-400">
                        <span className="min-w-0 truncate">{errorText}</span>
                        <Button variant="outline" size="sm" onClick={() => void refetch()} className="h-7 shrink-0 text-xs">Retry</Button>
                    </div>
                )}
                <div ref={paneRef} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden p-3" data-testid="mtproto-log">
                    {q.isError && !q.data ? (
                        <div role="alert" className="flex flex-col items-center gap-3 py-12 text-center">
                            <p className="max-w-md text-red-400">{errorText}</p>
                            <Button variant="outline" size="sm" onClick={() => void refetch()} className={control}>Retry</Button>
                        </div>
                    ) : q.isPending ? (
                        <p className="py-12 text-center text-muted-foreground opacity-60 animate-pulse">Loading…</p>
                    ) : parsed.length === 0 ? (
                        <p className="py-12 text-center text-muted-foreground opacity-60">Nothing logged yet.</p>
                    ) : visible.length === 0 && filtering ? (
                        <p className="py-12 text-center text-muted-foreground opacity-60">No lines match the filters.</p>
                    ) : (
                        visible.map((l, i) => (
                            <LogLine
                                key={i}
                                time={showTimestamps ? l.time : undefined}
                                level={l.level}
                                message={l.message}
                                query={query}
                            />
                        ))
                    )}
                </div>
                {!pinned && visible.length > 0 && (
                    <button
                        type="button"
                        onClick={() => setPinned(true)}
                        className="absolute bottom-4 left-1/2 flex h-9 -translate-x-1/2 items-center gap-1.5 rounded-full bg-foreground px-4 text-xs font-semibold text-background shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    >
                        <HiOutlineArrowDown className="w-3.5 h-3.5" /> Jump to latest
                    </button>
                )}
            </CardContent>
        </Card>
    )
}
