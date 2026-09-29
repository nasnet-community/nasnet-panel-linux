import { Badge } from "@/components/ui/badge"
import { CircularProgress } from "@/components/ui/circular-progress"
import type { StarlinkObstructionMap, StarlinkStatus } from "@/lib/types"
import {
    attitudeStateLabel, attitudeStateTone, bearingToCardinal, clearanceGaugeColor, hasAlignmentTelemetry,
} from "./starlink-helpers"
import { SkyDisc, useSkyMapStats } from "./starlink-obstruction-map"
import { KeyValue, StarlinkCard, T } from "./starlink-ui"

interface StarlinkSkyCardProps {
    status: StarlinkStatus
    mapData?: StarlinkObstructionMap
    onClick: () => void
}

const TONE_TEXT = { emerald: "text-emerald-400", amber: "text-amber-400", red: "text-red-400" } as const

// Sky & Signal and Alignment were two half-empty cards. One card now: the
// map as the picture, clear sky as the number, pointing as the facts. The
// dials live in the drawer.
export function StarlinkSkyCard({ status, mapData, onClick }: StarlinkSkyCardProps) {
    const clearPct = (1 - status.obstruction_fraction) * 100
    const hasMap = !!mapData && mapData.num_rows > 0 && mapData.num_cols > 0 && !!mapData.snr?.length
    const stats = useSkyMapStats(hasMap ? mapData : undefined)
    const telemetry = hasAlignmentTelemetry(status)
    const tone = clearanceGaugeColor(clearPct)

    return (
        <StarlinkCard title="Sky" onClick={onClick} ariaLabel={`Sky. ${clearPct.toFixed(1)}% clear. Open map and alignment.`}>
            <div className="flex flex-col items-center gap-4 sm:flex-row lg:flex-col 2xl:flex-row">
                <div className="shrink-0">
                    {hasMap ? (
                        <SkyDisc
                            data={{ ...mapData, boresight_azimuth_deg: status.boresight_azimuth_deg, attitude_estimation_state: status.attitude_estimation_state }}
                            size={150}
                            detail="thumb"
                        />
                    ) : (
                        <CircularProgress value={clearPct} size={110} strokeWidth={6} color={tone} showValue={false}>
                            <span className="text-sm font-bold tabular-nums">{clearPct.toFixed(1)}%</span>
                        </CircularProgress>
                    )}
                </div>
                <div className="flex w-full min-w-0 flex-col gap-2">
                    <p className="flex items-baseline gap-1.5">
                        <span className={`${T.value} ${TONE_TEXT[tone]}`}>{clearPct.toFixed(1)}</span>
                        <span className={T.unit}>% clear</span>
                    </p>
                    <p className={`${T.meta} -mt-1.5`}>
                        {hasMap ? `${stats.mappedPct.toFixed(0)}% of the sky mapped` : "Map loading…"}
                    </p>
                    <KeyValue label="Obstructed">
                        {status.currently_obstructed ? <span className="text-red-400">Now</span> : <span className="text-muted-foreground">No</span>}
                    </KeyValue>
                    <KeyValue label="SNR">
                        {status.is_snr_persistently_low
                            ? <Badge variant="outline" className="border-red-400/30 bg-red-400/5 px-2 py-0 text-[12px] text-red-400">Low</Badge>
                            : status.is_snr_above_noise_floor
                                ? <Badge variant="outline" className="border-emerald-400/30 bg-emerald-400/5 px-2 py-0 text-[12px] text-emerald-400">Good</Badge>
                                : <Badge variant="outline" className="border-amber-400/30 bg-amber-400/5 px-2 py-0 text-[12px] text-amber-400">Fair</Badge>}
                    </KeyValue>
                    <KeyValue label="Tilt">{status.tilt_angle_deg.toFixed(1)}°</KeyValue>
                    <KeyValue label="Azimuth">
                        {status.boresight_azimuth_deg.toFixed(0)}° {bearingToCardinal(status.boresight_azimuth_deg)}
                        {telemetry && <span className="text-muted-foreground"> → {status.desired_boresight_azimuth_deg.toFixed(0)}°</span>}
                    </KeyValue>
                    <KeyValue label="Elevation">
                        {status.boresight_elevation_deg.toFixed(0)}°
                        {telemetry && <span className="text-muted-foreground"> → {status.desired_boresight_elevation_deg.toFixed(0)}°</span>}
                    </KeyValue>
                    {telemetry && (
                        <KeyValue label="Attitude">
                            <span className={TONE_TEXT[attitudeStateTone(status.attitude_estimation_state)]}>{attitudeStateLabel(status.attitude_estimation_state)}</span>
                            {status.attitude_uncertainty_deg > 0 && <span className="text-muted-foreground"> ±{status.attitude_uncertainty_deg.toFixed(1)}°</span>}
                        </KeyValue>
                    )}
                </div>
            </div>
        </StarlinkCard>
    )
}
