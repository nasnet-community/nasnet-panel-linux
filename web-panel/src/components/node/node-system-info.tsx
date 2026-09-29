import type { ReactNode } from "react"
import { ArrowLeftRight, Clock, Cpu, Loader2, Monitor, Package, Server } from "lucide-react"
import { FaApple, FaCentos, FaLinux, FaRedhat, FaUbuntu, FaWindows } from "react-icons/fa"
import { SiAlpinelinux, SiArchlinux, SiDebian } from "react-icons/si"
import { CopyableText } from "@/components/ui/copyable-text"
import { CountryFlag, countryName } from "@/components/ui/country-flag"
import { useNodeHostInfo } from "@/lib/queries/use-nodes"
import type { Node, NodeOverview } from "@/lib/types"
import { cn, formatBytes } from "@/lib/utils"
import { isLocalNode } from "@/lib/node-kind"
import { T } from "./node-ui"
import { OverviewCard } from "./overview/overview-card"

interface NodeSystemInfoProps {
    node: Node
    overview?: NodeOverview
    compact?: boolean
}

function osIcon(os = "", platform = "") {
    const p = platform.toLowerCase()
    const o = os.toLowerCase()
    const cls = "h-5 w-5 shrink-0"
    if (p.includes("ubuntu")) return <FaUbuntu className={cn(cls, "text-orange-500")} />
    if (p.includes("debian")) return <SiDebian className={cn(cls, "text-red-500")} />
    if (p.includes("centos")) return <FaCentos className={cn(cls, "text-purple-500")} />
    if (p.includes("redhat") || p.includes("rhel")) return <FaRedhat className={cn(cls, "text-red-600")} />
    if (p.includes("arch")) return <SiArchlinux className={cn(cls, "text-blue-500")} />
    if (p.includes("alpine")) return <SiAlpinelinux className={cn(cls, "text-blue-700")} />
    if (o.includes("darwin") || o.includes("macos")) return <FaApple className={cn(cls, "text-gray-500")} />
    if (o.includes("windows")) return <FaWindows className={cn(cls, "text-blue-400")} />
    return <FaLinux className={cn(cls, "text-muted-foreground")} />
}

function uptimeFromBoot(bootTime: number): string {
    if (!bootTime) return "—"
    const s = Math.floor(Date.now() / 1000) - bootTime
    const d = Math.floor(s / 86400)
    const h = Math.floor((s % 86400) / 3600)
    const m = Math.floor((s % 3600) / 60)
    if (d > 0) return `${d}d ${h}h`
    if (h > 0) return `${h}h ${m}m`
    return `${m}m`
}

function Spec({ label, icon, value, sub, align = "center", compact }: {
    label: string
    icon: ReactNode
    value: ReactNode
    sub?: ReactNode
    align?: "center" | "start"
    compact?: boolean
}) {
    return (
        <div className="min-w-0">
            <div className="mb-1.5 text-[12px] leading-4 text-muted-foreground/80">{label}</div>
            <div className={cn("flex gap-2.5", align === "center" ? "items-center" : "items-start")}>
                <span className={cn("flex w-5 shrink-0 justify-center", align === "start" && "pt-px")}>{icon}</span>
                <div className="min-w-0">
                    <div className={cn("font-semibold", compact ? "text-sm leading-[19px]" : "text-[14px] leading-[18px]")}>{value}</div>
                    {sub && <div className={cn("text-muted-foreground", compact ? "text-xs leading-4" : "text-[12px] leading-4")}>{sub}</div>}
                </div>
            </div>
        </div>
    )
}

const MODE_LABEL: Record<NodeOverview["connection"]["mode"], string> = {
    local: "Built in",
    direct: "Direct",
    reverse: "Reverse tunnel",
    stealth: "Stealth tunnel",
}

