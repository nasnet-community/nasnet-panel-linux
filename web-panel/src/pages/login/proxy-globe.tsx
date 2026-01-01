import { useEffect, useRef, useState } from "react"
import createGlobe from "cobe"

const MARKERS: { location: [number, number]; size: number }[] = [
    // ── Major global hubs (larger dots) ─────────────────────────────────────
    { location: [51.5074, -0.1278], size: 0.07 },    // London
    { location: [48.8566, 2.3522], size: 0.06 },     // Paris
    { location: [35.6762, 139.6503], size: 0.07 },   // Tokyo
    { location: [38.9072, -77.0369], size: 0.07 },   // Washington D.C.
    { location: [55.7558, 37.6173], size: 0.06 },    // Moscow
    { location: [39.9042, 116.4074], size: 0.06 },   // Beijing

    // ── Key regional capitals (medium dots) ─────────────────────────────────
    { location: [52.5200, 13.4050], size: 0.05 },    // Berlin
    { location: [28.6139, 77.2090], size: 0.05 },    // New Delhi
    { location: [-15.7975, -47.8919], size: 0.05 },  // Brasília
    { location: [-35.2809, 149.1300], size: 0.05 },  // Canberra
    { location: [30.0444, 31.2357], size: 0.05 },    // Cairo
    { location: [37.5665, 126.978], size: 0.05 },    // Seoul
    { location: [45.4215, -75.6972], size: 0.05 },   // Ottawa
    { location: [19.4326, -99.1332], size: 0.05 },   // Mexico City
    { location: [1.3521, 103.8198], size: 0.05 },    // Singapore
    { location: [-34.6037, -58.3816], size: 0.05 },  // Buenos Aires

    // ── Spread across continents (smaller dots) ─────────────────────────────
    { location: [59.3293, 18.0686], size: 0.04 },    // Stockholm
    { location: [41.9028, 12.4964], size: 0.04 },    // Rome
    { location: [40.4168, -3.7038], size: 0.04 },    // Madrid
    { location: [50.4501, 30.5234], size: 0.04 },    // Kyiv
    { location: [64.1466, -21.9426], size: 0.04 },   // Reykjavik
    { location: [59.9139, 10.7522], size: 0.04 },    // Oslo
    { location: [13.7563, 100.5018], size: 0.04 },   // Bangkok
    { location: [-6.2088, 106.8456], size: 0.04 },   // Jakarta
    { location: [14.5995, 120.9842], size: 0.04 },   // Manila
    { location: [21.0278, 105.8342], size: 0.04 },   // Hanoi
    { location: [35.6892, 51.3890], size: 0.06 },    // Tehran
    { location: [24.7136, 46.6753], size: 0.04 },    // Riyadh
    { location: [24.4539, 54.3773], size: 0.04 },    // Abu Dhabi
    { location: [-1.2921, 36.8219], size: 0.04 },    // Nairobi
    { location: [9.0765, 7.3986], size: 0.04 },      // Abuja
    { location: [-25.7479, 28.2293], size: 0.04 },   // Pretoria
    { location: [9.0250, 38.7469], size: 0.04 },     // Addis Ababa
    { location: [-33.4489, -70.6693], size: 0.04 },  // Santiago
    { location: [4.7110, -74.0721], size: 0.04 },    // Bogotá
    { location: [-12.0464, -77.0428], size: 0.04 },  // Lima
    { location: [-41.2865, 174.7762], size: 0.04 },  // Wellington
    { location: [51.1694, 71.4491], size: 0.04 },    // Astana
    { location: [23.1136, -82.3666], size: 0.04 },   // Havana
]

const PACKET_COLORS: [number, number, number][] = [
    [0.1, 0.85, 1.0],  // cyan
    [0.2, 0.55, 1.0],  // blue
    [0.0, 1.0, 0.55],  // green
    [0.75, 0.45, 1.0], // purple
]

type Vec3 = [number, number, number]

type Packet = {
    from: number
    to: number
    progress: number
    speed: number
    color: Vec3
    trailLen: number
    arcAlt: number
}

type Burst = {
    x: number
    y: number
    color: Vec3
    age: number
    particles: { angle: number; speed: number; size: number }[]
}

type Spark = {
    x: number
    y: number
    vx: number
    vy: number
    color: Vec3
    life: number
    decay: number
    size: number
}

