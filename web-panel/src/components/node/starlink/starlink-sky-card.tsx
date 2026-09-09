import { Badge } from "@/components/ui/badge"
import { CircularProgress } from "@/components/ui/circular-progress"
import type { StarlinkObstructionMap, StarlinkStatus } from "@/lib/types"
import { clearanceGaugeColor } from "./starlink-helpers"
import { SkyDisc, useSkyMapStats } from "./starlink-obstruction-map"
import { KeyValue, StarlinkCard, T } from "./starlink-ui"

interface StarlinkSkyCardProps {
    status: StarlinkStatus
    mapData?: StarlinkObstructionMap
    onClick: () => void
}

/**
 * Signal & Dish and Obstruction Map used to be two cards side by side, and
 * between them they printed the clear-sky figure three times — once in a
 * ring, once as "99.3% Clear", once as "0.7% Obstructed" — from two different
 * sources that disagreed (status.obstruction_fraction vs. a cell count). One
 * card, one headline number, and the map earns its place as a real
 * thumbnail instead of the decorative compass glyph that sat there before.
 */
export function StarlinkSkyCard({ status, mapData, onClick }: StarlinkSkyCardProps) {
    const clearPct = (1 - status.obstruction_fraction) * 100
    const hasMap = !!mapData && mapData.num_rows > 0 && mapData.num_cols > 0 && !!mapData.snr?.length
    const stats = useSkyMapStats(hasMap ? mapData : undefined)

    return (
        <StarlinkCard
            title="Sky & Signal"
            onClick={onClick}
            ariaLabel={`Sky and signal. ${clearPct.toFixed(1)}% clear sky. Open details.`}
        >
            <div className="flex items-center gap-4 sm:gap-5">
                <div className="flex shrink-0 flex-col items-center gap-1.5">
                    <CircularProgress
                        value={clearPct}
                        size={68}
                        strokeWidth={5}
                        color={clearanceGaugeColor(clearPct)}
                        showValue={false}
                    >
                        <span className="text-sm font-bold tracking-tight tabular-nums">{clearPct.toFixed(1)}%</span>
                    </CircularProgress>
                    <span className={T.meta}>clear sky</span>
                </div>

                {hasMap && (
                    <div className="flex shrink-0 flex-col items-center gap-1.5">
                        <SkyDisc
                            data={{
                                ...mapData,
                                boresight_azimuth_deg: status.boresight_azimuth_deg,
                                attitude_estimation_state: status.attitude_estimation_state,
                            }}
                            size={84}
                            detail="thumb"
                        />
                        <span className={T.meta}>{stats.mappedPct.toFixed(0)}% mapped</span>
                    </div>
                )}

                <div className="flex min-w-0 flex-1 flex-col gap-2">
                    <KeyValue label="SNR">
                        {status.is_snr_above_noise_floor && !status.is_snr_persistently_low ? (
                            <Badge variant="outline" className="border-emerald-400/30 bg-emerald-400/5 px-2 py-0 text-[11px] text-emerald-400">Good</Badge>
                        ) : status.is_snr_persistently_low ? (
                            <Badge variant="outline" className="border-red-400/30 bg-red-400/5 px-2 py-0 text-[11px] text-red-400">Low</Badge>
                        ) : (
                            <Badge variant="outline" className="border-amber-400/30 bg-amber-400/5 px-2 py-0 text-[11px] text-amber-400">Fair</Badge>
                        )}
                    </KeyValue>
                    <KeyValue label="GPS">
                        {status.gps_valid ? `${status.gps_sats} sats` : "Invalid"}
                    </KeyValue>
                    <KeyValue label="Ethernet">
                        {status.eth_speed_mbps >= 1000
                            ? `${(status.eth_speed_mbps / 1000).toFixed(0)} Gbps`
                            : `${status.eth_speed_mbps} Mbps`}
                    </KeyValue>
                    <KeyValue label="Obstructed">
                        {status.currently_obstructed
                            ? <span className="text-red-400">Now</span>
                            : <span className="text-muted-foreground">No</span>}
                    </KeyValue>
                </div>
            </div>
        </StarlinkCard>
    )
}
