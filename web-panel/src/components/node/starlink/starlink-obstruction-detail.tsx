import type { StarlinkObstructionMap, StarlinkStatus } from "@/lib/types"
import { formatDurationSecs } from "./starlink-helpers"
import { StarlinkObstructionMapView } from "./starlink-obstruction-map"
import { KeyValue } from "./starlink-ui"

interface StarlinkObstructionDetailProps {
    status: StarlinkStatus
    mapData: StarlinkObstructionMap
}

function frameLabel(frame: string): string {
    switch (frame) {
        case "FRAME_EARTH": return "Earth-aligned"
        case "FRAME_UT": return "Dish-aligned"
        default: return "Unknown frame"
    }
}

export function StarlinkObstructionDetail({ status, mapData }: StarlinkObstructionDetailProps) {
    // Empty / unreachable map → backend returns NumRows=0; nothing to render.
    const hasMap = mapData && mapData.num_rows > 0 && mapData.num_cols > 0 && mapData.snr && mapData.snr.length > 0

    return (
        <div className="space-y-4">
            {hasMap ? (
                <StarlinkObstructionMapView
                    data={{
                        ...mapData,
                        boresight_azimuth_deg: status.boresight_azimuth_deg,
                        attitude_estimation_state: status.attitude_estimation_state,
                    }}
                    compact={false}
                />
            ) : (
                <div className="flex items-center justify-center h-[280px] text-sm text-muted-foreground rounded-2xl border-2 border-dashed border-border">
                    No obstruction map available
                </div>
            )}
            <div className="space-y-2.5 border-t border-border pt-4">
                <KeyValue label="Avg prolonged duration">{formatDurationSecs(status.avg_prolonged_obstruction_duration_s)}</KeyValue>
                <KeyValue label="Avg prolonged interval">{formatDurationSecs(status.avg_prolonged_obstruction_interval_s)}</KeyValue>
                {hasMap && (
                    <>
                        <KeyValue label="Reference frame">{frameLabel(mapData.reference_frame)}</KeyValue>
                        {mapData.max_theta_deg > 0 && (
                            <KeyValue label="Field of view">&plusmn;{mapData.max_theta_deg.toFixed(0)}&deg; from zenith</KeyValue>
                        )}
                        <KeyValue label="Grid">{mapData.num_cols}&times;{mapData.num_rows}</KeyValue>
                    </>
                )}
            </div>
        </div>
    )
}
