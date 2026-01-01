import { useState } from "react"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Checkbox } from "@/components/ui/checkbox"
import { AgentCertificate } from "@/lib/types"
import { MoreHorizontal, Trash2, Copy, Eye, XCircle, RefreshCw, Download, ArrowUpDown, ArrowUp, ArrowDown } from "lucide-react"
import { cn, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"
import { useRevokeCertificate, useDeleteCertificate, useRenewCertificate } from "@/lib/queries"
import { CertDetailsDialog } from "./cert-details-dialog"
import { useCertificatesStore, type CertSortField } from "@/store/certificates-store"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

interface CertificatesTableProps {
    certificates: AgentCertificate[]
    isLoading: boolean
}

function getTypeBadgeClass(type: string) {
    switch (type) {
        case "ca": return "border-purple-200 bg-purple-50 text-purple-700 dark:border-purple-900 dark:bg-purple-950/30 dark:text-purple-400"
        case "master": return "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-400"
        case "public": return "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-900 dark:bg-orange-950/30 dark:text-orange-400"
        case "agent": return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-400"
        default: return ""
    }
}

function getExpiryColor(days: number) {
    if (days < 0) return "text-destructive"
    if (days <= 7) return "text-red-600 dark:text-red-400"
    if (days <= 30) return "text-amber-600 dark:text-amber-400"
    return "text-emerald-600 dark:text-emerald-400"
}

function getProgressIndicatorColor(days: number) {
    if (days < 0) return "[&>div]:bg-muted-foreground/30"
    if (days <= 7) return "[&>div]:bg-red-500"
    if (days <= 30) return "[&>div]:bg-amber-500"
    return "[&>div]:bg-emerald-500"
}

export function CertificatesTable({ certificates, isLoading }: CertificatesTableProps) {
    const [revokeId, setRevokeId] = useState<number | null>(null)
    const [deleteId, setDeleteId] = useState<number | null>(null)
    const [renewId, setRenewId] = useState<number | null>(null)
    const [detailsCert, setDetailsCert] = useState<AgentCertificate | null>(null)

    const { sortBy, sortDir, toggleSort, selectedIds, toggleSelection, selectAll, clearSelection } = useCertificatesStore()

    const revokeMutation = useRevokeCertificate()
    const deleteMutation = useDeleteCertificate()
    const renewMutation = useRenewCertificate()

    // Sort certificates
    const sortedCerts = [...certificates].sort((a, b) => {
        let cmp = 0
        switch (sortBy) {
            case "name": cmp = a.common_name.localeCompare(b.common_name); break
            case "expiry": cmp = a.days_until_expiry - b.days_until_expiry; break
            case "created": cmp = new Date(a.created_at).getTime() - new Date(b.created_at).getTime(); break
            case "type": cmp = a.type.localeCompare(b.type); break
        }
        return sortDir === "asc" ? cmp : -cmp
    })

    const selectableCerts = sortedCerts.filter(c => c.type !== "ca")
    const allSelected = selectableCerts.length > 0 && selectableCerts.every(c => selectedIds.has(c.id))
    const someSelected = selectableCerts.some(c => selectedIds.has(c.id))

    const handleSelectAll = () => {
        if (allSelected) {
            clearSelection()
        } else {
            selectAll(selectableCerts.map(c => c.id))
        }
    }

    const handleCopy = async (text: string, label?: string) => {
        await copyToClipboard(text)
        toast.success(label ? `${label} copied` : "Copied to clipboard")
    }

    const handleDownload = (cert: AgentCertificate) => {
        const content = `Certificate: ${cert.common_name}\nSerial: ${cert.serial_number}\nType: ${cert.type}\nValid Until: ${new Date(cert.not_after).toISOString()}\n\nTo download the full PEM file, click View Details -> Certificate tab.`
        const blob = new Blob([content], { type: "text/plain" })
        const url = URL.createObjectURL(blob)
        const a = document.createElement("a")
        a.href = url
        a.download = `${cert.common_name.replace(/[^a-zA-Z0-9]/g, "_")}.txt`
        a.click()
        URL.revokeObjectURL(url)
        toast.success("Certificate info downloaded")
    }

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

    const SortIcon = ({ field }: { field: CertSortField }) => {
        if (sortBy !== field) return <ArrowUpDown className="h-3 w-3 ml-1 opacity-30" />
        return sortDir === "asc"
            ? <ArrowUp className="h-3 w-3 ml-1" />
            : <ArrowDown className="h-3 w-3 ml-1" />
    }

    const SortableHeader = ({ field, children, className }: { field: CertSortField; children: React.ReactNode; className?: string }) => (
        <TableHead className={className}>
            <button
                className="flex items-center hover:text-foreground transition-colors -ml-1 px-1"
                onClick={() => toggleSort(field)}
            >
                {children}
                <SortIcon field={field} />
            </button>
        </TableHead>
    )

    return (
        <div className="space-y-4">
            {/* Table */}
            <div className="rounded-md border">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[40px]">
                                <Checkbox
                                    checked={allSelected ? true : someSelected ? "indeterminate" : false}
                                    onCheckedChange={handleSelectAll}
                                    aria-label="Select all"
                                />
                            </TableHead>
                            <SortableHeader field="type">Type</SortableHeader>
                            <SortableHeader field="name">Common Name</SortableHeader>
                            <TableHead className="hidden md:table-cell">Serial</TableHead>
                            <TableHead>Status</TableHead>
                            <SortableHeader field="expiry">Expires</SortableHeader>
                            <TableHead className="w-[80px]">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {isLoading ? (
                            <TableRow>
                                <TableCell colSpan={7} className="h-24 text-center">
                                    Loading certificates...
                                </TableCell>
                            </TableRow>
                        ) : sortedCerts.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={7} className="h-24 text-center text-muted-foreground">
                                    No certificates found matching your filters.
                                </TableCell>
                            </TableRow>
                        ) : (
                            sortedCerts.map((cert) => {
                                const totalDays = Math.max(
                                    Math.round((new Date(cert.not_after).getTime() - new Date(cert.not_before || cert.created_at).getTime()) / (1000 * 60 * 60 * 24)),
                                    1
                                )
                                const progressPercent = Math.max(0, Math.min(100, (cert.days_until_expiry / totalDays) * 100))

                                return (
                                    <TableRow key={cert.id} className={cn(selectedIds.has(cert.id) && "bg-muted/50")}>
                                        <TableCell>
                                            {cert.type !== "ca" ? (
                                                <Checkbox
                                                    checked={selectedIds.has(cert.id)}
                                                    onCheckedChange={() => toggleSelection(cert.id)}
                                                    aria-label={`Select ${cert.common_name}`}
                                                />
                                            ) : <div className="w-4" />}
                                        </TableCell>
                                        <TableCell>
                                            <Badge variant="outline" className={cn(
                                                "capitalize font-normal",
                                                getTypeBadgeClass(cert.type),
                                            )}>
                                                {cert.type}
                                            </Badge>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-2 group">
                                                <span className="font-mono text-sm">{cert.common_name}</span>
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity"
                                                    onClick={() => handleCopy(cert.common_name, "Common name")}
                                                >
                                                    <Copy className="h-3 w-3 text-muted-foreground" />
                                                </Button>
                                            </div>
                                        </TableCell>
                                        <TableCell className="hidden md:table-cell">
                                            <div className="flex items-center gap-2 group">
                                                <span className="font-mono text-xs text-muted-foreground">
                                                    {cert.serial_number.length > 16 ? `${cert.serial_number.slice(0, 16)}...` : cert.serial_number}
                                                </span>
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-5 w-5 opacity-0 group-hover:opacity-100 transition-opacity"
                                                    onClick={() => handleCopy(cert.serial_number, "Serial number")}
                                                >
                                                    <Copy className="h-3 w-3 text-muted-foreground" />
                                                </Button>
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            {getStatusBadge(cert)}
                                        </TableCell>
                                        <TableCell>
                                            <div className="space-y-1 min-w-[100px]">
                                                <span className={cn("text-sm font-medium", getExpiryColor(cert.days_until_expiry))}>
                                                    {cert.days_until_expiry > 0 ? `${cert.days_until_expiry} days` : "Expired"}
                                                </span>
                                                <Progress
                                                    value={progressPercent}
                                                    className={cn("h-1", getProgressIndicatorColor(cert.days_until_expiry))}
                                                />
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            <DropdownMenu>
                                                <DropdownMenuTrigger asChild>
                                                    <Button variant="ghost" size="icon" className="h-8 w-8">
                                                        <MoreHorizontal className="h-4 w-4" />
                                                    </Button>
                                                </DropdownMenuTrigger>
                                                <DropdownMenuContent align="end">
                                                    <DropdownMenuLabel>Actions</DropdownMenuLabel>
                                                    <DropdownMenuItem onClick={() => handleCopy(cert.common_name, "Common name")}>
                                                        <Copy className="mr-2 h-4 w-4" />
                                                        Copy CN
                                                    </DropdownMenuItem>
                                                    <DropdownMenuItem onClick={() => setDetailsCert(cert)}>
                                                        <Eye className="mr-2 h-4 w-4" />
                                                        View Details
                                                    </DropdownMenuItem>
                                                    <DropdownMenuItem onClick={() => handleDownload(cert)}>
                                                        <Download className="mr-2 h-4 w-4" />
                                                        Download Info
                                                    </DropdownMenuItem>
                                                    {!cert.is_revoked && cert.type !== "ca" && (
                                                        <>
                                                            <DropdownMenuItem onClick={() => setRenewId(cert.id)}>
                                                                <RefreshCw className="mr-2 h-4 w-4" />
                                                                Reissue / Renew
                                                            </DropdownMenuItem>
                                                            <DropdownMenuItem onClick={() => setRevokeId(cert.id)}>
                                                                <XCircle className="mr-2 h-4 w-4" />
                                                                Revoke
                                                            </DropdownMenuItem>
                                                        </>
                                                    )}
                                                    {cert.type !== "ca" && (
                                                        <DropdownMenuItem
                                                            className="text-destructive focus:text-destructive"
                                                            onClick={() => setDeleteId(cert.id)}
                                                        >
                                                            <Trash2 className="mr-2 h-4 w-4" />
                                                            Delete
                                                        </DropdownMenuItem>
                                                    )}
                                                </DropdownMenuContent>
                                            </DropdownMenu>
                                        </TableCell>
                                    </TableRow>
                                )
                            })
                        )}
                    </TableBody>
                </Table>
            </div>

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

            {/* Certificate Details Dialog */}
            <CertDetailsDialog
                certificate={detailsCert}
                open={!!detailsCert}
                onOpenChange={(open) => !open && setDetailsCert(null)}
            />
        </div>
    )
}

function getStatusBadge(cert: AgentCertificate) {
    if (cert.is_revoked) {
        return <Badge variant="danger">Revoked</Badge>
    }
    if (cert.days_until_expiry < 0) {
        return <Badge variant="danger">Expired</Badge>
    }
    if (cert.days_until_expiry < 30) {
        return <Badge variant="warning">Expiring</Badge>
    }
    return <Badge variant="success">Valid</Badge>
}
