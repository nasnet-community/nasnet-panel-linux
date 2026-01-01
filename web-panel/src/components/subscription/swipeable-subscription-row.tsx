import React, { useRef, useCallback, useEffect } from "react"
import { motion, useMotionValue, useTransform, animate } from "framer-motion"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import { cn, formatCompact, getExpiryInfo } from "@/lib/utils"
import type { Subscription } from "@/lib/types"
import { HiOutlineClipboardCopy, HiOutlineGlobeAlt, HiOutlinePause, HiOutlinePlay } from "react-icons/hi"
import { Trash2 } from "lucide-react"
import { getAccentSeverity, getUsageBarColor, getUsageTextColor } from "@/lib/constants/usage-thresholds"
import { subscriptionLabel } from "@/lib/subscription-label"

interface SwipeableSubscriptionRowProps {
    subscription: Subscription
    maxUsage?: number
    connectedIPs?: string[]
    isSelected: boolean
    isMultiSelectMode: boolean
    isAnyMutationPending: boolean
    shouldClose: boolean
    onOpen: (id: number) => void
    onTap: (sub: Subscription) => void
    onToggle: (sub: Subscription, checked: boolean) => void
    onToggleSelect: (id: number) => void
    onLongPress: (id: number) => void
    onCopyLink: (sub: Subscription) => void
    onPauseResume: (sub: Subscription) => void
    onDelete: (sub: Subscription) => void
}

const ACTION_WIDTH = 64
const TOTAL_ACTION_WIDTH = ACTION_WIDTH * 3 // 192px for 3 buttons
const SNAP_THRESHOLD = -80

