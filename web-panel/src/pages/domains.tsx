import { useState, useMemo } from "react"
import { PageHeader } from "@/components/shared/page-header"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { RefreshCw, Search, Globe, Shield, FileText, AlertTriangle } from "lucide-react"
import { useSNIs, useDeleteSNI, useRenewSNICert, useSNIUsage } from "@/lib/queries"
import { useQueryClient } from "@tanstack/react-query"
import { cn } from "@/lib/utils"
import { EmptyState } from "@/components/ui/empty-state"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { AddDomainDialog } from "@/components/domains/add-domain-dialog"
import { DomainsTable } from "@/components/domains/domains-table"
import { DomainDetailsDialog } from "@/components/domains/domain-details-dialog"
import { EditDomainDialog } from "@/components/domains/edit-domain-dialog"
import type { SNI } from "@/lib/types"

export default function DomainsPage() {
    const queryClient = useQueryClient()
    const { data: domains = [], isLoading, isRefetching } = useSNIs()

    const [searchQuery, setSearchQuery] = useState("")
    const [deleteId, setDeleteId] = useState<number | null>(null)
    const [renewId, setRenewId] = useState<number | null>(null)
    const [detailsDomain, setDetailsDomain] = useState<SNI | null>(null)
    const [editDomain, setEditDomain] = useState<SNI | null>(null)

    const deleteMutation = useDeleteSNI()
    const renewMutation = useRenewSNICert()
    const { data: deleteUsage } = useSNIUsage(deleteId ?? 0, deleteId !== null)
    const deleteBlocked = (deleteUsage ?? 0) > 0

    const handleRefresh = () => {
        queryClient.invalidateQueries({ queryKey: ['sni'] })
    }

    const filteredDomains = useMemo(() => {
        if (!searchQuery) return domains
        const q = searchQuery.toLowerCase()
        return domains.filter(d =>
            d.name.toLowerCase().includes(q) ||
            d.domain.toLowerCase().includes(q)
        )
    }, [domains, searchQuery])

    // Stats
    const stats = useMemo(() => {
        const total = domains.length
        const acme = domains.filter(d => d.is_auto_issued).length
        const manual = total - acme
        const now = new Date()
        const expiring = domains.filter(d => {
            if (!d.expires_at || d.expires_at === "0001-01-01T00:00:00Z") return false
            const exp = new Date(d.expires_at)
            const daysLeft = Math.ceil((exp.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
            return daysLeft >= 0 && daysLeft <= 30
        }).length
        return { total, acme, manual, expiring }
    }, [domains])

    const handleDelete = () => {
        if (deleteId) {
            deleteMutation.mutate(deleteId, { onSuccess: () => setDeleteId(null) })
        }
    }

    const handleRenew = () => {
        if (renewId) {
            renewMutation.mutate(renewId, { onSuccess: () => setRenewId(null) })
        }
    }

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            <PageHeader
                title="Domains"
                description="Manage domain TLS certificates for SNI routing"
                actions={
                    <>
                        <Button variant="outline" size="sm" onClick={handleRefresh} disabled={isLoading || isRefetching}>
                            <RefreshCw className={cn("h-4 w-4 mr-1.5", (isLoading || isRefetching) && "animate-spin")} />
                            <span className="hidden sm:inline">Refresh</span>
                        </Button>
                        <AddDomainDialog />
                    </>
                }
            />

            {/* Stats Row */}
            {!isLoading && domains.length > 0 && (
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-border/50 bg-card/50">
                        <Globe className="h-4 w-4 text-muted-foreground" />
                        <div>
                            <p className="text-xs text-muted-foreground">Total</p>
                            <p className="text-sm font-semibold">{stats.total}</p>
                        </div>
                    </div>
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-border/50 bg-card/50">
                        <Shield className="h-4 w-4 text-blue-500" />
                        <div>
                            <p className="text-xs text-muted-foreground">ACME</p>
                            <p className="text-sm font-semibold">{stats.acme}</p>
                        </div>
                    </div>
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-border/50 bg-card/50">
                        <FileText className="h-4 w-4 text-emerald-500" />
                        <div>
                            <p className="text-xs text-muted-foreground">Manual</p>
                            <p className="text-sm font-semibold">{stats.manual}</p>
                        </div>
                    </div>
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-border/50 bg-card/50">
                        <AlertTriangle className={cn("h-4 w-4", stats.expiring > 0 ? "text-amber-500" : "text-muted-foreground")} />
                        <div>
                            <p className="text-xs text-muted-foreground">Expiring Soon</p>
                            <p className="text-sm font-semibold">{stats.expiring}</p>
                        </div>
                    </div>
                </div>
            )}

            {/* Search */}
            <div className="flex items-center gap-2">
                <div className="relative flex-1">
                    <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input
                        placeholder="Search domains..."
                        className="pl-9 h-9"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                    />
                </div>
            </div>

            {/* Content */}
            {filteredDomains.length === 0 && !isLoading ? (
                <EmptyState
                    icon={Globe}
                    title="No domains found"
                    description={searchQuery ? "Try a different search query" : "Add a domain with its TLS certificate to get started"}
                />
            ) : (
                <DomainsTable
                    domains={filteredDomains}
                    isLoading={isLoading}
                    onViewDetails={setDetailsDomain}
                    onEdit={setEditDomain}
                    onDelete={setDeleteId}
                    onRenew={setRenewId}
                />
            )}

            {/* Delete Confirmation Dialog */}
            <Dialog open={!!deleteId} onOpenChange={(open) => !open && setDeleteId(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Delete Domain?</DialogTitle>
                        <DialogDescription>
                            {deleteBlocked
                                ? `This domain is in use by ${deleteUsage} inbound${deleteUsage === 1 ? "" : "s"}. Detach or replace its certificate on those inbounds before deleting.`
                                : "Are you sure you want to delete this domain and its certificate? This action cannot be undone."}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setDeleteId(null)}>Cancel</Button>
                        <Button
                            variant="destructive"
                            onClick={handleDelete}
                            disabled={deleteMutation.isPending || deleteBlocked}
                        >
                            {deleteMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                            Delete Domain
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Renew Confirmation Dialog */}
            <Dialog open={!!renewId} onOpenChange={(open) => !open && setRenewId(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Renew Certificate?</DialogTitle>
                        <DialogDescription>
                            This will attempt to renew the ACME certificate for this domain.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setRenewId(null)}>Cancel</Button>
                        <Button
                            onClick={handleRenew}
                            disabled={renewMutation.isPending}
                        >
                            {renewMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                            Renew Certificate
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Details Dialog */}
            <DomainDetailsDialog
                domain={detailsDomain}
                open={!!detailsDomain}
                onOpenChange={(open) => !open && setDetailsDomain(null)}
            />

            {/* Edit Dialog */}
            <EditDomainDialog
                domain={editDomain}
                open={!!editDomain}
                onOpenChange={(open) => !open && setEditDomain(null)}
            />
        </div>
    )
}
