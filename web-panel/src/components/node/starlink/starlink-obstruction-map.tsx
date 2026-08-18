import { useRef, useEffect, useMemo, useState } from "react"
import {
    OBSTRUCTION_COLORS,
    classifyObstructionCell,
    obstructionCellColor,
    isAttitudeConverged,
} from "./starlink-helpers"

interface StarlinkObstructionMapViewProps {
    data: {
        snr: (number | null)[]
        num_rows: number
        num_cols: number
        reference_frame?: string
        // Dish boresight azimuth + attitude filter state. Needed to rotate a
        // dish-relative (FRAME_UT) map into compass coordinates.
        boresight_azimuth_deg?: number
        attitude_estimation_state?: string
    }
    compact?: boolean
}

// Reference-frame geometry, straight from the dish API contract:
//   FRAME_EARTH — top-centre cell points at true north, so the grid is
//                 already a plan-view compass rose (right edge = east).
//   FRAME_UT    — bottom-centre cell points along the dish boresight.
//
// So FRAME_UT always needs at least a half turn to put "forward" at the top,
// and a further `boresightAzimuth` turn to land north at the top. The extra
// turn is only applied once the attitude filter has converged; before that
// the reported azimuth is noise and would spin the map at random.
function mapOrientation(data: StarlinkObstructionMapViewProps["data"]) {
    if (data.reference_frame !== "FRAME_UT") {
        return {
            rotationDeg: 0,
            compass: true,
            labels: { top: "N", right: "E", bottom: "S", left: "W" },
        }
    }

    const az = data.boresight_azimuth_deg
    const trusted = typeof az === "number" && Number.isFinite(az) &&
        isAttitudeConverged(data.attitude_estimation_state)

    if (!trusted) {
        return {
            rotationDeg: -180,
            compass: false,
            labels: { top: "Fwd", right: "Stbd", bottom: "Aft", left: "Port" },
        }
    }
    return {
        rotationDeg: az - 180,
        compass: true,
        labels: { top: "N", right: "E", bottom: "S", left: "W" },
    }
}

const LABEL_PAD = 20 // px reserved around the disc for the cardinal labels

