import React, { useRef, useCallback, useEffect } from "react"
import { motion, useMotionValue, useTransform, animate } from "framer-motion"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { cn, formatBytes } from "@/lib/utils"
import type { Outbound, OutboundTestEntry } from "@/lib/types"
import { HiOutlineCog, HiOutlineStatusOnline, HiOutlineBan, HiChevronRight, HiChevronDown } from "react-icons/hi"
import { Trash2, Power } from "lucide-react"
import { protocolColors } from "./protocol-badge"
import { OutboundTestCell } from "./network/outbound-test-cell"

interface SwipeableOutboundRowProps {
    outbound: Outbound
    route: string
    trafficBytes: number
    isDefault: boolean
    isSelected: boolean
    isMultiSelectMode: boolean
    shouldClose: boolean
    isExpanded: boolean
    testEntry: OutboundTestEntry | null
    isTesting: boolean
    canTest: boolean
    testDisabled: boolean
    onOpen: (id: number) => void
    onTap: (outbound: Outbound) => void
    onToggleSelect: (id: number) => void
    onLongPress: (id: number) => void
    onEdit: (outbound: Outbound) => void
    onTest: (outbound: Outbound) => void
    onViewTestResult: (outbound: Outbound) => void
    onToggleDisabled: (outbound: Outbound) => void
    onDelete: (outbound: Outbound) => void
    expandedContent?: React.ReactNode
}

const ACTION_WIDTH = 64
const TOTAL_ACTION_WIDTH = ACTION_WIDTH * 4 // Edit, Test, Enable/Disable, Delete
const SNAP_THRESHOLD = -80

