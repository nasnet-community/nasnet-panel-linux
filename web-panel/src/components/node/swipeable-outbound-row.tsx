import React, { useRef, useCallback, useEffect } from "react"
import { motion, useMotionValue, useTransform, animate } from "framer-motion"
import { Badge } from "@/components/ui/badge"
import { cn, formatBytes, countryFlag, formatRelativeTime } from "@/lib/utils"
import type { Outbound, OutboundTestEntry } from "@/lib/types"
import { HiOutlinePencil, HiOutlineStatusOnline, HiOutlineCheckCircle, HiOutlineXCircle, HiOutlineUpload, HiOutlineDownload, HiOutlineBan } from "react-icons/hi"
import { Trash2, Power } from "lucide-react"
import { ProtocolBadge } from "./protocol-badge"

interface SwipeableOutboundRowProps {
    outbound: Outbound
    shouldClose: boolean
    onOpen: (id: number) => void
    onEdit: (outbound: Outbound) => void
    onDelete: (outbound: Outbound) => void
    onToggleDisabled: (outbound: Outbound) => void
    onTest?: (outbound: Outbound) => void
    isTesting?: boolean
    /** Mirrors the desktop row: nothing to probe, or a Test All is mid-flight. */
    testDisabled?: boolean
    testEntry?: OutboundTestEntry | null
    onViewTestResult?: (outbound: Outbound) => void
}

const ACTION_WIDTH = 64
const TOTAL_ACTION_WIDTH = ACTION_WIDTH * 4 // 256px for 4 buttons
const SNAP_THRESHOLD = -60

