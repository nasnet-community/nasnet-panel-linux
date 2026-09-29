import { Badge } from "@/components/ui/badge"
import { CircularProgress } from "@/components/ui/circular-progress"
import type { StarlinkStatus } from "@/lib/types"
import { clearanceGaugeColor } from "./starlink-helpers"
import { T } from "./starlink-ui"

interface StarlinkSignalDetailProps {
    status: StarlinkStatus
}

/** Headline clear-sky reading — the first thing in the Sky & Signal drawer. */
export function StarlinkSignalGauge({ status }: StarlinkSignalDetailProps) {
    const clearPct = (1 - status.obstruction_fraction) * 100

    return (
        <div className="flex flex-col items-center gap-2">
            <CircularProgress value={clearPct} size={100} strokeWidth={6} color={clearanceGaugeColor(clearPct)} showValue={false}>
                <span className="text-2xl font-bold tracking-tight tabular-nums">{clearPct.toFixed(1)}%</span>
                <span className={T.meta}>clear sky</span>
            </CircularProgress>
            <div className="flex gap-2">
                {status.is_snr_above_noise_floor && !status.is_snr_persistently_low ? (
                    <Badge variant="outline" className="border-emerald-400/30 bg-emerald-400/5 text-xs text-emerald-400">SNR good</Badge>
                ) : status.is_snr_persistently_low ? (
                    <Badge variant="outline" className="border-red-400/30 bg-red-400/5 text-xs text-red-400">Low SNR</Badge>
                ) : (
                    <Badge variant="outline" className="border-amber-400/30 bg-amber-400/5 text-xs text-amber-400">SNR fair</Badge>
                )}
                {status.currently_obstructed && (
                    <Badge variant="outline" className="border-red-400/30 bg-red-400/5 text-xs text-red-400">Obstructed</Badge>
                )}
            </div>
        </div>
    )
}
