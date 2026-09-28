import { useEffect, useMemo, useState } from "react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Loader2, ChevronRight, CheckCircle2, XCircle, Cpu, Server } from "lucide-react"
import { toast } from "@/lib/toast"
import { formatDate } from "@/lib/utils"
import { useNodes, useBulkUpdateXrayVersion } from "@/lib/queries"
import { getXrayReleases } from "@/lib/api/nodes"
import type { XrayRelease, NodeBulkActionResponse } from "@/lib/api/nodes"

interface BulkXrayVersionDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    selectedIds: number[]
    onComplete?: () => void
}

type Phase = "select" | "running" | "result"

// BulkXrayVersionDialog: select → running → result phases in one dialog
// (each node up to ~5 min server-side).
export function BulkXrayVersionDialog({
    open,
    onOpenChange,
    selectedIds,
    onComplete,
}: BulkXrayVersionDialogProps) {
    const [phase, setPhase] = useState<Phase>("select")
    const [releases, setReleases] = useState<XrayRelease[]>([])
    const [loadingReleases, setLoadingReleases] = useState(false)
    const [selectedVersion, setSelectedVersion] = useState<string | null>(null)
    const [result, setResult] = useState<NodeBulkActionResponse | null>(null)

    const { data: allNodes } = useNodes()
    const bulkMutate = useBulkUpdateXrayVersion()

    // Map id → friendly node descriptor for the target list + result rows.
    const nodesById = useMemo(() => {
        const m = new Map<number, { name: string; ip: string; xrayVersion?: string }>()
        if (allNodes) {
            for (const n of allNodes) {
                m.set(n.id, {
                    name: n.name,
                    ip: n.ip,
                    xrayVersion: (n as { xray_version?: string }).xray_version,
                })
            }
        }
        return m
    }, [allNodes])

    const targetNodes = useMemo(() => {
        return selectedIds
            .map((id) => ({ id, ...nodesById.get(id) }))
            .filter((n) => n.name)
    }, [selectedIds, nodesById])

    // Fetch releases on open. Reset state when reopened so the operator
    // gets a fresh dialog rather than stale "result" view.
    useEffect(() => {
        if (!open) return
        setPhase("select")
        setSelectedVersion(null)
        setResult(null)
        setLoadingReleases(true)
        getXrayReleases()
            .then((res) => {
                if (res.success && res.data) {
                    setReleases(res.data)
                } else {
                    toast.error("Failed to fetch releases")
                }
            })
            .catch(() => toast.error("Failed to fetch releases"))
            .finally(() => setLoadingReleases(false))
    }, [open])

    const handleRun = async () => {
        if (!selectedVersion || selectedIds.length === 0) return
        setPhase("running")
        try {
            const res = await bulkMutate.mutateAsync({
                ids: selectedIds,
                version: selectedVersion,
            })
            setResult(res)
            setPhase("result")
            // List refresh is fired by the mutation's onSuccess already.
            onComplete?.()
        } catch {
            // mutation's onError surfaces a toast; drop back to select so
            // the operator can retry without losing version pick.
            setPhase("select")
        }
    }

    const handleClose = () => {
        if (phase === "running") return // block close while in flight
        onOpenChange(false)
    }

    // Group nodes for the result view so the operator sees failures first.
    const resultRows = useMemo(() => {
        if (!result) return [] as Array<{ id: number; success: boolean; error?: string }>
        const rows = Object.entries(result.results).map(([idStr, r]) => ({
            id: Number(idStr),
            success: r.success,
            error: r.error,
        }))
        rows.sort((a, b) => Number(a.success) - Number(b.success))
        return rows
    }, [result])

    return (
        <Dialog open={open} onOpenChange={handleClose}>
            <DialogContent className="max-w-2xl">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <Cpu className="w-4 h-4 text-indigo-500" />
                        Bulk Xray-core Update
                    </DialogTitle>
                    <DialogDescription>
                        {phase === "select" && (
                            <>
                                Updating <span className="font-mono font-medium text-foreground">{selectedIds.length}</span>{" "}
                                node{selectedIds.length === 1 ? "" : "s"}. Each node is updated independently;
                                failures don&apos;t cancel the rest.
                            </>
                        )}
                        {phase === "running" && (
                            <>Pushing to nodes. Up to ~5 minutes per node; 8 run concurrently.</>
                        )}
                        {phase === "result" && result && (
                            <>
                                {result.succeeded} / {result.total} succeeded
                                {result.total - result.succeeded > 0 && (
                                    <> · {result.total - result.succeeded} failed</>
                                )}
                            </>
                        )}
                    </DialogDescription>
                </DialogHeader>

                {phase === "select" && (
                    <div className="grid grid-cols-1 md:grid-cols-[1fr_220px] gap-4 mt-2">
                        {/* Version picker */}
                        <div className="flex flex-col gap-2 min-w-0">
                            <div className="flex items-center justify-between">
                                <span className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                    Target version
                                </span>
                                {releases.length > 0 && (
                                    <span className="text-[11px] text-muted-foreground/70 font-mono">
                                        {releases.length} releases
                                    </span>
                                )}
                            </div>
                            <ScrollArea className="h-[280px] rounded-lg border border-border/50">
                                {loadingReleases ? (
                                    <div className="flex items-center justify-center py-12">
                                        <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
                                    </div>
                                ) : releases.length === 0 ? (
                                    <div className="text-center py-12 text-sm text-muted-foreground">
                                        No releases available
                                    </div>
                                ) : (
                                    <div className="flex flex-col">
                                        {releases.map((release, idx) => {
                                            const isLatest = idx === 0
                                            const isSelected = release.version === selectedVersion
                                            return (
                                                <button
                                                    key={release.version}
                                                    onClick={() => setSelectedVersion(release.version)}
                                                    className={`flex items-center justify-between px-3 py-2.5 text-left transition-colors border-b border-border/30 last:border-b-0 ${
                                                        isSelected
                                                            ? "bg-indigo-500/10"
                                                            : "hover:bg-muted/40"
                                                    }`}
                                                >
                                                    <div className="flex flex-col gap-0.5">
                                                        <div className="flex items-center gap-2">
                                                            <span className="text-sm font-mono font-medium">
                                                                v{release.version}
                                                            </span>
                                                            {isLatest && (
                                                                <Badge
                                                                    variant="outline"
                                                                    className="text-[10px] px-1.5 py-0 h-4 uppercase font-bold text-amber-500 border-amber-500/30"
                                                                >
                                                                    Latest
                                                                </Badge>
                                                            )}
                                                            {isSelected && (
                                                                <Badge
                                                                    variant="outline"
                                                                    className="text-[10px] px-1.5 py-0 h-4 uppercase font-bold text-indigo-500 border-indigo-500/30"
                                                                >
                                                                    Selected
                                                                </Badge>
                                                            )}
                                                        </div>
                                                        <span className="text-[12px] text-muted-foreground">
                                                            {formatDate(release.published_at, "long")}
                                                        </span>
                                                    </div>
                                                    <ChevronRight
                                                        className={`w-3.5 h-3.5 transition-colors ${
                                                            isSelected ? "text-indigo-500" : "text-muted-foreground/40"
                                                        }`}
                                                    />
                                                </button>
                                            )
                                        })}
                                    </div>
                                )}
                            </ScrollArea>
                        </div>

                        {/* Target nodes summary */}
                        <div className="flex flex-col gap-2 min-w-0">
                            <div className="flex items-center justify-between">
                                <span className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                    Targets
                                </span>
                                <Badge variant="secondary" className="font-mono text-[11px] px-1.5 py-0 h-4">
                                    {selectedIds.length}
                                </Badge>
                            </div>
                            <ScrollArea className="h-[280px] rounded-lg border border-border/50 bg-muted/20">
                                <div className="flex flex-col p-1">
                                    {targetNodes.length === 0 ? (
                                        <div className="text-center py-8 text-[12px] text-muted-foreground">
                                            No nodes selected
                                        </div>
                                    ) : (
                                        targetNodes.map((n) => {
                                            const current = n.xrayVersion?.replace(/^v/, "")
                                            const alreadyOn = current && current === selectedVersion
                                            return (
                                                <div
                                                    key={n.id}
                                                    className="flex items-center gap-1.5 px-2 py-1.5 rounded text-[12px]"
                                                >
                                                    <Server className="w-3 h-3 text-muted-foreground/60 shrink-0" />
                                                    <span className="flex-1 truncate font-medium">{n.name}</span>
                                                    {alreadyOn && (
                                                        <Badge
                                                            variant="outline"
                                                            className="text-[9px] px-1 py-0 h-3.5 uppercase font-bold text-emerald-500 border-emerald-500/30"
                                                        >
                                                            On
                                                        </Badge>
                                                    )}
                                                </div>
                                            )
                                        })
                                    )}
                                </div>
                            </ScrollArea>
                        </div>
                    </div>
                )}

                {phase === "running" && (
                    <div className="flex flex-col items-center justify-center py-12 gap-3">
                        <Loader2 className="w-7 h-7 animate-spin text-indigo-500" />
                        <div className="text-center">
                            <p className="text-sm font-medium">
                                Updating to v{selectedVersion} on {selectedIds.length} nodes
                            </p>
                            <p className="text-xs text-muted-foreground mt-1">
                                Each node runs independently · keep this dialog open
                            </p>
                        </div>
                    </div>
                )}

                {phase === "result" && result && (
                    <div className="flex flex-col gap-3 mt-2">
                        <div className="flex items-center gap-2">
                            {result.total - result.succeeded === 0 ? (
                                <Badge
                                    variant="outline"
                                    className="text-emerald-600 border-emerald-500/40 bg-emerald-500/5 font-medium"
                                >
                                    <CheckCircle2 className="w-3 h-3 mr-1" />
                                    All {result.total} updated
                                </Badge>
                            ) : (
                                <>
                                    <Badge
                                        variant="outline"
                                        className="text-emerald-600 border-emerald-500/40 bg-emerald-500/5 font-medium"
                                    >
                                        <CheckCircle2 className="w-3 h-3 mr-1" />
                                        {result.succeeded} ok
                                    </Badge>
                                    <Badge
                                        variant="outline"
                                        className="text-rose-600 border-rose-500/40 bg-rose-500/5 font-medium"
                                    >
                                        <XCircle className="w-3 h-3 mr-1" />
                                        {result.total - result.succeeded} failed
                                    </Badge>
                                </>
                            )}
                            <span className="text-[12px] text-muted-foreground ml-auto font-mono">
                                v{selectedVersion}
                            </span>
                        </div>

                        <ScrollArea className="h-[260px] rounded-lg border border-border/50">
                            <div className="flex flex-col">
                                {resultRows.map((row) => {
                                    const node = nodesById.get(row.id)
                                    return (
                                        <div
                                            key={row.id}
                                            className={`flex items-start gap-2 px-3 py-2 border-b border-border/30 last:border-b-0 ${
                                                row.success ? "" : "bg-rose-500/5"
                                            }`}
                                        >
                                            {row.success ? (
                                                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500 mt-0.5 shrink-0" />
                                            ) : (
                                                <XCircle className="w-3.5 h-3.5 text-rose-500 mt-0.5 shrink-0" />
                                            )}
                                            <div className="flex-1 min-w-0">
                                                <div className="flex items-center gap-2 text-sm">
                                                    <span className="font-medium truncate">
                                                        {node?.name || `Node #${row.id}`}
                                                    </span>
                                                    {node?.ip && (
                                                        <span className="text-[11px] text-muted-foreground font-mono">
                                                            {node.ip}
                                                        </span>
                                                    )}
                                                </div>
                                                {row.error && (
                                                    <p className="text-[12px] text-rose-600/90 dark:text-rose-400/90 mt-0.5 break-words">
                                                        {row.error}
                                                    </p>
                                                )}
                                            </div>
                                        </div>
                                    )
                                })}
                            </div>
                        </ScrollArea>
                    </div>
                )}

                <DialogFooter className="mt-4">
                    {phase === "select" && (
                        <>
                            <Button variant="ghost" onClick={() => onOpenChange(false)}>
                                Cancel
                            </Button>
                            <Button
                                onClick={handleRun}
                                disabled={!selectedVersion || selectedIds.length === 0}
                            >
                                {selectedVersion
                                    ? `Update ${selectedIds.length} node${
                                          selectedIds.length === 1 ? "" : "s"
                                      } to v${selectedVersion}`
                                    : "Select a version"}
                            </Button>
                        </>
                    )}
                    {phase === "running" && (
                        <Button variant="ghost" disabled className="w-full">
                            Working…
                        </Button>
                    )}
                    {phase === "result" && (
                        <>
                            <Button
                                variant="ghost"
                                onClick={() => {
                                    setPhase("select")
                                    setResult(null)
                                }}
                            >
                                Update again
                            </Button>
                            <Button onClick={() => onOpenChange(false)}>Done</Button>
                        </>
                    )}
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
