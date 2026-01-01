import { cn } from "@/lib/utils"

interface SectionHeaderProps {
    children: React.ReactNode
    /** Importance tier — controls visual weight so destructive/identity sections stand out. */
    tone?: "muted" | "default" | "danger"
    right?: React.ReactNode
    className?: string
}

/**
 * Section heading for the subscription details sheet. Three tones give the
 * previously-flat section stack a visible hierarchy: muted for low-frequency
 * fields, default for most sections, danger for destructive blocks.
 */
export function SectionHeader({ children, tone = "muted", right, className }: SectionHeaderProps) {
    return (
        <div className={cn("flex items-center justify-between", className)}>
            <h3
                className={cn(
                    "text-xs font-semibold uppercase tracking-wider",
                    tone === "muted" && "text-muted-foreground",
                    tone === "default" && "text-foreground/80",
                    tone === "danger" && "text-red-500 dark:text-red-400",
                )}
            >
                {children}
            </h3>
            {right}
        </div>
    )
}

/** Utility row used within sections. */
export function StatRow({
    label,
    value,
    extra,
}: {
    label: React.ReactNode
    value: React.ReactNode
    extra?: React.ReactNode
}) {
    return (
        <div className="flex items-center justify-between py-0.5">
            <span className="text-sm text-muted-foreground">{label}</span>
            <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{value}</span>
                {extra}
            </div>
        </div>
    )
}
