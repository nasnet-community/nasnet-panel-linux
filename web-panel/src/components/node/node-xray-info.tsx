import { useCallback, useEffect, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { ChevronDown, ChevronUp, Loader2, Play, RotateCcw, Square, TriangleAlert, Upload } from "lucide-react"
import { toast } from "@/lib/toast"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { getNodeRecentLogs, getNodeStats, pushNodeConfig, restartXrayProcess, startXrayProcess, stopXrayProcess } from "@/lib/api/nodes"
import type { LogEntry } from "@/lib/api/nodes"
import { queryKeys } from "@/lib/queries"
import type { Node, NodeOverview, NodeStats } from "@/lib/types"
import { cn, formatUptime } from "@/lib/utils"
import { OverviewCard } from "./overview/overview-card"
import { XrayLogo } from "./xray-logo"
import { XrayVersionDialog } from "./xray-version-dialog"

interface NodeXrayInfoProps {
    node: Node
    stats?: NodeStats
    configState?: NodeOverview["config"]["state"]
    isLoading?: boolean
    /** Phone layout: 44px controls, push as its own button. */
    compact?: boolean
}

type XrayAction = "start" | "stop" | "restart"

// "Xray 1.8.4 (Xray, ...) ..." → "26.7.28"
export function cleanXrayVersion(raw?: string): string | undefined {
    if (!raw) return undefined
    const parts = raw.split(" ")
    if (parts.length >= 2 && parts[0] === "Xray") return parts[1]
    return raw.replace(/^v/, "").substring(0, 20)
}

const rowCls = "flex items-center justify-between gap-2"

export function NodeXrayInfo({ node, stats, configState, isLoading, compact }: NodeXrayInfoProps) {
    const nodeId = node.id
    const confirm = useConfirm()
    const queryClient = useQueryClient()
    const [busy, setBusy] = useState<XrayAction | "push" | null>(null)
    const [errorLogs, setErrorLogs] = useState<LogEntry[]>([])
    const [showLogs, setShowLogs] = useState(false)
    const [logsLoading, setLogsLoading] = useState(false)
    const [actionError, setActionError] = useState<string | null>(null)
    const [showRecovery, setShowRecovery] = useState(false)

    const online = node.is_online
    const known = online && !!stats?.xray_status
    const running = known && stats?.xray_status === "running"
    const stopped = known && !running
    const version = cleanXrayVersion(stats?.xray_version)
    const recovery = node.last_crash_recovery
    const connected = stats?.online_users ?? 0

    // Reads the agent directly so the card shows the result of an action
    // instead of waiting for the next sweep.
    const refreshNow = useCallback(async () => {
        try {
            const res = await getNodeStats(nodeId, true)
            if (res.success && res.data) {
                queryClient.setQueryData(queryKeys.nodeStats(nodeId), { ...res.data, stats_unavailable: false, received_at: Date.now() })
            }
        } catch { /* the next sweep catches up */ }
        void queryClient.invalidateQueries({ queryKey: queryKeys.nodeOverview(nodeId) })
    }, [nodeId, queryClient])

    const fetchErrorLogs = useCallback(async () => {
        setLogsLoading(true)
        try {
            const res = await getNodeRecentLogs(nodeId, 50)
            if (res.success && res.data) {
                const errors = res.data.filter(l => l.level === "error" || l.level === "warning")
                setErrorLogs(errors.length > 0 ? errors : res.data.slice(-20))
            }
        } catch { /* logs are supplementary */ } finally {
            setLogsLoading(false)
        }
    }, [nodeId])

    // Reverse-mode nodes can't stream logs yet; don't ask on every render.
    const canTailLogs = node.connect_mode !== "reverse"

    useEffect(() => {
        if (stopped && canTailLogs) {
            void fetchErrorLogs()
        } else {
            setErrorLogs([])
            setShowLogs(false)
            setActionError(null)
        }
    }, [stopped, canTailLogs, fetchErrorLogs])

    const run = async (action: XrayAction) => {
        setBusy(action)
        setActionError(null)
        try {
            const res = action === "start" ? await startXrayProcess(nodeId)
                : action === "stop" ? await stopXrayProcess(nodeId)
                : await restartXrayProcess(nodeId)
            if (res.success) {
                toast.success(action === "start" ? "Xray started" : action === "stop" ? "Xray stopped" : "Xray restarted")
                await refreshNow()
            } else {
                setActionError(res.error || `Couldn't ${action} Xray`)
                toast.error(`Couldn't ${action} Xray`)
                setTimeout(() => void fetchErrorLogs(), 500)
                setShowLogs(true)
            }
        } catch {
            toast.error(`Couldn't ${action} Xray: the request failed`)
        } finally {
            setBusy(null)
        }
    }

    const askRestart = async () => {
        const ok = await confirm({
            title: `Restart Xray on ${node.name}?`,
            description: connected > 0
                ? `Xray restarts with its current config. The ${connected} ${connected === 1 ? "person" : "people"} connected right now drop for a few seconds, then reconnect on their own.`
                : "Xray restarts with its current config. Anyone who connects in the next few seconds may need to retry.",
            confirmLabel: "Restart Xray",
        })
        if (ok) await run("restart")
    }

    const askStop = async () => {
        const ok = await confirm({
            title: `Stop Xray on ${node.name}?`,
            description: `${connected > 0 ? `All ${connected} connected ${connected === 1 ? "person loses their" : "people lose their"} connection, and n` : "N"}obody can connect to this node until you start Xray again.`,
            confirmLabel: "Stop Xray",
            variant: "destructive",
        })
        if (ok) await run("stop")
    }

    const push = async () => {
        setBusy("push")
        try {
            const res = await pushNodeConfig(nodeId)
            if (res.success) {
                toast.success("Config pushed")
                await refreshNow()
            } else {
                toast.error(res.error || "Couldn't push the config")
            }
        } catch {
            toast.error("Couldn't push the config: the request failed")
        } finally {
            setBusy(null)
        }
    }

    const status = !online ? { label: "Unknown", tone: "text-muted-foreground", ring: "bg-muted/30 ring-muted-foreground/25 text-muted-foreground" }
        : running ? { label: "Active", tone: "text-emerald-500", ring: "bg-emerald-500/10 ring-emerald-500/25 text-emerald-500" }
        : stopped ? { label: "Stopped", tone: "text-red-500", ring: "bg-red-500/10 ring-red-500/25 text-red-500" }
        : { label: "Checking", tone: "text-muted-foreground", ring: "bg-muted/30 ring-muted-foreground/25 text-muted-foreground" }

    const drifted = online && configState === "drifted"
    const recoveryLabel = !node.crash_recovery_settings?.enabled ? "Off"
        : !recovery ? "On"
        : recovery.exhausted ? "Exhausted"
        : recovery.success ? "Recovered"
        : "Failed"
    const recoveryTone = recoveryLabel === "Exhausted" || recoveryLabel === "Failed" ? "text-red-400"
        : recoveryLabel === "Recovered" ? "text-emerald-400"
        : "text-muted-foreground"

    const ctrl = compact ? "h-11 text-xs" : "h-7 text-[11px]"
    const btnBase = cn("w-full rounded-lg font-bold uppercase tracking-[0.06em]", ctrl)

    return (
        <OverviewCard
            glow="indigo"
            title="Xray process"
            aside={
                <div className="flex items-center gap-1.5">
                    {(isLoading || busy) && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" aria-hidden />}
                    <XrayVersionDialog nodeId={nodeId} currentVersion={version ? `v${version}` : "Unknown"} isOnline={online} onRefresh={() => void refreshNow()} />
                </div>
            }
            className="group"
        >
            <div className="flex items-center gap-3">
                <div className={cn("flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-full ring-2", status.ring)}>
                    <XrayLogo className="h-4 w-4" />
                </div>
                <div className="flex min-w-0 flex-col">
                    <span className={cn("text-sm font-bold leading-[18px]", status.tone)}>{status.label}</span>
                    <span className="truncate font-mono text-[12px] leading-[14px] text-muted-foreground">
                        {online ? (version ? `v${version}` : "—") : version ? `last seen v${version}` : "—"}
                    </span>
                </div>
            </div>

            <div className={cn("flex flex-col border-t border-dashed border-border/70 pt-2.5 font-bold uppercase tracking-[0.06em]", compact ? "gap-2.5 text-xs" : "gap-[7px] text-[12px] leading-[14px]")}>
                <div className={rowCls}><span className="text-muted-foreground">PID</span><span className="font-mono">{running && stats?.xray_pid ? stats.xray_pid : "—"}</span></div>
                <div className={rowCls}><span className="text-muted-foreground">Uptime</span><span className="font-mono">{running && stats?.process_uptime ? formatUptime(stats.process_uptime) : "—"}</span></div>
                <div className={rowCls}>
                    <span className="text-muted-foreground">Config</span>
                    <span className="inline-flex items-center gap-1.5">
                        {!online || configState === undefined || configState === "unknown"
                            ? <span className="text-muted-foreground">—</span>
                            : drifted ? <span className="text-amber-500">Drifted</span>
                            : <span>In sync</span>}
                        {drifted && !compact && (
                            <button
                                type="button"
                                onClick={() => void push()}
                                disabled={busy !== null}
                                title="Push config"
                                aria-label="Push config to the node"
                                className="-my-1 inline-flex h-5 w-5 items-center justify-center rounded-[5px] border border-amber-500/35 bg-amber-500/10 text-amber-500 transition-colors hover:bg-amber-500/20 disabled:opacity-50"
                            >
                                {busy === "push" ? <Loader2 className="h-3 w-3 animate-spin" /> : <Upload className="h-3 w-3" />}
                            </button>
                        )}
                    </span>
                </div>
                <div className={rowCls}><span className="text-muted-foreground">Crash recovery</span><span className={recoveryTone}>{recoveryLabel}</span></div>
            </div>

            {/* Recovery details — when stopped and a recovery command ran */}
            {stopped && recovery && (
                <div className="border-t border-dashed border-border/70 pt-2.5">
                    <button
                        type="button"
                        className={cn("flex w-full items-center justify-between text-[12px] font-bold uppercase tracking-wider transition-colors", recoveryTone)}
                        onClick={() => setShowRecovery(v => !v)}
                        aria-expanded={showRecovery}
                    >
                        <span className="flex items-center gap-1.5">
                            <RotateCcw className="h-3 w-3" />
                            Recovery {recoveryLabel.toLowerCase()}
                            <span className="font-normal text-muted-foreground">
                                ({recovery.attempt_num}/{recovery.max_attempts > 0 ? recovery.max_attempts : "∞"})
                            </span>
                        </span>
                        {showRecovery ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                    </button>
                    {showRecovery && (
                        <div className="mt-2 space-y-1.5 rounded-lg border border-border/40 bg-black/40 p-2">
                            <div className="text-[12px] text-muted-foreground">{new Date(recovery.timestamp).toLocaleString()}</div>
                            {recovery.error && <div className="break-all font-mono text-[12px] text-red-400">RPC error: {recovery.error}</div>}
                            {!recovery.exhausted && !recovery.error && (
                                <>
                                    <div className="font-mono text-[12px] text-muted-foreground">Exit code: {recovery.exit_code}</div>
                                    {recovery.stdout && <div className="max-h-24 overflow-y-auto whitespace-pre-wrap break-all font-mono text-[12px] text-muted-foreground/80">{recovery.stdout}</div>}
                                    {recovery.stderr && <div className="max-h-24 overflow-y-auto whitespace-pre-wrap break-all font-mono text-[12px] text-red-400/80">{recovery.stderr}</div>}
                                </>
                            )}
                        </div>
                    )}
                </div>
            )}

            {/* Recent errors — when stopped or an action failed */}
            {stopped && (errorLogs.length > 0 || actionError) && (
                <div className="border-t border-dashed border-border/70 pt-2.5">
                    <button
                        type="button"
                        className="flex w-full items-center justify-between text-[12px] font-bold uppercase tracking-wider text-red-400 transition-colors hover:text-red-300"
                        onClick={() => setShowLogs(v => !v)}
                        aria-expanded={showLogs}
                    >
                        <span className="flex items-center gap-1.5">
                            <TriangleAlert className="h-3 w-3" />
                            {actionError ? "Startup error" : "Recent logs"}
                            {logsLoading && <Loader2 className="h-3 w-3 animate-spin" />}
                        </span>
                        {showLogs ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                    </button>
                    {showLogs && (
                        <div className="mt-2 max-h-48 space-y-0.5 overflow-y-auto rounded-lg border border-red-500/10 bg-black/40 p-2">
                            {actionError && <div className="mb-1 whitespace-pre-wrap break-all border-b border-red-500/10 pb-1 font-mono text-[12px] text-red-400">{actionError}</div>}
                            {errorLogs.map((log, i) => (
                                <div key={i} className={cn("break-all font-mono text-[12px] leading-relaxed", log.level === "error" ? "text-red-400" : log.level === "warning" ? "text-yellow-400" : "text-muted-foreground")}>
                                    <span className="text-muted-foreground/50">{new Date(log.timestamp).toLocaleTimeString()} </span>
                                    {log.message}
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            )}

            <div className="mt-auto flex flex-col gap-2">
                {drifted && compact && (
                    <Button variant="outline" onClick={() => void push()} disabled={busy !== null} className={cn(btnBase, "border-amber-500/35 bg-amber-500/5 text-amber-500 hover:bg-amber-500/10 hover:text-amber-500")}>
                        {busy === "push" ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <Upload className="mr-1.5 h-3.5 w-3.5" />}
                        Push config
                    </Button>
                )}
                {stopped ? (
                    <Button onClick={() => void run("start")} disabled={busy !== null} className={cn(btnBase, "bg-emerald-600 text-white hover:bg-emerald-700")}>
                        {busy === "start" ? <Loader2 className="mr-1.5 h-3 w-3 animate-spin" /> : <Play className="mr-1.5 h-3 w-3" />}
                        Start Xray
                    </Button>
                ) : (
                    <div className="grid grid-cols-2 gap-1.5">
                        <Button
                            variant="outline"
                            onClick={() => void askRestart()}
                            disabled={!running || busy !== null}
                            title={online ? undefined : "Needs the node online"}
                            className={cn(btnBase, "px-2")}
                        >
                            {busy === "restart" ? <Loader2 className="mr-1.5 h-3 w-3 animate-spin" /> : <RotateCcw className="mr-1.5 h-3 w-3" />}
                            Restart
                        </Button>
                        <Button
                            variant="outline"
                            onClick={() => void askStop()}
                            disabled={!running || busy !== null}
                            title={online ? undefined : "Needs the node online"}
                            className={cn(btnBase, "px-2", running && "border-red-500/25 text-red-500 hover:bg-red-500/10 hover:text-red-500")}
                        >
                            {busy === "stop" ? <Loader2 className="mr-1.5 h-3 w-3 animate-spin" /> : <Square className="mr-1.5 h-3 w-3" />}
                            Stop
                        </Button>
                    </div>
                )}
            </div>
        </OverviewCard>
    )
}
