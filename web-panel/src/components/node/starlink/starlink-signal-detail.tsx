import { Badge } from "@/components/ui/badge"
import { CircularProgress } from "@/components/ui/circular-progress"
import type { StarlinkStatus } from "@/lib/types"
import { clearanceGaugeColor, formatUptime } from "./starlink-helpers"
import { T } from "./starlink-ui"

interface StarlinkSignalDetailProps {
    status: StarlinkStatus
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex items-center justify-between gap-3 text-xs leading-4">
            <span className="shrink-0 text-muted-foreground">{label}</span>
            <span className="min-w-0 break-words text-right font-mono font-semibold tabular-nums">{children}</span>
        </div>
    )
}

function SectionHeader({ title }: { title: string }) {
    return (
        <div className="mt-2 border-t border-border pt-4">
            <p className={`${T.eyebrow} mb-2`}>{title}</p>
        </div>
    )
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

/** Everything the dish reports about itself, below the gauge and the map. */
export function StarlinkSignalFacts({ status }: StarlinkSignalDetailProps) {
    return (
        <div className="space-y-3">
            <div className="space-y-2.5">
                <DetailRow label="Ethernet">{status.eth_speed_mbps >= 1000 ? `${(status.eth_speed_mbps / 1000).toFixed(0)} Gbps` : `${status.eth_speed_mbps} Mbps`}</DetailRow>
                <DetailRow label="Uptime">{formatUptime(status.uptime_s)}</DetailRow>
                <DetailRow label="Boot count">{status.boot_count}</DetailRow>
                <DetailRow label="GPS">
                    <span className="flex items-center justify-end gap-2">
                        <Badge variant={status.gps_valid ? "outline" : "danger"} className={`text-xs ${status.gps_valid ? "border-emerald-400/30 text-emerald-400" : ""}`}>
                            {status.gps_valid ? "Valid" : "Invalid"}
                        </Badge>
                        <span>{status.gps_sats} sats</span>
                    </span>
                </DetailRow>
            </div>

            <SectionHeader title="Alignment" />
            <div className="space-y-2.5">
                <DetailRow label="Tilt">{status.tilt_angle_deg.toFixed(1)}&deg;</DetailRow>
                <DetailRow label="Azimuth">{status.boresight_azimuth_deg.toFixed(1)}&deg;</DetailRow>
                <DetailRow label="Elevation">{status.boresight_elevation_deg.toFixed(1)}&deg;</DetailRow>
            </div>

            <SectionHeader title="Connectivity" />
            <div className="space-y-2.5">
                <DetailRow label="Mobility">{status.mobility_class || "Unknown"}</DetailRow>
                <DetailRow label="Service">{status.class_of_service || "Unknown"}</DetailRow>
                <DetailRow label="Cell / sat / GW">{status.cell_id} / {status.satellite_id} / {status.gateway_id}</DetailRow>
                <DetailRow label="Backup beam">
                    <Badge variant={status.on_backup_beam ? "danger" : "outline"} className="text-xs">
                        {status.on_backup_beam ? "Yes" : "No"}
                    </Badge>
                </DetailRow>
                {status.disablement_code && status.disablement_code !== "OKAY" && (
                    <DetailRow label="Disablement"><Badge variant="danger" className="text-xs">{status.disablement_code}</Badge></DetailRow>
                )}
            </div>

            <SectionHeader title="Location & device" />
            <div className="space-y-2.5">
                <DetailRow label="Coordinates">{status.latitude.toFixed(4)}, {status.longitude.toFixed(4)}</DetailRow>
                <DetailRow label="Altitude">{status.altitude.toFixed(1)} m</DetailRow>
                <DetailRow label="Hardware">{status.hardware_version || "—"}</DetailRow>
                <DetailRow label="Software">{status.software_version || "—"}</DetailRow>
                {status.country_code && <DetailRow label="Country">{status.country_code}</DetailRow>}
            </div>
        </div>
    )
}
