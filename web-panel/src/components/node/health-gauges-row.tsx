import { useState } from "react"
import { motion, AnimatePresence } from "framer-motion"
import { Card } from "@/components/ui/card"
import { CircularProgress } from "@/components/ui/circular-progress"
import { StatsChart } from "@/components/stats-chart"
import { Loader2, ChevronDown } from "lucide-react"
import { useChartPalette } from "@/lib/design/palette"
import type { NodeDataPoint } from "@/lib/types"

type MetricKey = "cpu" | "memory" | "disk"

interface HealthGaugesRowProps {
    cpu?: number
    memory?: number
    disk?: number
    chartData: NodeDataPoint[]
    isLoading: boolean
}

export function HealthGaugesRow({ cpu, memory, disk, chartData, isLoading }: HealthGaugesRowProps) {
    const c = useChartPalette()
    const [expandedMetric, setExpandedMetric] = useState<MetricKey | null>(null)

    const metrics: {
        key: MetricKey
        label: string
        dataKey: keyof NodeDataPoint
        color: "blue" | "purple" | "amber"
        chartColor: string
    }[] = [
        { key: "cpu", label: "CPU", dataKey: "cpu", color: "blue", chartColor: c.info },
        { key: "memory", label: "Memory", dataKey: "memory", color: "purple", chartColor: c.chart6 },
        { key: "disk", label: "Storage", dataKey: "disk", color: "amber", chartColor: c.warning },
    ]

    const values: Record<MetricKey, number | undefined> = { cpu, memory, disk }

    const getMax = (key: keyof NodeDataPoint) => {
        if (chartData.length === 0) return 0
        return Math.max(...chartData.map(d => (d[key] as number) || 0))
    }

    const getMin = (key: keyof NodeDataPoint) => {
        if (chartData.length === 0) return 0
        const vals = chartData.map(d => (d[key] as number) || 0).filter(v => v > 0)
        return vals.length > 0 ? Math.min(...vals) : 0
    }

    const getAvg = (key: keyof NodeDataPoint) => {
        if (chartData.length === 0) return 0
        const vals = chartData.map(d => (d[key] as number) || 0)
        return vals.reduce((a, b) => a + b, 0) / vals.length
    }

    const handleToggle = (key: MetricKey) => {
        setExpandedMetric(prev => (prev === key ? null : key))
    }

    const expandedConfig = expandedMetric ? metrics.find(m => m.key === expandedMetric) : null

    return (
        <Card className="rounded-2xl p-3 bg-card/50 backdrop-blur-sm border-white/5">
            {/* Gauge Row */}
            <div className="flex justify-evenly items-center">
                {metrics.map(metric => {
                    const val = values[metric.key]
                    const isExpanded = expandedMetric === metric.key

                    return (
                        <button
                            key={metric.key}
                            onClick={() => handleToggle(metric.key)}
                            className="flex flex-col items-center gap-1.5 py-1 px-2 rounded-xl transition-colors active:bg-muted/50"
                        >
                            <div className="relative">
                                <CircularProgress
                                    value={val ?? 0}
                                    size={72}
                                    strokeWidth={6}
                                    color={metric.color}
                                    showValue={false}
                                >
                                    <span className="text-lg font-bold tracking-tight">
                                        {val !== undefined ? `${val.toFixed(0)}%` : "—"}
                                    </span>
                                </CircularProgress>
                                {isLoading && (
                                    <Loader2 className={`absolute -top-0.5 -right-0.5 w-3 h-3 animate-spin text-${metric.color === "amber" ? "amber" : metric.color}-500/50`} />
                                )}
                            </div>
                            <div className="flex items-center gap-1">
                                <span className="text-[11px] uppercase font-bold text-muted-foreground tracking-wider">
                                    {metric.label}
                                </span>
                                <ChevronDown
                                    className={`w-3 h-3 text-muted-foreground/50 transition-transform duration-200 ${isExpanded ? "rotate-180" : ""}`}
                                />
                            </div>
                            <span className="text-[11px] text-muted-foreground/70 font-medium">
                                Max {getMax(metric.dataKey).toFixed(0)}%
                            </span>
                        </button>
                    )
                })}
            </div>

            {/* Expandable Chart Area */}
            <AnimatePresence mode="wait">
                {expandedMetric && expandedConfig && (
                    <motion.div
                        key={expandedMetric}
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.25, ease: "easeInOut" }}
                        className="overflow-hidden"
                    >
                        <div className="pt-3 border-t border-white/5 mt-3">
                            {/* Min / Avg / Max stats row */}
                            <div className="flex justify-between px-2 mb-2">
                                <div className="flex flex-col items-center">
                                    <span className="text-[11px] uppercase font-bold text-muted-foreground/60 tracking-wider">Min</span>
                                    <span className="text-xs font-bold font-mono">{getMin(expandedConfig.dataKey).toFixed(1)}%</span>
                                </div>
                                <div className="flex flex-col items-center">
                                    <span className="text-[11px] uppercase font-bold text-muted-foreground/60 tracking-wider">Avg</span>
                                    <span className="text-xs font-bold font-mono">{getAvg(expandedConfig.dataKey).toFixed(1)}%</span>
                                </div>
                                <div className="flex flex-col items-center">
                                    <span className="text-[11px] uppercase font-bold text-muted-foreground/60 tracking-wider">Max</span>
                                    <span className="text-xs font-bold font-mono">{getMax(expandedConfig.dataKey).toFixed(1)}%</span>
                                </div>
                            </div>
                            <StatsChart
                                data={chartData}
                                dataKey={expandedConfig.dataKey}
                                color={expandedConfig.chartColor}
                                height={100}
                            />
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </Card>
    )
}