export function NodeSystemInfo({ node, overview, compact }: NodeSystemInfoProps) {
    const { data: info, isLoading } = useNodeHostInfo(node.id)
    const stale = !node.is_online || info?.stale
    const iconCls = "h-5 w-5"

    const local = isLocalNode(node)
    const agentVersion = overview?.agent.version || node.agent_version
    const mode = overview?.connection.mode ?? (node.is_stealth ? "stealth" : node.connect_mode)
    const rtt = local ? null : overview?.connection.rtt_ms

    return (
        <OverviewCard glow="emerald" title="System specifications" aside={stale && info ? <span className={T.meta}>last known</span> : undefined} className="gap-3.5">
            {isLoading && !info ? (
                <div className="flex min-h-[140px] items-center justify-center">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground/50" />
                </div>
            ) : (
                <>
                    {info && (
                        <div className="grid grid-cols-2 gap-x-3 gap-y-4">
                            <Spec compact={compact} label="Operating system" icon={osIcon(info.os, info.platform)}
                                value={<span className="capitalize">{info.platform || info.os} <span className="font-normal text-muted-foreground">{info.platform_version}</span></span>}
                                sub={<span className="block truncate font-mono" title={info.kernel_version}>{info.kernel_version}</span>} />
                            <Spec compact={compact} label="Processor" icon={<Cpu className={cn(iconCls, "text-blue-400")} />}
                                value={<span className="block truncate" title={info.cpu_model_name}>{info.cpu_model_name || "Unknown CPU"}</span>}
                                sub={`${info.cpu_cores} cores, ${info.arch}`} />
                            <Spec compact={compact} label="Hardware" icon={<Monitor className={cn(iconCls, "text-indigo-400")} />}
                                value={`${formatBytes(info.total_memory)}${compact ? "" : " RAM"}`}
                                sub={info.total_swap > 0 ? `${formatBytes(info.total_swap)} swap` : "No swap"} />
                            <Spec compact={compact} label="System uptime" icon={<Clock className={cn(iconCls, stale ? "text-muted-foreground" : "text-emerald-400")} />}
                                value={stale ? <span className="text-muted-foreground">Unknown</span> : uptimeFromBoot(info.boot_time)}
                                sub={stale ? undefined : "since boot"} />
                        </div>
                    )}
                    {info && <div className="h-px bg-border/60" />}
                    <div className="flex flex-col gap-3.5">
                        <Spec compact={compact} label="Identity"
                            icon={<CountryFlag code={node.country_code} />}
                            value={node.country_code ? countryName(node.country_code) : "Unknown"}
                            sub={node.ip ? <CopyableText text={node.ip} className="font-mono text-[12px] text-muted-foreground" /> : undefined} />
                        <Spec compact={compact} label="Infrastructure" align="start" icon={<Server className={cn(iconCls, "text-indigo-400")} />}
                            value={<span className="font-medium">{node.datacenter || "—"}</span>}
                            sub={info?.hostname ? <>Hostname <span className="font-mono">{info.hostname}</span></> : undefined} />
                        {local ? (
                            <Spec compact={compact} label="Panel" icon={<Package className={cn(iconCls, "text-sky-400")} />}
                                value={<span className="font-mono font-medium">{agentVersion ? `Panel v${agentVersion.replace(/^v/, "")}` : "—"}</span>}
                                sub="Updates with the panel" />
                        ) : (
                            <Spec compact={compact} label="Agent" icon={<Package className={cn(iconCls, "text-sky-400")} />}
                                value={<span className="font-mono font-medium">{agentVersion || "—"}</span>}
                                sub={overview?.agent.update_available ? <span className="text-amber-500">{overview.agent.bundled_version} available</span> : undefined} />
                        )}
                        <Spec compact={compact} label="Connection" icon={<ArrowLeftRight className={cn(iconCls, node.is_online ? "text-teal-400" : "text-red-400")} />}
                            value={MODE_LABEL[mode as NodeOverview["connection"]["mode"]] ?? mode}
                            sub={!node.is_online
                                ? <span className="text-red-400">{mode === "direct" ? "Unreachable" : "Tunnel closed"}</span>
                                : rtt != null ? (rtt < 1 ? "Under 1 ms latency" : `${rtt} ms latency`) : undefined} />
                    </div>
                </>
            )}
        </OverviewCard>
    )
}
