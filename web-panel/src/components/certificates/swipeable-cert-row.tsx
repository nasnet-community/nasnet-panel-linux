import React, { useRef, useCallback, useEffect } from "react"
import { motion, useMotionValue, useTransform, animate } from "framer-motion"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Progress } from "@/components/ui/progress"
import { cn } from "@/lib/utils"
import type { AgentCertificate } from "@/lib/types"
import { Eye, RefreshCw, XCircle, Trash2 } from "lucide-react"

const ACTION_WIDTH = 64

function getActions(cert: AgentCertificate) {
    if (cert.type === "ca") return { count: 1, width: 64 } // View only
    if (cert.is_revoked) return { count: 2, width: 128 } // View + Delete
    return { count: 4, width: 256 } // View + Renew + Revoke + Delete
}

function getTypeBadgeClass(type: string) {
    switch (type) {
        case "ca": return "border-purple-200 bg-purple-50 text-purple-700 dark:border-purple-900 dark:bg-purple-950/30 dark:text-purple-400"
        case "master": return "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-400"
        case "agent": return "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-900 dark:bg-orange-950/30 dark:text-orange-400"
        case "public": return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-400"
        default: return ""
    }
}

function getExpiryColor(days: number) {
    if (days < 0) return "text-destructive"
    if (days <= 7) return "text-red-600 dark:text-red-400"
    if (days <= 30) return "text-amber-600 dark:text-amber-400"
    return "text-emerald-600 dark:text-emerald-400"
}

function getProgressColor(days: number) {
    if (days < 0) return "[&>div]:bg-muted-foreground/30"
    if (days <= 7) return "[&>div]:bg-red-500"
    if (days <= 30) return "[&>div]:bg-amber-500"
    return "[&>div]:bg-emerald-500"
}

function getStatusBadge(cert: AgentCertificate) {
    if (cert.is_revoked) return <Badge variant="danger" className="text-[10px] px-1.5 py-0">Revoked</Badge>
    if (cert.days_until_expiry < 0) return <Badge variant="danger" className="text-[10px] px-1.5 py-0">Expired</Badge>
    if (cert.days_until_expiry <= 30) return <Badge variant="warning" className="text-[10px] px-1.5 py-0">Expiring</Badge>
    return <Badge variant="success" className="text-[10px] px-1.5 py-0">Valid</Badge>
}

interface SwipeableCertRowProps {
    cert: AgentCertificate
    shouldClose: boolean
    isSelected: boolean
    multiSelectMode: boolean
    onOpen: (id: number) => void
    onViewDetails: (cert: AgentCertificate) => void
    onRenew: (cert: AgentCertificate) => void
    onRevoke: (cert: AgentCertificate) => void
    onDelete: (cert: AgentCertificate) => void
    onToggleSelect: (id: number) => void
}

