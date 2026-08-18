import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { CircularProgress } from "@/components/ui/circular-progress"
import { Satellite } from "lucide-react"
import type { StarlinkStatus } from "@/lib/types"
import { clearanceGaugeColor } from "./starlink-helpers"

interface StarlinkSignalCardProps {
    status: StarlinkStatus
    onClick: () => void
}

export function StarlinkSignalCard({ status, onClick }: StarlinkSignalCardProps) {
    const clearPct = (1 - status.obstruction_fraction) * 100

    return (
        <Card
            role="button"
            tabIndex={0}
            aria-label={`Signal and Dish. ${clearPct.toFixed(0)}% clear. Open details.`}
            className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-purple-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5 cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={onClick}
            onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick() } }}
        >
            <div className="flex items-center justify-between mb-3">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Signal & Dish</p>
                <span className="text-[10px] text-muted-foreground/40 group-hover:text-muted-foreground/70 transition-colors">details &rarr;</span>
            </div>
            <div className="flex items-center gap-4">
                <CircularProgress value={clearPct} size={60} strokeWidth={5} color={clearanceGaugeColor(clearPct)} showValue={false}>
                    <span className="text-sm font-bold tracking-tight">{clearPct.toFixed(0)}%</span>
                    <span className="text-[7px] uppercase text-muted-foreground tracking-wider">Clear</span>
                </CircularProgress>
                <div className="flex flex-col gap-1.5 min-w-0">
                    <div className="flex gap-1.5">
                        {status.is_snr_above_noise_floor && !status.is_snr_persistently_low ? (
                            <Badge variant="outline" className="text-emerald-400 border-emerald-400/30 bg-emerald-400/5 text-[10px]">SNR Good</Badge>
                        ) : status.is_snr_persistently_low ? (
                            <Badge variant="outline" className="text-red-400 border-red-400/30 bg-red-400/5 text-[10px]">Low SNR</Badge>
                        ) : (
                            <Badge variant="outline" className="text-amber-400 border-amber-400/30 bg-amber-400/5 text-[10px]">SNR Fair</Badge>
                        )}
                    </div>
                    <span className="text-[11px] text-muted-foreground">
                        GPS {status.gps_valid ? "Valid" : "Invalid"} &middot; {status.gps_sats} sats
                    </span>
                    <span className="text-[11px] text-muted-foreground">
                        Eth {status.eth_speed_mbps >= 1000 ? `${(status.eth_speed_mbps / 1000).toFixed(0)} Gbps` : `${status.eth_speed_mbps} Mbps`}{" "}
                        &middot; Tilt {status.tilt_angle_deg.toFixed(1)}&deg;
                    </span>
                </div>
            </div>
        </Card>
    )
}
