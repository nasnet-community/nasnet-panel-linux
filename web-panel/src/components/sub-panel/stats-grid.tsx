import { useRef, useEffect, useMemo, useState } from "react"
import { motion, useMotionValue, useSpring, useInView } from "framer-motion"
import { Card, CardContent } from "@/components/ui/card"
import { CircularProgress } from "@/components/ui/circular-progress"
import { Database, Clock, Zap } from "lucide-react"
import { formatBytes } from "@/lib/utils"
import { useSubExhaustionPrediction } from "@/lib/queries/use-sub-panel-analytics"
import type { SubPanelData } from "@/lib/types/sub-panel"
import { DataDetailDialog } from "./data-detail-dialog"
import { TimeDetailDialog } from "./time-detail-dialog"

function getColor(percent: number): "emerald" | "amber" | "red" {
    if (percent > 90) return "red"
    if (percent > 70) return "amber"
    return "emerald"
}

function parseValueAndSuffix(display: string): { num: number; suffix: string; decimals: number; numeric: boolean } {
    const match = display.match(/^([\d.]+)\s*(.*)$/)
    if (match) {
        const parts = match[1].split(".")
        const decimals = parts[1] ? parts[1].length : 0
        return { num: parseFloat(match[1]), suffix: match[2] ? " " + match[2] : "", decimals, numeric: true }
    }
    return { num: 0, suffix: display, decimals: 0, numeric: false }
}

function AnimatedNumber({ value, className }: { value: string; className?: string }) {
    const ref = useRef<HTMLSpanElement>(null)
    const isInView = useInView(ref, { once: true })
    const { num, suffix, decimals, numeric } = parseValueAndSuffix(value)

    const motionValue = useMotionValue(0)
    const spring = useSpring(motionValue, { stiffness: 35, damping: 15, mass: 1 })

    useEffect(() => {
        if (numeric && isInView) {
            motionValue.set(num)
        }
    }, [numeric, isInView, num, motionValue])

    useEffect(() => {
        if (!numeric) return
        const unsubscribe = spring.on("change", (latest) => {
            if (ref.current) {
                const formatted = decimals > 0 ? latest.toFixed(decimals) : Math.round(latest).toString()
                ref.current.textContent = formatted + suffix
            }
        })
        return unsubscribe
    }, [numeric, spring, num, suffix, decimals])

    // Non-numeric values (dates, "Unlimited") render as-is — never run through the counter.
    return <span ref={ref} className={className}>{value}</span>
}

function AnimatedDaysCount({ days }: { days: number }) {
    const ref = useRef<HTMLSpanElement>(null)
    const isInView = useInView(ref, { once: true })

    const motionValue = useMotionValue(0)
    const spring = useSpring(motionValue, { stiffness: 28, damping: 14, mass: 1.2 })

    useEffect(() => {
        if (isInView) {
            motionValue.set(days)
        }
    }, [isInView, days, motionValue])

    useEffect(() => {
        const unsubscribe = spring.on("change", (latest) => {
            if (ref.current) {
                ref.current.textContent = Math.round(latest).toString()
            }
        })
        return unsubscribe
    }, [spring])

    return <span ref={ref}>{days}</span>
}

function StatRow({ label, value, animated = true }: { label: string; value: string; animated?: boolean }) {
    return (
        <div className="flex justify-between sm:justify-start sm:flex-col sm:items-start gap-0.5">
            <p className="text-xs text-muted-foreground font-medium uppercase tracking-wide">{label}</p>
            <p className="text-sm md:text-base font-semibold">
                {animated ? <AnimatedNumber value={value} /> : value}
            </p>
        </div>
    )
}

function parseDaysFromRemaining(timeRemaining: string): number {
    const match = timeRemaining.match(/^(\d+)\s*days?/)
    return match ? parseInt(match[1], 10) : 0
}

function formatCompactRemaining(timeRemaining: string): string {
    const days = timeRemaining.match(/(\d+)\s*days?/)
    const hours = timeRemaining.match(/(\d+)\s*hours?/)
    const mins = timeRemaining.match(/(\d+)\s*min/)
    const parts: string[] = []
    if (days) parts.push(`${days[1]}d`)
    if (hours) parts.push(`${hours[1]}h`)
    if (!days && mins) parts.push(`${mins[1]}m`)
    return parts.join(" ") || timeRemaining
}

