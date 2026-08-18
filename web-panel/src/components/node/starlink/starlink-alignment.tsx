import { Card } from "@/components/ui/card"
import type { StarlinkStatus } from "@/lib/types"
import {
    actuatorStateLabel,
    attitudeStateLabel,
    attitudeStateTone,
    bearingToCardinal,
    hasAlignmentTelemetry,
    isAttitudeConverged,
} from "./starlink-helpers"

interface StarlinkAlignmentProps {
    status: StarlinkStatus
}

const TONE_TEXT = {
    emerald: "text-emerald-400",
    amber: "text-amber-400",
    red: "text-red-400",
} as const

const TONE_DOT = {
    emerald: "bg-emerald-400",
    amber: "bg-amber-400",
    red: "bg-red-500",
} as const

const NEEDLE = "#f59e0b"
const WEDGE = "rgba(255,255,255,0.22)"

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
    for (let deg = 0; deg < 360; deg += 5) {
        const major = deg % 45 === 0
        const len = major ? 6 : 3
        const [x1, y1] = polar(cx, cy, r - len, deg)
        const [x2, y2] = polar(cx, cy, r, deg)
        ticks.push(
            <line
                key={deg}
                x1={x1} y1={y1} x2={x2} y2={y2}
                stroke="currentColor"
                strokeWidth={major ? 1.4 : 1}
                opacity={major ? 0.75 : 0.3}
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
        <svg viewBox={`0 0 ${size} ${size}`} className="w-full h-auto max-w-[220px]" role="img"
            aria-label={`Dish rotation ${az.toFixed(0)} degrees, ${bearingToCardinal(az)}`}>
            <TickRing cx={c} cy={c} r={ringR} />

            {unc > 0.5 && (
                <path d={wedgePath(c, c, ringR - 6, az - unc, az + unc)} fill={WEDGE} />
            )}

            {showDesired && (
                <line
                    {...(() => {
                        const [dx, dy] = polar(c, c, ringR - 6, desiredAz)
                        return { x1: c, y1: c, x2: dx, y2: dy }
                    })()}
                    stroke="rgba(255,255,255,0.5)" strokeWidth={1.5} strokeDasharray="4 3"
                />
            )}

            {/* Needle first, then the dish body on top — matches the dish UI,
                where the terminal sits over its own heading line. */}
            <line x1={c} y1={c} x2={needleX} y2={needleY} stroke={NEEDLE} strokeWidth={2} strokeLinecap="round" />
            <rect x={c - 13} y={c - 8} width={26} height={30} rx={4}
                fill={trusted ? "#f4f4f5" : "rgba(244,244,245,0.45)"} />

            <text x={c} y={10} textAnchor="middle" className="fill-foreground text-[11px] font-bold">N</text>
            <text x={c} y={size - 2} textAnchor="middle" className="fill-foreground text-[11px] font-bold">S</text>
            <text x={size - 1} y={c + 4} textAnchor="end" className="fill-foreground text-[11px] font-bold">E</text>
            <text x={1} y={c + 4} textAnchor="start" className="fill-foreground text-[11px] font-bold">W</text>
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
    for (let deg = 0; deg <= MAX_EL; deg += 5) {
        const major = deg % 45 === 0
        const len = major ? 7 : 3.5
        const [x1, y1] = polar(cx, cy, r - len, bearing(deg))
        const [x2, y2] = polar(cx, cy, r, bearing(deg))
        ticks.push(
            <line key={deg} x1={x1} y1={y1} x2={x2} y2={y2}
                stroke="currentColor" strokeWidth={major ? 1.4 : 1} opacity={major ? 0.75 : 0.3} />,
        )
    }
    const [horizonLx, horizonLy] = polar(cx, cy, r - 14, bearing(0))
    const [zenithLx, zenithLy] = polar(cx, cy, r - 14, bearing(90))

    return (
        <svg viewBox={`0 0 ${size} ${size}`} className="w-full h-auto max-w-[220px]" role="img"
            aria-label={`Dish elevation ${el.toFixed(0)} degrees, tilt ${status.tilt_angle_deg.toFixed(0)} degrees from vertical`}>
            <g className="text-foreground">{ticks}</g>

            {unc > 0.5 && (
                <path d={wedgePath(cx, cy, r - 8, bearing(el) - unc, bearing(el) + unc)} fill={WEDGE} />
            )}

            {showDesired && (
                <line x1={cx} y1={cy} x2={dx} y2={dy}
                    stroke="rgba(255,255,255,0.5)" strokeWidth={1.5} strokeDasharray="4 3" />
            )}

            <line x1={cx} y1={cy} x2={nx} y2={ny} stroke={NEEDLE} strokeWidth={2} strokeLinecap="round" />
            <line x1={fx1} y1={fy1} x2={fx2} y2={fy2}
                stroke="#f4f4f5" strokeWidth={6} strokeLinecap="round" />

            {/* Without these the arc's two ends are indistinguishable. Both
                are nudged off-axis so the needle can't sit on top of them. */}
            <text x={horizonLx - 4} y={horizonLy - 6} textAnchor="end"
                className="fill-muted-foreground text-[8px] font-medium">0°</text>
            <text x={zenithLx - 7} y={zenithLy + 8} textAnchor="end"
                className="fill-muted-foreground text-[8px] font-medium">90°</text>
        </svg>
    )
}

function DialCard({
    title, value, sub, tone, children,
}: {
    title: string
    value: string
    sub: string
    tone: "emerald" | "amber" | "red"
    children: React.ReactNode
}) {
    return (
        <Card className="rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
            <div className="flex items-center justify-between mb-1">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">{title}</p>
                <span className={`w-1.5 h-1.5 rounded-full ${TONE_DOT[tone]}`} aria-hidden />
            </div>
            <div className="flex items-baseline gap-2">
                <span className="text-2xl font-bold tracking-tight tabular-nums">{value}</span>
                <span className="text-[11px] text-muted-foreground">{sub}</span>
            </div>
            <div className="flex justify-center mt-2">{children}</div>
        </Card>
    )
}

export function StarlinkAlignment({ status }: StarlinkAlignmentProps) {
    const hasTelemetry = hasAlignmentTelemetry(status)
    // Without the extended fields there is nothing to be red about — the dish
    // heading itself is still reported, only the filter state is missing.
    const tone = hasTelemetry ? attitudeStateTone(status.attitude_estimation_state) : "amber"
    const hasActuators = status.has_actuators === "HAS_ACTUATORS_YES"

    return (
        <div className="space-y-2">
            <div className="flex items-center justify-between px-1">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Alignment</p>
                <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
                    {hasTelemetry ? (
                        <span>
                            Attitude <span className={`font-bold ${TONE_TEXT[tone]}`}>{attitudeStateLabel(status.attitude_estimation_state)}</span>
                            {status.attitude_uncertainty_deg > 0 && (
                                <span className="text-muted-foreground/60"> &plusmn;{status.attitude_uncertainty_deg.toFixed(1)}&deg;</span>
                            )}
                        </span>
                    ) : (
                        <span className="text-muted-foreground/60">Attitude detail needs a newer node agent</span>
                    )}
                    {hasActuators && (
                        <span className="hidden sm:inline">
                            Motors <span className="font-bold text-foreground/80">{actuatorStateLabel(status.actuator_state)}</span>
                        </span>
                    )}
                </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 md:gap-4">
                <DialCard
                    title="Rotation"
                    value={`${status.boresight_azimuth_deg.toFixed(0)}°`}
                    sub={bearingToCardinal(status.boresight_azimuth_deg)}
                    tone={tone}
                >
                    <RotationDial status={status} />
                </DialCard>
                <DialCard
                    title="Tilt"
                    value={`${status.boresight_elevation_deg.toFixed(0)}°`}
                    sub={`elevation · ${status.tilt_angle_deg.toFixed(0)}° from vertical`}
                    tone={tone}
                >
                    <TiltDial status={status} />
                </DialCard>
            </div>
        </div>
    )
}
