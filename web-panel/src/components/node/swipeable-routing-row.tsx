import React, { useRef, useCallback, useEffect } from "react"
import { motion, useMotionValue, useTransform, animate } from "framer-motion"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import type { RoutingRule } from "@/lib/types"
import { HiOutlinePencil, HiOutlineChevronUp, HiOutlineChevronDown } from "react-icons/hi"
import { Trash2 } from "lucide-react"

interface SwipeableRoutingRowProps {
    rule: RoutingRule & { _unsaved?: boolean }
    shouldClose: boolean
    onOpen: (id: number) => void
    onEdit: (rule: RoutingRule) => void
    onDelete: (rule: RoutingRule) => void
    onToggle?: (rule: RoutingRule) => void
    onMoveUp?: () => void
    onMoveDown?: () => void
    isFirst?: boolean
    isLast?: boolean
}

const ACTION_WIDTH = 64
const TOTAL_ACTION_WIDTH = ACTION_WIDTH * 2 // 128px for 2 buttons
const SNAP_THRESHOLD = -60

export const SwipeableRoutingRow = React.memo(function SwipeableRoutingRow({
    rule,
    shouldClose,
    onOpen,
    onEdit,
    onDelete,
    onToggle,
    onMoveUp,
    onMoveDown,
    isFirst,
    isLast,
}: SwipeableRoutingRowProps) {
    const x = useMotionValue(0)
    const isDragging = useRef(false)
    const isOpen = useRef(false)
    const isUnsaved = rule._unsaved

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
            onOpen(rule.id)
        } else {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
        }
        setTimeout(() => { isDragging.current = false }, 50)
    }, [x, onOpen, rule.id])

    const handleRowClick = useCallback(() => {
        if (isDragging.current) return

        if (isOpen.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpen.current = false
            return
        }

        if (!isUnsaved) onEdit(rule)
    }, [onEdit, rule, x, isUnsaved])

    const handleAction = useCallback((action: () => void) => {
        action()
        animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
        isOpen.current = false
    }, [x])

    return (
        <div className="relative overflow-hidden">
            {/* Action buttons behind the row (only for saved rules) */}
            {!isUnsaved && (
                <motion.div
                    className="absolute inset-y-0 right-0 flex"
                    style={{ opacity: actionOpacity }}
                >
                    <button
                        className="w-16 flex flex-col items-center justify-center gap-1 bg-blue-600 text-white active:bg-blue-700"
                        onClick={() => handleAction(() => onEdit(rule))}
                    >
                        <HiOutlinePencil className="w-5 h-5" />
                        <span className="text-[10px] font-medium">Edit</span>
                    </button>
                    <button
                        className="w-16 flex flex-col items-center justify-center gap-1 bg-red-600 text-white active:bg-red-700"
                        onClick={() => handleAction(() => onDelete(rule))}
                    >
                        <Trash2 className="w-5 h-5" />
                        <span className="text-[10px] font-medium">Delete</span>
                    </button>
                </motion.div>
            )}

            {/* Draggable row content */}
            <motion.div
                className={cn("relative bg-background px-3 py-2.5 cursor-pointer active:bg-muted/30", isUnsaved && "bg-blue-500/[0.03]")}
                style={{ x }}
                drag={isUnsaved ? false : "x"}
                dragConstraints={{ left: -TOTAL_ACTION_WIDTH, right: 0 }}
                dragElastic={{ left: 0.1, right: 0.5 }}
                onDragStart={handleDragStart}
                onDragEnd={handleDragEnd}
                onClick={handleRowClick}
            >
                <div className="flex gap-2">
                    {/* Move up/down buttons */}
                    {!isUnsaved && (
                        <div className="flex flex-col items-center justify-center gap-0.5 -ml-1">
                            <button
                                className="p-0.5 rounded text-muted-foreground hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                                disabled={isFirst}
                                onClick={(e) => { e.stopPropagation(); onMoveUp?.() }}
                            >
                                <HiOutlineChevronUp className="w-3.5 h-3.5" />
                            </button>
                            <button
                                className="p-0.5 rounded text-muted-foreground hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                                disabled={isLast}
                                onClick={(e) => { e.stopPropagation(); onMoveDown?.() }}
                            >
                                <HiOutlineChevronDown className="w-3.5 h-3.5" />
                            </button>
                        </div>
                    )}

                    <div className="flex-1 min-w-0">
                {/* Line 1: rule_tag + badges + enabled */}
                <div className="flex items-center gap-2">
                    <span className="font-mono font-semibold text-sm flex-1 min-w-0 truncate">{rule.rule_tag}</span>
                    {isUnsaved && (
                        <Badge className="text-[10px] px-1.5 py-0 h-5 shrink-0 bg-blue-500/15 text-blue-400 border-blue-500/30">Unsaved</Badge>
                    )}
                    <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-5 shrink-0">{rule.priority}</Badge>
                    {isUnsaved ? (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-5 shrink-0 text-blue-400 border-blue-500/30">Pending</Badge>
                    ) : (
                        <button onClick={(e) => { e.stopPropagation(); onToggle?.(rule) }}>
                            <Badge
                                variant={rule.enabled ? "success" : "secondary"}
                                className="text-[10px] px-1.5 py-0 h-5 shrink-0"
                            >
                                {rule.enabled ? "On" : "Off"}
                            </Badge>
                        </button>
                    )}
                </div>

                {/* Line 2: → outbound_tag */}
                <p className="text-xs font-mono text-muted-foreground mt-1">
                    <span className="text-muted-foreground/60">→</span>{" "}
                    {rule.outbound_tag || rule.balancing_tag || "—"}
                </p>

                {/* Line 3: Matcher badges */}
                <div className="flex flex-wrap items-center gap-1 mt-1.5">
                    {rule.domain_rules?.length > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-5">domains</Badge>}
                    {rule.geoip_rules?.length > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-5">geoip</Badge>}
                    {rule.port_rules?.length > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-5">ports</Badge>}
                    {rule.protocol_rules?.length > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-5">protocol</Badge>}
                    {rule.inbound_tags?.length > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-5">inbounds</Badge>}
                    {!rule.domain_rules?.length && !rule.geoip_rules?.length && !rule.port_rules?.length && !rule.protocol_rules?.length && !rule.inbound_tags?.length && (
                        <span className="text-muted-foreground text-[10px]">no matchers</span>
                    )}
                </div>
                    </div>
                </div>
            </motion.div>
        </div>
    )
})
