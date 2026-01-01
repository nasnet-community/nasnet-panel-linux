import { useRef, useState, useEffect } from "react"
import type { UserDetails } from "@/lib/types"
import { formatBytes } from "@/lib/utils"
import { cn } from "@/lib/utils"

interface CollapsibleSummaryProps {
    user: UserDetails
    children: React.ReactNode
    onExpand?: () => void
}

export function CollapsibleSummary({ user, children, onExpand }: CollapsibleSummaryProps) {
    const sentinelRef = useRef<HTMLDivElement>(null)
    const [collapsed, setCollapsed] = useState(false)

    useEffect(() => {
        const sentinel = sentinelRef.current
        if (!sentinel) return

        const observer = new IntersectionObserver(
            ([entry]) => setCollapsed(!entry.isIntersecting),
            { threshold: 0 }
        )
        observer.observe(sentinel)
        return () => observer.disconnect()
    }, [])

    const totalData = user.total_data_upload + user.total_data_download

    return (
        <>
            {/* Sentinel — placed at top of summary strip */}
            <div ref={sentinelRef} className="h-0 w-full" aria-hidden="true" />

            {/* Full summary (visible when not collapsed) */}
            <div
                className={cn(
                    "transition-all duration-200 overflow-hidden",
                    collapsed ? "max-h-0 opacity-0" : "max-h-[300px] opacity-100"
                )}
                aria-hidden={collapsed}
            >
                {children}
            </div>

            {/* Collapsed ticker (visible when collapsed) */}
            {collapsed && (
                <button
                    onClick={() => {
                        sentinelRef.current?.scrollIntoView({ behavior: "smooth", block: "start" })
                        onExpand?.()
                    }}
                    className="w-full flex items-center justify-between px-4 py-2 bg-background/95 backdrop-blur text-xs border-b"
                >
                    <div className="flex gap-3 text-muted-foreground">
                        <span>Subs: <b className="text-primary">{user.active_subscriptions}</b></span>
                        <span>Data: <b className="text-foreground">{formatBytes(totalData)}</b></span>
                    </div>
                    <span className="text-muted-foreground/50">{"\u25BC"}</span>
                </button>
            )}
        </>
    )
}
