import { useRef, useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"
import { motion, useMotionValue, useSpring, useInView } from "framer-motion"
import { Card, CardContent } from "@/components/ui/card"
import { CircularProgress } from "@/components/ui/circular-progress"
import { AlertCircle, AlertTriangle, CircleCheck, Clock, Database, Zap } from "lucide-react"
import type { LucideIcon } from "lucide-react"
import { cn, formatBytes, parseDateLocal } from "@/lib/utils"
import { useSubExhaustionPrediction } from "@/lib/queries/use-sub-panel-analytics"
import type { SubPanelData, SubPanelExhaustionPrediction } from "@/lib/types/sub-panel"
import { DataDetailDialog } from "./data-detail-dialog"
import { TimeDetailDialog } from "./time-detail-dialog"
import {
    compactRemaining,
    dataLeftPercent,
    dataTone,
    hasEndDate,
    hasEnded,
    isCancelled,
    isPastEnd,
    shortDate,
    timeLeftPercent,
    timeTone,
    wholeDaysLeft,
    type Tone,
} from "./plan-state"

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
            <p className="text-xs text-muted-foreground font-medium">{label}</p>
            <p className="text-sm md:text-base font-semibold">
                {animated ? <AnimatedNumber value={value} /> : value}
            </p>
        </div>
    )
}

/** "26.82 GB" → 26.8 over "GB left": one decimal fits the ring at any size. */
function ringFigure(display: string): { num: string; unit: string } {
    const m = display.match(/^([\d.]+)\s*(\S+)/)
    if (!m) return { num: display, unit: "" }
    const n = parseFloat(m[1])
    return { num: n >= 100 ? Math.round(n).toString() : String(+n.toFixed(1)), unit: m[2] }
}

const TONE_ICON: Record<Tone, string> = {
    emerald: "text-emerald-600 dark:text-emerald-400",
    amber: "text-amber-600 dark:text-amber-400",
    red: "text-red-600 dark:text-red-400",
    zinc: "text-muted-foreground",
}

/** Card footer: a one-line status. */
function CardFooter({
    icon: Icon,
    tone,
    children,
}: {
    icon: LucideIcon
    tone: Tone
    children: ReactNode
}) {
    return (
        <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 pt-1.5 border-t border-border/30">
            <span className="flex items-start gap-1.5 min-w-0">
                <Icon className={cn("w-3.5 h-3.5 shrink-0 mt-px", TONE_ICON[tone])} />
                <span className="text-xs text-muted-foreground">{children}</span>
            </span>
        </div>
    )
}

function DataFooter({ data, prediction }: { data: SubPanelData; prediction?: SubPanelExhaustionPrediction }) {
    const hasForecast = !!prediction && prediction.confidence > 0 && prediction.daily_avg_bytes > 0

    if (data.is_unlimited) {
        // Nothing to run out of; the pace is still worth showing.
        return hasForecast ? (
            <CardFooter icon={Zap} tone="amber">{formatBytes(prediction.daily_avg_bytes, 1)}/day avg</CardFooter>
        ) : null
    }
    if (data.status === "traffic_exhausted" || dataLeftPercent(data) <= 0) {
        return <CardFooter icon={AlertCircle} tone="red">Out of data</CardFooter>
    }
    if (hasEnded(data)) {
        // A cancelled plan is already said three times over in the time card.
        return isCancelled(data) ? null : <CardFooter icon={Clock} tone="zinc">Paused until it's renewed</CardFooter>
    }
    if (data.status !== "active") return null
    if (!hasForecast) {
        return <CardFooter icon={Zap} tone="zinc">Forecast after a day of use</CardFooter>
    }
    const lastsUntil = prediction.exhaustion_date ? shortDate(parseDateLocal(prediction.exhaustion_date)) : null
    if (prediction.will_exhaust_first && lastsUntil) {
        return <CardFooter icon={AlertTriangle} tone="amber">Runs out ~{lastsUntil}</CardFooter>
    }
    if (hasEndDate(data) && data.end_date) {
        return <CardFooter icon={CircleCheck} tone="emerald">Lasts past {shortDate(new Date(data.end_date))}</CardFooter>
    }
    return lastsUntil ? <CardFooter icon={CircleCheck} tone="emerald">Lasts until ~{lastsUntil}</CardFooter> : null
}

