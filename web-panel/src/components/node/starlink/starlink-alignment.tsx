import type { StarlinkStatus } from "@/lib/types"
import {
    actuatorStateLabel,
    attitudeStateLabel,
    attitudeStateTone,
    bearingToCardinal,
    hasAlignmentTelemetry,
    isAttitudeConverged,
} from "./starlink-helpers"
import { KeyValue, StarlinkCard, T } from "./starlink-ui"

interface StarlinkAlignmentProps {
    status: StarlinkStatus
}

const TONE_TEXT = {
    emerald: "text-emerald-400",
    amber: "text-amber-400",
    red: "text-red-400",
} as const

const NEEDLE = "#f59e0b"

// polar → cartesian on an SVG canvas whose y axis grows downward.
// `deg` is a clockwise bearing with 0 pointing up.
function polar(cx: number, cy: number, r: number, deg: number): [number, number] {
    const rad = ((deg - 90) * Math.PI) / 180
    return [cx + r * Math.cos(rad), cy + r * Math.sin(rad)]
}

// Filled pie slice spanning [fromDeg, toDeg] clockwise — the uncertainty cone.
function wedgePath(cx: number, cy: number, r: number, fromDeg: number, toDeg: number): string {
    const span = toDeg - fromDeg
    if (span <= 0) return ""
    if (span >= 360) {
        return `M ${cx - r} ${cy} A ${r} ${r} 0 1 1 ${cx + r} ${cy} A ${r} ${r} 0 1 1 ${cx - r} ${cy} Z`
    }
    const [x1, y1] = polar(cx, cy, r, fromDeg)
    const [x2, y2] = polar(cx, cy, r, toDeg)
    const largeArc = span > 180 ? 1 : 0
    return `M ${cx} ${cy} L ${x1} ${y1} A ${r} ${r} 0 ${largeArc} 1 ${x2} ${y2} Z`
}

function TickRing({ cx, cy, r }: { cx: number; cy: number; r: number }) {
    const ticks = []
    for (let deg = 0; deg < 360; deg += 15) {
        const major = deg % 45 === 0
        const len = major ? 8 : 4
        const [x1, y1] = polar(cx, cy, r - len, deg)
        const [x2, y2] = polar(cx, cy, r, deg)
        ticks.push(
            <line
                key={deg}
                x1={x1} y1={y1} x2={x2} y2={y2}
                stroke="currentColor"
                strokeWidth={major ? 1.6 : 1.2}
                opacity={major ? 0.7 : 0.28}
            />,
        )
    }
    return <g className="text-foreground">{ticks}</g>
}

// ─── Rotation: plan view of the dish, needle = boresight azimuth ────
function RotationDial({ status }: { status: StarlinkStatus }) {
    const size = 200
    const c = size / 2
    const ringR = c - 18 // leaves the rim clear of the N/E/S/W letters
    const az = status.boresight_azimuth_deg
    const desiredAz = status.desired_boresight_azimuth_deg
    // Dim the terminal body only when the filter actively says the heading is
    // unconverged — not merely because an older agent omits the field.
    const trusted = !hasAlignmentTelemetry(status) || isAttitudeConverged(status.attitude_estimation_state)
    // Uncertainty is 1-sigma; show it as a ±cone, clamped so a faulted filter
    // doesn't paint the entire dial.
    const unc = Math.min(Math.max(status.attitude_uncertainty_deg, 0), 180)
    const [needleX, needleY] = polar(c, c, ringR - 6, az)
    const showDesired = hasAlignmentTelemetry(status) && Number.isFinite(desiredAz) &&
        Math.abs(((desiredAz - az + 540) % 360) - 180) > 1.5

    return (
        <svg viewBox={`0 0 ${size} ${size}`} className="h-auto w-full max-w-[112px]" role="img"
            aria-label={`Dish rotation ${az.toFixed(0)} degrees, ${bearingToCardinal(az)}`}>
            <TickRing cx={c} cy={c} r={ringR} />

            {unc > 0.5 && (
                <path d={wedgePath(c, c, ringR - 6, az - unc, az + unc)} fill="currentColor" className="text-foreground" opacity={0.18} />
            )}

            {showDesired && (
                <line
                    {...(() => {
                        const [dx, dy] = polar(c, c, ringR - 6, desiredAz)
                        return { x1: c, y1: c, x2: dx, y2: dy }
                    })()}
                    stroke="currentColor" className="text-foreground" opacity={0.45} strokeWidth={2} strokeDasharray="5 4"
                />
            )}

            {/* Needle first, then the dish body on top — matches the dish UI,
                where the terminal sits over its own heading line. */}
            <line x1={c} y1={c} x2={needleX} y2={needleY} stroke={NEEDLE} strokeWidth={3} strokeLinecap="round" />
            <rect x={c - 13} y={c - 8} width={26} height={30} rx={4}
                fill="currentColor" className="text-foreground" opacity={trusted ? 0.92 : 0.4} />

            <text x={c} y={12} textAnchor="middle" className="fill-foreground text-[15px] font-bold">N</text>
            <text x={c} y={size - 2} textAnchor="middle" className="fill-muted-foreground text-[15px] font-bold">S</text>
            <text x={size - 1} y={c + 5} textAnchor="end" className="fill-muted-foreground text-[15px] font-bold">E</text>
            <text x={1} y={c + 5} textAnchor="start" className="fill-muted-foreground text-[15px] font-bold">W</text>
        </svg>
    )
}

