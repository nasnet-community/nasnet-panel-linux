import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Satellite } from "lucide-react"
import { useStarlinkStatus } from "@/lib/queries/use-starlink"

interface StarlinkQuickStatsProps {
    nodeId: number
    isOnline: boolean
    variant: "signal" | "throughput"
    onNavigate: () => void
}

export function StarlinkQuickStats({ nodeId, isOnline, variant, onNavigate }: StarlinkQuickStatsProps) {
    const { data: status } = useStarlinkStatus(nodeId, isOnline)

    if (!status) return null

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

    const hoverShadow = variant === "signal" ? "hover:shadow-sky-500/10" : "hover:shadow-teal-500/10"

    return (
        <Card
            role="button"
            tabIndex={0}
            aria-label={`${variant === "signal" ? "Starlink Signal" : "Starlink Throughput"}. Open Starlink page.`}
            className={`h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg ${hoverShadow} rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5 cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring`}
            onClick={onNavigate}
            onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onNavigate() } }}
        >
            <div className="flex items-center justify-between mb-3 relative z-10">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">
                    {variant === "signal" ? "Starlink Signal" : "Starlink Throughput"}
                </p>
                <Satellite className={`w-4 h-4 text-muted-foreground/30 ${status.available ? "group-hover:text-sky-400" : "text-red-400"} transition-colors`} />
            </div>
            {!status.available ? (
                <p className="text-sm text-red-400 font-medium">Dish Unreachable</p>
            ) : variant === "signal" ? (
                <div className="flex flex-col gap-2.5 mt-2">
                    <div className="flex justify-between items-center text-xs">
                        <span className="text-muted-foreground font-medium">Latency</span>
                        <span className="font-mono font-bold text-foreground text-[13px]">
                            {status.pop_ping_latency_ms.toFixed(0)} ms
                        </span>
                    </div>
                    <div className="flex justify-between items-center text-xs">
                        <span className="text-muted-foreground font-medium">Drop Rate</span>
                        <span className="font-mono font-bold text-foreground text-[13px]">
                            {(status.pop_ping_drop_rate * 100).toFixed(1)}%
                        </span>
                    </div>
                    <div className="flex justify-between items-center text-xs border-t border-white/5 pt-2.5 mt-0.5">
                        <span className="text-muted-foreground font-medium">Obstruction</span>
                        <div className="flex items-center gap-2">
                            <span className="font-mono font-bold text-foreground text-[13px]">
                                {(status.obstruction_fraction * 100).toFixed(1)}%
                            </span>
                            {alertCount > 0 && (
                                <Badge variant="danger" className="text-[10px] px-1.5 py-0">
                                    {alertCount}
                                </Badge>
                            )}
                        </div>
                    </div>
                </div>
            ) : (
                <div className="flex flex-col gap-2.5 mt-2">
                    <div className="flex justify-between items-center text-xs">
                        <span className="text-muted-foreground font-medium">Download</span>
                        <span className="font-mono font-bold text-foreground text-[13px]">
                            {(status.downlink_throughput_bps / 1_000_000).toFixed(0)} Mbps
                        </span>
                    </div>
                    <div className="flex justify-between items-center text-xs">
                        <span className="text-muted-foreground font-medium">Upload</span>
                        <span className="font-mono font-bold text-foreground text-[13px]">
                            {(status.uplink_throughput_bps / 1_000_000).toFixed(0)} Mbps
                        </span>
                    </div>
                    <div className="flex justify-between items-center text-xs border-t border-white/5 pt-2.5 mt-0.5">
                        <span className="text-muted-foreground font-medium">Ethernet</span>
                        <span className="font-mono font-bold text-foreground text-[13px]">
                            {status.eth_speed_mbps >= 1000
                                ? `${(status.eth_speed_mbps / 1000).toFixed(0)} Gbps`
                                : `${status.eth_speed_mbps} Mbps`}
                        </span>
                    </div>
                </div>
            )}
        </Card>
    )
}
