import { useRef, useEffect, useMemo } from "react"
import { snrToColor } from "./starlink-helpers"
import { useChartPalette } from "@/lib/design/palette"

interface StarlinkObstructionMapViewProps {
    data: {
        snr: (number | null)[]
        num_rows: number
        num_cols: number
        reference_frame?: string
        // boresight_azimuth_deg of the dish, used to rotate FRAME_UT into compass coordinates.
        boresight_azimuth_deg?: number
    }
    compact?: boolean
}

// In FRAME_UT the array is anchored to the dish's forward direction; the
// rest of the panel speaks compass directions, so we rotate FRAME_UT by the
// dish boresight so North/East stay correct on screen. FRAME_EARTH is
// already compass-aligned so no rotation is applied.
function frameLabels(frame?: string): { top: string; bottom: string; left: string; right: string } {
    if (frame === "FRAME_UT") {
        return { top: "Fwd", bottom: "Aft", left: "Port", right: "Stbd" }
    }
    return { top: "N", bottom: "S", left: "W", right: "E" }
}

export function StarlinkObstructionMapView({ data, compact = false }: StarlinkObstructionMapViewProps) {
    const c = useChartPalette()
    const canvasRef = useRef<HTMLCanvasElement>(null)
    const canvasSize = compact ? 240 : 480
    const labels = frameLabels(data.reference_frame)

    useEffect(() => {
        const canvas = canvasRef.current
        if (!canvas || !data.snr || data.snr.length === 0) return
        if (!data.num_cols || !data.num_rows) return

        const size = canvasSize
        const dpr = typeof window !== "undefined" ? (window.devicePixelRatio || 1) : 1
        canvas.width = size * dpr
        canvas.height = size * dpr
        canvas.style.width = `${size}px`
        canvas.style.height = `${size}px`
        const ctx = canvas.getContext("2d")
        if (!ctx) return
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0)

        ctx.fillStyle = "#0a0a0f"
        ctx.fillRect(0, 0, size, size)

        const numRows = data.num_rows
        const numCols = data.num_cols
        const centerX = size / 2
        const centerY = size / 2
        // Use the smaller of rows/cols so non-square maps still inscribe a
        // single circle without clipping the long axis.
        const maxR = Math.min(numRows, numCols) / 2
        const scale = (size / 2 - 20) / maxR

        // Clip cell rendering to the unit circle to prevent overdraw outside border
        ctx.save()
        ctx.beginPath()
        ctx.arc(centerX, centerY, maxR * scale, 0, Math.PI * 2)
        ctx.clip()

        // Concentric ring guides
        const ringRadii = [0.25, 0.5, 0.75]
        ctx.strokeStyle = c.grid
        ctx.lineWidth = 1
        for (const frac of ringRadii) {
            ctx.beginPath()
            ctx.arc(centerX, centerY, maxR * scale * frac, 0, Math.PI * 2)
            ctx.stroke()
        }

        // Crosshair lines (N-S and E-W)
        ctx.strokeStyle = c.grid
        ctx.lineWidth = 1
        ctx.beginPath()
        ctx.moveTo(centerX, centerY - maxR * scale)
        ctx.lineTo(centerX, centerY + maxR * scale)
        ctx.stroke()
        ctx.beginPath()
        ctx.moveTo(centerX - maxR * scale, centerY)
        ctx.lineTo(centerX + maxR * scale, centerY)
        ctx.stroke()

        // FRAME_UT is anchored to the dish — rotate the cell render so the
        // top of the canvas always points to compass North once the user has
        // a valid boresight. Without this, a dish pointing East would show
        // its forward obstructions at the top while the labels say "N".
        const rotateRad = data.reference_frame === "FRAME_UT" && typeof data.boresight_azimuth_deg === "number"
            ? (data.boresight_azimuth_deg * Math.PI) / 180
            : 0
        if (rotateRad !== 0) {
            ctx.translate(centerX, centerY)
            ctx.rotate(rotateRad)
            ctx.translate(-centerX, -centerY)
        }

        // Draw SNR cells with gradient colors. starlink-grpc-tools convention:
        // row 0 = top of sky map, increasing row = down. Canvas Y also grows
        // downward so we add (not subtract) dy * scale.
        for (let row = 0; row < numRows; row++) {
            for (let col = 0; col < numCols; col++) {
                const idx = row * numCols + col
                const snr = data.snr[idx]
                if (snr === undefined) continue

                const dx = col - numCols / 2
                const dy = row - numRows / 2
                const r = Math.sqrt(dx * dx + dy * dy)
                if (r > maxR) continue

                const px = centerX + dx * scale
                const py = centerY + dy * scale
                const cellSize = Math.max(scale + 0.5, 1.5)

                ctx.fillStyle = snrToColor(snr)
                ctx.fillRect(px - cellSize / 2, py - cellSize / 2, cellSize, cellSize)
            }
        }
        ctx.restore()

        // Circular border
        ctx.strokeStyle = c.border
        ctx.lineWidth = 1.5
        ctx.beginPath()
        ctx.arc(centerX, centerY, maxR * scale, 0, Math.PI * 2)
        ctx.stroke()

        // Cardinal labels (N/S/E/W for FRAME_EARTH, Fwd/Aft/Port/Stbd for FRAME_UT)
        ctx.fillStyle = c.label
        ctx.font = "bold 11px system-ui"
        ctx.textAlign = "center"
        ctx.fillText(labels.top, centerX, 14)
        ctx.fillText(labels.bottom, centerX, size - 4)
        ctx.textAlign = "left"
        ctx.fillText(labels.right, size - 16, centerY + 4)
        ctx.textAlign = "right"
        ctx.fillText(labels.left, 16, centerY + 4)

    }, [data, compact, canvasSize, labels, c.grid, c.border, c.label])

    const { clearPct, obstructedPct, weakPct } = useMemo(() => {
        if (!data.snr || !data.num_cols || !data.num_rows) return { clearPct: "0", obstructedPct: "0", weakPct: "0" }
        const maxR = Math.min(data.num_rows, data.num_cols) / 2
        let circleClear = 0
        let circleObstructed = 0
        let circleWeak = 0
        for (let row = 0; row < data.num_rows; row++) {
            for (let col = 0; col < data.num_cols; col++) {
                const dx = col - data.num_cols / 2
                const dy = row - data.num_rows / 2
                if (Math.sqrt(dx * dx + dy * dy) > maxR) continue
                const snr = data.snr[row * data.num_cols + col]
                if (snr === null || snr === undefined || isNaN(snr) || snr < 0) continue
                if (snr === 0) circleObstructed++
                else if (snr <= 3.0) circleWeak++
                else circleClear++
            }
        }
        const measured = circleClear + circleObstructed + circleWeak
        if (measured === 0) return { clearPct: "0", obstructedPct: "0", weakPct: "0" }
        return {
            clearPct: (circleClear / measured * 100).toFixed(1),
            obstructedPct: (circleObstructed / measured * 100).toFixed(1),
            weakPct: (circleWeak / measured * 100).toFixed(1),
        }
    }, [data])

    return (
        <div className="flex flex-col items-center gap-4">
            <div className="mx-auto aspect-square" style={{ maxWidth: canvasSize }}>
                <canvas
                    ref={canvasRef}
                    role="img"
                    aria-label={`Sky map: ${clearPct}% clear, ${weakPct}% weak signal, ${obstructedPct}% obstructed`}
                    className="rounded-full max-w-full"
                    style={{ width: `min(${canvasSize}px, 100%)`, height: "auto", aspectRatio: "1" }}
                />
            </div>

            {/* Gradient legend bar */}
            <div className="w-full max-w-[280px] space-y-1">
                <div className="h-2 rounded-full" style={{
                    background: "linear-gradient(to right, #ef4444, #f59e0b, #14b8a6)"
                }} />
                <div className="flex justify-between text-[10px] text-muted-foreground">
                    <span>Obstructed</span>
                    <span>Strong</span>
                </div>
                <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground mt-1">
                    <span className="w-2 h-2 rounded-full" style={{background: "#1a1a2e", border: "1px solid rgba(255,255,255,0.1)"}} />
                    <span>No data</span>
                </div>
            </div>

            {/* Summary stats */}
            <div className="w-full space-y-1.5">
                <div className="flex justify-between text-xs">
                    <span className="text-emerald-400">{clearPct}% Clear</span>
                    <span className="text-amber-400">{weakPct}% Weak</span>
                    <span className="text-red-400">{obstructedPct}% Obstructed</span>
                </div>
                <div className="w-full h-2.5 rounded-full bg-muted overflow-hidden flex">
                    <div className="h-full bg-emerald-500 transition-all" style={{ width: `${clearPct}%` }} />
                    <div className="h-full bg-amber-500 transition-all" style={{ width: `${weakPct}%` }} />
                    <div className="h-full bg-red-500 transition-all" style={{ width: `${obstructedPct}%` }} />
                </div>
            </div>
        </div>
    )
}