// ─── Tilt: side view, needle = boresight elevation above the horizon ──
function TiltDial({ status }: { status: StarlinkStatus }) {
    const size = 200
    // Pivot sits low and left of centre so the dish reads as tilting up off
    // its mast, as it does on the dish's own alignment screen. cx/r are then
    // chosen so the whole 0-135° sweep stays inside the viewBox.
    const cx = size * 0.42
    const cy = size * 0.78
    const r = size * 0.55
    const MAX_EL = 135
    // Elevation measured up from the horizon; the arc sweeps right (horizon)
    // through the top (zenith) and slightly past it.
    const el = Math.min(Math.max(status.boresight_elevation_deg, 0), MAX_EL)
    const desiredEl = Math.min(Math.max(status.desired_boresight_elevation_deg, 0), MAX_EL)
    const unc = Math.min(Math.max(status.attitude_uncertainty_deg, 0), 90)
    // Screen bearing for an elevation angle: 90° elevation = straight up.
    const bearing = (deg: number) => 90 - deg
    const [nx, ny] = polar(cx, cy, r - 8, bearing(el))
    const [dx, dy] = polar(cx, cy, r - 8, bearing(desiredEl))
    const showDesired = hasAlignmentTelemetry(status) &&
        Number.isFinite(desiredEl) && Math.abs(desiredEl - el) > 1.5

    // Dish face is perpendicular to the boresight.
    const faceHalf = 17
    const [fx1, fy1] = polar(cx, cy, faceHalf, bearing(el) - 90)
    const [fx2, fy2] = polar(cx, cy, faceHalf, bearing(el) + 90)

    const ticks = []
    for (let deg = 0; deg <= MAX_EL; deg += 15) {
        const major = deg % 45 === 0
        const len = major ? 9 : 4.5
        const [x1, y1] = polar(cx, cy, r - len, bearing(deg))
        const [x2, y2] = polar(cx, cy, r, bearing(deg))
        ticks.push(
            <line key={deg} x1={x1} y1={y1} x2={x2} y2={y2}
                stroke="currentColor" strokeWidth={major ? 1.6 : 1.2} opacity={major ? 0.7 : 0.28} />,
        )
    }
    const [horizonLx, horizonLy] = polar(cx, cy, r - 16, bearing(0))
    const [zenithLx, zenithLy] = polar(cx, cy, r - 16, bearing(90))

    return (
        <svg viewBox={`0 0 ${size} ${size}`} className="h-auto w-full max-w-[112px]" role="img"
            aria-label={`Dish elevation ${el.toFixed(0)} degrees, tilt ${status.tilt_angle_deg.toFixed(0)} degrees from vertical`}>
            <g className="text-foreground">{ticks}</g>

            {unc > 0.5 && (
                <path d={wedgePath(cx, cy, r - 8, bearing(el) - unc, bearing(el) + unc)} fill="currentColor" className="text-foreground" opacity={0.18} />
            )}

            {showDesired && (
                <line x1={cx} y1={cy} x2={dx} y2={dy}
                    stroke="currentColor" className="text-foreground" opacity={0.45} strokeWidth={2} strokeDasharray="5 4" />
            )}

            <line x1={cx} y1={cy} x2={nx} y2={ny} stroke={NEEDLE} strokeWidth={3} strokeLinecap="round" />
            <line x1={fx1} y1={fy1} x2={fx2} y2={fy2}
                stroke="currentColor" className="text-foreground" strokeWidth={7} strokeLinecap="round" />

            {/* Without these the arc's two ends are indistinguishable. Both
                are nudged off-axis so the needle can't sit on top of them. */}
            <text x={horizonLx - 4} y={horizonLy - 8} textAnchor="end"
                className="fill-muted-foreground text-[13px] font-medium">0°</text>
            <text x={zenithLx - 8} y={zenithLy + 10} textAnchor="end"
                className="fill-muted-foreground text-[13px] font-medium">90°</text>
        </svg>
    )
}