// The ring and the status pill already say "ending soon"; the footer only
// says when the plan is over.
function TimeFooter({ data }: { data: SubPanelData }) {
    if (!hasEndDate(data) || !hasEnded(data)) return null
    return <CardFooter icon={AlertCircle} tone="red">{isCancelled(data) ? "Cancelled" : "Expired"}</CardFooter>
}

const RING = 84

export function StatsGrid({ data, uuid }: { data: SubPanelData; uuid: string }) {
    const hasEnd = hasEndDate(data)
    const ended = hasEnded(data)
    const displayDays = hasEnd ? wholeDaysLeft(data) : 0
    const [dataDialogOpen, setDataDialogOpen] = useState(false)
    const [timeDialogOpen, setTimeDialogOpen] = useState(false)

    // Same query key as the forecast card, so this is a cache read, not a
    // second request.
    const { data: prediction } = useSubExhaustionPrediction(uuid)
    const dataColor = dataTone(data, prediction)
    const timeColor = timeTone(data)
    const remaining = useMemo(() => ringFigure(data.data_remaining_display), [data.data_remaining_display])

    return (
        <div className="grid grid-cols-2 gap-2 sm:gap-3 md:gap-4">
            {/* Data Usage Card */}
            <motion.div className="h-full" whileHover={{ y: -2 }} whileTap={{ scale: 0.97 }} transition={{ type: "spring", stiffness: 400, damping: 25 }}>
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0 cursor-pointer hover:border-border/80 hover:bg-card/70 transition-colors h-full" onClick={() => setDataDialogOpen(true)}>
                <CardContent className="p-3.5 sm:p-4 md:p-5 flex flex-col gap-2.5 md:gap-3 h-full">
                    <div className="flex items-center gap-1.5 md:gap-2">
                        <Database className="h-3.5 w-3.5 md:h-4 md:w-4 text-muted-foreground" />
                        <span className="text-xs md:text-sm font-medium text-muted-foreground">Data</span>
                    </div>
                    <div className="flex flex-col items-center gap-2.5 sm:flex-row sm:gap-3 md:gap-4">
                        {data.is_unlimited ? (
                            <div
                                className="flex flex-col items-center justify-center shrink-0 md:!w-24 md:!h-24"
                                style={{ width: RING, height: RING }}
                            >
                                <span className="text-3xl md:text-4xl font-bold text-emerald-700 dark:text-emerald-500 leading-none">∞</span>
                                <span className="text-[12px] font-medium text-muted-foreground mt-1">unlimited</span>
                            </div>
                        ) : (
                            <CircularProgress
                                value={dataLeftPercent(data)}
                                size={RING}
                                strokeWidth={6}
                                color={dataColor}
                                className="md:!w-24 md:!h-24"
                            >
                                <span className="text-lg md:text-xl font-bold tracking-tight leading-none">
                                    <AnimatedNumber value={remaining.num} />
                                </span>
                                <span className="text-[12px] font-medium text-muted-foreground mt-0.5">
                                    {remaining.unit} left
                                </span>
                            </CircularProgress>
                        )}
                        <div className="w-full space-y-1.5 md:space-y-2.5">
                            <StatRow label="Used" value={data.data_used_display} />
                            {!data.is_unlimited && (
                                <StatRow label="Limit" value={data.data_limit_display} />
                            )}
                        </div>
                    </div>
                    <div className="mt-auto">
                        <DataFooter data={data} prediction={prediction} />
                    </div>
                </CardContent>
            </Card>
            </motion.div>

            {/* Time Remaining Card */}
            <motion.div className="h-full" whileHover={{ y: -2 }} whileTap={{ scale: 0.97 }} transition={{ type: "spring", stiffness: 400, damping: 25 }}>
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0 cursor-pointer hover:border-border/80 hover:bg-card/70 transition-colors h-full" onClick={() => setTimeDialogOpen(true)}>
                <CardContent className="p-3.5 sm:p-4 md:p-5 flex flex-col gap-2.5 md:gap-3 h-full">
                    <div className="flex items-center gap-1.5 md:gap-2">
                        <Clock className="h-3.5 w-3.5 md:h-4 md:w-4 text-muted-foreground" />
                        <span className="text-xs md:text-sm font-medium text-muted-foreground">Time</span>
                    </div>
                    <div className="flex flex-col items-center gap-2.5 sm:flex-row sm:gap-3 md:gap-4">
                        {hasEnd ? (
                            <CircularProgress
                                value={ended ? 0 : timeLeftPercent(data)}
                                size={RING}
                                strokeWidth={6}
                                color={timeColor}
                                className="md:!w-24 md:!h-24"
                            >
                                {ended && isCancelled(data) ? (
                                    <span className="text-[13px] md:text-sm font-bold tracking-tight leading-none">Cancelled</span>
                                ) : ended ? (
                                    <>
                                        <span className="text-base md:text-lg font-bold tracking-tight leading-none">Ended</span>
                                        {data.end_date && (
                                            <span className="text-[12px] font-medium text-muted-foreground mt-0.5">
                                                {shortDate(new Date(data.end_date))}
                                            </span>
                                        )}
                                    </>
                                ) : displayDays > 0 ? (
                                    <>
                                        <span className="text-lg md:text-xl font-bold tracking-tight leading-none">
                                            <AnimatedDaysCount days={displayDays} />
                                        </span>
                                        <span className="text-[12px] font-medium text-muted-foreground mt-0.5">
                                            {displayDays === 1 ? "day left" : "days left"}
                                        </span>
                                    </>
                                ) : (
                                    <>
                                        <span className="text-lg md:text-xl font-bold tracking-tight leading-none">
                                            {compactRemaining(data)}
                                        </span>
                                        <span className="text-[12px] font-medium text-muted-foreground mt-0.5">left</span>
                                    </>
                                )}
                            </CircularProgress>
                        ) : (
                            <div
                                className="flex flex-col items-center justify-center shrink-0 md:!w-24 md:!h-24"
                                style={{ width: RING, height: RING }}
                            >
                                <span className="text-3xl md:text-4xl font-bold text-emerald-700 dark:text-emerald-500 leading-none">∞</span>
                                <span className="text-[12px] font-medium text-muted-foreground mt-1">no expiry</span>
                            </div>
                        )}
                        <div className="w-full space-y-1.5 md:space-y-2.5">
                            {hasEnd && data.end_date ? (
                                <StatRow label={isPastEnd(data) ? "Ended" : "Ends"} value={shortDate(new Date(data.end_date))} animated={false} />
                            ) : (
                                <StatRow label="Active for" value={data.start_date ? `${Math.floor((Date.now() - new Date(data.start_date).getTime()) / 86400000)}d` : "—"} animated={false} />
                            )}
                            {data.start_date && (
                                <StatRow label="Started" value={shortDate(new Date(data.start_date))} animated={false} />
                            )}
                        </div>
                    </div>
                    <div className="mt-auto">
                        <TimeFooter data={data} />
                    </div>
                </CardContent>
            </Card>
            </motion.div>

            <DataDetailDialog open={dataDialogOpen} onOpenChange={setDataDialogOpen} data={data} uuid={uuid} />
            <TimeDetailDialog open={timeDialogOpen} onOpenChange={setTimeDialogOpen} data={data} uuid={uuid} />
        </div>
    )
}
