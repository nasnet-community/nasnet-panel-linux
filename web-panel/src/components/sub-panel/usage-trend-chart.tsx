import { useState, useMemo } from "react"
import { useReducedMotion } from "framer-motion"
import { BarChart, Bar, XAxis, Tooltip, ResponsiveContainer } from "recharts"
import { Card, CardContent } from "@/components/ui/card"
import { TrendingUp } from "lucide-react"
import { cn } from "@/lib/utils"
import { useUsageTrend } from "@/lib/queries/use-usage-trend"
import type { UsageTrendRange } from "@/lib/types/sub-panel"
import { buildChartPoints, UNIT_FACTOR, type ChartPoint } from "./usage-trend-transforms"
import { useChartPalette } from "@/lib/design/palette"

type Props = { uuid: string }

export function UsageTrendChart({ uuid }: Props) {
  const c = useChartPalette()
  const UPLOAD_COLOR = c.chart5
  const DOWNLOAD_COLOR = c.success
  const LEGACY_COLOR = c.neutral
  const [range, setRange] = useState<UsageTrendRange>("7d")
  const reducedMotion = useReducedMotion()
  const { data, isLoading, isFetching, error } = useUsageTrend(uuid, range)

  const rangeDays = range === "7d" ? 7 : 30
  const points = useMemo(() => {
    if (!data) return []
    return buildChartPoints(data.points, rangeDays)
  }, [data, rangeDays])

  const hasLegacy = points.some(p => p.isLegacy)
  const unit = data?.unit_hint ?? "MB"

  return (
    <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0 mt-3 md:mt-4">
      <CardContent className="p-3.5 sm:p-4 md:p-5">
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center gap-2">
            <TrendingUp className="w-4 h-4 text-emerald-400" />
            <h3 className="text-xs md:text-sm font-medium text-muted-foreground uppercase tracking-wider">
              Traffic Usage
            </h3>
          </div>
          <RangeToggle range={range} onChange={setRange} />
        </div>

        <ChartBody
          loading={isLoading}
          refetching={isFetching && !isLoading}
          error={Boolean(error)}
          points={points}
          unit={unit}
          reducedMotion={Boolean(reducedMotion)}
          rangeDays={rangeDays}
        />

        <div className="flex items-center gap-4 mt-2 text-[10px] text-muted-foreground/70">
          <span className="flex items-center gap-1.5">
            <span className="inline-block w-2.5 h-2.5 rounded-sm" style={{
              background: `repeating-linear-gradient(45deg, ${UPLOAD_COLOR}, ${UPLOAD_COLOR} 1.5px, rgba(0,0,0,0.45) 1.5px, rgba(0,0,0,0.45) 3px)`,
            }} />
            ▲ Upload
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block w-2.5 h-2.5 rounded-sm" style={{ background: DOWNLOAD_COLOR }} />
            ▼ Download
          </span>
          {hasLegacy && <LegendDot color={LEGACY_COLOR} label="split unavailable" />}
        </div>
      </CardContent>
    </Card>
  )
}

function RangeToggle({ range, onChange }: { range: UsageTrendRange; onChange: (r: UsageTrendRange) => void }) {
  return (
    <div className="inline-flex rounded-md border border-border/50 p-0.5 text-[11px]" role="tablist" aria-label="Time range">
      {(["7d", "30d"] as const).map(r => (
        <button
          key={r}
          type="button"
          role="tab"
          aria-selected={range === r}
          onClick={() => onChange(r)}
          className={cn(
            "px-2 py-0.5 rounded-[5px] transition-colors",
            range === r ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground",
          )}
        >
          {r}
        </button>
      ))}
    </div>
  )
}

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <span className="flex items-center gap-1.5">
      <span className="inline-block w-2.5 h-2.5 rounded-sm" style={{ background: color }} />
      {label}
    </span>
  )
}

// theme-aware axis tick — uses CSS class instead of hardcoded fill
function AxisTick({ x, y, payload }: { x?: number; y?: number; payload?: { value: string } }) {
  return (
    <text x={x} y={y} dy={12} textAnchor="middle" className="fill-muted-foreground" style={{ fontSize: 10 }}>
      {payload ? formatAxisDate(payload.value) : ""}
    </text>
  )
}

