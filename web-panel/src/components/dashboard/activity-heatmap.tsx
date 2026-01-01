import { useMemo } from "react"
import { WidgetWrapper } from "./widget-wrapper"
import { cn } from "@/lib/utils"
import { Grid3x3 } from "lucide-react"
import { useUserActivityHeatmap } from "@/lib/queries"

interface ActivityHeatmapProps {
    isEditMode?: boolean
}

function getIntensityColor(value: number, max: number): string {
    if (max === 0 || value === 0) return "bg-muted/30"
    const ratio = value / max
    if (ratio > 0.75) return "bg-emerald-500"
    if (ratio > 0.5) return "bg-emerald-400/80"
    if (ratio > 0.25) return "bg-emerald-400/50"
    if (ratio > 0) return "bg-emerald-400/25"
    return "bg-muted/30"
}

export function ActivityHeatmap({ isEditMode }: ActivityHeatmapProps) {
    const { data: heatmapData } = useUserActivityHeatmap()

    // Generate 7 days x 24 hours grid
    const grid = useMemo(() => {
        const days = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
        const hours = Array.from({ length: 24 }, (_, i) => i)

        if (!heatmapData) {
            // No data - show empty grid
            return { days, hours, cells: days.map(() => hours.map(() => 0)), max: 0 }
        }

        // Fill from API data (hour-based counts)
        const cells = days.map(() =>
            hours.map((h) => {
                const entry = heatmapData.find((d) => d.hour === h)
                return entry?.count ?? 0
            })
        )
        const max = Math.max(...cells.flat(), 1)
        return { days, hours, cells, max }
    }, [heatmapData])

    return (
        <WidgetWrapper
            title="Activity Heatmap"
            icon={<Grid3x3 className="w-4 h-4 text-emerald-500" />}
            isEditMode={isEditMode}
            headerRight={
                <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                    <span>Less</span>
                    <div className="flex gap-0.5">
                        <div className="w-2.5 h-2.5 rounded-sm bg-muted/30" />
                        <div className="w-2.5 h-2.5 rounded-sm bg-emerald-400/25" />
                        <div className="w-2.5 h-2.5 rounded-sm bg-emerald-400/50" />
                        <div className="w-2.5 h-2.5 rounded-sm bg-emerald-400/80" />
                        <div className="w-2.5 h-2.5 rounded-sm bg-emerald-500" />
                    </div>
                    <span>More</span>
                </div>
            }
        >
            {!heatmapData ? (
                <div className="flex items-center justify-center h-full min-h-[150px] text-sm text-muted-foreground">
                    <div className="text-center">
                        <Grid3x3 className="w-8 h-8 mx-auto mb-2 opacity-30" />
                        <p>No activity data available</p>
                        <p className="text-[10px] mt-1">Heatmap will populate when the API provides data</p>
                    </div>
                </div>
            ) : (
                <div className="space-y-1">
                    {/* Hour labels */}
                    <div className="flex gap-[2px] ml-8">
                        {grid.hours.map((h) => (
                            <div key={h} className="flex-1 text-center">
                                {h % 3 === 0 && (
                                    <span className="text-[8px] text-muted-foreground">{h}</span>
                                )}
                            </div>
                        ))}
                    </div>
                    {/* Grid rows */}
                    {grid.days.map((day, dayIdx) => (
                        <div key={day} className="flex items-center gap-1">
                            <span className="text-[9px] text-muted-foreground w-7 text-right shrink-0">{day}</span>
                            <div className="flex gap-[2px] flex-1">
                                {grid.hours.map((_, hourIdx) => (
                                    <div
                                        key={hourIdx}
                                        className={cn(
                                            "flex-1 aspect-square rounded-sm transition-colors min-w-[6px]",
                                            getIntensityColor(grid.cells[dayIdx][hourIdx], grid.max)
                                        )}
                                        title={`${day} ${hourIdx}:00 — ${grid.cells[dayIdx][hourIdx]} connections`}
                                    />
                                ))}
                            </div>
                        </div>
                    ))}
                </div>
            )}
        </WidgetWrapper>
    )
}
