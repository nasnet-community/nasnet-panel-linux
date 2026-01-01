import { useState, useMemo, useCallback } from "react"
import { PageHeader } from "@/components/shared/page-header"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { RefreshCw, Search, Lock, Globe, Calendar, List, CheckSquare } from "lucide-react"
import { useCertificates, useRevokeCertificate, useDeleteCertificate, useRenewCertificate } from "@/lib/queries"
import { useQueryClient } from "@tanstack/react-query"
import { cn } from "@/lib/utils"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useCertificatesStore } from "@/store/certificates-store"
import { CAStatusBanner } from "@/components/certificates/ca-status-banner"
import { CertStatsRow } from "@/components/certificates/cert-stats-row"
import { CertificatesTable } from "@/components/certificates/certificates-table"
import { SwipeableCertRow } from "@/components/certificates/swipeable-cert-row"
import { BulkActionsBar } from "@/components/certificates/bulk-actions-bar"
import { CertTimeline } from "@/components/certificates/cert-timeline"
import { IssueCertDialog } from "@/components/certificates/issue-cert-dialog"
import { CertDetailsDialog } from "@/components/certificates/cert-details-dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import type { AgentCertificate } from "@/lib/types"
import { toast } from "sonner"

export default function CertificatesPage() {
    const queryClient = useQueryClient()
    const { data: certificates = [], isLoading, isRefetching } = useCertificates()
    const isMobile = useIsMobile()

    const {
        activeTab,
        setActiveTab,
        activeFilter,
        searchQuery,
        setSearchQuery,
        viewMode,
        toggleViewMode,
        selectedIds,
        toggleSelection,
        clearSelection,
    } = useCertificatesStore()

    // Action state for mobile
    const [openRowId, setOpenRowId] = useState<number | null>(null)
    const [detailsCert, setDetailsCert] = useState<AgentCertificate | null>(null)
    const [revokeId, setRevokeId] = useState<number | null>(null)
    const [deleteId, setDeleteId] = useState<number | null>(null)
    const [renewId, setRenewId] = useState<number | null>(null)
    const [multiSelectMode, setMultiSelectMode] = useState(false)

    const revokeMutation = useRevokeCertificate()
    const deleteMutation = useDeleteCertificate()
    const renewMutation = useRenewCertificate()

    const handleRefresh = () => {
        queryClient.invalidateQueries({ queryKey: ['certificates'] })
    }

    // Filter certs by tab
    const tabCerts = useMemo(() => {
        return certificates.filter(c =>
            activeTab === "internal"
                ? (c.type === "ca" || c.type === "master" || c.type === "agent")
                : c.type === "public"
        )
    }, [certificates, activeTab])

    // Apply filter + search
    const filteredCerts = useMemo(() => {
        let certs = tabCerts

        // Status filter
        if (activeFilter && activeFilter !== "all") {
            certs = certs.filter(cert => {
                switch (activeFilter) {
                    case "valid": return cert.is_valid && !cert.is_revoked && cert.days_until_expiry >= 0
                    case "expiring": return !cert.is_revoked && cert.days_until_expiry > 0 && cert.days_until_expiry <= 30
                    case "expired": return cert.days_until_expiry < 0 && !cert.is_revoked
                    case "revoked": return cert.is_revoked
                    default: return true
                }
            })
        }

        // Search
        if (searchQuery) {
            const q = searchQuery.toLowerCase()
            certs = certs.filter(c =>
                c.common_name.toLowerCase().includes(q) ||
                c.serial_number.toLowerCase().includes(q)
            )
        }

        return certs
    }, [tabCerts, activeFilter, searchQuery])

    // Handlers
    const handleRevoke = () => {
        if (revokeId) {
            revokeMutation.mutate(revokeId, { onSuccess: () => setRevokeId(null) })
        }
    }

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

    const handleBulkRevoke = useCallback(async (ids: number[]) => {
        for (const id of ids) {
            try {
                await new Promise<void>((resolve, reject) => {
                    revokeMutation.mutate(id, {
                        onSuccess: () => resolve(),
                        onError: (err) => reject(err),
                    })
                })
            } catch {
                // Continue with next
            }
        }
        clearSelection()
        setMultiSelectMode(false)
        toast.success(`Revoked ${ids.length} certificate(s)`)
    }, [revokeMutation, clearSelection])

    const handleBulkDelete = useCallback(async (ids: number[]) => {
        for (const id of ids) {
            try {
                await new Promise<void>((resolve, reject) => {
                    deleteMutation.mutate(id, {
                        onSuccess: () => resolve(),
                        onError: (err) => reject(err),
                    })
                })
            } catch {
                // Continue with next
            }
        }
        clearSelection()
        setMultiSelectMode(false)
        toast.success(`Deleted ${ids.length} certificate(s)`)
    }, [deleteMutation, clearSelection])

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            {/* Header */}
            <PageHeader
                title="Certificates"
                description="Manage mTLS and public domain certificates"
                actions={
                    <>
                        <Button variant="outline" size="sm" onClick={handleRefresh} disabled={isLoading || isRefetching}>
                            <RefreshCw className={cn("h-4 w-4 mr-1.5", (isLoading || isRefetching) && "animate-spin")} />
                            <span className="hidden sm:inline">Refresh</span>
                        </Button>
                        {activeTab === "public" && <IssueCertDialog />}
                    </>
                }
            />

            {/* CA Status Banner */}
            <CAStatusBanner certificates={certificates} />

            {/* Segmented Tabs */}
            <Tabs
                value={activeTab}
                onValueChange={(v) => setActiveTab(v as "internal" | "public")}
                className="w-full"
            >
                <TabsList className="grid w-full grid-cols-2">
                    <TabsTrigger value="internal" className="gap-1.5">
                        <Lock className="h-3.5 w-3.5" />
                        Internal (mTLS)
                    </TabsTrigger>
                    <TabsTrigger value="public" className="gap-1.5">
                        <Globe className="h-3.5 w-3.5" />
                        Public (Domains)
                    </TabsTrigger>
                </TabsList>
            </Tabs>

            {/* Stats Row */}
            <CertStatsRow certificates={certificates} />

            {/* Search + Controls */}
            <div className="flex items-center gap-2">
                <div className="relative flex-1">
                    <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input
                        placeholder="Search certificates..."
                        className="pl-9 h-9"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                    />
                </div>
                <Button
                    variant="outline"
                    size="icon"
                    className={cn("h-9 w-9 shrink-0", viewMode === "timeline" && "bg-primary text-primary-foreground")}
                    onClick={toggleViewMode}
                    title={viewMode === "list" ? "Timeline View" : "List View"}
                >
                    {viewMode === "list" ? <Calendar className="h-4 w-4" /> : <List className="h-4 w-4" />}
                </Button>
                {isMobile && (
                    <Button
                        variant="outline"
                        size="icon"
                        className={cn("h-9 w-9 shrink-0", multiSelectMode && "bg-primary text-primary-foreground")}
                        onClick={() => {
                            setMultiSelectMode(!multiSelectMode)
                            if (multiSelectMode) clearSelection()
                        }}
                        title="Select Multiple"
                    >
                        <CheckSquare className="h-4 w-4" />
                    </Button>
                )}
            </div>

            {/* Content */}
            {viewMode === "timeline" ? (
                <div className="rounded-lg border border-border/50 p-4 bg-card/50">
                    <CertTimeline
                        certificates={filteredCerts}
                        onCertClick={setDetailsCert}
                    />
                </div>
            ) : isMobile ? (
                /* Mobile card list */
                <div className="rounded-lg border border-border/50 overflow-hidden divide-y-0">
                    {isLoading ? (
                        <div className="py-12 text-center text-muted-foreground text-sm">
                            Loading certificates...
                        </div>
                    ) : filteredCerts.length === 0 ? (
                        <EmptyState
                            icon={activeTab === "internal" ? Lock : Globe}
                            title="No certificates found"
                            description={searchQuery ? "Try a different search query" : "No certificates match the current filter"}
                        />
                    ) : (
                        filteredCerts.map(cert => (
                            <SwipeableCertRow
                                key={cert.id}
                                cert={cert}
                                shouldClose={openRowId !== cert.id}
                                isSelected={selectedIds.has(cert.id)}
                                multiSelectMode={multiSelectMode}
                                onOpen={setOpenRowId}
                                onViewDetails={setDetailsCert}
                                onRenew={(c) => setRenewId(c.id)}
                                onRevoke={(c) => setRevokeId(c.id)}
                                onDelete={(c) => setDeleteId(c.id)}
                                onToggleSelect={toggleSelection}
                            />
                        ))
                    )}
                </div>
            ) : (
                /* Desktop data table */
                <CertificatesTable certificates={filteredCerts} isLoading={isLoading} />
            )}

            {/* Bulk Actions */}
            <BulkActionsBar
                onBulkRevoke={handleBulkRevoke}
                onBulkDelete={handleBulkDelete}
                isRevoking={revokeMutation.isPending}
                isDeleting={deleteMutation.isPending}
            />

            {/* Revoke Confirmation Dialog */}
            <Dialog open={!!revokeId} onOpenChange={(open) => !open && setRevokeId(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Revoke Certificate?</DialogTitle>
                        <DialogDescription>
                            Are you sure you want to revoke this certificate?
                            This action cannot be undone and the certificate will be immediately invalid.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setRevokeId(null)}>Cancel</Button>
                        <Button
                            variant="destructive"
                            onClick={handleRevoke}
                            disabled={revokeMutation.isPending}
                        >
                            {revokeMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                            Revoke Certificate
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <Dialog open={!!deleteId} onOpenChange={(open) => !open && setDeleteId(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Delete Certificate?</DialogTitle>
                        <DialogDescription>
                            Are you sure you want to delete this certificate record?
                            This will remove it from the database entirely.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setDeleteId(null)}>Cancel</Button>
                        <Button
                            variant="destructive"
                            onClick={handleDelete}
                            disabled={deleteMutation.isPending}
                        >
                            {deleteMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                            Delete Certificate
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
                            This will attempt to issue a new certificate for this Common Name.
                            For Public certificates, this will trigger a new HTTP-01 challenge.
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

            {/* Certificate Details Dialog (mobile tap or timeline click) */}
            <CertDetailsDialog
                certificate={detailsCert}
                open={!!detailsCert}
                onOpenChange={(open) => !open && setDetailsCert(null)}
            />
        </div>
    )
}
