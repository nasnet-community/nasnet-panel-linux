import React, { useRef, useCallback, useEffect } from "react"
import { motion, useMotionValue, useTransform, animate } from "framer-motion"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { cn, formatBytes } from "@/lib/utils"
import type { Inbound } from "@/lib/types"
import { HiOutlineCog, HiOutlineClipboardCopy, HiOutlineUsers, HiOutlineStatusOnline, HiOutlineBan, HiChevronRight, HiChevronDown } from "react-icons/hi"
import { Trash2, Power, ArrowRightLeft } from "lucide-react"
import { protocolColors } from "./protocol-badge"

interface SwipeableInboundRowProps {
    inbound: Inbound
    accountCount: number
    onlineCount: number
    totalTraffic: number
    isSelected: boolean
    isMultiSelectMode: boolean
    shouldClose: boolean
    isExpanded: boolean
    onOpen: (id: number) => void
    onTap: (inbound: Inbound) => void
    onToggleSelect: (id: number) => void
    onLongPress: (id: number) => void
    onEdit: (inbound: Inbound) => void
    onCopy: (inbound: Inbound) => void
    onToggleDisabled: (inbound: Inbound) => void
    onDelete: (inbound: Inbound) => void
    onMigrate: (inbound: Inbound) => void
    expandedContent?: React.ReactNode
}

const ACTION_WIDTH = 64
const TOTAL_ACTION_WIDTH = ACTION_WIDTH * 5 // 320px for 5 buttons
const SNAP_THRESHOLD = -80

