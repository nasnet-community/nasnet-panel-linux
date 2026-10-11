import { useEffect, useState } from "react"
import type { MtprotoNode } from "@/lib/api/mtproto"

/** One reading of the node page's 10 s poll. */
export interface MtprotoLivePoint {
    t: number
    down: number
    up: number
    streams: number
    users: number | null
    rtt: number | null
}

const KEEP = 90 // 15 minutes of 10 s polls

/** Average round trip of the connections that are up, or null with none. */
export function avgRtt(r: MtprotoNode | undefined): number | null {
    const up = (r?.link?.conns ?? []).filter((c) => c.up)
    if (!up.length) return null
    return Math.round(up.reduce((s, c) => s + c.rtt_ms, 0) / up.length)
}

/** Readings collected while the page is open, for the Live range and the link sparkline. */
export function useMtprotoLive(nodeId: number, r: MtprotoNode | undefined, updatedAt: number): MtprotoLivePoint[] {
    const [points, setPoints] = useState<MtprotoLivePoint[]>([])
    useEffect(() => setPoints([]), [nodeId])
    useEffect(() => {
        if (!r?.running || !updatedAt) return
        const p: MtprotoLivePoint = {
            t: updatedAt,
            down: r.throughput?.down_bps ?? 0,
            up: r.throughput?.up_bps ?? 0,
            streams: r.link?.streams ?? 0,
            users: r.active_users ?? null,
            rtt: avgRtt(r),
        }
        setPoints((prev) => (prev.length && prev[prev.length - 1].t >= p.t ? prev : [...prev, p].slice(-KEEP)))
        // one point per fetch; the rest of r changes with it
    }, [updatedAt])
    return points
}