export const SwipeableOutboundRow = React.memo(function SwipeableOutboundRow({
    outbound,
    route,
    trafficBytes,
    isDefault,
    isSelected,
    isMultiSelectMode,
    shouldClose,
    isExpanded,
    testEntry,
    isTesting,
    canTest,
    testDisabled,
    onOpen,
    onTap,
    onToggleSelect,
    onLongPress,
    onEdit,
    onTest,
    onViewTestResult,
    onToggleDisabled,
    onDelete,
    expandedContent,
}: SwipeableOutboundRowProps) {
    const x = useMotionValue(0)
    const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
    const dragEndTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
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
            onLongPress(outbound.id)
        }, 500)
    }, [isMultiSelectMode, onLongPress, outbound.id])

    const handleTouchMove = useCallback((e: React.TouchEvent) => {
        if (!touchStart.current) return
        const touch = e.touches[0]
        const dx = Math.abs(touch.clientX - touchStart.current.x)
        const dy = Math.abs(touch.clientY - touchStart.current.y)
        if (dx > 10 || dy > 10) cancelLongPress()
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
            onOpen(outbound.id)
        } else {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
        if (dragEndTimeoutRef.current) clearTimeout(dragEndTimeoutRef.current)
        dragEndTimeoutRef.current = setTimeout(() => { isDragging.current = false }, 50)
    }, [x, onOpen, outbound.id])

    const handleRowClick = useCallback((e: React.MouseEvent) => {
        if (isDragging.current) return
        const target = e.target as HTMLElement
        if (target.closest('[role="checkbox"]') || target.closest("button")) return

        if (isMultiSelectMode) {
            onToggleSelect(outbound.id)
            return
        }

        if (isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
            return
        }

        onTap(outbound)
    }, [isMultiSelectMode, onToggleSelect, onTap, outbound, x])

    const handleAction = useCallback((action: () => void) => {
        action()
        animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
        isOpen.current = false
    }, [x])

    const disabled = outbound.is_disabled

    return (
        <div className="relative overflow-hidden">
            {/* Action buttons behind the row */}
            <motion.div
                className="absolute inset-y-0 right-0 flex"
                style={{ opacity: actionOpacity, pointerEvents: actionPointerEvents }}
            >
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-blue-600 text-white active:bg-blue-700"
                    onClick={() => handleAction(() => onEdit(outbound))}
                >
                    <HiOutlineCog className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Edit</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-emerald-600 text-white active:bg-emerald-700 disabled:opacity-50"
                    onClick={() => handleAction(() => onTest(outbound))}
                    disabled={!canTest || isTesting || testDisabled}
                >
                    <HiOutlineStatusOnline className={cn("w-5 h-5", isTesting && "animate-pulse")} />
                    <span className="text-[10px] font-medium">{isTesting ? "…" : "Test"}</span>
                </button>
                <button
                    className={cn(
                        "w-16 flex flex-col items-center justify-center gap-1 text-white",
                        disabled ? "bg-green-600 active:bg-green-700" : "bg-orange-600 active:bg-orange-700",
                    )}
                    onClick={() => handleAction(() => onToggleDisabled(outbound))}
                >
                    {disabled ? <Power className="w-5 h-5" /> : <HiOutlineBan className="w-5 h-5" />}
                    <span className="text-[10px] font-medium">{disabled ? "Enable" : "Disable"}</span>
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
                    isSelected && "bg-primary/5",
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
                {/* Line 1: checkbox/status + tag + default chip + chevron */}
                <div className="flex items-center gap-2.5">
                    <div className="shrink-0 w-6 flex items-center justify-center">
                        {isMultiSelectMode ? (
                            <Checkbox
                                checked={isSelected}
                                onCheckedChange={() => onToggleSelect(outbound.id)}
                                role="checkbox"
                            />
                        ) : (
                            <span className={cn(
                                "inline-flex rounded-full h-2.5 w-2.5",
                                disabled ? "border-[1.5px] border-muted-foreground/50" : "bg-emerald-500",
                            )} />
                        )}
                    </div>

                    <div className="flex-1 min-w-0 flex items-center gap-1.5">
                        <span className={cn("font-mono font-semibold text-sm truncate", disabled && "text-muted-foreground")}>
                            {outbound.tag}
                        </span>
                        {isDefault && (
                            <Badge variant="outline" className="h-4 px-1 text-[9px] uppercase tracking-wide font-medium text-primary border-primary/30 bg-primary/5 shrink-0">
                                Default
                            </Badge>
                        )}
                        {disabled && <span className="text-[11px] text-muted-foreground shrink-0">Disabled</span>}
                    </div>

                    <div className="shrink-0">
                        {isExpanded
                            ? <HiChevronDown className="w-4 h-4 text-muted-foreground" />
                            : <HiChevronRight className="w-4 h-4 text-muted-foreground" />}
                    </div>
                </div>

                {/* Line 2: where it sends traffic */}
                <p className="mt-0.5 ml-[34px] font-mono text-[11px] text-muted-foreground truncate" title={route}>
                    {route}
                </p>

                {/* Line 3: protocol, traffic, test */}
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 mt-1.5 ml-[34px] font-mono text-[11px] text-muted-foreground tabular-nums">
                    <Badge
                        variant="outline"
                        className={cn(
                            "font-mono text-[10px] px-1.5 py-0 h-5",
                            protocolColors[outbound.protocol.toLowerCase()] || "",
                            disabled && "opacity-60",
                        )}
                    >
                        {outbound.protocol.toUpperCase()}
                    </Badge>
                    {trafficBytes > 0 && <span>{formatBytes(trafficBytes)}</span>}
                    <OutboundTestCell
                        entry={testEntry}
                        isTesting={isTesting}
                        canTest={canTest}
                        disabled={testDisabled}
                        protocol={outbound.protocol}
                        onTest={() => onTest(outbound)}
                        onView={() => onViewTestResult(outbound)}
                        align="left"
                    />
                </div>
            </motion.div>

            {/* Expanded content — z-10 to stay above the swipe action buttons */}
            {isExpanded && expandedContent && (
                <div className="relative z-10 border-t border-white/5 bg-muted/5 p-2 md:p-4">
                    {expandedContent}
                </div>
            )}
        </div>
    )
})
