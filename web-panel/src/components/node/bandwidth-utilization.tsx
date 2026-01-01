import { Card } from "@/components/ui/card"
import { HiOutlineLightningBolt } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { CircularProgress } from "@/components/ui/circular-progress"
import { formatSpeed } from "@/lib/utils"
import type { Node, NodeStats } from "@/lib/types"

interface BandwidthUtilizationProps {
    node: Node
    stats?: NodeStats
    isLoading: boolean
}

function getUtilColor(pct: number): "emerald" | "amber" | "red" {
    if (pct < 60) return "emerald"
    if (pct < 85) return "amber"
    return "red"
}

function getUtilBarColor(pct: number): string {
    if (pct < 60) return "bg-emerald-500"
    if (pct < 85) return "bg-amber-500"
    return "bg-red-500"
}

function getUtilTextColor(pct: number): string {
    if (pct < 60) return "text-emerald-500"
    if (pct < 85) return "text-amber-500"
    return "text-red-500"
}

export function BandwidthUtilization({ node, stats, isLoading }: BandwidthUtilizationProps) {
    const bwEnabled = node.bandwidth_settings?.enabled
    const totalBwMbps = node.bandwidth_settings?.total_bw || 0
    const currentSpeedBytes = (stats?.up_speed || 0) + (stats?.down_speed || 0)
    const currentSpeedMbps = (currentSpeedBytes * 8) / 1_000_000

    const utilPct = totalBwMbps > 0 ? Math.min((currentSpeedMbps / totalBwMbps) * 100, 100) : 0

    if (bwEnabled && totalBwMbps > 0) {
        return (
            <Card className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-teal-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
                <div className="flex items-center justify-between mb-3 relative z-10">
                    <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Bandwidth</p>
                    <HiOutlineLightningBolt className="w-4 h-4 text-muted-foreground/30 group-hover:text-teal-400 transition-colors" />
                </div>
                <div className="flex flex-col items-center gap-1">
                    <CircularProgress
                        value={utilPct}
                        size={80}
                        strokeWidth={6}
                        color={getUtilColor(utilPct)}
                        showValue={false}
                    >
                        <span className={`text-lg font-bold tracking-tight ${getUtilTextColor(utilPct)}`}>
                            {utilPct.toFixed(0)}%
                        </span>
                    </CircularProgress>
                    <div className="text-center mt-1">
                        <p className="text-[11px] text-muted-foreground font-medium">
                            {currentSpeedMbps.toFixed(1)} <span className="text-muted-foreground/50">of</span> {totalBwMbps} Mbps
                        </p>
                    </div>
                </div>
                {isLoading && (
                    <div className="absolute top-4 right-4">
                        <Loader2 className="w-4 h-4 animate-spin text-teal-500/50" />
                    </div>
                )}
            </Card>
        )
    }

    // Unlimited mode — show current speed only
    return (
        <Card className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-teal-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
            <div className="flex items-center justify-between mb-3 relative z-10">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Bandwidth</p>
                <HiOutlineLightningBolt className="w-4 h-4 text-muted-foreground/30 group-hover:text-teal-400 transition-colors" />
            </div>
            <div className="flex flex-col gap-2.5 mt-2">
                <div className="flex justify-between items-center text-xs">
                    <span className="text-muted-foreground font-medium">Upload</span>
                    <span className="font-mono font-bold text-foreground text-[13px]">
                        {formatSpeed(stats?.up_speed || 0)}
                    </span>
                </div>
                <div className="flex justify-between items-center text-xs">
                    <span className="text-muted-foreground font-medium">Download</span>
                    <span className="font-mono font-bold text-foreground text-[13px]">
                        {formatSpeed(stats?.down_speed || 0)}
                    </span>
                </div>
                <div className="flex justify-between items-center text-xs border-t border-white/5 pt-2.5 mt-0.5">
                    <span className="text-muted-foreground font-medium">Limit</span>
                    <span className="font-mono text-[13px] text-muted-foreground/70">Unlimited</span>
                </div>
            </div>
            {isLoading && (
                <div className="absolute top-4 right-4">
                    <Loader2 className="w-4 h-4 animate-spin text-teal-500/50" />
                </div>
            )}
        </Card>
    )
}

// Compact mobile variant
export function BandwidthUtilizationCompact({ node, stats, isLoading }: BandwidthUtilizationProps) {
    const bwEnabled = node.bandwidth_settings?.enabled
    const totalBwMbps = node.bandwidth_settings?.total_bw || 0
    const currentSpeedBytes = (stats?.up_speed || 0) + (stats?.down_speed || 0)
    const currentSpeedMbps = (currentSpeedBytes * 8) / 1_000_000
    const utilPct = totalBwMbps > 0 ? Math.min((currentSpeedMbps / totalBwMbps) * 100, 100) : 0

    return (
        <Card className="h-full relative overflow-hidden transition-shadow duration-300 rounded-2xl p-3 bg-card/50 backdrop-blur-sm border-white/5">
            <div className="flex items-center justify-between mb-2 relative z-10">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.12em]">Bandwidth</p>
                <HiOutlineLightningBolt className="w-3.5 h-3.5 text-muted-foreground/30" />
            </div>
            {bwEnabled && totalBwMbps > 0 ? (
                <div className="flex flex-col gap-2">
                    <div className="flex items-center gap-2">
                        <div className="flex-1 h-2 rounded-full bg-muted/30 overflow-hidden">
                            <div
                                className={`h-full rounded-full transition-all duration-700 ease-out ${getUtilBarColor(utilPct)}`}
                                style={{ width: `${utilPct}%` }}
                            />
                        </div>
                        <span className={`font-mono font-bold text-xs tabular-nums ${getUtilTextColor(utilPct)}`}>
                            {utilPct.toFixed(0)}%
                        </span>
                    </div>
                    <div className="flex justify-between items-center text-xs">
                        <span className="text-muted-foreground font-medium">Speed</span>
                        <span className="font-mono font-bold text-foreground text-[11px]">
                            {currentSpeedMbps.toFixed(1)} / {totalBwMbps} Mbps
                        </span>
                    </div>
                </div>
            ) : (
                <div className="flex flex-col gap-2">
                    <div className="flex justify-between items-center text-xs">
                        <span className="text-muted-foreground font-medium">Up</span>
                        <span className="font-mono font-bold text-foreground text-[11px]">{formatSpeed(stats?.up_speed || 0)}</span>
                    </div>
                    <div className="flex justify-between items-center text-xs">
                        <span className="text-muted-foreground font-medium">Down</span>
                        <span className="font-mono font-bold text-foreground text-[11px]">{formatSpeed(stats?.down_speed || 0)}</span>
                    </div>
                    <div className="text-[10px] text-muted-foreground/50 text-center">Unlimited</div>
                </div>
            )}
            {isLoading && (
                <div className="absolute top-3 right-3">
                    <Loader2 className="w-3 h-3 animate-spin text-teal-500/50" />
                </div>
            )}
        </Card>
    )
}
