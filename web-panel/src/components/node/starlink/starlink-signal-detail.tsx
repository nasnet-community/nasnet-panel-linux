import { Badge } from "@/components/ui/badge"
import { CircularProgress } from "@/components/ui/circular-progress"
import type { StarlinkStatus } from "@/lib/types"
import { clearanceGaugeColor, formatUptime } from "./starlink-helpers"

interface StarlinkSignalDetailProps {
    status: StarlinkStatus
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex justify-between items-center text-sm">
            <span className="text-muted-foreground font-medium">{label}</span>
            <span className="font-mono font-bold text-sm">{children}</span>
        </div>
    )
}

function SectionHeader({ title }: { title: string }) {
    return (
        <div className="border-t border-white/5 pt-4 mt-2">
            <p className="text-xs uppercase font-bold text-muted-foreground/60 tracking-[0.15em] mb-2">{title}</p>
        </div>
    )
}

export function StarlinkSignalDetail({ status }: StarlinkSignalDetailProps) {
    const clearPct = (1 - status.obstruction_fraction) * 100

    return (
        <div className="space-y-3">
            {/* Gauge */}
            <div className="flex flex-col items-center gap-2">
                <CircularProgress value={clearPct} size={100} strokeWidth={6} color={clearanceGaugeColor(clearPct)} showValue={false}>
                    <span className="text-2xl font-bold tracking-tight">{clearPct.toFixed(1)}%</span>
                    <span className="text-xs uppercase text-muted-foreground tracking-wider">Clear Sky</span>
                </CircularProgress>
                <div className="flex gap-2">
                    {status.is_snr_above_noise_floor && !status.is_snr_persistently_low ? (
                        <Badge variant="outline" className="text-emerald-400 border-emerald-400/30 bg-emerald-400/5 text-xs">SNR Good</Badge>
                    ) : status.is_snr_persistently_low ? (
                        <Badge variant="outline" className="text-red-400 border-red-400/30 bg-red-400/5 text-xs">Low SNR</Badge>
                    ) : (
                        <Badge variant="outline" className="text-amber-400 border-amber-400/30 bg-amber-400/5 text-xs">SNR Fair</Badge>
                    )}
                    {status.currently_obstructed && (
                        <Badge variant="outline" className="text-red-400 border-red-400/30 bg-red-400/5 text-xs">Obstructed</Badge>
                    )}
                </div>
            </div>

            {/* Device Info */}
            <div className="space-y-2.5">
                <DetailRow label="Ethernet">{status.eth_speed_mbps >= 1000 ? `${(status.eth_speed_mbps / 1000).toFixed(0)} Gbps` : `${status.eth_speed_mbps} Mbps`}</DetailRow>
                <DetailRow label="Uptime">{formatUptime(status.uptime_s)}</DetailRow>
                <DetailRow label="Boot Count">{status.boot_count}</DetailRow>
                <DetailRow label="GPS">
                    <div className="flex items-center gap-2">
                        <Badge variant={status.gps_valid ? "outline" : "danger"} className={`text-xs ${status.gps_valid ? "text-emerald-400 border-emerald-400/30" : ""}`}>
                            {status.gps_valid ? "Valid" : "Invalid"}
                        </Badge>
                        <span className="text-sm font-mono">{status.gps_sats} sats</span>
                    </div>
                </DetailRow>
            </div>

            {/* Alignment */}
            <SectionHeader title="Alignment" />
            <div className="space-y-2.5">
                <DetailRow label="Tilt">{status.tilt_angle_deg.toFixed(1)}&deg;</DetailRow>
                <DetailRow label="Azimuth">{status.boresight_azimuth_deg.toFixed(1)}&deg;</DetailRow>
                <DetailRow label="Elevation">{status.boresight_elevation_deg.toFixed(1)}&deg;</DetailRow>
            </div>

            {/* Connectivity */}
            <SectionHeader title="Connectivity" />
            <div className="space-y-2.5">
                <DetailRow label="Mobility">{status.mobility_class || "Unknown"}</DetailRow>
                <DetailRow label="Service">{status.class_of_service || "Unknown"}</DetailRow>
                <DetailRow label="Cell / Sat / GW">{status.cell_id} / {status.satellite_id} / {status.gateway_id}</DetailRow>
                <DetailRow label="Backup Beam">
                    <Badge variant={status.on_backup_beam ? "danger" : "outline"} className="text-xs">
                        {status.on_backup_beam ? "Yes" : "No"}
                    </Badge>
                </DetailRow>
                {status.disablement_code && status.disablement_code !== "OKAY" && (
                    <DetailRow label="Disablement"><Badge variant="danger" className="text-xs">{status.disablement_code}</Badge></DetailRow>
                )}
            </div>

            {/* Location & Device */}
            <SectionHeader title="Location & Device" />
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
