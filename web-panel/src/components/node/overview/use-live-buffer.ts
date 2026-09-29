import { useEffect, useMemo, useState } from "react"
import { useNodeStatsHistory } from "@/lib/queries"
import type { NodeDataPoint, NodeStats } from "@/lib/types"
import type { LivePoint } from "./overview-helpers"

const WINDOW_MS = 5 * 60_000

function fromHistory(d: NodeDataPoint): LivePoint {
    return {
        t: Date.parse(d.created_at),
        up: d.up_speed,
        down: d.down_speed,
        tcp: d.tcp_count ?? 0,
        udp: d.udp_count ?? 0,
        fd: d.fd_count ?? 0,
        users: (d as NodeDataPoint & { online_users?: number }).online_users ?? 0,
    }
}

/**
 * The last five minutes at full 5s resolution for the chart's Live range:
 * seeded once from recorded history, then extended with every reading SSE
 * delivers into the stats cache.
 */
export function useLiveBuffer(nodeId: number, stats: NodeStats | undefined, enabled: boolean): LivePoint[] {
    const { data: history } = useNodeStatsHistory(nodeId, 60, enabled)
    const [appended, setAppended] = useState<LivePoint[]>([])

    useEffect(() => setAppended([]), [nodeId])

    const sampledAt = stats?.stats_unavailable ? undefined : stats?.sampled_at
    useEffect(() => {
        if (!stats || !sampledAt) return
        const t = Date.parse(sampledAt)
        setAppended(prev => {
            if (prev.length && prev[prev.length - 1].t >= t) return prev
            const point: LivePoint = {
                t,
                up: stats.up_speed ?? 0,
                down: stats.down_speed ?? 0,
                tcp: stats.tcp_count ?? 0,
                udp: stats.udp_count ?? 0,
                fd: stats.fd_count ?? 0,
                users: stats.online_users ?? 0,
            }
            return [...prev, point].slice(-80)
        })
        // Only a new sample matters; the rest of `stats` changes with it.
    }, [sampledAt])

    return useMemo(() => {
        const byT = new Map<number, LivePoint>()
        for (const p of (history ?? []).map(fromHistory)) byT.set(p.t, p)
        for (const p of appended) byT.set(p.t, p)
        const all = [...byT.values()].sort((a, b) => a.t - b.t)
        const last = all.length ? all[all.length - 1].t : 0
        return all.filter(p => p.t > last - WINDOW_MS)
    }, [history, appended])
}