export const SwipeableOutboundRow = React.memo(function SwipeableOutboundRow({
    outbound,
    shouldClose,
    onOpen,
    onEdit,
    onDelete,
    onToggleDisabled,
    onTest,
    isTesting,
    testDisabled,
    testEntry,
    onViewTestResult,
}: SwipeableOutboundRowProps) {
    const x = useMotionValue(0)
    const isDragging = useRef(false)
    const isOpen = useRef(false)

    useEffect(() => {
        if (shouldClose && isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
    }, [shouldClose, x])

    const actionOpacity = useTransform(x, [-TOTAL_ACTION_WIDTH, -30, 0], [1, 0.5, 0])

    const handleDragStart = useCallback(() => {
        isDragging.current = true
    }, [])

    const handleDragEnd = useCallback((_: unknown, info: { offset: { x: number } }) => {
        const shouldOpen = info.offset.x < SNAP_THRESHOLD
        if (shouldOpen) {
            animate(x, -TOTAL_ACTION_WIDTH, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = true
            onOpen(outbound.id)
        } else {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
        setTimeout(() => { isDragging.current = false }, 50)
    }, [x, onOpen, outbound.id])

    const handleRowClick = useCallback(() => {
        if (isDragging.current) return

        if (isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
            return
        }

        onEdit(outbound)
    }, [onEdit, outbound, x])

    const handleAction = useCallback((action: () => void) => {
        action()
        animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
        isOpen.current = false
    }, [x])

    // Build destination display
    let destination = "—"
    if (outbound.address && outbound.port) {
        destination = `${outbound.address}:${outbound.port}`
    } else if (outbound.protocol === "freedom") {
        destination = "Direct"
    } else if (outbound.protocol === "blackhole") {
        destination = "Blocked"
    }

    const isSpecial = outbound.protocol === "freedom" || outbound.protocol === "blackhole"
    const hasTraffic = (outbound.uplink && outbound.uplink > 0) || (outbound.downlink && outbound.downlink > 0)

    return (
        <div className="relative overflow-hidden">
            {/* Action buttons behind the row */}
            <motion.div
                className="absolute inset-y-0 right-0 flex"
                style={{ opacity: actionOpacity }}
            >
                <button
                    className={cn(
                        "w-16 flex flex-col items-center justify-center gap-1 text-white",
                        outbound.is_disabled ? "bg-green-600 active:bg-green-700" : "bg-orange-600 active:bg-orange-700"
                    )}
                    onClick={() => handleAction(() => onToggleDisabled(outbound))}
                >
                    {outbound.is_disabled ? <Power className="w-5 h-5" /> : <HiOutlineBan className="w-5 h-5" />}
                    <span className="text-[10px] font-medium">{outbound.is_disabled ? "Enable" : "Disable"}</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-emerald-600 text-white active:bg-emerald-700 disabled:opacity-40"
                    onClick={() => onTest && handleAction(() => onTest(outbound))}
                    disabled={isTesting || testDisabled}
                >
                    <HiOutlineStatusOnline className={cn("w-5 h-5", isTesting && "animate-pulse")} />
                    <span className="text-[10px] font-medium">{isTesting ? "..." : "Test"}</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-blue-600 text-white active:bg-blue-700"
                    onClick={() => handleAction(() => onEdit(outbound))}
                >
                    <HiOutlinePencil className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Edit</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-red-600 text-white active:bg-red-700"
                    onClick={() => handleAction(() => onDelete(outbound))}
                >
                    <Trash2 className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Delete</span>
                </button>
            </motion.div>

            {/* Draggable row content */}
            <motion.div
                className={cn(
                    "relative bg-background px-3 py-2.5 cursor-pointer active:bg-muted/30",
                    outbound.is_disabled && "opacity-50"
                )}
                style={{ x }}
                drag="x"
                dragConstraints={{ left: -TOTAL_ACTION_WIDTH, right: 0 }}
                dragElastic={{ left: 0.1, right: 0.5 }}
                onDragStart={handleDragStart}
                onDragEnd={handleDragEnd}
                onClick={handleRowClick}
            >
                <div className="flex items-start justify-between gap-2">
                    <div className="flex-1 min-w-0">
                        {/* Line 1: tag + ProtocolBadge */}
                        <div className="flex items-center gap-2">
                            <span className="font-mono font-semibold text-sm">{outbound.tag}</span>
                            <ProtocolBadge protocol={outbound.protocol} />
                        </div>

                        {/* Line 2: Destination */}
                        <p className={cn(
                            "text-xs font-mono mt-1",
                            isSpecial ? "text-primary font-medium" : "text-muted-foreground"
                        )}>
                            {destination}
                        </p>

                        {/* Line 3: Network + Security badges + Traffic (if not freedom/blackhole) */}
                        <div className="flex items-center gap-1 mt-1.5 flex-wrap">
                            {!isSpecial && (
                                <>
                                    <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-5">
                                        {outbound.network || "tcp"}
                                    </Badge>
                                    <Badge
                                        variant={outbound.security === "reality" ? "success" : outbound.security === "tls" ? "default" : "secondary"}
                                        className="text-[10px] px-1.5 py-0 h-5"
                                    >
                                        {outbound.security || "none"}
                                    </Badge>
                                </>
                            )}
                            {hasTraffic && (
                                <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-5 font-mono bg-emerald-500/5 border-emerald-500/20 text-emerald-400">
                                    <HiOutlineUpload className="w-2.5 h-2.5 mr-0.5" />
                                    {formatBytes(outbound.uplink || 0)}
                                    <span className="mx-0.5 opacity-50">/</span>
                                    {formatBytes(outbound.downlink || 0)}
                                    <HiOutlineDownload className="w-2.5 h-2.5 ml-0.5" />
                                </Badge>
                            )}
                        </div>
                    </div>

                    {/* Test result indicator (right side) */}
                    {testEntry && (
                        <button
                            onClick={(e) => {
                                e.stopPropagation()
                                onViewTestResult?.(outbound)
                            }}
                            className="flex flex-col items-center gap-0.5 pt-1 shrink-0"
                        >
                            <span className="inline-flex items-center gap-1">
                                {testEntry.result.status === "not_applicable" ? (
                                    <span className="text-[11px] font-medium text-muted-foreground/70">N/A</span>
                                ) : testEntry.result.success ? (
                                    <HiOutlineCheckCircle className="w-5 h-5 text-emerald-500" />
                                ) : (
                                    <HiOutlineXCircle className="w-5 h-5 text-red-500" />
                                )}
                                {testEntry.result.country && (
                                    <span className="text-sm leading-none">{countryFlag(testEntry.result.country)}</span>
                                )}
                            </span>
                            {testEntry.result.latency_ms > 0 && (
                                <span className={cn(
                                    "text-[10px] font-mono font-medium",
                                    testEntry.result.latency_ms < 300 ? "text-emerald-500" :
                                    testEntry.result.latency_ms < 800 ? "text-amber-500" : "text-red-500"
                                )}>
                                    {testEntry.result.latency_ms}ms
                                </span>
                            )}
                            <span className="text-[10px] text-muted-foreground/60">
                                {formatRelativeTime(testEntry.tested_at)}
                            </span>
                        </button>
                    )}
                </div>
            </motion.div>
        </div>
    )
})