function ChartBody({
  loading, refetching, error, points, unit, reducedMotion, rangeDays,
}: {
  loading: boolean
  refetching: boolean
  error: boolean
  points: ChartPoint[]
  unit: "KB" | "MB" | "GB"
  reducedMotion: boolean
  rangeDays: number
}) {
  const c = useChartPalette()
  const UPLOAD_COLOR = c.chart5
  const DOWNLOAD_COLOR = c.success
  const LEGACY_COLOR = c.neutral
  if (loading) {
    return <div role="status" aria-label="Loading traffic chart" className="h-[140px] rounded-md bg-muted/30 animate-pulse" />
  }
  if (error) {
    return <p className="h-[140px] flex items-center justify-center text-xs text-muted-foreground/70">Trend unavailable</p>
  }
  if (points.length === 0 || points.every(p => p.total === 0)) {
    return <p className="h-[140px] flex items-center justify-center text-xs text-muted-foreground/70">No traffic recorded yet</p>
  }

  // recharts: interval 0 = show every tick; non-zero skips that many between ticks.
  const tickInterval = rangeDays === 30 ? 4 : 0

  return (
    <div className="text-muted-foreground/40" style={{ opacity: refetching ? 0.6 : 1, transition: "opacity 150ms" }}>
      <ResponsiveContainer width="100%" height={140}>
        <BarChart data={points} margin={{ top: 8, right: 0, left: 0, bottom: 0 }}>
          <defs>
            <pattern id="sub-upload-hatch" patternUnits="userSpaceOnUse" width="5" height="5" patternTransform="rotate(45)">
              <rect width="5" height="5" fill={UPLOAD_COLOR} />
              <line x1="0" y1="0" x2="0" y2="5" stroke="rgba(0,0,0,0.45)" strokeWidth="2.5" />
            </pattern>
          </defs>
          <XAxis
            dataKey="date"
            tick={<AxisTick />}
            axisLine={false}
            tickLine={false}
            interval={tickInterval}
          />
          <Tooltip content={<ChartTooltip unit={unit} />} cursor={{ fill: "currentColor", fillOpacity: 0.06 }} />
          <Bar dataKey="download" stackId="t" fill={DOWNLOAD_COLOR} isAnimationActive={!reducedMotion} />
          <Bar dataKey="upload" stackId="t" fill="url(#sub-upload-hatch)" isAnimationActive={!reducedMotion} radius={[2, 2, 0, 0]} />
          <Bar dataKey="legacyTotal" stackId="t" fill={LEGACY_COLOR} isAnimationActive={!reducedMotion} radius={[2, 2, 0, 0]} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

function formatAxisDate(iso: string): string {
  const d = new Date(iso + "T00:00:00Z")
  return d.toLocaleDateString(undefined, { month: "numeric", day: "numeric" })
}

type TooltipProps = {
  unit: "KB" | "MB" | "GB"
  active?: boolean
  payload?: Array<{ payload: ChartPoint }>
}

function ChartTooltip({ unit, active, payload }: TooltipProps) {
  const c = useChartPalette()
  const UPLOAD_COLOR = c.chart5
  const DOWNLOAD_COLOR = c.success
  if (!active || !payload || !payload.length) return null
  const row = payload[0].payload
  const localDate = new Date(row.date + "T00:00:00Z").toLocaleDateString(undefined, { month: "short", day: "numeric" })

  if (row.total === 0) {
    return (
      <div className="rounded-md border border-border/50 bg-background/95 px-2.5 py-1.5 text-[11px] shadow-md">
        <div className="font-medium">{localDate}</div>
        <div className="text-muted-foreground">No traffic</div>
      </div>
    )
  }

  if (row.isLegacy) {
    return (
      <div className="rounded-md border border-border/50 bg-background/95 px-2.5 py-1.5 text-[11px] shadow-md">
        <div className="font-medium">{localDate}</div>
        <div>Total {formatValue(row.total, unit)}</div>
        <div className="text-muted-foreground/70">Split not tracked for this day</div>
      </div>
    )
  }

  return (
    <div className="rounded-md border border-border/50 bg-background/95 px-2.5 py-1.5 text-[11px] shadow-md space-y-0.5">
      <div className="font-medium">{localDate}</div>
      <div className="flex items-center gap-2"><span style={{ color: UPLOAD_COLOR }}>▲</span>Upload {formatValue(row.upload, unit)}</div>
      <div className="flex items-center gap-2"><span style={{ color: DOWNLOAD_COLOR }}>▼</span>Download {formatValue(row.download, unit)}</div>
      <div className="text-muted-foreground">Total {formatValue(row.total, unit)}</div>
    </div>
  )
}

function formatValue(bytes: number, unit: "KB" | "MB" | "GB"): string {
  const value = bytes / UNIT_FACTOR[unit]
  return `${value.toFixed(2)} ${unit}`
}
