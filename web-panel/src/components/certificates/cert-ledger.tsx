import { useMemo } from "react"
import { ChevronRight, Eye, MoreHorizontal, Search, Trash2, XCircle } from "lucide-react"
import { toast } from "@/lib/toast"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useDeleteCert, useRevokeCert } from "@/lib/queries"
import type { CertSummary } from "@/lib/types"
import { cn, formatDate } from "@/lib/utils"
import { useCertificatesStore, type LedgerFilter } from "@/store/certificates-store"
import { certErrorMessage, certTypeLabel, daysLabel, isCurrentCertRefusal, toneBadge, type Tone } from "./cert-helpers"
import { cardShell, T } from "./cert-ui"

type Status = "valid" | "expiring" | "expired" | "revoked" | "orphan" | "legacy"

const STATUS: Record<Status, { label: string; tone: Tone }> = {
    valid: { label: "Valid", tone: "ok" },
    expiring: { label: "Expiring", tone: "warn" },
    expired: { label: "Expired", tone: "danger" },
    revoked: { label: "Revoked", tone: "muted" },
    orphan: { label: "Orphan", tone: "warn" },
    legacy: { label: "Legacy", tone: "muted" },
}

const FILTERS: { key: LedgerFilter; label: string }[] = [
    { key: "all", label: "All" },
    { key: "valid", label: "Valid" },
    { key: "expiring", label: "Expiring" },
    { key: "revoked", label: "Revoked" },
    { key: "orphan", label: "Orphan" },
    { key: "legacy", label: "Legacy" },
]

const CURRENT_MSG = "That is a node's current certificate. Revoke it from the node row."

function statusOf(c: CertSummary): Status {
    if (c.is_revoked) return "revoked"
    if (c.days_left < 0) return "expired"
    if (c.orphan) return "orphan"
    if (c.type === "master" || c.type === "public") return "legacy"
    if (c.days_left <= 30) return "expiring"
    return "valid"
}

function matches(c: CertSummary, f: LedgerFilter): boolean {
    if (f === "all") return true
    if (f === "legacy") return c.type === "master" || c.type === "public"
    return statusOf(c) === f
}

