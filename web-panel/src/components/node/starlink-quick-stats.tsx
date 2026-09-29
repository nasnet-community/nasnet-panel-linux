import { useStarlinkStatus } from "@/lib/queries/use-starlink"
import { T } from "./node-ui"
import { OverviewCard, RowDivider, StatRow } from "./overview/overview-card"

interface StarlinkQuickStatsProps {
    nodeId: number
    isOnline: boolean
    variant: "signal" | "throughput"
    onNavigate: () => void
    /** Phone layout: three rows, short labels. */
    compact?: boolean
}

export function StarlinkQuickStats({ nodeId, isOnline, variant, onNavigate, compact }: StarlinkQuickStatsProps) {
    const { data: status } = useStarlinkStatus(nodeId, isOnline)
    const title = variant === "signal"
        ? (compact ? "Starlink" : "Starlink signal")
        : (compact ? "Dish speed" : "Starlink throughput")
    const card = (children: React.ReactNode) => (
        <OverviewCard title={title} glow={variant === "signal" ? "sky" : "teal"} onOpen={onNavigate} openLabel={`${title}. Open the Starlink tab.`}>
            {children}
        </OverviewCard>
    )

    if (!isOnline) {
        return card(
            <p className={T.meta}>
                {variant === "signal" ? "The dish is read through the agent, so its status is unavailable while the node is offline." : "Unavailable while the node is offline."}
            </p>,
        )
    }
    if (!status) return card(<p className={T.meta}>Reading the dish…</p>)
    if (!status.available) return card(<p className="text-sm font-medium text-red-400">Dish unreachable</p>)

    if (variant === "signal") {
        const alertCount = [
            status.alert_thermal_shutdown,
            status.alert_thermal_throttle,
            status.alert_motors_stuck,
            status.alert_no_ethernet_link,
            status.alert_is_heating,
            status.alert_slow_ethernet,
            status.alert_power_save_idle,
            status.alert_mast_not_near_vertical,
            status.alert_roaming,
            status.alert_unexpected_location,
            status.alert_install_pending,
        ].filter(Boolean).length
        return card(
            <div>
                <StatRow label="Latency">{status.pop_ping_latency_ms.toFixed(0)} ms</StatRow>
                <StatRow label={compact ? "Drops" : "Drop rate"}>{(status.pop_ping_drop_rate * 100).toFixed(1)}%</StatRow>
                {!compact && <RowDivider />}
                <StatRow label={compact ? "Obstructed" : "Obstruction"}>{(status.obstruction_fraction * 100).toFixed(1)}%</StatRow>
                {!compact && (
                    <StatRow label="Dish alerts">
                        {alertCount > 0 ? <span className="text-red-400">{alertCount}</span> : <span className="font-medium text-muted-foreground">None</span>}
                    </StatRow>
                )}
            </div>,
        )
    }

    return card(
        <div>
            <StatRow label={compact ? "Down" : "Download"}>{(status.downlink_throughput_bps / 1_000_000).toFixed(0)} Mbps</StatRow>
            <StatRow label={compact ? "Up" : "Upload"}>{(status.uplink_throughput_bps / 1_000_000).toFixed(0)} Mbps</StatRow>
            {!compact && <RowDivider />}
            <StatRow label="Ethernet">
                {status.eth_speed_mbps >= 1000 ? `${(status.eth_speed_mbps / 1000).toFixed(0)} Gbps` : `${status.eth_speed_mbps} Mbps`}
            </StatRow>
        </div>,
    )
}
