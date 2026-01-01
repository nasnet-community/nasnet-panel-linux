import { Card } from "@/components/ui/card"
import { HiOutlineChartBar } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { useNodeHostInfo } from "@/lib/queries/use-nodes"
import type { NodeStats } from "@/lib/types"

interface LoadAverageWidgetProps {
    nodeId: number
    stats?: NodeStats
    isLoading: boolean
}

function getLoadColor(load: number, cores: number): string {
    if (cores <= 0) cores = 1
    const ratio = load / cores
    if (ratio < 0.7) return "bg-emerald-500"
    if (ratio < 1.0) return "bg-amber-500"
    return "bg-red-500"
}

function getLoadTextColor(load: number, cores: number): string {
    if (cores <= 0) cores = 1
    const ratio = load / cores
    if (ratio < 0.7) return "text-emerald-500"
    if (ratio < 1.0) return "text-amber-500"
    return "text-red-500"
}

const periods = [
    { key: "load_avg_1" as const, label: "1m" },
    { key: "load_avg_5" as const, label: "5m" },
    { key: "load_avg_15" as const, label: "15m" },
]

export function LoadAverageWidget({ nodeId, stats, isLoading }: LoadAverageWidgetProps) {
    const { data: hostInfo } = useNodeHostInfo(nodeId)
    const cores = hostInfo?.cpu_cores || 1

    return (
        <Card className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-orange-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
            <div className="flex items-center justify-between mb-3 relative z-10">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Load Average</p>
                <div className="flex items-center gap-1.5">
                    <span className="text-[10px] text-muted-foreground/50 font-mono">{cores} cores</span>
                    <HiOutlineChartBar className="w-4 h-4 text-muted-foreground/30 group-hover:text-orange-400 transition-colors" />
                </div>
            </div>
            <div className="flex flex-col gap-2.5">
                {periods.map(({ key, label }) => {
                    const value = stats?.[key] ?? 0
                    const pct = Math.min((value / cores) * 100, 100)

                    return (
                        <div key={key} className="flex items-center gap-2.5">
                            <span className="text-[11px] text-muted-foreground font-bold w-6 shrink-0">{label}</span>
                            <div className="flex-1 h-2 rounded-full bg-muted/30 overflow-hidden">
                                <div
                                    className={`h-full rounded-full transition-all duration-700 ease-out ${getLoadColor(value, cores)}`}
                                    style={{ width: `${pct}%` }}
                                />
                            </div>
                            <span className={`font-mono font-bold text-[13px] w-10 text-right tabular-nums ${getLoadTextColor(value, cores)}`}>
                                {value.toFixed(2)}
                            </span>
                        </div>
                    )
                })}
            </div>
            {isLoading && (
                <div className="absolute top-4 right-4">
                    <Loader2 className="w-4 h-4 animate-spin text-orange-500/50" />
                </div>
            )}
        </Card>
    )
}

// Compact mobile variant
export function LoadAverageCompact({ nodeId, stats, isLoading }: LoadAverageWidgetProps) {
    const { data: hostInfo } = useNodeHostInfo(nodeId)
    const cores = hostInfo?.cpu_cores || 1

    return (
        <Card className="h-full relative overflow-hidden transition-shadow duration-300 rounded-2xl p-3 bg-card/50 backdrop-blur-sm border-white/5">
            <div className="flex items-center justify-between mb-2 relative z-10">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.12em]">Load Avg</p>
                <span className="text-[10px] text-muted-foreground/50 font-mono">{cores}c</span>
            </div>
            <div className="flex flex-col gap-1.5">
                {periods.map(({ key, label }) => {
                    const value = stats?.[key] ?? 0
                    return (
                        <div key={key} className="flex justify-between items-center text-xs">
                            <span className="text-muted-foreground font-medium">{label}</span>
                            <span className={`font-mono font-bold ${getLoadTextColor(value, cores)}`}>
                                {value.toFixed(2)}
                            </span>
                        </div>
                    )
                })}
            </div>
            {isLoading && (
                <div className="absolute top-3 right-3">
                    <Loader2 className="w-3 h-3 animate-spin text-orange-500/50" />
                </div>
            )}
        </Card>
    )
}
