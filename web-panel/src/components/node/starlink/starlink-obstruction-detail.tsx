import type { StarlinkObstructionMap, StarlinkStatus } from "@/lib/types"
import { StarlinkObstructionMapView } from "./starlink-obstruction-map"

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
                <div className="flex items-center justify-center h-[280px] text-sm text-muted-foreground border-2 border-dashed border-white/5 rounded-2xl">
                    No obstruction map available
                </div>
            )}
            <div className="space-y-3 pt-3 border-t border-white/5">
                <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground font-medium">Avg Prolonged Duration</span>
                    <span className="font-mono font-bold">{status.avg_prolonged_obstruction_duration_s.toFixed(1)}s</span>
                </div>
                <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground font-medium">Avg Prolonged Interval</span>
                    <span className="font-mono font-bold">{status.avg_prolonged_obstruction_interval_s.toFixed(1)}s</span>
                </div>
                {hasMap && (
                    <>
                        <div className="flex justify-between text-sm">
                            <span className="text-muted-foreground font-medium">Reference Frame</span>
                            <span className="font-mono font-bold">{frameLabel(mapData.reference_frame)}</span>
                        </div>
                        {mapData.max_theta_deg > 0 && (
                            <div className="flex justify-between text-sm">
                                <span className="text-muted-foreground font-medium">Field of View</span>
                                <span className="font-mono font-bold">&plusmn;{mapData.max_theta_deg.toFixed(0)}&deg; from zenith</span>
                            </div>
                        )}
                        <div className="flex justify-between text-sm">
                            <span className="text-muted-foreground font-medium">Grid</span>
                            <span className="font-mono font-bold">{mapData.num_cols}&times;{mapData.num_rows}</span>
                        </div>
                    </>
                )}
            </div>
        </div>
    )
}
