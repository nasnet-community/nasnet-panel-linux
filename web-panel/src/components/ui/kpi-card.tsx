import { useEffect, useRef, useState } from "react"
import { cn } from "@/lib/utils"
import { HiArrowUp, HiArrowDown } from "react-icons/hi"

function AnimatedNumber({ value, prefix = "", suffix = "" }: { value: number; prefix?: string; suffix?: string }) {
    const [displayValue, setDisplayValue] = useState(value)
    const prevValueRef = useRef(value)

    useEffect(() => {
        const from = prevValueRef.current
        prevValueRef.current = value
        if (from === value) return

        const duration = 800
        const steps = 24
        const diff = value - from
        const stepValue = diff / steps
        let current = from

        const timer = setInterval(() => {
            current += stepValue
            if ((diff > 0 && current >= value) || (diff < 0 && current <= value)) {
                setDisplayValue(value)
                clearInterval(timer)
            } else {
                setDisplayValue(Math.floor(current))
            }
        }, duration / steps)

        return () => clearInterval(timer)
    }, [value])

    return <>{prefix}{displayValue.toLocaleString()}{suffix}</>
}

interface KpiCardProps {
    title: string
    value: string | number
    description?: string
    icon: React.ComponentType<{ className?: string }>
    trend?: "up" | "down"
    trendValue?: string
    variant?: "default" | "success" | "warning" | "danger"
    compact?: boolean
}

export function KpiCard({
    title,
    value,
    description,
    icon: Icon,
    trend,
    trendValue,
    variant = "default",
    compact = false,
}: KpiCardProps) {
    const accentColors = {
        default: "border-l-primary/50",
        success: "border-l-emerald-500/50",
        warning: "border-l-amber-500/50",
        danger: "border-l-red-500/50",
    }

    const iconColors = {
        default: "text-primary bg-primary/10",
        success: "text-emerald-500 bg-emerald-500/10",
        warning: "text-amber-500 bg-amber-500/10",
        danger: "text-red-500 bg-red-500/10",
    }

    return (
        <div className={cn(
            "relative rounded-lg border border-border bg-card/50 backdrop-blur-sm transition-all duration-200 hover:shadow-md hover:border-border/80 border-l-2",
            accentColors[variant],
            compact ? "p-3 md:p-4 lg:p-5" : "p-4 md:p-5 lg:p-6"
        )}>
            <div className="flex items-start justify-between gap-2 md:gap-3">
                <div className="space-y-1 min-w-0 flex-1">
                    <p className="text-[10px] sm:text-[11px] md:text-xs font-medium text-muted-foreground uppercase tracking-wider truncate">{title}</p>
                    <div className={cn("font-bold tracking-tight", compact ? "text-lg md:text-xl lg:text-2xl" : "text-2xl md:text-3xl")}>
                        {typeof value === "number" ? <AnimatedNumber value={value} /> : value}
                    </div>
                </div>
                <div className={cn("p-1.5 md:p-2 lg:p-2.5 rounded-md shrink-0", iconColors[variant])}>
                    <Icon className="w-3.5 h-3.5 md:w-4 md:h-4 lg:w-5 lg:h-5" />
                </div>
            </div>
            {(description || (trend && trendValue)) && (
                <div className="mt-2 md:mt-3 flex items-center justify-between gap-2">
                    {description && (
                        <p className="text-[10px] sm:text-[11px] md:text-xs text-muted-foreground truncate">{description}</p>
                    )}
                    {trend && trendValue && (
                        <span className={cn(
                            "flex items-center text-[10px] sm:text-[11px] font-bold px-1.5 py-0.5 md:px-2 md:py-1 rounded-full shrink-0",
                            trend === "up" ? "text-emerald-500 bg-emerald-500/10" : "text-red-500 bg-red-500/10"
                        )}>
                            {trend === "up" ? <HiArrowUp className="w-2.5 h-2.5 md:w-3 md:h-3 mr-0.5 md:mr-1" /> : <HiArrowDown className="w-2.5 h-2.5 md:w-3 md:h-3 mr-0.5 md:mr-1" />}
                            {trendValue}
                        </span>
                    )}
                </div>
            )}
        </div>
    )
}
