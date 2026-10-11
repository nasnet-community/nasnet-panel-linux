import { useMemo } from "react"
import { useNavigate } from "react-router"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { Skeleton } from "@/components/ui/skeleton"
import { useNodeOverview, useNodeStatsSeries, useRefreshMtprotoAllowlist, useMtprotoAllowlist, useMtprotoNode, useVPNStatus } from "@/lib/queries"
import type { Node, NodeStatBucket } from "@/lib/types"
import { DisconnectionHistory } from "@/components/node/disconnection-history"
import { UptimeTimeline } from "@/components/node/uptime-timeline"
import type { SparkPoint } from "@/components/node/overview/area-sparkline"
import { useMtprotoActions } from "./mtproto-actions"
import { Attention } from "./overview/attention"
import { ActivityCard } from "./overview/activity-card"
import { CoverSiteCard } from "./overview/cover-site-card"
import { DetailsCard } from "./overview/details-card"
import { ProxyCard, StreamsCard, ThroughputCard, UsersCard } from "./overview/kpi-cards"
import { MtprotoCharts } from "./overview/mtproto-charts"
import { mtprotoIssues, vpnDown, type IssueAction } from "./overview/mtproto-health"
import { AgentCard, EgressCard, LinkCard, TrafficCard } from "./overview/status-cards"
import { useMtprotoLive, type MtprotoLivePoint } from "./overview/use-mtproto-live"

interface MtprotoOverviewProps {
    node: Node
    onNavigateTab?: (tab: string) => void
}

type Spark = { users: SparkPoint[]; streams: SparkPoint[]; throughput: SparkPoint[] }

/** The last hour on its minute grid, so a missing minute is a gap. */
function hourSparks(points: NodeStatBucket[], from: number, to: number, step: number): Spark {
    const byT = new Map(points.map((p) => [p.t, p]))
    const out: Spark = { users: [], streams: [], throughput: [] }
    for (let t = Math.floor(from / step) * step; t <= to; t += step) {
        const p = byT.get(t)
        const at = t * 1000
        out.users.push({ t: at, v: p ? Math.round(p.users_max) : null })
        out.streams.push({ t: at, v: p ? Math.round(p.tcp_avg) : null })
        out.throughput.push({ t: at, v: p ? p.down_avg + p.up_avg : null })
    }
    return out
}

// until a minute of history exists, the page's own readings draw the sparklines
function liveSparks(points: MtprotoLivePoint[]): Spark {
    return {
        users: points.map((p) => ({ t: p.t, v: p.users })),
        streams: points.map((p) => ({ t: p.t, v: p.streams })),
        throughput: points.map((p) => ({ t: p.t, v: p.down + p.up })),
    }
}

/** An MTProto node at a glance: what needs doing, who's using it, and how its parts are. */
export function MtprotoOverview({ node, onNavigateTab }: MtprotoOverviewProps) {
    const isMobile = useIsMobile()
    const q = useMtprotoNode(node.id)
    const allow = useMtprotoAllowlist()
    const refreshAllow = useRefreshMtprotoAllowlist()
    // the panel's own traffic leaves through the VPN, when there is one
    const vpn = useVPNStatus()
    const navigate = useNavigate()
    const { data: overview } = useNodeOverview(node.id)
    const { data: hour } = useNodeStatsSeries(node.id, "1h")
    const actions = useMtprotoActions(node, q.data, () => void q.refetch())
    const live = useMtprotoLive(node.id, q.data, q.dataUpdatedAt)

    const sparks = useMemo(() => {
        const recorded = hour?.points.length ? hourSparks(hour.points, hour.from, hour.to, hour.step_sec) : null
        return recorded ?? liveSparks(live)
    }, [hour, live])

    if (q.isLoading) {
        return (
            <div className="space-y-4" aria-busy="true">
                <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">{[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-[150px] rounded-2xl" />)}</div>
                <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">{[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-[170px] rounded-2xl" />)}</div>
                <Skeleton className="h-[340px] rounded-2xl" />
            </div>
        )
    }
    if (q.isError || !q.data) {
        return <p role="alert" className="text-sm text-red-500">{(q.error as Error | null)?.message ?? "Could not load this MTProto node"}</p>
    }

    const r = q.data
    const online = node.is_online && actions.linkUp
    const noVPN = vpnDown(vpn.data)
    const issues = mtprotoIssues(node, r, allow.data, noVPN)
    const go = (tab: string) => onNavigateTab?.(tab)
    const onAction = (a: IssueAction) => {
        switch (a) {
            case "repair": return actions.repair()
            case "restart": return void actions.restartProxy()
            case "start-proxy": return actions.startProxy()
            case "update-agent": return void actions.updateAgent()
            case "update-proxy": return void actions.updateProxy()
            case "refresh-list": return refreshAllow.mutate()
            case "tunnels": return go("tunnels")
            case "settings": return go("settings")
            case "logs": return go("logs")
            case "vpn": return navigate("/router?tab=vpn")
        }
    }
    const now = r.throughput || r.link
        ? { down: r.throughput?.down_bps ?? 0, up: r.throughput?.up_bps ?? 0, streams: r.link?.streams ?? 0, users: r.active_users }
        : undefined

    return (
        <div className="@container space-y-4">
            <Attention issues={issues} onAction={onAction} busy={refreshAllow.isPending ? "refresh-list" : null} />

            <div className="grid grid-cols-2 gap-3 md:gap-4 @4xl:grid-cols-4">
                <UsersCard r={r} spark={sparks.users} online={online} />
                <StreamsCard r={r} spark={sparks.streams} online={online} />
                {/* a phone is too narrow for these two side by side */}
                <ThroughputCard r={r} spark={sparks.throughput} online={online} className="col-span-2 @lg:col-span-1" />
                <ProxyCard r={r} actions={actions} compact={isMobile} className="col-span-2 @lg:col-span-1" />
            </div>

            <div className="grid grid-cols-1 gap-3 @xl:grid-cols-2 md:gap-4 @5xl:grid-cols-4">
                <LinkCard r={r} />
                <TrafficCard r={r} today={overview?.today} />
                <EgressCard r={r} allow={allow.data} noVPN={noVPN} onRefresh={() => refreshAllow.mutate()} refreshing={refreshAllow.isPending} />
                <AgentCard r={r} linkUp={actions.linkUp} onUpdate={() => void actions.updateAgent()} />
            </div>

            <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
                <MtprotoCharts node={node} live={live} now={now} compact={isMobile} className="lg:col-span-2" />
                <DetailsCard node={node} r={r} />
            </div>

            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                <CoverSiteCard r={r} linkUp={actions.linkUp} onChange={() => go("tunnels")} />
                <ActivityCard nodeId={node.id} />
            </div>

            <UptimeTimeline nodeId={node.id} isOnline={node.is_online} createdAt={node.created_at} />
            <DisconnectionHistory nodeId={node.id} />
            {actions.dialogs}
        </div>
    )
}
