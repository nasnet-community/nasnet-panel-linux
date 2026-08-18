import { motion } from "framer-motion"
import { cn } from "@/lib/utils"

const statusConfig: Record<string, { label: string; textClass: string; bgClass: string }> = {
    active: { label: "Active", textClass: "text-emerald-700 dark:text-emerald-400", bgClass: "bg-emerald-500/10 border-emerald-500/20" },
    paused: { label: "Paused", textClass: "text-amber-700 dark:text-amber-400", bgClass: "bg-amber-500/10 border-amber-500/20" },
    expired: { label: "Expired", textClass: "text-red-600 dark:text-red-400", bgClass: "bg-red-500/10 border-red-500/20" },
    cancelled: { label: "Cancelled", textClass: "text-red-600 dark:text-red-400", bgClass: "bg-red-500/10 border-red-500/20" },
    traffic_exhausted: { label: "Data Finished", textClass: "text-red-600 dark:text-red-400", bgClass: "bg-red-500/10 border-red-500/20" },
    unknown: { label: "Unknown", textClass: "text-muted-foreground", bgClass: "bg-zinc-500/10 border-zinc-500/20" },
    pending: { label: "Pending", textClass: "text-muted-foreground", bgClass: "bg-zinc-500/10 border-zinc-500/20" },
}

interface StatusBadgesProps {
    status: string
}

export function StatusBadges({ status }: StatusBadgesProps) {
    const config = statusConfig[status] || statusConfig.unknown

    return (
        <motion.span
            initial={{ scale: 0.7, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            transition={{ type: "spring", stiffness: 400, damping: 15, delay: 0.3 }}
            className={cn(
                "inline-flex items-center rounded-full border px-2.5 md:px-3 py-0.5 md:py-1 text-xs font-semibold shrink-0",
                config.bgClass, config.textClass
            )}
        >
            {config.label}
        </motion.span>
    )
}