export const SwipeableInboundRow = React.memo(function SwipeableInboundRow({
    inbound,
    accountCount,
    onlineCount,
    totalTraffic,
    isSelected,
    isMultiSelectMode,
    shouldClose,
    isExpanded,
    onOpen,
    onTap,
    onToggleSelect,
    onLongPress,
    onEdit,
    onCopy,
    onToggleDisabled,
    onDelete,
    onMigrate,
    expandedContent,
}: SwipeableInboundRowProps) {
    const x = useMotionValue(0)
    const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
    const dragEndTimeoutRef = useRef<NodeJS.Timeout | null>(null)
    const touchStart = useRef<{ x: number; y: number } | null>(null)
    const isDragging = useRef(false)
    const isOpen = useRef(false)

    // Snap back when another row opens
    useEffect(() => {
        if (shouldClose && isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
    }, [shouldClose, x])

    useEffect(() => {
        return () => {
            if (dragEndTimeoutRef.current) clearTimeout(dragEndTimeoutRef.current)
        }
    }, [])

    // Action button opacity and interactivity based on drag position
    const actionOpacity = useTransform(x, [-TOTAL_ACTION_WIDTH, -40, 0], [1, 0.5, 0])
    const actionPointerEvents = useTransform(x, (latest) => latest < -10 ? "auto" : "none")

    const cancelLongPress = useCallback(() => {
        if (longPressTimer.current) {
            clearTimeout(longPressTimer.current)
            longPressTimer.current = null
        }
    }, [])

    const handleTouchStart = useCallback((e: React.TouchEvent) => {
        if (isMultiSelectMode) return
        const touch = e.touches[0]
        touchStart.current = { x: touch.clientX, y: touch.clientY }
        isDragging.current = false
        longPressTimer.current = setTimeout(() => {
            longPressTimer.current = null
            onLongPress(inbound.id)
        }, 500)
    }, [isMultiSelectMode, onLongPress, inbound.id])

    const handleTouchMove = useCallback((e: React.TouchEvent) => {
        if (!touchStart.current) return
        const touch = e.touches[0]
        const dx = Math.abs(touch.clientX - touchStart.current.x)
        const dy = Math.abs(touch.clientY - touchStart.current.y)
        if (dx > 10 || dy > 10) {
            cancelLongPress()
        }
    }, [cancelLongPress])

    const handleTouchEnd = useCallback(() => {
        cancelLongPress()
        touchStart.current = null
    }, [cancelLongPress])

    const handleDragStart = useCallback(() => {
        isDragging.current = true
        cancelLongPress()
    }, [cancelLongPress])

    const handleDragEnd = useCallback((_: unknown, info: { offset: { x: number } }) => {
        const shouldOpen = info.offset.x < SNAP_THRESHOLD
        if (shouldOpen) {
            animate(x, -TOTAL_ACTION_WIDTH, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = true
            onOpen(inbound.id)
        } else {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
        if (dragEndTimeoutRef.current) clearTimeout(dragEndTimeoutRef.current)
        dragEndTimeoutRef.current = setTimeout(() => { isDragging.current = false }, 50)
    }, [x, onOpen, inbound.id])

    const handleRowClick = useCallback((e: React.MouseEvent) => {
        if (isDragging.current) return
        const target = e.target as HTMLElement
        if (target.closest('[role="checkbox"]')) return

        if (isMultiSelectMode) {
            onToggleSelect(inbound.id)
            return
        }

        if (isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
            return
        }

        onTap(inbound)
    }, [isMultiSelectMode, onToggleSelect, onTap, inbound, x])

    const handleAction = useCallback((action: () => void) => {
        action()
        animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
        isOpen.current = false
    }, [x])

    const hasOnline = onlineCount > 0

    return (
        <div className="relative overflow-hidden">
            {/* Action buttons behind the row */}
            <motion.div
                className="absolute inset-y-0 right-0 flex"
                style={{ opacity: actionOpacity, pointerEvents: actionPointerEvents }}
            >
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-blue-600 text-white active:bg-blue-700"
                    onClick={() => handleAction(() => onEdit(inbound))}
                >
                    <HiOutlineCog className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Edit</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-amber-600 text-white active:bg-amber-700"
                    onClick={() => handleAction(() => onCopy(inbound))}
                >
                    <HiOutlineClipboardCopy className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Copy</span>
                </button>
                <button
                    className={cn(
                        "w-16 flex flex-col items-center justify-center gap-1 text-white",
                        inbound.is_disabled ? "bg-green-600 active:bg-green-700" : "bg-orange-600 active:bg-orange-700"
                    )}
                    onClick={() => handleAction(() => onToggleDisabled(inbound))}
                >
                    {inbound.is_disabled ? <Power className="w-5 h-5" /> : <HiOutlineBan className="w-5 h-5" />}
                    <span className="text-[10px] font-medium">{inbound.is_disabled ? "Enable" : "Disable"}</span>
                </button>
                <button
                    className="w-16 h-full flex flex-col items-center justify-center text-white bg-blue-500"
                    onClick={() => handleAction(() => onMigrate(inbound))}
                >
                    <ArrowRightLeft className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Migrate</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-red-600 text-white active:bg-red-700"
                    onClick={() => handleAction(() => onDelete(inbound))}
                >
                    <Trash2 className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Delete</span>
                </button>
            </motion.div>

            {/* Draggable row content */}
            <motion.div
                className={cn(
                    "relative bg-background px-3 py-2.5 cursor-pointer active:bg-muted/30",
                    isSelected && "bg-primary/5"
                )}
                style={{ x }}
                drag={isMultiSelectMode ? false : "x"}
                dragConstraints={{ left: -TOTAL_ACTION_WIDTH, right: 0 }}
                dragElastic={{ left: 0.1, right: 0.5 }}
                onDragStart={handleDragStart}
                onDragEnd={handleDragEnd}
                onClick={handleRowClick}
                onTouchStart={handleTouchStart}
                onTouchMove={handleTouchMove}
                onTouchEnd={handleTouchEnd}
            >
                {/* Line 1: Checkbox/status + tag:port + chevron */}
                <div className="flex items-center gap-2.5">
                    <div className="shrink-0 w-6 flex items-center justify-center">
                        {isMultiSelectMode ? (
                            <Checkbox
                                checked={isSelected}
                                onCheckedChange={() => onToggleSelect(inbound.id)}
                                role="checkbox"
                            />
                        ) : (
                            <span className="relative flex h-2.5 w-2.5">
                                {hasOnline && (
                                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />
                                )}
                                <span className={cn(
                                    "relative inline-flex rounded-full h-2.5 w-2.5",
                                    hasOnline ? "bg-green-500" : accountCount > 0 ? "bg-amber-500/60" : "bg-zinc-500/40"
                                )} />
                            </span>
                        )}
                    </div>

                    <div className="flex-1 min-w-0">
                        <span className="font-mono font-semibold text-sm">{inbound.tag}</span>
                        <span className="font-mono text-primary font-bold text-sm">:{inbound.port}</span>
                    </div>

                    <div className="shrink-0">
                        {isExpanded ? (
                            <HiChevronDown className="w-4 h-4 text-muted-foreground" />
                        ) : (
                            <HiChevronRight className="w-4 h-4 text-muted-foreground" />
                        )}
                    </div>
                </div>

                {/* Line 2: Protocol + Network + Security badges */}
                <div className="flex items-center gap-1 mt-1.5 ml-[34px]">
                    <Badge
                        variant="outline"
                        className={cn(
                            "font-mono text-[10px] px-1.5 py-0 h-5",
                            protocolColors[inbound.protocol.toLowerCase()] || ""
                        )}
                    >
                        {inbound.protocol.toUpperCase()}
                    </Badge>
                    <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-zinc-500/10 border-zinc-500/20">
                        {(inbound.network || "tcp").toUpperCase()}
                    </Badge>
                    <Badge
                        variant="outline"
                        className={cn(
                            "text-[10px] px-1.5 py-0 h-5",
                            inbound.security === "reality" && "bg-gradient-to-r from-cyan-500/20 to-purple-500/20 text-cyan-400 border-cyan-500/30",
                            inbound.security === "tls" && "bg-emerald-500/15 text-emerald-400 border-emerald-500/30",
                            (!inbound.security || inbound.security === "none") && "bg-zinc-500/15 text-zinc-400 border-zinc-500/30"
                        )}
                    >
                        {(inbound.security || "none").toUpperCase()}
                    </Badge>
                </div>

                {/* Line 3: Account stats */}
                <div className="flex items-center gap-3 mt-1.5 ml-[34px] text-xs text-muted-foreground">
                    <div className="flex items-center gap-1">
                        <HiOutlineUsers className="w-3.5 h-3.5" />
                        <span className="font-mono">{accountCount}</span>
                    </div>
                    {onlineCount > 0 && (
                        <div className="flex items-center gap-1 text-green-500">
                            <HiOutlineStatusOnline className="w-3.5 h-3.5" />
                            <span className="font-mono">{onlineCount}</span>
                        </div>
                    )}
                    {totalTraffic > 0 && (
                        <span className="font-mono text-violet-400">{formatBytes(totalTraffic)}</span>
                    )}
                </div>
            </motion.div>

            {/* Expanded content - z-10 to stay above swipe action buttons */}
            {isExpanded && expandedContent && (
                <div className="relative z-10 border-t border-white/5 bg-muted/5 p-2 md:p-4">
                    {expandedContent}
                </div>
            )}
        </div>
    )
})