export const SwipeableCertRow = React.memo(function SwipeableCertRow({
    cert,
    shouldClose,
    isSelected,
    multiSelectMode,
    onOpen,
    onViewDetails,
    onRenew,
    onRevoke,
    onDelete,
    onToggleSelect,
}: SwipeableCertRowProps) {
    const x = useMotionValue(0)
    const isDragging = useRef(false)
    const isOpenRef = useRef(false)

    const { count: actionCount, width: totalActionWidth } = getActions(cert)
    const SNAP_THRESHOLD = actionCount >= 3 ? -80 : -60
    const canDrag = actionCount > 0

    useEffect(() => {
        if (shouldClose && isOpenRef.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpenRef.current = false
        }
    }, [shouldClose, x])

    const actionOpacity = useTransform(x, [-totalActionWidth, -40, 0], [1, 0.5, 0])

    const handleDragStart = useCallback(() => {
        isDragging.current = true
    }, [])

    const handleDragEnd = useCallback((_: unknown, info: { offset: { x: number } }) => {
        const shouldOpen = info.offset.x < SNAP_THRESHOLD
        if (shouldOpen) {
            animate(x, -totalActionWidth, { type: "spring", stiffness: 300, damping: 30 })
            isOpenRef.current = true
            onOpen(cert.id)
        } else {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpenRef.current = false
        }
        setTimeout(() => { isDragging.current = false }, 50)
    }, [x, onOpen, cert.id, totalActionWidth, SNAP_THRESHOLD])

    const handleRowClick = useCallback((e: React.MouseEvent) => {
        if (isDragging.current) return
        const target = e.target as HTMLElement
        if (target.closest("button") || target.closest("input")) return

        if (isOpenRef.current) {
            animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
            isOpenRef.current = false
            return
        }

        if (multiSelectMode) {
            onToggleSelect(cert.id)
            return
        }

        onViewDetails(cert)
    }, [onViewDetails, cert, x, multiSelectMode, onToggleSelect])

    const handleAction = useCallback((action: () => void) => {
        action()
        animate(x, 0, { type: "spring", stiffness: 300, damping: 30 })
        isOpenRef.current = false
    }, [x])

    // Calculate progress percentage (assume 365 day max validity)
    const totalDays = Math.max(
        Math.round((new Date(cert.not_after).getTime() - new Date(cert.not_before || cert.created_at).getTime()) / (1000 * 60 * 60 * 24)),
        1
    )
    const progressPercent = Math.max(0, Math.min(100, (cert.days_until_expiry / totalDays) * 100))

    return (
        <div className="relative overflow-hidden border-b border-border/50 last:border-b-0">
            {/* Action buttons behind */}
            {canDrag && (
                <motion.div
                    className="absolute inset-y-0 right-0 flex"
                    style={{ opacity: actionOpacity }}
                >
                    <button
                        className="w-16 flex flex-col items-center justify-center gap-1 bg-blue-600 text-white active:bg-blue-700"
                        onClick={() => handleAction(() => onViewDetails(cert))}
                    >
                        <Eye className="w-5 h-5" />
                        <span className="text-[10px] font-medium">View</span>
                    </button>
                    {!cert.is_revoked && cert.type !== "ca" && (
                        <>
                            <button
                                className="w-16 flex flex-col items-center justify-center gap-1 bg-emerald-600 text-white active:bg-emerald-700"
                                onClick={() => handleAction(() => onRenew(cert))}
                            >
                                <RefreshCw className="w-5 h-5" />
                                <span className="text-[10px] font-medium">Renew</span>
                            </button>
                            <button
                                className="w-16 flex flex-col items-center justify-center gap-1 bg-amber-600 text-white active:bg-amber-700"
                                onClick={() => handleAction(() => onRevoke(cert))}
                            >
                                <XCircle className="w-5 h-5" />
                                <span className="text-[10px] font-medium">Revoke</span>
                            </button>
                        </>
                    )}
                    {cert.type !== "ca" && (
                        <button
                            className="w-16 flex flex-col items-center justify-center gap-1 bg-red-600 text-white active:bg-red-700"
                            onClick={() => handleAction(() => onDelete(cert))}
                        >
                            <Trash2 className="w-5 h-5" />
                            <span className="text-[10px] font-medium">Delete</span>
                        </button>
                    )}
                </motion.div>
            )}

            {/* Main card content */}
            <motion.div
                className="relative bg-background px-4 py-3 cursor-pointer active:bg-muted/30"
                style={{ x }}
                drag={canDrag && !multiSelectMode ? "x" : false}
                dragConstraints={{ left: -totalActionWidth, right: 0 }}
                dragElastic={{ left: 0.1, right: 0.5 }}
                onDragStart={handleDragStart}
                onDragEnd={handleDragEnd}
                onClick={handleRowClick}
            >
                <div className="flex items-start gap-3">
                    {/* Multi-select checkbox */}
                    {multiSelectMode && cert.type !== "ca" && (
                        <div className="flex items-center pt-1">
                            <Checkbox
                                checked={isSelected}
                                onCheckedChange={() => onToggleSelect(cert.id)}
                            />
                        </div>
                    )}

                    <div className="flex-1 min-w-0">
                        {/* Row 1: Type badge + CN + Status */}
                        <div className="flex items-center gap-2">
                            <Badge variant="outline" className={cn(
                                "capitalize text-[10px] px-1.5 py-0 font-normal shrink-0",
                                getTypeBadgeClass(cert.type)
                            )}>
                                {cert.type}
                            </Badge>
                            <span className="flex-1 min-w-0 text-sm font-medium truncate font-mono">
                                {cert.common_name}
                            </span>
                            {getStatusBadge(cert)}
                        </div>

                        {/* Row 2: Serial + Expiry */}
                        <div className="flex items-center justify-between mt-1.5 gap-2">
                            <span className="text-xs text-muted-foreground font-mono truncate">
                                {cert.serial_number.length > 16
                                    ? `${cert.serial_number.slice(0, 16)}...`
                                    : cert.serial_number}
                            </span>
                            <div className="flex items-center gap-2 shrink-0">
                                <span className={cn("text-xs font-medium", getExpiryColor(cert.days_until_expiry))}>
                                    {cert.days_until_expiry > 0 ? `${cert.days_until_expiry}d` : "Expired"}
                                </span>
                            </div>
                        </div>

                        {/* Row 3: Expiry progress bar */}
                        <div className="mt-1.5">
                            <Progress
                                value={progressPercent}
                                className={cn("h-1", getProgressColor(cert.days_until_expiry))}
                            />
                        </div>
                    </div>
                </div>
            </motion.div>
        </div>
    )
})
