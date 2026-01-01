import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export const protocolColors: Record<string, string> = {
    vless: "bg-purple-500/10 text-purple-500 border-purple-500/20",
    vmess: "bg-blue-500/10 text-blue-500 border-blue-500/20",
    trojan: "bg-red-500/10 text-red-500 border-red-500/20",
    shadowsocks: "bg-green-500/10 text-green-500 border-green-500/20",
    http: "bg-amber-500/10 text-amber-500 border-amber-500/20",
    socks: "bg-orange-500/10 text-orange-500 border-orange-500/20",
    freedom: "bg-cyan-500/10 text-cyan-500 border-cyan-500/20",
    blackhole: "bg-gray-500/10 text-muted-foreground border-gray-500/20",
}

export function ProtocolBadge({ protocol }: { protocol: string }) {
    return (
        <Badge
            variant="outline"
            className={cn("font-mono text-xs", protocolColors[protocol.toLowerCase()] || "")}
        >
            {protocol}
        </Badge>
    )
}
