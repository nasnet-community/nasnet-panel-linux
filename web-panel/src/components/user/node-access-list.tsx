import { useUserAccounts } from "@/lib/queries"
import type { UserAccountInfo } from "@/lib/types"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

interface NodeAccessListProps {
    userId: number
}

function countryFlag(code: string): string {
    if (!code || code.length !== 2) return ""
    return String.fromCodePoint(
        ...code.toUpperCase().split("").map(c => 0x1F1E6 + c.charCodeAt(0) - 65)
    )
}

function StatusDot({ status }: { status: string }) {
    return (
        <span className={cn(
            "inline-block w-1.5 h-1.5 rounded-full shrink-0",
            status === "active" ? "bg-emerald-500" :
            status === "disabled" ? "bg-red-500" : "bg-muted-foreground/50"
        )} />
    )
}

export function NodeAccessList({ userId }: NodeAccessListProps) {
    const { data: accounts, isLoading } = useUserAccounts(userId)

    if (isLoading) {
        return (
            <div className="space-y-2">
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
            </div>
        )
    }

    if (!accounts || accounts.length === 0) {
        return (
            <p className="text-xs text-muted-foreground/60 text-center py-2">No active accounts</p>
        )
    }

    // Group by node
    const grouped = accounts.reduce<Record<string, UserAccountInfo[]>>((acc, a) => {
        const key = `${a.node_id}-${a.node_name}`
        if (!acc[key]) acc[key] = []
        acc[key].push(a)
        return acc
    }, {})

    return (
        <div className="space-y-2">
            {Object.entries(grouped).map(([key, nodeAccounts]) => {
                const first = nodeAccounts[0]
                return (
                    <div key={key} className="rounded-md border p-2 space-y-1">
                        <div className="flex items-center gap-1.5 text-xs font-medium">
                            <span>{countryFlag(first.node_country)}</span>
                            <span className="truncate">{first.node_name}</span>
                        </div>
                        {nodeAccounts.map(a => (
                            <div key={a.account_id} className="flex items-center gap-1.5 pl-5 text-[11px] text-muted-foreground">
                                <StatusDot status={a.status} />
                                <Badge variant="outline" className="text-[9px] px-1 py-0 h-4 font-mono uppercase">
                                    {a.protocol}
                                </Badge>
                                <span className="truncate">{a.inbound_tag}</span>
                            </div>
                        ))}
                    </div>
                )
            })}
        </div>
    )
}