export function StatsGrid({ data, uuid }: { data: SubPanelData; uuid: string }) {
    const dataColor = data.is_unlimited ? "emerald" : getColor(data.data_usage_percent)
    const timeColor = getColor(data.time_used_percent)
    const hasEndDate = data.days_remaining >= 0
    const displayDays = hasEndDate ? parseDaysFromRemaining(data.time_remaining) : 0
    const [dataDialogOpen, setDataDialogOpen] = useState(false)
    const [timeDialogOpen, setTimeDialogOpen] = useState(false)

    // Share the forecast card's number instead of computing a lifetime average
    // here — two different "per day" figures on one page can't both be right.
    // Same query key, so this is a cache read, not a second request.
    const { data: prediction } = useSubExhaustionPrediction(uuid)

    const dailyBurnRate = useMemo(() => {
        if (data.is_unlimited) return null
        if (!prediction || prediction.daily_avg_bytes <= 0) return null
        return formatBytes(prediction.daily_avg_bytes, 1)
    }, [data.is_unlimited, prediction])

    return (
        <div className="grid grid-cols-2 gap-2 sm:gap-3 md:gap-4">
            {/* Data Usage Card */}
            <motion.div className="h-full" whileHover={{ y: -2 }} whileTap={{ scale: 0.97 }} transition={{ type: "spring", stiffness: 400, damping: 25 }}>
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0 cursor-pointer hover:border-border/80 hover:bg-card/70 transition-colors h-full" onClick={() => setDataDialogOpen(true)}>
                <CardContent className="p-3.5 sm:p-4 md:p-5">
                    <div className="flex items-center gap-1.5 md:gap-2 mb-2 md:mb-3">
                        <Database className="h-3.5 w-3.5 md:h-4 md:w-4 text-muted-foreground" />
                        <span className="text-xs md:text-sm font-medium text-muted-foreground uppercase tracking-wider">Data</span>
                    </div>
                    <div className="flex flex-col items-center gap-2.5 sm:flex-row sm:gap-3 md:gap-4">
                        {data.is_unlimited ? (
                            <div
                                className="flex flex-col items-center justify-center shrink-0 md:!w-24 md:!h-24"
                                style={{ width: 76, height: 76 }}
                            >
                                <span className="text-3xl md:text-4xl font-bold text-emerald-700 dark:text-emerald-500 leading-none">∞</span>
                            </div>
                        ) : (
                            <CircularProgress
                                value={data.data_usage_percent}
                                size={76}
                                strokeWidth={6}
                                color={dataColor}
                                showValue
                                label="used"
                                className="md:!w-24 md:!h-24"
                            />
                        )}
                        <div className="w-full space-y-1.5 md:space-y-2.5">
                            {!data.is_unlimited && (
                                <StatRow label="Left" value={data.data_remaining_display} />
                            )}
                            <StatRow label="Used" value={data.data_used_display} />
                            {!data.is_unlimited && (
                                <StatRow label="Limit" value={data.data_limit_display} />
                            )}
                            {dailyBurnRate && (
                                <div className="flex items-center gap-1.5 pt-1 border-t border-border/30">
                                    <Zap className="w-3.5 h-3.5 text-amber-400 shrink-0" />
                                    {/* Must stay on one line — a wrap here makes the Data
                                        card taller than Time and the grid row grows with it. */}
                                    <span
                                        className="text-xs text-muted-foreground whitespace-nowrap truncate"
                                        title="Recent daily average"
                                    >
                                        {dailyBurnRate}/day avg
                                    </span>
                                </div>
                            )}
                        </div>
                    </div>
                </CardContent>
            </Card>
            </motion.div>

            {/* Time Remaining Card */}
            <motion.div className="h-full" whileHover={{ y: -2 }} whileTap={{ scale: 0.97 }} transition={{ type: "spring", stiffness: 400, damping: 25 }}>
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0 cursor-pointer hover:border-border/80 hover:bg-card/70 transition-colors h-full" onClick={() => setTimeDialogOpen(true)}>
                <CardContent className="p-3.5 sm:p-4 md:p-5">
                    <div className="flex items-center gap-1.5 md:gap-2 mb-2 md:mb-3">
                        <Clock className="h-3.5 w-3.5 md:h-4 md:w-4 text-muted-foreground" />
                        <span className="text-xs md:text-sm font-medium text-muted-foreground uppercase tracking-wider">Time</span>
                    </div>
                    <div className="flex flex-col items-center gap-2.5 sm:flex-row sm:gap-3 md:gap-4">
                        {hasEndDate ? (
                            <CircularProgress
                                value={data.time_used_percent}
                                size={76}
                                strokeWidth={6}
                                color={timeColor}
                                showValue={false}
                                className="md:!w-24 md:!h-24"
                            >
                                <span className="text-lg md:text-xl font-bold tracking-tight leading-none">
                                    <AnimatedDaysCount days={displayDays} />
                                </span>
                                <span className="text-xs uppercase font-medium text-muted-foreground tracking-wider mt-0.5">
                                    days left
                                </span>
                            </CircularProgress>
                        ) : (
                            <div
                                className="flex flex-col items-center justify-center shrink-0 md:!w-24 md:!h-24"
                                style={{ width: 76, height: 76 }}
                            >
                                <span className="text-3xl md:text-4xl font-bold text-emerald-700 dark:text-emerald-500 leading-none">∞</span>
                                <span className="text-xs uppercase font-medium text-muted-foreground tracking-wider mt-1">
                                    no expiry
                                </span>
                            </div>
                        )}
                        <div className="w-full space-y-1.5 md:space-y-2.5">
                            {hasEndDate ? (
                                <StatRow label="Left" value={formatCompactRemaining(data.time_remaining)} animated={false} />
                            ) : (
                                <StatRow label="Active for" value={data.start_date ? `${Math.floor((Date.now() - new Date(data.start_date).getTime()) / 86400000)}d` : "—"} animated={false} />
                            )}
                            {data.start_date && (
                                <StatRow label="Start" value={new Date(data.start_date).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" })} />
                            )}
                            {/* Only shown when there IS an end date. A plan duration
                                printed next to "no expiry" reads as a countdown that
                                doesn't exist. */}
                            {data.end_date && (
                                <StatRow label="End" value={new Date(data.end_date).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" })} />
                            )}
                        </div>
                    </div>
                </CardContent>
            </Card>
            </motion.div>

            <DataDetailDialog open={dataDialogOpen} onOpenChange={setDataDialogOpen} data={data} uuid={uuid} />
            <TimeDetailDialog open={timeDialogOpen} onOpenChange={setTimeDialogOpen} data={data} uuid={uuid} />
        </div>
    )
}
