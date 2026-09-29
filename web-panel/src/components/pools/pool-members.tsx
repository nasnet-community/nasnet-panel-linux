import { ArrowDown, ArrowUp } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { protocolColors } from "@/components/node/protocol-badge"
import type { OutboundPool } from "@/lib/api/outbound-pools"
import type { Outbound } from "@/lib/types"
import { cn, formatBytes } from "@/lib/utils"
import { fullDate, relativeTime } from "./pool-display"

const GRID = "grid items-center gap-x-3 grid-cols-[10px_minmax(0,1fr)_76px] sm:grid-cols-[10px_minmax(0,1fr)_72px_88px_76px]"
const head = "text-[12px] font-medium uppercase tracking-wide text-muted-foreground"

/** The outbounds the balancer is using right now, busiest first. */
export function PoolMembers({ pool, members }: { pool: OutboundPool, members: Outbound[] }) {
    if (!members.length) return <div className="px-5 py-10 text-center">
        <p className="text-sm font-medium">No active members</p>
        <p className="text-xs text-muted-foreground mt-1.5">Traffic for <span className="font-mono">{pool.balancer.tag}</span> goes to the fallback <span className="font-mono">{pool.balancer.fallback_tag}</span> until a scan passes.</p>
    </div>
    const sorted = [...members].sort((a, b) => ((b.uplink ?? 0) + (b.downlink ?? 0)) - ((a.uplink ?? 0) + (a.downlink ?? 0)))
    return <div>
        <div className={cn(GRID, "h-9 px-4 sm:px-5 bg-muted/40")}>
            <span />
            <span className={head}>Address</span>
            <span className={cn(head, "hidden sm:block")}>Protocol</span>
            <span className={cn(head, "text-right")}>Traffic</span>
            <span className={cn(head, "text-right hidden sm:block")}>Joined</span>
        </div>
        <ul className="divide-y">
            {sorted.map(member => {
                const traffic = (member.uplink ?? 0) + (member.downlink ?? 0)
                return <li key={member.id} className={cn(GRID, "min-h-12 px-4 sm:px-5 py-2 hover:bg-muted/30 transition-colors")}>
                    <span className="size-2 rounded-full bg-emerald-500 ring-4 ring-emerald-500/15" aria-label="Active" />
                    <div className="min-w-0">
                        <p className="font-mono text-sm font-semibold truncate" title={member.address}>{member.address}{member.port ? `:${member.port}` : ""}</p>
                        <p className="text-[12px] text-muted-foreground truncate font-mono" title={member.tag}>{member.remark || member.tag}</p>
                    </div>
                    <div className="hidden sm:block">
                        <Badge variant="outline" className={cn("font-mono text-[11px] px-1.5 py-0 h-5", protocolColors[member.protocol.toLowerCase()] || "")}>{member.protocol.toUpperCase()}</Badge>
                    </div>
                    <div className="text-right font-mono text-sm tabular-nums text-muted-foreground">
                        {traffic > 0 ? <Tooltip>
                            <TooltipTrigger asChild><span className="cursor-help">{formatBytes(traffic, 1)}</span></TooltipTrigger>
                            <TooltipContent className="font-mono text-xs">
                                <span className="inline-flex items-center gap-1"><ArrowUp className="size-3" />{formatBytes(member.uplink ?? 0)}</span>
                                <span className="mx-2 opacity-50">/</span>
                                <span className="inline-flex items-center gap-1"><ArrowDown className="size-3" />{formatBytes(member.downlink ?? 0)}</span>
                            </TooltipContent>
                        </Tooltip> : <span className="text-muted-foreground/40">—</span>}
                    </div>
                    <span className="hidden sm:block text-right text-xs text-muted-foreground tabular-nums" title={fullDate(member.created_at)}>{relativeTime(member.created_at)}</span>
                </li>
            })}
        </ul>
    </div>
}