const MAX_SPARKS = 60
const MAX_BURSTS = 8
const NUM_STARS = 100
const ORBIT_POINTS = 150
const ORBIT_RADIUS = 1.15
const ORBIT_TILT = 0.45     // radians (~25°)
const MESH_DOT_THRESHOLD = 0.55  // ~56° great-circle distance

function latLngToXYZ(lat: number, lng: number): Vec3 {
    const latR = (lat * Math.PI) / 180
    const lngR = (lng * Math.PI) / 180
    return [
        Math.cos(latR) * Math.cos(lngR),
        Math.sin(latR),
        -Math.cos(latR) * Math.sin(lngR),
    ]
}

function rotate(p: Vec3, phi: number, theta: number): Vec3 {
    const cp = Math.cos(phi), sp = Math.sin(phi)
    const ct = Math.cos(theta), st = Math.sin(theta)
    return [
        cp * p[0] + sp * p[2],
        sp * st * p[0] + ct * p[1] - cp * st * p[2],
        -sp * ct * p[0] + st * p[1] + cp * ct * p[2],
    ]
}

function slerp(a: Vec3, b: Vec3, t: number): Vec3 {
    const dot = Math.max(-1, Math.min(1, a[0] * b[0] + a[1] * b[1] + a[2] * b[2]))
    const omega = Math.acos(dot)
    if (Math.abs(omega) < 0.001) {
        return [a[0] + t * (b[0] - a[0]), a[1] + t * (b[1] - a[1]), a[2] + t * (b[2] - a[2])]
    }
    const s = Math.sin(omega)
    const s1 = Math.sin((1 - t) * omega) / s
    const s2 = Math.sin(t * omega) / s
    return [s1 * a[0] + s2 * b[0], s1 * a[1] + s2 * b[1], s1 * a[2] + s2 * b[2]]
}

function arcPoint(a: Vec3, b: Vec3, t: number, arcAlt: number): Vec3 {
    const p = slerp(a, b, t)
    const lift = 1 + arcAlt * 4 * t * (1 - t)
    return [p[0] * lift, p[1] * lift, p[2] * lift]
}

function project(p: Vec3, cx: number, cy: number, r: number) {
    return { x: cx + p[0] * r, y: cy - p[1] * r, visible: p[2] > 0 }
}

function makePacket(): Packet {
    let from: number, to: number
    do {
        from = Math.floor(Math.random() * MARKERS.length)
        to = Math.floor(Math.random() * MARKERS.length)
    } while (from === to)
    return {
        from,
        to,
        progress: Math.random(),
        speed: 0.0025 + Math.random() * 0.004,
        color: PACKET_COLORS[Math.floor(Math.random() * PACKET_COLORS.length)],
        trailLen: 0.07 + Math.random() * 0.1,
        arcAlt: 0.18 + Math.random() * 0.32,
    }
}

function makeBurst(x: number, y: number, color: Vec3): Burst {
    const particles = Array.from({ length: 5 + Math.floor(Math.random() * 4) }, () => ({
        angle: Math.random() * Math.PI * 2,
        speed: 0.8 + Math.random() * 1.5,
        size: 0.8 + Math.random() * 1.2,
    }))
    return { x, y, color, age: 0, particles }
}

// Precompute orbital ring points in 3D (tilted circle outside the sphere)
function makeOrbitPoints(): Vec3[] {
    const points: Vec3[] = []
    const ct = Math.cos(ORBIT_TILT), st = Math.sin(ORBIT_TILT)
    for (let i = 0; i <= ORBIT_POINTS; i++) {
        const angle = (i / ORBIT_POINTS) * Math.PI * 2
        const x = ORBIT_RADIUS * Math.cos(angle)
        const y0 = ORBIT_RADIUS * Math.sin(angle)
        // Tilt around the X axis
        points.push([x, y0 * ct, y0 * st])
    }
    return points
}

// Precompute mesh edges: pairs of marker indices that are close enough
function buildMeshEdges(markerXYZ: Vec3[]): [number, number][] {
    const edges: [number, number][] = []
    for (let i = 0; i < markerXYZ.length; i++) {
        for (let j = i + 1; j < markerXYZ.length; j++) {
            const a = markerXYZ[i], b = markerXYZ[j]
            const dot = a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
            if (dot > MESH_DOT_THRESHOLD) {
                edges.push([i, j])
            }
        }
    }
    return edges
}

