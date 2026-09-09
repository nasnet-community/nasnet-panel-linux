import { useRef, useEffect, useMemo, useState } from "react"
import { Info } from "lucide-react"
import {
    classifyObstructionCell,
    obstructionCellColor,
    SKY_DISC_BG,
    isHeadingTrusted,
} from "./starlink-helpers"
import { T } from "./starlink-ui"

export interface SkyMapData {
    snr: (number | null)[]
    num_rows: number
    num_cols: number
    reference_frame?: string
    // Dish boresight azimuth + attitude filter state. Needed to label a
    // dish-relative (FRAME_UT) map with compass points.
    boresight_azimuth_deg?: number
    attitude_estimation_state?: string
}

interface StarlinkObstructionMapViewProps {
    data: SkyMapData
    compact?: boolean
}

/** A rim label and where it sits, in degrees clockwise from the top. */
export interface CompassLabel {
    text: string
    angleDeg: number
}

export interface MapOrientation {
    /** Flip the grid left-to-right before drawing. */
    mirrorX: boolean
    /** True when the labels are real compass points, not dish-relative. */
    compass: boolean
    labels: CompassLabel[]
}

// Orientation was checked against the dish at 192.168.100.1 and the Starlink
// app side by side (Sept 2026, FRAME_UT, boresight az 1.9°):
//
//   - The raw FRAME_UT grid has the boresight at bottom-centre and shows the
//     sky as seen looking up along it, so east is on the RIGHT.
//   - The app mirrors that into a plan view (east on the LEFT once north is
//     at the bottom), keeps the boresight at the bottom, and places N/E/S/W
//     around the rim from the azimuth.
//
// The evidence for the mirror: a lone obstructed cell at the raw grid's
// left-middle sits under the app's "W" (right-middle), and the diagonal
// obstruction band at the raw upper-right is at the app's upper-left. The
// previous renderer drew the raw grid unmirrored and turned it north-up,
// which put every obstruction on the wrong side of the sky.
//
// FRAME_EARTH is documented (starlink-grpc-tools) as north at top-centre,
// east on the right. No live sample to check, so it is drawn unmirrored.
export function mapOrientation(data: SkyMapData): MapOrientation {
    if (data.reference_frame !== "FRAME_UT") {
        return { mirrorX: false, compass: true, labels: cardinalLabels(0) }
    }

    const az = data.boresight_azimuth_deg
    if (!isHeadingTrusted(az, data.attitude_estimation_state)) {
        // Forward stays at the bottom. In a plan view with forward at the
        // bottom, starboard (forward + 90°) lands on the left.
        return {
            mirrorX: true,
            compass: false,
            labels: [
                { text: "Fwd", angleDeg: 180 },
                { text: "Aft", angleDeg: 0 },
                { text: "Stbd", angleDeg: 270 },
                { text: "Port", angleDeg: 90 },
            ],
        }
    }

    // Bearing B lands at B − az + 180: the boresight (B = az) stays at the
    // bottom, and for a north-facing dish that is exactly the app's S-top,
    // N-bottom, E-left, W-right.
    return { mirrorX: true, compass: true, labels: cardinalLabels(180 - (az as number)) }
}

function cardinalLabels(offsetDeg: number): CompassLabel[] {
    const points: [string, number][] = [["N", 0], ["E", 90], ["S", 180], ["W", 270]]
    return points.map(([text, bearing]) => ({
        text,
        angleDeg: (((bearing + offsetDeg) % 360) + 360) % 360,
    }))
}

export interface SkyMapStats {
    clearPct: number
    obstructedPct: number
    mappedPct: number
    measured: number
}

