import React, { useRef, useCallback, useEffect } from "react"
import { motion, useMotionValue, useTransform, animate } from "framer-motion"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { cn, formatCompact } from "@/lib/utils"
import type { Account } from "@/lib/admin-api"
import { HiOutlineClipboardCopy, HiOutlineRefresh } from "react-icons/hi"
import { Trash2 } from "lucide-react"

interface SwipeableAccountRowProps {
    account: Account
    isSelected: boolean
    isMultiSelectMode: boolean
    isAnyMutationPending: boolean
    shouldClose: boolean
    onOpen: (id: number) => void
    onTap: (account: Account) => void
    onToggleSelect: (id: number) => void
    onLongPress: (id: number) => void
    onCopyLink: (account: Account) => void
    onSync: (account: Account) => void
    onDelete: (account: Account) => void
}

const ACTION_WIDTH = 64
const TOTAL_ACTION_WIDTH = ACTION_WIDTH * 3
const SNAP_THRESHOLD = -80

export const SwipeableAccountRow = React.memo(function SwipeableAccountRow({
    account: acc,
    isSelected,
    isMultiSelectMode,
    isAnyMutationPending,
    shouldClose,
    onOpen,
    onTap,
    onToggleSelect,
    onLongPress,
    onCopyLink,
    onSync,
    onDelete,
}: SwipeableAccountRowProps) {
    const x = useMotionValue(0)
    const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
    const dragEndTimeoutRef = useRef<NodeJS.Timeout | null>(null)
    const touchStart = useRef<{ x: number; y: number } | null>(null)
    const isDragging = useRef(false)
    const isOpen = useRef(false)

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
            onLongPress(acc.id)
        }, 500)
    }, [isMultiSelectMode, onLongPress, acc.id])

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
            onOpen(acc.id)
        } else {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
        if (dragEndTimeoutRef.current) clearTimeout(dragEndTimeoutRef.current)
        dragEndTimeoutRef.current = setTimeout(() => { isDragging.current = false }, 50)
    }, [x, onOpen, acc.id])

    const handleRowClick = useCallback((e: React.MouseEvent) => {
        if (isDragging.current) return
        const target = e.target as HTMLElement
        if (target.closest('[role="checkbox"]')) return

        if (isMultiSelectMode) {
            onToggleSelect(acc.id)
            return
        }

        if (isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
            return
        }

        onTap(acc)
    }, [isMultiSelectMode, onToggleSelect, onTap, acc, x])

    const handleAction = useCallback((action: () => void) => {
        action()
        animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
        isOpen.current = false
    }, [x])

    // Shared data logic
    const isShared = !!acc.subscription
    const totalLimit = isShared
        ? (acc.subscription?.custom_data_limit ?? acc.subscription?.data_limit ?? 0)
        : acc.data_limit
    const totalUsed = isShared
        ? (acc.subscription?.data_used ?? 0)
        : acc.data_used
    const usagePercent = totalLimit > 0 ? Math.min((totalUsed / totalLimit) * 100, 100) : 0
    const isOnline = acc.last_activity_at && (new Date().getTime() - new Date(acc.last_activity_at).getTime()) < 10 * 1000

    const statusVariant = acc.status === "active" ? "success" : acc.status === "disabled" ? "danger" : "secondary"

    return (
        <div className="relative overflow-hidden">
            {/* Action buttons behind the row */}
            <motion.div
                className="absolute inset-y-0 right-0 flex"
                style={{ opacity: actionOpacity }}
            >
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-blue-600 text-white active:bg-blue-700"
                    onClick={() => handleAction(() => onCopyLink(acc))}
                >
                    <HiOutlineClipboardCopy className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Copy</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-amber-600 text-white active:bg-amber-700"
                    onClick={() => handleAction(() => onSync(acc))}
                    disabled={isAnyMutationPending}
                >
                    <HiOutlineRefresh className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Sync</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-red-600 text-white active:bg-red-700"
                    onClick={() => handleAction(() => onDelete(acc))}
                >
                    <Trash2 className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Delete</span>
                </button>
            </motion.div>

            {/* Draggable row content */}
            <motion.div
                className={cn(
                    "relative bg-background px-4 py-3 cursor-pointer active:bg-muted/30",
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
                {/* Line 1: Online dot / Checkbox + Email + Status badge */}
                <div className="flex items-center gap-3">
                    <div className="shrink-0 w-6 flex items-center justify-center">
                        {isMultiSelectMode ? (
                            <Checkbox
                                checked={isSelected}
                                onCheckedChange={() => onToggleSelect(acc.id)}
                                role="checkbox"
                            />
                        ) : (
                            <span className="relative flex h-2.5 w-2.5">
                                {isOnline && <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />}
                                <span className={cn(
                                    "relative inline-flex rounded-full h-2.5 w-2.5",
                                    isOnline ? "bg-green-500" : "bg-slate-400/50"
                                )} />
                            </span>
                        )}
                    </div>

                    <div className="flex-1 min-w-0">
                        <span className="text-sm font-medium truncate block">{acc.email}</span>
                    </div>

                    <div className="shrink-0">
                        <Badge variant={statusVariant} className="capitalize text-xs">
                            {acc.status}
                        </Badge>
                    </div>
                </div>

                {/* Line 2: Usage bar + compact text */}
                <div className="flex items-center gap-3 mt-1.5 ml-[36px]">
                    <div className="flex-1 flex items-center gap-2">
                        <div className="flex-1 h-1.5 bg-muted rounded-full overflow-hidden">
                            <div
                                className={cn(
                                    "h-full rounded-full transition-all",
                                    acc.status === "disabled" ? "bg-muted-foreground/30" :
                                    usagePercent > 90 ? "bg-red-500" :
                                    usagePercent > 70 ? "bg-amber-500" : "bg-emerald-500"
                                )}
                                style={{ width: totalLimit === 0 ? "5%" : `${usagePercent}%` }}
                            />
                        </div>
                        <span className="text-xs text-muted-foreground whitespace-nowrap shrink-0">
                            {isShared ? (
                                `${formatCompact(acc.data_used)} (${formatCompact(totalUsed)}/${totalLimit > 0 ? formatCompact(totalLimit) : "∞"})`
                            ) : totalLimit === 0 ? (
                                `${formatCompact(acc.data_used)} / ∞`
                            ) : (
                                `${formatCompact(acc.data_used)}/${formatCompact(totalLimit)}`
                            )}
                        </span>
                    </div>
                </div>
            </motion.div>
        </div>
    )
})
