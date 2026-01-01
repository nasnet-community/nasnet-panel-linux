import { cn } from "@/lib/utils"

interface MetricBarProps {
    value: number
    max?: number
    label?: string
    showValue?: boolean
    size?: "sm" | "md"
    className?: string
}

function getBarColor(percent: number): string {
    if (percent >= 80) return "bg-red-500"
    if (percent >= 50) return "bg-amber-500"
    return "bg-emerald-500"
}

function getTextColor(percent: number): string {
    if (percent >= 80) return "text-red-500"
    if (percent >= 50) return "text-amber-500"
    return "text-emerald-500"
}

export function MetricBar({ value, max = 100, label, showValue = true, size = "sm", className }: MetricBarProps) {
    const percent = Math.min(Math.max((value / max) * 100, 0), 100)

    return (
        <div className={cn("space-y-1", className)}>
            {(label || showValue) && (
                <div className="flex items-center justify-between">
                    {label && <span className="text-[10px] sm:text-xs md:text-[13px] text-muted-foreground font-medium uppercase tracking-wider">{label}</span>}
                    {showValue && <span className={cn("text-[10px] sm:text-xs md:text-[13px] font-bold", getTextColor(percent))}>{percent.toFixed(0)}%</span>}
                </div>
            )}
            <div
                className={cn(
                    "w-full rounded-full bg-muted/50 overflow-hidden",
                    size === "sm" ? "h-1.5 md:h-2" : "h-2.5 md:h-3"
                )}
                role="progressbar"
                aria-valuenow={Math.round(percent)}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-label={label ? `${label}: ${percent.toFixed(0)}%` : `${percent.toFixed(0)}%`}
            >
                <div
                    className={cn("h-full rounded-full transition-all duration-500", getBarColor(percent))}
                    style={{ width: `${percent}%` }}
                />
            </div>
        </div>
    )
}

interface CategoryBarProps {
    values: Array<{ value: number; color: string; label?: string }>
    className?: string
}

export function CategoryBar({ values, className }: CategoryBarProps) {
    const total = values.reduce((sum, v) => sum + v.value, 0)
    if (total === 0) return null

    return (
        <div className={cn("flex w-full h-2 rounded-full overflow-hidden bg-muted/50", className)}>
            {values.map((v, i) => (
                <div
                    key={i}
                    className={cn("h-full transition-all duration-500", v.color)}
                    style={{ width: `${(v.value / total) * 100}%` }}
                    title={v.label ? `${v.label}: ${v.value}` : String(v.value)}
                />
            ))}
        </div>
    )
}

export { getBarColor, getTextColor }
