import { cn } from "@/lib/utils"

// One "<number> <noun>" pair for a section header's stat line.
export function Stat({ value, label, tone }: { value: string; label: string; tone?: "good" | "bad" | "muted" }) {
    return (
        <span className="inline-flex items-baseline gap-1 whitespace-nowrap">
            <b className={cn(
                "font-semibold tabular-nums",
                tone === "good" && "text-emerald-600 dark:text-emerald-400",
                tone === "bad" && "text-red-600 dark:text-red-400",
                tone === "muted" && "text-muted-foreground",
            )}>
                {value}
            </b>
            <span className="text-muted-foreground">{label}</span>
        </span>
    )
}
