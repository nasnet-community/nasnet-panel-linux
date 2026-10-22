import { cn } from "@/lib/utils"

export type NavBadgeTone = "warning" | "danger"

export interface NavBadgeValue {
    count: number
    tone: NavBadgeTone
    /** a dot instead of a number, for "there's something to see" */
    dot?: boolean
    label?: string
}

// Dark text on both fills; white on amber-500 was about 2.1:1.
const TONE: Record<NavBadgeTone, string> = {
    warning: "bg-status-warning text-status-warning-foreground",
    danger: "bg-status-danger text-zinc-950",
}

export function NavBadge({ value, className }: { value?: NavBadgeValue; className?: string }) {
    if (!value || !(value.count > 0)) return null
    if (value.dot) {
        return <span role="img" aria-label={value.label} className={cn(className, "size-2 min-w-0 rounded-full p-0", value.tone === "danger" ? "bg-status-danger" : "bg-status-warning")} />
    }
    return (
        <span className={cn("inline-flex items-center justify-center rounded-full font-mono font-bold tabular-nums leading-none", TONE[value.tone], className)}>
            {value.count > 99 ? "99+" : value.count}
        </span>
    )
}