export function StarlinkObstructionMapView({ data, compact = false }: StarlinkObstructionMapViewProps) {
    const canvasRef = useRef<HTMLCanvasElement>(null)
    const boxRef = useRef<HTMLDivElement>(null)
    const maxSize = compact ? 240 : 460
    const [cssSize, setCssSize] = useState(0)

    const { rotationDeg, compass, labels } = mapOrientation(data)

    // The canvas is laid out by CSS (width:100%, square) and its bitmap is
    // sized from the measured box. Writing canvas.style.width here instead
    // would clobber the responsive layout and overflow the drawer.
    useEffect(() => {
        const box = boxRef.current
        if (!box) return
        const measure = () => setCssSize(box.clientWidth)
        measure()
        const ro = new ResizeObserver(measure)
        ro.observe(box)
        return () => ro.disconnect()
    }, [])

    useEffect(() => {
        const canvas = canvasRef.current
        if (!canvas || cssSize <= 0) return
        if (!data.snr || data.snr.length === 0) return
        if (!data.num_cols || !data.num_rows) return

        const dpr = typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1
        canvas.width = Math.round(cssSize * dpr)
        canvas.height = Math.round(cssSize * dpr)
        const ctx = canvas.getContext("2d")
        if (!ctx) return
        // Draw in CSS pixels; the transform absorbs the device ratio.
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
        ctx.clearRect(0, 0, cssSize, cssSize)

        const numRows = data.num_rows
        const numCols = data.num_cols
        const center = cssSize / 2
        const ringR = center - 1
        const dataR = ringR - (compact ? 5 : 8)

        // Perimeter ticks — a coarse bearing scale, like the dish's own UI.
        ctx.strokeStyle = "rgba(255,255,255,0.28)"
        for (let deg = 0; deg < 360; deg += 5) {
            const major = deg % 45 === 0
            const len = major ? 7 : 3.5
            const rad = ((deg - 90) * Math.PI) / 180
            ctx.lineWidth = major ? 1.4 : 1
            ctx.globalAlpha = major ? 0.85 : 0.35
            ctx.beginPath()
            ctx.moveTo(center + Math.cos(rad) * (ringR - len), center + Math.sin(rad) * (ringR - len))
            ctx.lineTo(center + Math.cos(rad) * ringR, center + Math.sin(rad) * ringR)
            ctx.stroke()
        }
        ctx.globalAlpha = 1

        // Everything below is clipped to the data disc so rotated cells and
        // guide lines can never bleed past the rim.
        ctx.save()
        ctx.beginPath()
        ctx.arc(center, center, dataR, 0, Math.PI * 2)
        ctx.clip()

        ctx.fillStyle = "rgba(255,255,255,0.02)"
        ctx.fillRect(0, 0, cssSize, cssSize)

        // Elevation rings + cardinal crosshair (frame-independent guides).
        ctx.strokeStyle = "rgba(255,255,255,0.07)"
        ctx.lineWidth = 1
        for (const frac of [0.33, 0.66]) {
            ctx.beginPath()
            ctx.arc(center, center, dataR * frac, 0, Math.PI * 2)
            ctx.stroke()
        }
        ctx.beginPath()
        ctx.moveTo(center, center - dataR)
        ctx.lineTo(center, center + dataR)
        ctx.moveTo(center - dataR, center)
        ctx.lineTo(center + dataR, center)
        ctx.stroke()

        if (rotationDeg !== 0) {
            ctx.translate(center, center)
            ctx.rotate((rotationDeg * Math.PI) / 180)
            ctx.translate(-center, -center)
        }

        // Grid → disc. Cell centres sit at (col,row) offset from the grid
        // midpoint; the extra half cell keeps the disc centred for even and
        // odd grid sizes alike.
        const maxR = Math.min(numRows, numCols) / 2
        const scale = dataR / maxR
        const cellSize = Math.max(scale + 0.5, 1)
        const rowMid = (numRows - 1) / 2
        const colMid = (numCols - 1) / 2

        for (let row = 0; row < numRows; row++) {
            for (let col = 0; col < numCols; col++) {
                const idx = row * numCols + col
                if (idx >= data.snr.length) break
                const dx = col - colMid
                const dy = row - rowMid
                if (Math.sqrt(dx * dx + dy * dy) > maxR) continue

                const cell = classifyObstructionCell(data.snr[idx])
                if (cell === "nodata") continue // leave the unmapped sky bare

                // Row 0 is the top edge of the map in both frames and canvas
                // y also grows downward, so dy is added, not subtracted.
                ctx.fillStyle = obstructionCellColor(data.snr[idx])
                ctx.fillRect(
                    center + dx * scale - cellSize / 2,
                    center + dy * scale - cellSize / 2,
                    cellSize,
                    cellSize,
                )
            }
        }
        ctx.restore()

        // Rim
        ctx.strokeStyle = "rgba(255,255,255,0.14)"
        ctx.lineWidth = 1
        ctx.beginPath()
        ctx.arc(center, center, dataR, 0, Math.PI * 2)
        ctx.stroke()
    }, [data, cssSize, compact, rotationDeg])

    const stats = useMemo(() => {
        const empty = { clearPct: 0, obstructedPct: 0, mappedPct: 0, measured: 0 }
        if (!data.snr || !data.num_cols || !data.num_rows) return empty

        const maxR = Math.min(data.num_rows, data.num_cols) / 2
        const rowMid = (data.num_rows - 1) / 2
        const colMid = (data.num_cols - 1) / 2
        let clear = 0
        let obstructed = 0
        let inDisc = 0

        for (let row = 0; row < data.num_rows; row++) {
            for (let col = 0; col < data.num_cols; col++) {
                const idx = row * data.num_cols + col
                if (idx >= data.snr.length) break
                const dx = col - colMid
                const dy = row - rowMid
                if (Math.sqrt(dx * dx + dy * dy) > maxR) continue
                inDisc++
                const cell = classifyObstructionCell(data.snr[idx])
                if (cell === "clear") clear++
                else if (cell === "obstructed") obstructed++
            }
        }

        const measured = clear + obstructed
        if (measured === 0) return { ...empty, mappedPct: 0 }
        return {
            clearPct: (clear / measured) * 100,
            obstructedPct: (obstructed / measured) * 100,
            mappedPct: inDisc > 0 ? (measured / inDisc) * 100 : 0,
            measured,
        }
    }, [data])

    return (
        <div className="flex flex-col items-center gap-4">
            <div className="relative w-full" style={{ maxWidth: maxSize }}>
                <div ref={boxRef} className="relative w-full aspect-square" style={{ padding: LABEL_PAD }}>
                    <canvas
                        ref={canvasRef}
                        role="img"
                        aria-label={`Sky map, ${compass ? "north up" : "dish forward up"}: ${stats.clearPct.toFixed(1)}% clear, ${stats.obstructedPct.toFixed(1)}% obstructed, ${stats.mappedPct.toFixed(0)}% of the sky mapped`}
                        className="block w-full h-full"
                    />
                </div>
                {/* Cardinal labels live in HTML, outside the disc — canvas text
                    at a fixed offset used to get clipped by the canvas edge. */}
                <span className="absolute top-0 left-1/2 -translate-x-1/2 text-[11px] font-bold text-foreground/80 leading-5">{labels.top}</span>
                <span className="absolute bottom-0 left-1/2 -translate-x-1/2 text-[11px] font-bold text-foreground/80 leading-5">{labels.bottom}</span>
                <span className="absolute left-0 top-1/2 -translate-y-1/2 text-[11px] font-bold text-foreground/80 leading-5">{labels.left}</span>
                <span className="absolute right-0 top-1/2 -translate-y-1/2 text-[11px] font-bold text-foreground/80 leading-5">{labels.right}</span>
            </div>

            {!compass && (
                <p className="text-[10px] text-amber-400/80 text-center -mt-2">
                    Dish-relative view — heading unavailable, so the map cannot be turned to north.
                </p>
            )}

            {/* Legend — the dish reports each cell as blocked or not, so a
                discrete key is the honest representation. */}
            <div className="flex items-center justify-center gap-4 text-[10px] text-muted-foreground">
                <LegendSwatch color={OBSTRUCTION_COLORS.clear} label="Clear" />
                <LegendSwatch color={OBSTRUCTION_COLORS.obstructed} label="Obstructed" />
                <LegendSwatch color={OBSTRUCTION_COLORS.nodata} label="Not mapped" outline />
            </div>

            {/* Summary stats */}
            <div className="w-full space-y-1.5">
                <div className="flex justify-between text-xs">
                    <span className="text-emerald-400">{stats.clearPct.toFixed(1)}% Clear</span>
                    <span className="text-red-400">{stats.obstructedPct.toFixed(1)}% Obstructed</span>
                </div>
                <div className="w-full h-2.5 rounded-full bg-muted overflow-hidden flex">
                    <div className="h-full bg-emerald-500 transition-all" style={{ width: `${stats.clearPct}%` }} />
                    <div className="h-full bg-red-500 transition-all" style={{ width: `${stats.obstructedPct}%` }} />
                </div>
                <p className="text-[10px] text-muted-foreground/70">
                    {stats.measured > 0
                        ? `${stats.mappedPct.toFixed(0)}% of the visible sky mapped so far (${stats.measured.toLocaleString()} cells)`
                        : "The dish has not mapped any sky yet — this fills in over ~12 hours."}
                </p>
            </div>
        </div>
    )
}

function LegendSwatch({ color, label, outline = false }: { color: string; label: string; outline?: boolean }) {
    return (
        <span className="flex items-center gap-1.5">
            <span
                className="w-2.5 h-2.5 rounded-[3px]"
                style={{ background: color, border: outline ? "1px solid rgba(255,255,255,0.14)" : undefined }}
            />
            {label}
        </span>
    )
}