export function StarlinkAlignment({ status }: StarlinkAlignmentProps) {
    const hasTelemetry = hasAlignmentTelemetry(status)
    // Without the extended fields there is nothing to be red about — the dish
    // heading itself is still reported, only the filter state is missing.
    const tone = hasTelemetry ? attitudeStateTone(status.attitude_estimation_state) : "amber"
    const hasActuators = status.has_actuators === "HAS_ACTUATORS_YES"

    return (
        <StarlinkCard
            title="Alignment"
            // A bare amber dot in this slot told the reader nothing. The
            // sentence it stood for is short enough to just print.
            aside={
                <span className={`${T.meta} truncate`}>
                    {hasTelemetry ? (
                        <>
                            Attitude <span className={`font-semibold ${TONE_TEXT[tone]}`}>{attitudeStateLabel(status.attitude_estimation_state)}</span>
                            {status.attitude_uncertainty_deg > 0 && (
                                <span className="tabular-nums"> ±{status.attitude_uncertainty_deg.toFixed(1)}°</span>
                            )}
                        </>
                    ) : (
                        "Attitude detail needs a newer node agent"
                    )}
                </span>
            }
        >
            <div className="grid grid-cols-2 gap-4 sm:flex sm:items-center sm:gap-5">
                <div className="flex shrink-0 flex-col items-center gap-1.5">
                    <RotationDial status={status} />
                    <span className={`${T.meta} whitespace-nowrap`}>
                        <span className="font-semibold tabular-nums text-foreground">{status.boresight_azimuth_deg.toFixed(0)}°</span>
                        {" "}azimuth · {bearingToCardinal(status.boresight_azimuth_deg)}
                    </span>
                </div>

                <div className="flex shrink-0 flex-col items-center gap-1.5">
                    <TiltDial status={status} />
                    <span className={`${T.meta} whitespace-nowrap`}>
                        <span className="font-semibold tabular-nums text-foreground">{status.boresight_elevation_deg.toFixed(0)}°</span>
                        {" "}elevation
                    </span>
                </div>

                <div className="col-span-2 flex min-w-0 flex-1 flex-col gap-2 sm:col-auto">
                    <KeyValue label="Tilt">{status.tilt_angle_deg.toFixed(1)}°</KeyValue>
                    <KeyValue label="Mast">
                        {status.alert_mast_not_near_vertical
                            ? <span className="text-amber-400">Off vertical</span>
                            : <span className="text-emerald-400">Near vertical</span>}
                    </KeyValue>
                    {hasActuators && (
                        <KeyValue label="Motors">{actuatorStateLabel(status.actuator_state)}</KeyValue>
                    )}
                </div>
            </div>
        </StarlinkCard>
    )
}
