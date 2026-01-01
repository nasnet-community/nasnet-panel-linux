import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { MoreHorizontal, Pencil, Trash2, Eye, RefreshCw } from "lucide-react"
import type { SNI } from "@/lib/types"
import { useSNIUsage } from "@/lib/queries"
import { cn } from "@/lib/utils"

interface DomainsTableProps {
    domains: SNI[]
    isLoading: boolean
    onViewDetails: (domain: SNI) => void
    onEdit: (domain: SNI) => void
    onDelete: (id: number) => void
    onRenew: (id: number) => void
}

function getMode(sni: SNI): { label: string; variant: "default" | "secondary" | "outline" } {
    if (sni.is_auto_issued) return { label: "ACME", variant: "default" }
    if (sni.use_path_mode) return { label: "Path", variant: "outline" }
    return { label: "Content", variant: "secondary" }
}

function getExpiryStatus(expiresAt: string): { label: string; className: string } {
    if (!expiresAt || expiresAt === "0001-01-01T00:00:00Z") {
        return { label: "N/A", className: "text-muted-foreground" }
    }
    const now = new Date()
    const expiry = new Date(expiresAt)
    const daysUntil = Math.ceil((expiry.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))

    if (daysUntil < 0) return { label: `Expired ${Math.abs(daysUntil)}d ago`, className: "text-red-500" }
    if (daysUntil <= 7) return { label: `${daysUntil}d left`, className: "text-red-500" }
    if (daysUntil <= 30) return { label: `${daysUntil}d left`, className: "text-amber-500" }
    return { label: expiry.toLocaleDateString(), className: "text-muted-foreground" }
}

// UsedByCell fetches how many inbounds reference this domain's cert. One query
// per row — fine for the small domain list, cached for a minute.
function UsedByCell({ sniId }: { sniId: number }) {
    const { data: usedBy } = useSNIUsage(sniId)
    if (usedBy === undefined) return <span className="text-xs text-muted-foreground">—</span>
    if (usedBy === 0) return <span className="text-xs text-muted-foreground">—</span>
    return <span className="text-xs">{usedBy} inbound{usedBy === 1 ? "" : "s"}</span>
}

export function DomainsTable({ domains, isLoading, onViewDetails, onEdit, onDelete, onRenew }: DomainsTableProps) {
    if (isLoading) {
        return (
            <div className="py-12 text-center text-muted-foreground text-sm">
                Loading domains...
            </div>
        )
    }

    return (
        <div className="rounded-lg border border-border/50 overflow-hidden">
            <div className="overflow-x-auto">
                <table className="w-full text-sm">
                    <thead>
                        <tr className="border-b bg-muted/30">
                            <th className="text-left font-medium text-muted-foreground px-4 py-3">Name</th>
                            <th className="text-left font-medium text-muted-foreground px-4 py-3">Domain</th>
                            <th className="text-left font-medium text-muted-foreground px-4 py-3">Mode</th>
                            <th className="text-left font-medium text-muted-foreground px-4 py-3">Expiry</th>
                            <th className="text-left font-medium text-muted-foreground px-4 py-3">ALPN</th>
                            <th className="text-left font-medium text-muted-foreground px-4 py-3">Used by</th>
                            <th className="text-right font-medium text-muted-foreground px-4 py-3 w-[60px]">Actions</th>
                        </tr>
                    </thead>
                    <tbody className="divide-y divide-border/50">
                        {domains.map((sni) => {
                            const mode = getMode(sni)
                            const expiry = getExpiryStatus(sni.expires_at)
                            return (
                                <tr
                                    key={sni.id}
                                    className="hover:bg-muted/20 transition-colors cursor-pointer"
                                    onClick={() => onViewDetails(sni)}
                                >
                                    <td className="px-4 py-3">
                                        <div className="font-medium">{sni.name}</div>
                                    </td>
                                    <td className="px-4 py-3">
                                        <code className="text-xs bg-muted/50 px-1.5 py-0.5 rounded font-mono">{sni.domain}</code>
                                    </td>
                                    <td className="px-4 py-3">
                                        <Badge variant={mode.variant} className="text-xs">
                                            {mode.label}
                                        </Badge>
                                        {sni.is_auto_issued && sni.auto_renew && (
                                            <Badge variant="outline" className="text-xs ml-1">Auto-renew</Badge>
                                        )}
                                    </td>
                                    <td className="px-4 py-3">
                                        <span className={cn("text-xs", expiry.className)}>{expiry.label}</span>
                                    </td>
                                    <td className="px-4 py-3">
                                        <span className="text-xs text-muted-foreground font-mono">{sni.alpn || "h2,http/1.1"}</span>
                                    </td>
                                    <td className="px-4 py-3">
                                        <UsedByCell sniId={sni.id} />
                                    </td>
                                    <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                                        <DropdownMenu>
                                            <DropdownMenuTrigger asChild>
                                                <Button variant="ghost" size="icon" className="h-8 w-8">
                                                    <MoreHorizontal className="h-4 w-4" />
                                                </Button>
                                            </DropdownMenuTrigger>
                                            <DropdownMenuContent align="end">
                                                <DropdownMenuItem onClick={() => onViewDetails(sni)}>
                                                    <Eye className="h-4 w-4 mr-2" />
                                                    View Details
                                                </DropdownMenuItem>
                                                <DropdownMenuItem onClick={() => onEdit(sni)}>
                                                    <Pencil className="h-4 w-4 mr-2" />
                                                    Edit
                                                </DropdownMenuItem>
                                                {sni.is_auto_issued && (
                                                    <DropdownMenuItem onClick={() => onRenew(sni.id)}>
                                                        <RefreshCw className="h-4 w-4 mr-2" />
                                                        Renew Certificate
                                                    </DropdownMenuItem>
                                                )}
                                                <DropdownMenuSeparator />
                                                <DropdownMenuItem onClick={() => onDelete(sni.id)} className="text-red-600 focus:text-red-600">
                                                    <Trash2 className="h-4 w-4 mr-2" />
                                                    Delete
                                                </DropdownMenuItem>
                                            </DropdownMenuContent>
                                        </DropdownMenu>
                                    </td>
                                </tr>
                            )
                        })}
                    </tbody>
                </table>
            </div>
        </div>
    )
}