export function CertLedger({ certificates, onView }: { certificates: CertSummary[]; onView: (c: CertSummary) => void }) {
    const { ledgerOpen, setLedgerOpen, filter, setFilter, search, setSearch, selected, toggleSelected, setSelected, clearSelected } = useCertificatesStore()
    const confirm = useConfirm()
    const revoke = useRevokeCert()
    const del = useDeleteCert()
    const isMobile = useIsMobile()

    const rows = useMemo(() => {
        const q = search.trim().toLowerCase()
        return certificates.filter(c => matches(c, filter) && (!q
            || c.common_name.toLowerCase().includes(q)
            || c.serial.toLowerCase().includes(q)
            || (c.node_name ?? "").toLowerCase().includes(q)))
    }, [certificates, filter, search])
    const selectable = rows.filter(c => c.type !== "ca")
    const picked = selectable.filter(c => selected.has(c.id))
    const allPicked = selectable.length > 0 && picked.length === selectable.length

    const single = (c: CertSummary, kind: "revoke" | "delete") => void confirm({
        title: kind === "revoke" ? `Revoke ${c.common_name}?` : `Delete ${c.common_name}?`,
        description: kind === "revoke"
            ? "Revoked certificates stay on the denylist until they expire."
            : "A valid certificate is revoked first, so it stays on the denylist until it expires.",
        variant: "destructive",
        confirmLabel: kind === "revoke" ? "Revoke" : "Delete",
        onConfirm: async () => {
            try {
                if (kind === "revoke") await revoke.mutateAsync({ id: c.id })
                else await del.mutateAsync({ id: c.id })
                toast.success(kind === "revoke" ? "Certificate revoked" : "Certificate deleted")
            } catch (e) {
                toast.error(isCurrentCertRefusal(e) ? CURRENT_MSG : certErrorMessage(e))
                throw e
            }
        },
    })

    const bulk = (kind: "revoke" | "delete") => {
        const targets = kind === "revoke" ? picked.filter(c => !c.is_revoked) : picked
        if (targets.length === 0) return
        void confirm({
            title: `${kind === "revoke" ? "Revoke" : "Delete"} ${targets.length} certificate${targets.length === 1 ? "" : "s"}?`,
            description: "Current node certificates are skipped; handle those from the node row.",
            variant: "destructive",
            confirmLabel: kind === "revoke" ? "Revoke" : "Delete",
            onConfirm: async () => {
                let done = 0
                let skipped = 0
                const failed: string[] = []
                for (const c of targets) {
                    try {
                        if (kind === "revoke") await revoke.mutateAsync({ id: c.id })
                        else await del.mutateAsync({ id: c.id })
                        done++
                    } catch (e) {
                        if (isCurrentCertRefusal(e)) skipped++
                        else failed.push(`${c.common_name}: ${certErrorMessage(e)}`)
                    }
                }
                clearSelected()
                const parts = [`${kind === "revoke" ? "Revoked" : "Deleted"} ${done}`]
                if (skipped) parts.push(`skipped ${skipped} current node cert${skipped === 1 ? "" : "s"}`)
                if (failed.length) toast.warning(`${parts.join(", ")}. Failed: ${failed.join("; ")}`)
                else toast.success(parts.join(", "))
            },
        })
    }

    const menu = (c: CertSummary) => (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" className="h-11 w-11 sm:h-8 sm:w-8" aria-label={`Actions for ${c.common_name}`}>
                    <MoreHorizontal className="h-4 w-4" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => onView(c)}><Eye className="mr-2 h-4 w-4" />View</DropdownMenuItem>
                {c.type !== "ca" && !c.is_revoked && (
                    <DropdownMenuItem onClick={() => single(c, "revoke")}><XCircle className="mr-2 h-4 w-4" />Revoke</DropdownMenuItem>
                )}
                {c.type !== "ca" && (
                    <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={() => single(c, "delete")}>
                        <Trash2 className="mr-2 h-4 w-4" />Delete
                    </DropdownMenuItem>
                )}
            </DropdownMenuContent>
        </DropdownMenu>
    )

    const statusBadge = (c: CertSummary) => {
        const s = STATUS[statusOf(c)]
        return <Badge variant="outline" className={cn("px-2 py-0 text-[12px]", toneBadge[s.tone])}>{s.label}</Badge>
    }

    return (
        <Card className={cn(cardShell, "gap-0 p-0")}>
            <button type="button" className="flex min-h-12 w-full items-center gap-2 px-4 text-left" onClick={() => setLedgerOpen(!ledgerOpen)} aria-expanded={ledgerOpen}>
                <ChevronRight className={cn("h-4 w-4 text-muted-foreground transition-transform", ledgerOpen && "rotate-90")} />
                <span className={T.eyebrow}>All certificates · {certificates.length}</span>
            </button>
            {ledgerOpen && (
                <div className="flex flex-col gap-3 border-t border-border/60 p-4">
                    <div className="relative">
                        <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                        <Input className="h-9 pl-9" placeholder="Search by name, serial or node" value={search} onChange={e => setSearch(e.target.value)} />
                    </div>
                    <div className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-1">
                        {FILTERS.map(f => {
                            const n = certificates.filter(c => matches(c, f.key)).length
                            return (
                                <button key={f.key} type="button" onClick={() => setFilter(f.key)}
                                    className={cn("inline-flex h-8 shrink-0 items-center gap-1.5 rounded-full border px-3 text-xs font-medium transition-colors",
                                        filter === f.key ? "border-transparent bg-foreground text-background" : "border-border/60 hover:border-border")}>
                                    {f.label}<span className="text-[11px] opacity-70">{n}</span>
                                </button>
                            )
                        })}
                    </div>
                    {picked.length > 0 && (
                        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border/60 bg-muted/30 px-3 py-2 text-sm">
                            <span>{picked.length} selected</span>
                            <Button size="sm" variant="outline" className="h-8" onClick={() => bulk("revoke")}>Revoke</Button>
                            <Button size="sm" variant="outline" className="h-8 text-destructive" onClick={() => bulk("delete")}>Delete</Button>
                            <Button size="sm" variant="ghost" className="h-8" onClick={clearSelected}>Clear</Button>
                        </div>
                    )}
                    {rows.length === 0 ? (
                        <p className={T.meta}>No certificates match.</p>
                    ) : isMobile ? (
                        <ul className="flex flex-col divide-y divide-border/60">
                            {rows.map(c => (
                                <li key={c.id} className="flex items-center gap-3 py-2">
                                    {c.type !== "ca"
                                        ? <Checkbox checked={selected.has(c.id)} onCheckedChange={() => toggleSelected(c.id)} aria-label={`Select ${c.common_name}`} />
                                        : <span className="w-4" />}
                                    <button type="button" className="min-w-0 flex-1 text-left" onClick={() => onView(c)}>
                                        <div className="truncate font-mono text-sm">{c.common_name}</div>
                                        <div className={cn(T.meta, "flex items-center gap-1.5")}>
                                            <span>{certTypeLabel(c.type)}</span>
                                            {c.node_name && <span>· {c.node_name}</span>}
                                            <span>· {daysLabel(c.days_left)}</span>
                                        </div>
                                    </button>
                                    {statusBadge(c)}
                                    {menu(c)}
                                </li>
                            ))}
                        </ul>
                    ) : (
                        <div className="rounded-lg border border-border/60">
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead className="w-10">
                                            <Checkbox checked={allPicked ? true : picked.length > 0 ? "indeterminate" : false}
                                                onCheckedChange={() => (allPicked ? clearSelected() : setSelected(selectable.map(c => c.id)))} aria-label="Select all" />
                                        </TableHead>
                                        <TableHead>Type</TableHead>
                                        <TableHead>Name</TableHead>
                                        <TableHead>Serial</TableHead>
                                        <TableHead>Status</TableHead>
                                        <TableHead>Expires</TableHead>
                                        <TableHead className="w-12" />
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {rows.map(c => (
                                        <TableRow key={c.id} className={cn(selected.has(c.id) && "bg-muted/40")}>
                                            <TableCell>
                                                {c.type !== "ca" && <Checkbox checked={selected.has(c.id)} onCheckedChange={() => toggleSelected(c.id)} aria-label={`Select ${c.common_name}`} />}
                                            </TableCell>
                                            <TableCell className="text-xs text-muted-foreground">{certTypeLabel(c.type)}</TableCell>
                                            <TableCell>
                                                <button type="button" className="text-left" onClick={() => onView(c)}>
                                                    <div className="font-mono text-sm hover:underline">{c.common_name}</div>
                                                    {c.node_name && <div className={T.meta}>{c.node_name}</div>}
                                                </button>
                                            </TableCell>
                                            <TableCell className="font-mono text-xs text-muted-foreground">{c.serial.slice(0, 14)}…</TableCell>
                                            <TableCell>{statusBadge(c)}</TableCell>
                                            <TableCell className="text-xs">
                                                <div className="tabular-nums">{daysLabel(c.days_left)}</div>
                                                <div className={T.meta}>{formatDate(c.not_after)}</div>
                                            </TableCell>
                                            <TableCell>{menu(c)}</TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </div>
                    )}
                </div>
            )}
        </Card>
    )
}