export const SwipeableSubscriptionRow = React.memo(function SwipeableSubscriptionRow({
    subscription: sub,
    maxUsage = 0,
    connectedIPs = [],
    isSelected,
    isMultiSelectMode,
    isAnyMutationPending,
    shouldClose,
    onOpen,
    onTap,
    onToggle,
    onToggleSelect,
    onLongPress,
    onCopyLink,
    onPauseResume,
    onDelete,
}: SwipeableSubscriptionRowProps) {
    const x = useMotionValue(0)
    const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
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

    // Action button opacity based on drag position
    const actionOpacity = useTransform(x, [-TOTAL_ACTION_WIDTH, -40, 0], [1, 0.5, 0])

    const canToggle = sub.status === "active" || sub.status === "paused"

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
            onLongPress(sub.id)
        }, 500)
    }, [isMultiSelectMode, onLongPress, sub.id])

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
            onOpen(sub.id)
        } else {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
        setTimeout(() => { isDragging.current = false }, 50)
    }, [x, onOpen, sub.id])

    const handleRowClick = useCallback((e: React.MouseEvent) => {
        if (isDragging.current) return
        const target = e.target as HTMLElement
        if (target.closest('[role="switch"]') || target.closest('[role="checkbox"]')) return

        if (isMultiSelectMode) {
            onToggleSelect(sub.id)
            return
        }

        // If row is swiped open, close it instead of opening details
        if (isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
            return
        }

        onTap(sub)
    }, [isMultiSelectMode, onToggleSelect, onTap, sub, x])

    const handleAction = useCallback((action: () => void) => {
        action()
        animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
        isOpen.current = false
    }, [x])

    const dataLimit = sub.custom_data_limit ?? sub.data_limit
    const isUnlimited = dataLimit === 0
    const usagePercent = dataLimit > 0 ? Math.min((sub.data_used / dataLimit) * 100, 100) : 0
    const relativePercent = isUnlimited && maxUsage > 0 ? Math.max(2, (sub.data_used / maxUsage) * 100) : 0
    const isTerminated = sub.status === "cancelled" || sub.status === "expired"
    const expiryDate = sub.is_end_date_custom ? sub.custom_end_date : sub.end_date
    const accentColor = getAccentSeverity(usagePercent, sub.status, expiryDate)
    const isPaused = sub.status === "paused"

    return (
        <div className="relative overflow-hidden flex">
            {/* Left accent bar */}
            <div className={cn("w-[3px] shrink-0", accentColor)} />
            <div className="flex-1 relative overflow-hidden">
            {/* Action buttons behind the row */}
            <motion.div
                className="absolute inset-y-0 right-0 flex"
                style={{ opacity: actionOpacity }}
            >
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-blue-600 text-white active:bg-blue-700"
                    onClick={() => handleAction(() => onCopyLink(sub))}
                >
                    <HiOutlineClipboardCopy className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Copy</span>
                </button>
                <button
                    className={cn(
                        "w-16 flex flex-col items-center justify-center gap-1 text-white",
                        sub.status === "paused"
                            ? "bg-emerald-600 active:bg-emerald-700"
                            : "bg-amber-600 active:bg-amber-700"
                    )}
                    onClick={() => handleAction(() => onPauseResume(sub))}
                    disabled={!canToggle || isAnyMutationPending}
                >
                    {sub.status === "paused" ? (
                        <HiOutlinePlay className="w-5 h-5" />
                    ) : (
                        <HiOutlinePause className="w-5 h-5" />
                    )}
                    <span className="text-[10px] font-medium">
                        {sub.status === "paused" ? "Resume" : "Pause"}
                    </span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-red-600 text-white active:bg-red-700"
                    onClick={() => handleAction(() => onDelete(sub))}
                >
                    <Trash2 className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Delete</span>
                </button>
            </motion.div>

            {/* Draggable row content */}
            <motion.div
                className={cn(
                    "relative bg-background px-4 py-2 cursor-pointer active:bg-muted/30",
                    isTerminated && "opacity-50",
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
                {/* Line 1: Control + Name + Status */}
                <div className="flex items-center gap-3">
                    {/* Switch or Checkbox */}
                    <div className="shrink-0 w-10 flex items-center justify-center">
                        {isMultiSelectMode ? (
                            <Checkbox
                                checked={isSelected}
                                onCheckedChange={() => onToggleSelect(sub.id)}
                                role="checkbox"
                            />
                        ) : (
                            <Switch
                                checked={sub.status === "active"}
                                onCheckedChange={(checked) => onToggle(sub, checked)}
                                disabled={!canToggle || isAnyMutationPending}
                                className="data-[state=checked]:bg-emerald-500 scale-90"
                            />
                        )}
                    </div>

                    {/* Name + Expiry */}
                    <div className="flex-1 min-w-0">
                        <span className="text-sm font-medium truncate block">
                            {subscriptionLabel(sub) || (sub.user_id === null || sub.user_id === 0
                                ? "Manual"
                                : `User #${sub.user_id}`)}
                        </span>
                    </div>

                    {/* Right side: connected IPs + expiry info */}
                    <div className="shrink-0 flex items-center gap-1.5">
                        {connectedIPs.length > 0 && (
                            <span className="inline-flex items-center gap-0.5 text-[11px] text-emerald-500">
                                <HiOutlineGlobeAlt className="w-3 h-3" />
                                {connectedIPs.length}
                            </span>
                        )}
                        {isPaused ? (
                            <span className="text-[11px] text-amber-500">Paused</span>
                        ) : isTerminated ? (
                            <span className={cn("text-[11px]", sub.status === "expired" ? "text-red-400" : "text-muted-foreground")}>
                                {sub.status === "expired" ? "Expired" : "Cancelled"}
                            </span>
                        ) : (() => {
                            const info = getExpiryInfo(expiryDate)
                            return (
                                <span className={cn(
                                    "text-[11px]",
                                    info.variant === "danger" ? "text-red-400" :
                                    info.variant === "warning" ? "text-amber-400" :
                                    "text-muted-foreground"
                                )}>
                                    {info.text === "∞" ? "No expiry" :
                                     info.text === "Expired" ? "Expired" :
                                     info.text}
                                </span>
                            )
                        })()}
                    </div>
                </div>

                {/* Line 2: Usage bar + percentage */}
                <div className="mt-1 ml-[52px]">
                    <div className="h-1 bg-muted rounded-full overflow-hidden">
                        <div
                            className={cn(
                                "h-full rounded-full transition-all",
                                isTerminated ? "bg-muted-foreground/30" :
                                    isPaused ? "bg-amber-500/40" :
                                    isUnlimited ? "bg-emerald-500" :
                                        getUsageBarColor(usagePercent)
                            )}
                            style={{ width: isUnlimited ? `${relativePercent}%` : `${usagePercent}%` }}
                        />
                    </div>
                    <div className="flex justify-between mt-0.5">
                        <span className={cn(
                            "text-[10px] font-medium",
                            isTerminated ? "text-muted-foreground" :
                                isPaused ? "text-amber-500/50" :
                                isUnlimited ? "text-emerald-500" :
                                    getUsageTextColor(usagePercent)
                        )}>
                            {isUnlimited
                                ? `${formatCompact(sub.data_used)} used`
                                : `${Math.round(usagePercent)}% used`}
                        </span>
                        <span className="text-[10px] text-muted-foreground">
                            {isTerminated ? (
                                formatCompact(sub.data_used)
                            ) : isUnlimited ? (
                                `${formatCompact(sub.data_used)} / ∞`
                            ) : (
                                `${formatCompact(sub.data_used)}/${formatCompact(dataLimit)}`
                            )}
                        </span>
                    </div>
                </div>
            </motion.div>
            </div>
        </div>
    )
})
