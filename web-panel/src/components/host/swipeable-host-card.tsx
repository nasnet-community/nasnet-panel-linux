import React, { useRef, useCallback, useEffect } from "react"
import { motion, useMotionValue, useTransform, animate } from "framer-motion"
import { HiOutlinePencil, HiOutlineDuplicate, HiOutlineTrash } from "react-icons/hi"
import { cn } from "@/lib/utils"
import type { HostWithRelations } from "@/lib/types"
import { ServerHostCard } from "./server-host-card"
import { InfoHostCard } from "./info-host-card"

interface SwipeableHostCardProps {
    host: HostWithRelations
    isSelected: boolean
    isMultiSelectMode: boolean
    shouldClose: boolean
    onOpen: (id: number) => void
    onToggleSelect: () => void
    onLongPress: (id: number) => void
    onEdit: () => void
    onDelete: () => void
    onDuplicate: () => void
    onToggle: () => void
    onTagClick: (tag: string) => void
    onEnterMultiSelect: () => void
}

const ACTION_WIDTH = 64
const TOTAL_ACTION_WIDTH = ACTION_WIDTH * 3
const SNAP_THRESHOLD = -80

function isInfoHost(host: HostWithRelations): boolean {
    return !!host.plan_id && !host.inbound_id
}

export const SwipeableHostCard = React.memo(function SwipeableHostCard({
    host,
    isSelected,
    isMultiSelectMode,
    shouldClose,
    onOpen,
    onToggleSelect,
    onLongPress,
    onEdit,
    onDelete,
    onDuplicate,
    onToggle,
    onTagClick,
    onEnterMultiSelect,
}: SwipeableHostCardProps) {
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
            onLongPress(host.id)
        }, 500)
    }, [isMultiSelectMode, onLongPress, host.id])

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
            onOpen(host.id)
        } else {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
        if (dragEndTimeoutRef.current) clearTimeout(dragEndTimeoutRef.current)
        dragEndTimeoutRef.current = setTimeout(() => { isDragging.current = false }, 50)
    }, [x, onOpen, host.id])

    const handleAction = useCallback((action: () => void) => {
        action()
        animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
        isOpen.current = false
    }, [x])

    const infoHost = isInfoHost(host)

    // Prevent card's onClick from firing after drag
    const handleCardClick = useCallback((e: React.MouseEvent) => {
        if (isDragging.current) {
            e.stopPropagation()
            return
        }
        if (isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
            e.stopPropagation()
        }
    }, [x])

    return (
        <div className="relative overflow-hidden rounded-lg">
            {/* Action buttons behind */}
            <motion.div
                className="absolute inset-y-0 right-0 flex"
                style={{ opacity: actionOpacity }}
            >
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-blue-600 text-white active:bg-blue-700"
                    onClick={() => handleAction(onEdit)}
                >
                    <HiOutlinePencil className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Edit</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-muted-foreground text-white active:bg-muted-foreground/80"
                    onClick={() => handleAction(onDuplicate)}
                >
                    <HiOutlineDuplicate className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Duplicate</span>
                </button>
                <button
                    className="w-16 flex flex-col items-center justify-center gap-1 bg-red-600 text-white active:bg-red-700"
                    onClick={() => handleAction(onDelete)}
                >
                    <HiOutlineTrash className="w-5 h-5" />
                    <span className="text-[10px] font-medium">Delete</span>
                </button>
            </motion.div>

            {/* Draggable card */}
            <motion.div
                className="relative bg-background"
                style={{ x }}
                drag={isMultiSelectMode ? false : "x"}
                dragConstraints={{ left: -TOTAL_ACTION_WIDTH, right: 0 }}
                dragElastic={{ left: 0.1, right: 0.5 }}
                onDragStart={handleDragStart}
                onDragEnd={handleDragEnd}
                onClick={handleCardClick}
                onTouchStart={handleTouchStart}
                onTouchMove={handleTouchMove}
                onTouchEnd={handleTouchEnd}
            >
                {infoHost ? (
                    <InfoHostCard
                        host={host}
                        isSelected={isSelected}
                        isMultiSelectMode={isMultiSelectMode}
                        onToggleSelect={onToggleSelect}
                        onEdit={onEdit}
                        onDelete={onDelete}
                        onDuplicate={onDuplicate}
                        onToggle={onToggle}
                        onCtrlClick={onEnterMultiSelect}
                    />
                ) : (
                    <ServerHostCard
                        host={host}
                        isSelected={isSelected}
                        isMultiSelectMode={isMultiSelectMode}
                        onToggleSelect={onToggleSelect}
                        onEdit={onEdit}
                        onDelete={onDelete}
                        onDuplicate={onDuplicate}
                        onToggle={onToggle}
                        onTagClick={onTagClick}
                        onCtrlClick={onEnterMultiSelect}
                    />
                )}
            </motion.div>
        </div>
    )
})