// Precompute star positions (random points outside globe circle)
function makeStars(size: number, globeR: number): { x: number; y: number; brightness: number; radius: number }[] {
    const stars: { x: number; y: number; brightness: number; radius: number }[] = []
    const cx = size / 2, cy = size / 2
    const minDist = globeR + 15 // outside the globe + small margin
    for (let i = 0; i < NUM_STARS; i++) {
        let x: number, y: number
        do {
            x = Math.random() * size
            y = Math.random() * size
        } while (Math.hypot(x - cx, y - cy) < minDist)
        stars.push({
            x,
            y,
            brightness: 0.15 + Math.random() * 0.4,
            radius: 0.4 + Math.random() * 0.8,
        })
    }
    return stars
}

const BASE_THETA = 0.2
const ARC_STEPS = 80
const NUM_PACKETS = 10
const MESH_STEPS = 12  // slerp resolution for mesh lines

export default function ProxyGlobe() {
    const canvasRef = useRef<HTMLCanvasElement>(null)
    const overlayRef = useRef<HTMLCanvasElement>(null)
    const [opacity, setOpacity] = useState(0)

    useEffect(() => {
        let phi = 0
        let dynamicTheta = BASE_THETA
        let pointerInteracting: number | null = null
        let pointerInteractionMovement = 0

        const canvas = canvasRef.current!
        const overlay = overlayRef.current!
        const size = canvas.offsetWidth

        // Hidden by CSS on mobile — skip all WebGL/canvas work
        if (size === 0) return

        overlay.width = size
        overlay.height = size

        const markerXYZ: Vec3[] = MARKERS.map(m => latLngToXYZ(m.location[0], m.location[1]))
        const packets: Packet[] = Array.from({ length: NUM_PACKETS }, makePacket)
        const bursts: Burst[] = []
        const sparks: Spark[] = []
        const orbitPoints = makeOrbitPoints()
        const meshEdges = buildMeshEdges(markerXYZ)
        const cx = size / 2
        const cy = size / 2
        const globeR = size * 0.4

        // D: Pre-render star field to offscreen canvas
        const stars = makeStars(size, globeR)
        const starCanvas = document.createElement("canvas")
        starCanvas.width = size
        starCanvas.height = size
        const starCtx = starCanvas.getContext("2d")!
        for (const star of stars) {
            starCtx.beginPath()
            starCtx.arc(star.x, star.y, star.radius, 0, Math.PI * 2)
            starCtx.fillStyle = `rgba(180,220,255,${star.brightness})`
            starCtx.fill()
        }

        // cobe globe
        const globe = createGlobe(canvas, {
            devicePixelRatio: 2,
            width: size * 2,
            height: size * 2,
            phi: 0,
            theta: BASE_THETA,
            dark: 1,
            diffuse: 3,
            mapSamples: 16000,
            mapBrightness: 6,
            baseColor: [0.1, 0.1, 0.1],
            markerColor: [0.1, 0.8, 1],
            glowColor: [0.15, 0.3, 0.6],
            markers: MARKERS,
            onRender: (state) => {
                if (pointerInteracting === null) phi += 0.002
                state.phi = phi + pointerInteractionMovement
                state.theta = dynamicTheta
            },
        })

        const ctx = overlay.getContext("2d")!
        let animId: number

        function drawArcs() {
            ctx.clearRect(0, 0, size, size)
            const currentPhi = phi + pointerInteractionMovement
            const currentTheta = dynamicTheta

            // ── D: Star field (blit from offscreen canvas) ──────────────────────
            ctx.drawImage(starCanvas, 0, 0)

            // ── A: Atmosphere rim glow ──────────────────────────────────────────
            const atmosInner = globeR - 2
            const atmosOuter = globeR + 25
            const atmos = ctx.createRadialGradient(cx, cy, atmosInner, cx, cy, atmosOuter)
            atmos.addColorStop(0, "rgba(56,182,255,0)")
            atmos.addColorStop(0.15, "rgba(56,182,255,0.08)")
            atmos.addColorStop(0.45, "rgba(40,140,255,0.05)")
            atmos.addColorStop(0.75, "rgba(30,100,220,0.02)")
            atmos.addColorStop(1, "rgba(20,60,180,0)")
            ctx.beginPath()
            ctx.arc(cx, cy, atmosOuter, 0, Math.PI * 2)
            ctx.fillStyle = atmos
            ctx.fill()

            // ── C: Network mesh lines (faint connections between nearby markers)
            ctx.setLineDash([3, 4])
            for (const [i, j] of meshEdges) {
                const a = markerXYZ[i], b = markerXYZ[j]
                ctx.beginPath()
                let pen = false
                for (let s = 0; s <= MESH_STEPS; s++) {
                    const t = s / MESH_STEPS
                    const p = slerp(a, b, t)
                    const { x, y, visible } = project(rotate(p, currentPhi, currentTheta), cx, cy, globeR)
                    if (!visible) { pen = false; continue }
                    if (!pen) { ctx.moveTo(x, y); pen = true }
                    else ctx.lineTo(x, y)
                }
                ctx.strokeStyle = "rgba(56,182,255,0.06)"
                ctx.lineWidth = 0.6
                ctx.stroke()
            }
            ctx.setLineDash([])

            // ── Traffic arcs & packets ──────────────────────────────────────────
            for (const pkt of packets) {
                const a = markerXYZ[pkt.from]
                const b = markerXYZ[pkt.to]
                const [r, g, bl] = pkt.color

                // Faint pipe (full arc)
                ctx.beginPath()
                let pipePen = false
                for (let i = 0; i <= ARC_STEPS; i++) {
                    const pt = arcPoint(a, b, i / ARC_STEPS, pkt.arcAlt)
                    const { x, y, visible } = project(rotate(pt, currentPhi, currentTheta), cx, cy, globeR)
                    if (!visible) { pipePen = false; continue }
                    if (!pipePen) { ctx.moveTo(x, y); pipePen = true }
                    else ctx.lineTo(x, y)
                }
                ctx.strokeStyle = `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},0.10)`
                ctx.lineWidth = 1
                ctx.stroke()

                // Advance packet
                pkt.progress += pkt.speed
                if (pkt.progress >= 1) {
                    const destPt = rotate(markerXYZ[pkt.to], currentPhi, currentTheta)
                    const destProj = project(destPt, cx, cy, globeR)
                    if (destProj.visible && bursts.length < MAX_BURSTS) {
                        bursts.push(makeBurst(destProj.x, destProj.y, pkt.color))
                    }
                    Object.assign(pkt, makePacket())
                    pkt.progress = 0
                }

                // Glowing trail
                const headT = pkt.progress
                const tailT = Math.max(0, headT - pkt.trailLen)
                const steps = Math.ceil(pkt.trailLen * ARC_STEPS)

                type Pt2 = { x: number; y: number }
                const segments: Pt2[][] = []
                let seg: Pt2[] = []

                for (let i = 0; i <= steps; i++) {
                    const t = tailT + (headT - tailT) * (i / steps)
                    const pt = arcPoint(a, b, t, pkt.arcAlt)
                    const { x, y, visible } = project(rotate(pt, currentPhi, currentTheta), cx, cy, globeR)
                    if (visible) {
                        seg.push({ x, y })
                    } else {
                        if (seg.length >= 2) segments.push(seg)
                        seg = []
                    }
                }
                if (seg.length >= 2) segments.push(seg)

                for (const segment of segments) {
                    const tail = segment[0]
                    const head = segment[segment.length - 1]

                    const grad = ctx.createLinearGradient(tail.x, tail.y, head.x, head.y)
                    grad.addColorStop(0, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},0)`)
                    grad.addColorStop(0.6, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},0.55)`)
                    grad.addColorStop(1, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},1)`)

                    // Bloom pass
                    ctx.beginPath()
                    ctx.moveTo(segment[0].x, segment[0].y)
                    for (let i = 1; i < segment.length; i++) ctx.lineTo(segment[i].x, segment[i].y)
                    ctx.strokeStyle = `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},0.12)`
                    ctx.lineWidth = 5
                    ctx.stroke()

                    // Sharp pass
                    ctx.beginPath()
                    ctx.moveTo(segment[0].x, segment[0].y)
                    for (let i = 1; i < segment.length; i++) ctx.lineTo(segment[i].x, segment[i].y)
                    ctx.strokeStyle = grad
                    ctx.lineWidth = 1.5
                    ctx.stroke()

                    // Glowing dot at head
                    const glow = ctx.createRadialGradient(head.x, head.y, 0, head.x, head.y, 7)
                    glow.addColorStop(0, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},1)`)
                    glow.addColorStop(0.3, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},0.7)`)
                    glow.addColorStop(0.6, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},0.2)`)
                    glow.addColorStop(1, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},0)`)
                    ctx.beginPath()
                    ctx.arc(head.x, head.y, 7, 0, Math.PI * 2)
                    ctx.fillStyle = glow
                    ctx.fill()

                    // Spark particles
                    if (sparks.length < MAX_SPARKS && Math.random() < 0.35) {
                        const angle = Math.random() * Math.PI * 2
                        const spd = 0.3 + Math.random() * 0.6
                        sparks.push({
                            x: head.x,
                            y: head.y,
                            vx: Math.cos(angle) * spd,
                            vy: Math.sin(angle) * spd,
                            color: pkt.color,
                            life: 1,
                            decay: 0.015 + Math.random() * 0.02,
                            size: 0.6 + Math.random() * 1.0,
                        })
                    }
                }
            }

            // ── B: Orbital ring ─────────────────────────────────────────────────
            ctx.beginPath()
            let orbitPen = false
            for (const op of orbitPoints) {
                const rp = rotate(op, currentPhi, currentTheta)
                const { x, y, visible } = project(rp, cx, cy, globeR)
                if (!visible) { orbitPen = false; continue }
                if (!orbitPen) { ctx.moveTo(x, y); orbitPen = true }
                else ctx.lineTo(x, y)
            }
            ctx.strokeStyle = "rgba(100,180,255,0.12)"
            ctx.lineWidth = 0.8
            ctx.setLineDash([4, 6])
            ctx.stroke()
            ctx.setLineDash([])

            // Second orbital ring (different tilt for depth)
            ctx.beginPath()
            let orbit2Pen = false
            const ct2 = Math.cos(ORBIT_TILT * 1.8), st2 = Math.sin(ORBIT_TILT * 1.8)
            for (let i = 0; i <= ORBIT_POINTS; i++) {
                const angle = (i / ORBIT_POINTS) * Math.PI * 2
                const ox = ORBIT_RADIUS * 1.05 * Math.cos(angle)
                const oy0 = ORBIT_RADIUS * 1.05 * Math.sin(angle)
                const op2: Vec3 = [ox, oy0 * ct2, oy0 * st2]
                const rp = rotate(op2, currentPhi + 0.8, currentTheta)
                const { x, y, visible } = project(rp, cx, cy, globeR)
                if (!visible) { orbit2Pen = false; continue }
                if (!orbit2Pen) { ctx.moveTo(x, y); orbit2Pen = true }
                else ctx.lineTo(x, y)
            }
            ctx.strokeStyle = "rgba(140,120,255,0.08)"
            ctx.lineWidth = 0.6
            ctx.setLineDash([2, 8])
            ctx.stroke()
            ctx.setLineDash([])

            // ── Pulsing marker rings ────────────────────────────────────────────
            const now = Date.now()
            for (let i = 0; i < MARKERS.length; i++) {
                const pt = rotate(markerXYZ[i], currentPhi, currentTheta)
                const { x, y, visible } = project(pt, cx, cy, globeR)
                if (!visible || pt[2] < 0.2) continue

                const depthFade = Math.min(1, (pt[2] - 0.2) / 0.3)

                for (let wave = 0; wave < 2; wave++) {
                    const pulse = ((now / 2800 + i * 0.19 + wave * 0.5) % 1)
                    const radius = 3 + pulse * 14
                    const alpha = 0.45 * (1 - pulse) * depthFade
                    ctx.beginPath()
                    ctx.arc(x, y, radius, 0, Math.PI * 2)
                    ctx.strokeStyle = `rgba(25,204,255,${alpha})`
                    ctx.lineWidth = 0.8
                    ctx.stroke()
                }

                const centerGlow = ctx.createRadialGradient(x, y, 0, x, y, 3)
                centerGlow.addColorStop(0, `rgba(25,204,255,${0.8 * depthFade})`)
                centerGlow.addColorStop(1, "rgba(25,204,255,0)")
                ctx.beginPath()
                ctx.arc(x, y, 3, 0, Math.PI * 2)
                ctx.fillStyle = centerGlow
                ctx.fill()
            }

            // ── Arrival bursts ──────────────────────────────────────────────────
            for (let i = bursts.length - 1; i >= 0; i--) {
                const burst = bursts[i]
                const [r, g, bl] = burst.color
                burst.age += 0.025

                if (burst.age >= 1) {
                    bursts.splice(i, 1)
                    continue
                }

                const burstAlpha = 1 - burst.age

                const ringRadius = 4 + burst.age * 20
                ctx.beginPath()
                ctx.arc(burst.x, burst.y, ringRadius, 0, Math.PI * 2)
                ctx.strokeStyle = `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},${burstAlpha * 0.6})`
                ctx.lineWidth = 1.5 * (1 - burst.age)
                ctx.stroke()

                const ringRadius2 = 2 + burst.age * 30
                ctx.beginPath()
                ctx.arc(burst.x, burst.y, ringRadius2, 0, Math.PI * 2)
                ctx.strokeStyle = `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},${burstAlpha * 0.2})`
                ctx.lineWidth = 3 * (1 - burst.age)
                ctx.stroke()

                if (burst.age < 0.3) {
                    const flashAlpha = (1 - burst.age / 0.3) * 0.3
                    const flash = ctx.createRadialGradient(burst.x, burst.y, 0, burst.x, burst.y, 12)
                    flash.addColorStop(0, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},${flashAlpha})`)
                    flash.addColorStop(1, `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},0)`)
                    ctx.beginPath()
                    ctx.arc(burst.x, burst.y, 12, 0, Math.PI * 2)
                    ctx.fillStyle = flash
                    ctx.fill()
                }

                for (const p of burst.particles) {
                    const px = burst.x + Math.cos(p.angle) * p.speed * burst.age * 18
                    const py = burst.y + Math.sin(p.angle) * p.speed * burst.age * 18
                    const pAlpha = burstAlpha * 0.8
                    ctx.beginPath()
                    ctx.arc(px, py, p.size * (1 - burst.age * 0.5), 0, Math.PI * 2)
                    ctx.fillStyle = `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},${pAlpha})`
                    ctx.fill()
                }
            }

            // ── Spark particles ─────────────────────────────────────────────────
            for (let i = sparks.length - 1; i >= 0; i--) {
                const sp = sparks[i]
                sp.x += sp.vx
                sp.y += sp.vy
                sp.vx *= 0.97
                sp.vy *= 0.97
                sp.life -= sp.decay

                if (sp.life <= 0) {
                    sparks.splice(i, 1)
                    continue
                }

                const [r, g, bl] = sp.color
                const alpha = sp.life * 0.8
                ctx.beginPath()
                ctx.arc(sp.x, sp.y, sp.size * sp.life, 0, Math.PI * 2)
                ctx.fillStyle = `rgba(${r * 255 | 0},${g * 255 | 0},${bl * 255 | 0},${alpha})`
                ctx.fill()
            }

            animId = requestAnimationFrame(drawArcs)
        }

        drawArcs()

        // ── pointer interaction ────────────────────────────────────────────────
        const onPointerDown = (e: PointerEvent) => {
            pointerInteracting = e.clientX
            canvas.style.cursor = "grabbing"
        }
        const onPointerUp = () => {
            phi += pointerInteractionMovement
            pointerInteractionMovement = 0
            pointerInteracting = null
            canvas.style.cursor = "grab"
        }
        const onPointerMove = (e: PointerEvent) => {
            if (pointerInteracting !== null) {
                pointerInteractionMovement = (e.clientX - pointerInteracting) * 0.005
            }
            const rect = canvas.getBoundingClientRect()
            const normalY = (e.clientY - rect.top) / rect.height
            dynamicTheta = 0.1 + Math.max(0, Math.min(1, normalY)) * 0.2
        }

        canvas.addEventListener("pointerdown", onPointerDown)
        canvas.addEventListener("pointerup", onPointerUp)
        canvas.addEventListener("pointerout", onPointerUp)
        canvas.addEventListener("pointermove", onPointerMove)

        setTimeout(() => setOpacity(1), 0)

        return () => {
            cancelAnimationFrame(animId)
            canvas.removeEventListener("pointerdown", onPointerDown)
            canvas.removeEventListener("pointerup", onPointerUp)
            canvas.removeEventListener("pointerout", onPointerUp)
            canvas.removeEventListener("pointermove", onPointerMove)
            globe.destroy()
        }
    }, [])

    return (
        <div className="w-[min(72vh,620px)] aspect-square flex-shrink-0 relative">
            <canvas
                ref={canvasRef}
                className="w-full h-full transition-opacity duration-1000 ease-in-out cursor-grab active:cursor-grabbing"
                style={{ opacity }}
            />
            <canvas
                ref={overlayRef}
                className="absolute inset-0 w-full h-full pointer-events-none transition-opacity duration-1000 ease-in-out"
                style={{ opacity }}
            />
        </div>
    )
}