export function useSkyMapStats(data: SkyMapData | undefined): SkyMapStats {
    return useMemo(() => {
        const empty = { clearPct: 0, obstructedPct: 0, mappedPct: 0, measured: 0 }
        if (!data?.snr || !data.num_cols || !data.num_rows) return empty

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
}

// Dot lattice geometry. The app draws the sky as concentric rings of dots,
// not a pixel grid, so the 123×123 map is resampled onto rings: one dot per
// `pitch` px along each ring, and the dot takes the WORST measured cell
// under its footprint so a one-cell obstruction line cannot fall between
// two dots and vanish.
const FULL_RING_MIN = 18
const FULL_RING_MAX = 40
const FULL_PX_PER_RING = 8
const THUMB_RINGS = 10

// The dish silhouette in the middle, as a fraction of the disc diameter.
// Portrait, like the app: the flat edge at the bottom is the way the dish
// faces, which is what makes "boresight at the bottom" readable.
const DISH_W = { full: 0.22, thumb: 0.18 }
const DISH_H = { full: 0.34, thumb: 0.27 }

function roundedRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
    const rr = Math.min(r, w / 2, h / 2)
    ctx.beginPath()
    ctx.moveTo(x + rr, y)
    ctx.arcTo(x + w, y, x + w, y + h, rr)
    ctx.arcTo(x + w, y + h, x, y + h, rr)
    ctx.arcTo(x, y + h, x, y, rr)
    ctx.arcTo(x, y, x + w, y, rr)
    ctx.closePath()
}

/**
 * The sky disc itself, without labels, legend or stats. `size` fixes it in
 * CSS pixels (the card thumbnail); leaving it out makes the disc fill its
 * box and track resizes (the drawer). The disc always paints its own dark
 * ground and is transparent outside the circle, so it sits on either theme.
 */
export function SkyDisc({
    data, size, detail = "full",
}: {
    data: SkyMapData
    size?: number
    detail?: "full" | "thumb"
}) {
    const canvasRef = useRef<HTMLCanvasElement>(null)
    const boxRef = useRef<HTMLDivElement>(null)
    const [measured, setMeasured] = useState(0)
    const cssSize = size ?? measured
    const { mirrorX, compass } = mapOrientation(data)
    const thumb = detail === "thumb"

    // The canvas is laid out by CSS (width:100%, square) and its bitmap is
    // sized from the measured box. Writing canvas.style.width here instead
    // would clobber the responsive layout and overflow the drawer.
    useEffect(() => {
        if (size) return
        const box = boxRef.current
        if (!box) return
        const measure = () => setMeasured(box.clientWidth)
        measure()
        const ro = new ResizeObserver(measure)
        ro.observe(box)
        return () => ro.disconnect()
    }, [size])

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
        const snr = data.snr
        const center = cssSize / 2

        // Ring count → pitch → dot radius → disc radius, so the outermost
        // dots end inside the canvas instead of being shaved by its edge.
        const rings = thumb
            ? THUMB_RINGS
            : Math.max(FULL_RING_MIN, Math.min(FULL_RING_MAX, Math.round(center / FULL_PX_PER_RING)))
        const pitch = (center - 1) / (rings + 0.45)
        const dotR = pitch * (thumb ? 0.42 : 0.32)
        const discR = rings * pitch

        // Ground: a dark disc with the app's soft halo behind the dots.
        ctx.fillStyle = SKY_DISC_BG
        ctx.beginPath()
        ctx.arc(center, center, center, 0, Math.PI * 2)
        ctx.fill()
        const halo = ctx.createRadialGradient(center, center, 0, center, center, center)
        halo.addColorStop(0, "rgba(255,255,255,0.10)")
        halo.addColorStop(0.65, "rgba(255,255,255,0.035)")
        halo.addColorStop(1, "rgba(255,255,255,0)")
        ctx.fillStyle = halo
        ctx.beginPath()
        ctx.arc(center, center, center, 0, Math.PI * 2)
        ctx.fill()

        // Grid → disc. Cell centres sit at (col,row) offset from the grid
        // midpoint; the grid's inscribed circle maps onto the dot disc.
        const maxR = Math.min(numRows, numCols) / 2
        const scale = discR / maxR // px per cell
        const rowMid = (numRows - 1) / 2
        const colMid = (numCols - 1) / 2
        const foot = Math.max(0.5, pitch / scale / 2) // dot footprint, in cells
        const span = Math.ceil(foot)

        // Worst measured cell under a dot, or null for "no measured cell",
        // or undefined for "outside the map entirely".
        const sample = (px: number, py: number): number | null | undefined => {
            const gx = (mirrorX ? -px : px) / scale
            const gy = py / scale
            if (Math.sqrt(gx * gx + gy * gy) > maxR) return undefined
            const c0 = Math.round(colMid + gx)
            const r0 = Math.round(rowMid + gy)
            let worst: number | null = null
            let anyCell = false
            for (let r = r0 - span; r <= r0 + span; r++) {
                if (r < 0 || r >= numRows) continue
                for (let c = c0 - span; c <= c0 + span; c++) {
                    if (c < 0 || c >= numCols) continue
                    const dc = c - colMid - gx
                    const dr = r - rowMid - gy
                    const nearest = r === r0 && c === c0
                    if (!nearest && dc * dc + dr * dr > foot * foot) continue
                    const idx = r * numCols + c
                    if (idx >= snr.length) continue
                    anyCell = true
                    const v = snr[idx]
                    if (v === null || v === undefined || Number.isNaN(v) || v < 0) continue
                    worst = worst === null ? v : Math.min(worst, v)
                }
            }
            return anyCell ? worst : undefined
        }

        for (let k = 0; k <= rings; k++) {
            const r = k * pitch
            const n = k === 0 ? 1 : Math.round((2 * Math.PI * r) / pitch)
            // Half-step every other ring so the lattice reads as a mesh, not
            // as radial spokes.
            const phase = k % 2 === 0 ? 0 : Math.PI / n
            for (let i = 0; i < n; i++) {
                const phi = phase + (i / n) * Math.PI * 2 // clockwise from top
                const px = Math.sin(phi) * r
                const py = -Math.cos(phi) * r
                const v = sample(px, py)
                if (v === undefined) continue

                if (v === null) {
                    // Unmapped sky: faint, fading toward the rim like the app.
                    const fade = 1 - 0.55 * (r / discR)
                    ctx.fillStyle = `rgba(255,255,255,${(0.15 * fade).toFixed(3)})`
                } else {
                    ctx.fillStyle = obstructionCellColor(v)
                }
                ctx.beginPath()
                ctx.arc(center + px, center + py, dotR, 0, Math.PI * 2)
                ctx.fill()
            }
        }

        // The dish itself, sitting over the zenith.
        const w = discR * 2 * (thumb ? DISH_W.thumb : DISH_W.full)
        const h = discR * 2 * (thumb ? DISH_H.thumb : DISH_H.full)
        ctx.save()
        ctx.shadowColor = "rgba(0,0,0,0.6)"
        ctx.shadowBlur = thumb ? 4 : 14
        ctx.fillStyle = "#f4f4f5"
        roundedRect(ctx, center - w / 2, center - h / 2, w, h, thumb ? 2 : 6)
        ctx.fill()
        ctx.restore()
    }, [data, cssSize, thumb, mirrorX])

    const stats = useSkyMapStats(data)
    const label = `Sky map, ${compass ? "compass labelled" : "dish forward down"}: ${stats.clearPct.toFixed(1)}% clear, ${stats.obstructedPct.toFixed(1)}% obstructed, ${stats.mappedPct.toFixed(0)}% of the sky mapped`

    if (size) {
        return (
            <canvas
                ref={canvasRef}
                role="img"
                aria-label={label}
                className="block shrink-0"
                style={{ width: size, height: size }}
            />
        )
    }

    return (
        <div ref={boxRef} className="relative aspect-square w-full">
            <canvas ref={canvasRef} role="img" aria-label={label} className="block h-full w-full" />
        </div>
    )
}

