import { formatBytes } from "@/lib/utils"
import { Card } from "@/components/ui/card"
import { useNodeHostInfo } from "@/lib/queries/use-nodes"
import { Loader2 } from "lucide-react"
import {
    HiOutlineChip,
    HiOutlineDesktopComputer,
    HiOutlineServer,
    HiOutlineClock,
    HiOutlineGlobeAlt
} from "react-icons/hi"
import { FaLinux, FaUbuntu, FaCentos, FaRedhat, FaWindows, FaApple } from "react-icons/fa"
import { SiArchlinux, SiAlpinelinux, SiDebian } from "react-icons/si"
import { CopyableText } from "@/components/ui/copyable-text"

interface NodeSystemInfoProps {
    nodeId: number
    countryCode?: string
    datacenter?: string
    ip?: string
    hostname?: string
}

export function NodeSystemInfo({ nodeId, countryCode, datacenter, ip, hostname }: NodeSystemInfoProps) {
    const { data: info, isLoading } = useNodeHostInfo(nodeId)

    if (isLoading) {
        return (
            <Card className="p-6 flex items-center justify-center min-h-[140px]">
                <Loader2 className="w-6 h-6 animate-spin text-muted-foreground/50" />
            </Card>
        )
    }

    if (!info) return null

    // Determine OS Icon
    const getOsIcon = (os: string, platform: string) => {
        const p = (platform || "").toLowerCase()
        const o = (os || "").toLowerCase()

        if (p.includes("ubuntu")) return <FaUbuntu className="w-5 h-5 text-orange-500 shrink-0" />
        if (p.includes("debian")) return <SiDebian className="w-5 h-5 text-red-500 shrink-0" />
        if (p.includes("centos")) return <FaCentos className="w-5 h-5 text-purple-500 shrink-0" />
        if (p.includes("redhat") || p.includes("rhel")) return <FaRedhat className="w-5 h-5 text-red-600 shrink-0" />
        if (p.includes("arch")) return <SiArchlinux className="w-5 h-5 text-blue-500 shrink-0" />
        if (p.includes("alpine")) return <SiAlpinelinux className="w-5 h-5 text-blue-700 shrink-0" />
        if (o.includes("darwin") || o.includes("macos")) return <FaApple className="w-5 h-5 text-muted-foreground shrink-0" />
        if (o.includes("windows")) return <FaWindows className="w-5 h-5 text-blue-400 shrink-0" />
        return <FaLinux className="w-5 h-5 text-muted-foreground shrink-0" />
    }

    // Format uptime from boot_time timestamp
    const formatUptimeFromBoot = (bootTime: number) => {
        if (!bootTime) return "—"
        const now = Math.floor(Date.now() / 1000)
        const uptime = now - bootTime

        const days = Math.floor(uptime / 86400)
        const hours = Math.floor((uptime % 86400) / 3600)
        const minutes = Math.floor((uptime % 3600) / 60)

        if (days > 0) return `${days}d ${hours}h ${minutes}m`
        if (hours > 0) return `${hours}h ${minutes}m`
        return `${minutes}m`
    }

    return (
        <Card className="relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-emerald-500/10 rounded-2xl p-5 bg-card/50 backdrop-blur-sm border-white/5 h-full">
            <div className="flex items-center justify-between mb-4 relative z-10">
                <p className="text-[11px] uppercase font-bold text-muted-foreground tracking-widest">System Specifications</p>
                <HiOutlineServer className="w-4 h-4 text-muted-foreground/50 group-hover:text-emerald-500 transition-colors" />
            </div>

            <div className="grid grid-cols-2 gap-x-6 gap-y-4">
                {/* OS Info */}
                <div className="flex flex-col gap-1 min-w-0">
                    <span className="text-xs text-muted-foreground font-medium">Operating System</span>
                    <div className="flex items-center gap-2 min-w-0">
                        {getOsIcon(info.os, info.platform)}
                        <div className="flex flex-col min-w-0">
                            <span className="text-sm font-semibold capitalize text-foreground leading-none">
                                {info.platform || info.os} <span className="text-muted-foreground font-normal">{info.platform_version}</span>
                            </span>
                            <span className="text-[11px] text-muted-foreground font-mono mt-0.5 truncate" title={info.kernel_version}>
                                {info.kernel_version}
                            </span>
                        </div>
                    </div>
                </div>

                {/* CPU Info */}
                <div className="flex flex-col gap-1 min-w-0">
                    <span className="text-xs text-muted-foreground font-medium">Processor</span>
                    <div className="flex items-center gap-2 min-w-0">
                        <HiOutlineChip className="w-5 h-5 text-blue-500 shrink-0" />
                        <div className="flex flex-col min-w-0">
                            <span className="text-sm font-semibold text-foreground leading-none truncate" title={info.cpu_model_name}>
                                {info.cpu_model_name || "Unknown CPU"}
                            </span>
                            <span className="text-[11px] text-muted-foreground mt-0.5">
                                {info.cpu_cores} Cores / {info.arch}
                            </span>
                        </div>
                    </div>
                </div>

                {/* Hardware */}
                <div className="flex flex-col gap-1 min-w-0">
                    <span className="text-xs text-muted-foreground font-medium">Hardware</span>
                    <div className="flex items-center gap-2">
                        <HiOutlineDesktopComputer className="w-5 h-5 text-indigo-500 shrink-0" />
                        <div className="flex flex-col">
                            <span className="text-sm font-semibold text-foreground leading-none">
                                {formatBytes(info.total_memory)} <span className="text-muted-foreground font-normal text-xs">RAM</span>
                            </span>
                            {info.virtualization_system && (
                                <span className="text-[11px] text-muted-foreground mt-0.5 capitalize">
                                    {info.virtualization_system} {info.virtualization_role}
                                </span>
                            )}
                        </div>
                    </div>
                </div>

                {/* Uptime */}
                <div className="flex flex-col gap-1">
                    <span className="text-xs text-muted-foreground font-medium">System Uptime</span>
                    <div className="flex items-center gap-2">
                        <HiOutlineClock className="w-5 h-5 text-emerald-500 shrink-0" />
                        <div className="flex flex-col">
                            <span className="text-sm font-semibold text-foreground leading-none">
                                {formatUptimeFromBoot(info.boot_time)}
                            </span>
                            <span className="text-[11px] text-muted-foreground mt-0.5">
                                Since Boot
                            </span>
                        </div>
                    </div>
                </div>

                {/* Identity */}
                <div className="flex flex-col gap-1">
                    <span className="text-xs text-muted-foreground font-medium">Identity</span>
                    <div className="flex items-center gap-2">
                        <HiOutlineGlobeAlt className="w-5 h-5 text-teal-500 shrink-0" />
                        <div className="flex flex-col">
                            <span className="text-sm font-semibold text-foreground uppercase">
                                {countryCode || "Unknown"}
                            </span>
                            <CopyableText
                                text={ip || "—"}
                                className="text-[11px] text-muted-foreground mt-0.5"
                            />
                        </div>
                    </div>
                </div>

                {/* Infrastructure */}
                <div className="flex flex-col gap-1 min-w-0">
                    <span className="text-xs text-muted-foreground font-medium">Infrastructure</span>
                    <div className="flex items-center gap-2 min-w-0">
                        <HiOutlineServer className="w-5 h-5 text-indigo-400 shrink-0" />
                        <div className="flex flex-col min-w-0">
                            <span className="text-sm font-semibold text-foreground truncate" title={datacenter}>
                                {datacenter || "—"}
                            </span>
                            <span className="text-[11px] text-muted-foreground mt-0.5 truncate" title={info.hostname}>
                                {info.hostname || "—"}
                            </span>
                        </div>
                    </div>
                </div>
            </div>
        </Card>
    )
}
