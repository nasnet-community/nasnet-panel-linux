import { formatUptime } from "@/lib/utils"
import { Card } from "@/components/ui/card"
import { Loader2 } from "lucide-react"
import {
    HiOutlineStatusOnline,
    HiOutlineExclamationCircle,
    HiOutlinePlay,
    HiOutlineStop,
    HiOutlineRefresh,
    HiOutlineChevronDown,
    HiOutlineChevronUp,
} from "react-icons/hi"
import { Button } from "@/components/ui/button"
import { toast } from "sonner"
import { startXrayProcess, stopXrayProcess, restartXrayProcess, getNodeRecentLogs } from "@/lib/api/nodes"
import { useState, useEffect, useCallback } from "react"
import type { Node, NodeStats } from "@/lib/types"
import type { LogEntry } from "@/lib/api/nodes"
import { XrayVersionDialog } from "./xray-version-dialog"

interface NodeXrayInfoProps {
    node: Node
    stats?: NodeStats
    isLoading?: boolean
    onRefresh?: () => void
}

export function NodeXrayInfo({ node, stats, isLoading, onRefresh }: NodeXrayInfoProps) {
    const nodeId = node.id
    const [isActionLoading, setIsActionLoading] = useState(false)
    const [confirmingStop, setConfirmingStop] = useState(false)
    const [errorLogs, setErrorLogs] = useState<LogEntry[]>([])
    const [showLogs, setShowLogs] = useState(false)
    const [logsLoading, setLogsLoading] = useState(false)
    const [actionError, setActionError] = useState<string | null>(null)

    const isRunning = stats?.xray_status === "running"

    const fetchErrorLogs = useCallback(async () => {
        setLogsLoading(true)
        try {
            const res = await getNodeRecentLogs(nodeId, 50)
            if (res.success && res.data) {
                // Filter to only warning/error level logs
                const errors = res.data.filter(
                    (l) => l.level === "error" || l.level === "warning"
                )
                setErrorLogs(errors.length > 0 ? errors : res.data.slice(-20))
            }
        } catch {
            // Silently fail — logs are supplementary
        } finally {
            setLogsLoading(false)
        }
    }, [nodeId])

    // Fetch error logs when xray is stopped
    useEffect(() => {
        if (!isRunning && stats) {
            fetchErrorLogs()
        } else {
            setErrorLogs([])
            setShowLogs(false)
            setActionError(null)
        }
    }, [isRunning, stats, fetchErrorLogs])

    const handleAction = async (action: "start" | "stop" | "restart") => {
        setIsActionLoading(true)
        setConfirmingStop(false)
        setActionError(null)
        try {
            let res
            if (action === "start") res = await startXrayProcess(nodeId)
            else if (action === "stop") res = await stopXrayProcess(nodeId)
            else res = await restartXrayProcess(nodeId)

            if (res.success) {
                toast.success(res.data?.message || `Xray ${action} command sent`)
                setErrorLogs([])
                setShowLogs(false)
                onRefresh?.()
            } else {
                const errMsg = res.error || `Failed to ${action} Xray`
                setActionError(errMsg)
                toast.error(`Failed to ${action} Xray`)
                // Fetch fresh logs after failure
                setTimeout(() => fetchErrorLogs(), 500)
                setShowLogs(true)
            }
        } catch (error) {
            toast.error("An unexpected error occurred")
        } finally {
            setIsActionLoading(false)
        }
    }

    const handleStopClick = () => {
        if (confirmingStop) {
            handleAction("stop")
        } else {
            setConfirmingStop(true)
            setTimeout(() => setConfirmingStop(false), 3000)
        }
    }

    // Clean version string: "Xray 1.8.4 (Xray, ...) ..." -> "v1.8.4"
    const getCleanVersion = (raw?: string) => {
        if (!raw) return "Unknown"
        const parts = raw.split(" ")
        if (parts.length >= 2 && parts[0] === "Xray") {
            return `v${parts[1]}`
        }
        return raw.substring(0, 20)
    }

    const hasLogs = errorLogs.length > 0 || actionError
    const [showRecovery, setShowRecovery] = useState(false)
    const recovery = node.last_crash_recovery

    return (
        <Card className="relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-indigo-500/10 rounded-2xl p-4 flex flex-col justify-between bg-card/50 backdrop-blur-sm border-white/5">
            <div className="flex flex-col gap-3 relative z-10">
                {/* Header */}
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                        <p className="text-[11px] uppercase font-bold text-muted-foreground tracking-widest">Xray Process</p>
                        {(isLoading || isActionLoading) && (
                            <Loader2 className="w-3 h-3 animate-spin text-indigo-500" />
                        )}
                    </div>
                    <XrayVersionDialog
                        nodeId={nodeId}
                        currentVersion={getCleanVersion(stats?.xray_version)}
                        isOnline={isRunning}
                        onRefresh={onRefresh}
                    />
                </div>

                {/* Status Section */}
                <div className="flex items-center gap-3">
                    <div className={`relative flex items-center justify-center w-8 h-8 rounded-full ring-2 transition-all duration-500 ${isRunning
                        ? "bg-emerald-500/10 text-emerald-500 ring-emerald-500/20"
                        : "bg-red-500/10 text-red-500 ring-red-500/20"
                        }`}>
                        {isRunning ? (
                            <HiOutlineStatusOnline className="w-4 h-4" />
                        ) : (
                            <HiOutlineExclamationCircle className="w-4 h-4" />
                        )}
                        {isRunning && (
                            <span className="absolute inset-0 rounded-full animate-ping bg-emerald-500/20" />
                        )}
                    </div>

                    <div className="flex flex-col">
                        <span className={`text-sm font-bold tracking-tight ${isRunning ? "text-emerald-500" : "text-red-500"}`}>
                            {isRunning ? "Active" : "Stopped"}
                        </span>
                        <span className="text-[11px] text-muted-foreground font-mono">
                            {getCleanVersion(stats?.xray_version)}
                        </span>
                    </div>
                </div>

                {/* Metrics */}
                <div className="flex flex-col gap-1.5 border-t border-dashed border-border/50 pt-2.5">
                    <div className="flex justify-between items-center text-[11px] uppercase font-bold tracking-wider">
                        <span className="text-muted-foreground">PID</span>
                        <span className="font-mono text-foreground">
                            {stats?.xray_pid ? stats.xray_pid : "—"}
                        </span>
                    </div>
                    <div className="flex justify-between items-center text-[11px] uppercase font-bold tracking-wider">
                        <span className="text-muted-foreground">Uptime</span>
                        <span className="font-mono text-foreground">
                            {stats?.process_uptime ? formatUptime(stats.process_uptime) : "—"}
                        </span>
                    </div>
                </div>

                {/* Recovery Status Section (shown when stopped and recovery was attempted) */}
                {!isRunning && recovery && (
                    <div className="border-t border-dashed border-border/50 pt-2.5">
                        <button
                            type="button"
                            className={`flex items-center justify-between w-full text-[11px] uppercase font-bold tracking-wider transition-colors ${
                                recovery.exhausted
                                    ? "text-red-400 hover:text-red-300"
                                    : recovery.success
                                      ? "text-emerald-400 hover:text-emerald-300"
                                      : "text-amber-400 hover:text-amber-300"
                            }`}
                            onClick={() => setShowRecovery(!showRecovery)}
                        >
                            <span className="flex items-center gap-1.5">
                                <HiOutlineRefresh className="w-3 h-3" />
                                Recovery {recovery.exhausted ? "Exhausted" : recovery.success ? "Succeeded" : "Failed"}
                                <span className="text-muted-foreground font-normal">
                                    {recovery.max_attempts > 0
                                        ? `(${recovery.attempt_num}/${recovery.max_attempts})`
                                        : `(${recovery.attempt_num}/\u221e)`}
                                </span>
                            </span>
                            {showRecovery ? (
                                <HiOutlineChevronUp className="w-3 h-3" />
                            ) : (
                                <HiOutlineChevronDown className="w-3 h-3" />
                            )}
                        </button>

                        {showRecovery && (
                            <div className="mt-2 rounded-lg bg-black/50 border border-border/10 p-2 space-y-1.5">
                                <div className="text-[10px] text-muted-foreground">
                                    {new Date(recovery.timestamp).toLocaleString()}
                                </div>
                                {recovery.error && (
                                    <div className="text-[10px] font-mono text-red-400 break-all">
                                        RPC Error: {recovery.error}
                                    </div>
                                )}
                                {!recovery.exhausted && !recovery.error && (
                                    <>
                                        <div className="text-[10px] font-mono text-muted-foreground">
                                            Exit code: {recovery.exit_code}
                                        </div>
                                        {recovery.stdout && (
                                            <div className="text-[10px] font-mono text-muted-foreground/80 whitespace-pre-wrap break-all max-h-24 overflow-y-auto">
                                                {recovery.stdout}
                                            </div>
                                        )}
                                        {recovery.stderr && (
                                            <div className="text-[10px] font-mono text-red-400/80 whitespace-pre-wrap break-all max-h-24 overflow-y-auto">
                                                {recovery.stderr}
                                            </div>
                                        )}
                                    </>
                                )}
                            </div>
                        )}
                    </div>
                )}

                {/* Error Logs Section (shown when xray is stopped or action failed) */}
                {!isRunning && hasLogs && (
                    <div className="border-t border-dashed border-border/50 pt-2.5">
                        <button
                            type="button"
                            className="flex items-center justify-between w-full text-[11px] uppercase font-bold tracking-wider text-red-400 hover:text-red-300 transition-colors"
                            onClick={() => setShowLogs(!showLogs)}
                        >
                            <span className="flex items-center gap-1.5">
                                <HiOutlineExclamationCircle className="w-3 h-3" />
                                {actionError ? "Startup Error" : "Recent Logs"}
                                {logsLoading && <Loader2 className="w-3 h-3 animate-spin" />}
                            </span>
                            {showLogs ? (
                                <HiOutlineChevronUp className="w-3 h-3" />
                            ) : (
                                <HiOutlineChevronDown className="w-3 h-3" />
                            )}
                        </button>

                        {showLogs && (
                            <div className="mt-2 max-h-48 overflow-y-auto rounded-lg bg-black/50 border border-red-500/10 p-2 space-y-0.5">
                                {actionError && (
                                    <div className="text-[10px] font-mono text-red-400 whitespace-pre-wrap break-all pb-1 mb-1 border-b border-red-500/10">
                                        {actionError}
                                    </div>
                                )}
                                {errorLogs.map((log, i) => (
                                    <div
                                        key={i}
                                        className={`text-[10px] font-mono leading-relaxed break-all ${
                                            log.level === "error"
                                                ? "text-red-400"
                                                : log.level === "warning"
                                                  ? "text-yellow-400"
                                                  : "text-muted-foreground"
                                        }`}
                                    >
                                        <span className="text-muted-foreground/50">
                                            {new Date(log.timestamp).toLocaleTimeString()}{" "}
                                        </span>
                                        {log.message}
                                    </div>
                                ))}
                                {errorLogs.length === 0 && !actionError && (
                                    <div className="text-[10px] text-muted-foreground italic">No recent logs available</div>
                                )}
                            </div>
                        )}
                    </div>
                )}
            </div>

            {/* Actions */}
            <div className="mt-3">
                {isRunning ? (
                    <Button
                        variant="outline"
                        size="sm"
                        className={`h-7 w-full text-[10px] uppercase font-bold tracking-wider transition-all rounded-lg ${
                            confirmingStop
                                ? "bg-red-500/10 text-red-500 border-red-500/50"
                                : "text-red-500/80 border-red-500/20 hover:bg-red-500/10 hover:text-red-500 hover:border-red-500/50"
                        }`}
                        onClick={handleStopClick}
                        disabled={isActionLoading}
                    >
                        <HiOutlineStop className="w-3 h-3 mr-1.5 shrink-0" />
                        {confirmingStop ? "Confirm?" : "Stop"}
                    </Button>
                ) : (
                    <Button
                        variant="default"
                        size="sm"
                        className="h-7 w-full text-[10px] uppercase font-bold tracking-wider bg-emerald-600 hover:bg-emerald-700 text-white shadow-lg shadow-emerald-500/20 rounded-lg"
                        onClick={() => handleAction("start")}
                        disabled={isActionLoading}
                    >
                        <HiOutlinePlay className="w-3 h-3 mr-1.5 shrink-0" />
                        Start Process
                    </Button>
                )}
            </div>
        </Card>
    )
}