// Rim letters sit just inside the disc, over the outer rings, on the dark
// ground — so they are white in both themes and never clipped by the box.
const LABEL_RADIUS_PCT = 44

export function StarlinkObstructionMapView({ data, compact = false }: StarlinkObstructionMapViewProps) {
    const maxSize = compact ? 260 : 460
    const { compass, labels } = mapOrientation(data)
    const stats = useSkyMapStats(data)

    // Qualitative on purpose. The gauge above quotes the dish's time-based
    // obstruction fraction; a cell-count percentage here would be a second,
    // different number for the same idea.
    const headline = stats.measured === 0
        ? "Your Starlink has not mapped any sky yet."
        : stats.obstructedPct < 1
            ? "Your Starlink has an unobstructed view of the sky."
            : stats.obstructedPct < 5
                ? "Your Starlink has a few obstructions in view."
                : "Your Starlink has significant obstructions in view."

    return (
        <div className="flex flex-col items-center gap-4">
            <div className="relative w-full" style={{ maxWidth: maxSize }}>
                <SkyDisc data={data} />
                {labels.map((l) => {
                    const rad = (l.angleDeg * Math.PI) / 180
                    return (
                        <span
                            key={l.text}
                            aria-hidden
                            className="pointer-events-none absolute -translate-x-1/2 -translate-y-1/2 text-[13px] font-bold leading-none text-white/90"
                            style={{
                                left: `${50 + LABEL_RADIUS_PCT * Math.sin(rad)}%`,
                                top: `${50 - LABEL_RADIUS_PCT * Math.cos(rad)}%`,
                                textShadow: "0 0 6px rgba(0,0,0,0.95), 0 0 2px rgba(0,0,0,0.9)",
                            }}
                        >
                            {l.text}
                        </span>
                    )
                })}
            </div>

            {!compass && (
                <p className={`${T.meta} -mt-2 text-center text-amber-400/80`}>
                    Dish-relative view — heading unavailable, so the compass points cannot be placed.
                </p>
            )}

            {/* Legend, in the app's words. Swatches use ink tokens rather
                than the canvas colours: white-on-white would vanish on a
                light card. */}
            <div className={`flex flex-wrap items-center justify-center gap-x-4 gap-y-1 ${T.meta}`}>
                <LegendDot className="bg-foreground/25" label="Unmapped" />
                <LegendDot className="bg-foreground" label="Clear view" />
                <LegendDot className="bg-[#ef3340]" label="Obstructions" />
            </div>

            <div className="flex w-full items-start gap-3 rounded-xl border border-border bg-muted/30 px-4 py-3 text-xs leading-4 text-muted-foreground">
                <Info className="mt-px h-4 w-4 shrink-0" aria-hidden />
                <p>
                    <span className="text-foreground">{headline}</span>{" "}
                    {stats.measured > 0
                        ? `The map gets more accurate as the dish collects data — ${stats.mappedPct.toFixed(0)}% of the visible sky is mapped so far.`
                        : "The map fills in over the first ~12 hours as the dish collects data."}
                </p>
            </div>
        </div>
    )
}

function LegendDot({ className, label }: { className: string; label: string }) {
    return (
        <span className="flex items-center gap-1.5">
            <span className={`h-2.5 w-2.5 rounded-full ${className}`} />
            {label}
        </span>
    )
}
