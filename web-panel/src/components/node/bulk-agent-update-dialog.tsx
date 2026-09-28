import { useRef, useState } from "react"
import { CheckCircle2, Download, Loader2, Server, XCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
    Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { useNodes } from "@/lib/queries"
import { useBulkUpdateAgent } from "@/lib/queries/use-bulk-agent-update"
import type { NodeBulkActionResponse } from "@/lib/api/nodes"

interface BulkAgentUpdateDialogProps {
    selectedIds: number[]
    onClose: () => void
}

// Mounted for one selection snapshot; closing and reopening starts a new run.
export function BulkAgentUpdateDialog({ selectedIds, onClose }: BulkAgentUpdateDialogProps) {
    const [targetIds] = useState(() => [...new Set(selectedIds)])
    const [result, setResult] = useState<NodeBulkActionResponse | null>(null)
    const [error, setError] = useState<string | null>(null)
    const [runningIds, setRunningIds] = useState<number[]>([])
    const runningRef = useRef(false)
    const { data: nodes } = useNodes()
    const update = useBulkUpdateAgent()
    const running = runningIds.length > 0
    const nodesById = new Map(nodes?.map((node) => [node.id, node]))
    const failedIds = result
        ? targetIds.filter((id) => !result.results[String(id)]?.success)
        : []
    const displayedIds = result
        ? [...targetIds].sort((a, b) => Number(result.results[String(a)]?.success) - Number(result.results[String(b)]?.success))
        : targetIds

    const run = async (ids: number[]) => {
        if (runningRef.current || ids.length === 0 || ids.length > 500) return
        runningRef.current = true
        setRunningIds(ids)
        setError(null)
        try {
            const response = await update.mutateAsync(ids)
            setResult((previous) => {
                const results = { ...previous?.results }
                // Keep successful nodes from earlier attempts and replace only
                // this attempt's results. Missing results are never successes.
                for (const id of ids) {
                    results[String(id)] = response.results[String(id)] ?? {
                        success: false,
                        error: "No result returned for this node. Check its status before retrying.",
                    }
                }
                return {
                    results,
                    total: targetIds.length,
                    succeeded: targetIds.filter((id) => results[String(id)]?.success).length,
                }
            })
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Agent update failed")
        } finally {
            runningRef.current = false
            setRunningIds([])
        }
    }

    return (
        <Dialog open onOpenChange={(open) => { if (!open && !runningRef.current) onClose() }}>
            <DialogContent className="max-w-xl overflow-y-auto" onEscapeKeyDown={(event) => { if (runningRef.current) event.preventDefault() }}>
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <Download className="h-4 w-4 text-blue-500" />
                        Bulk Agent Update
                    </DialogTitle>
                    <DialogDescription>
                        Update the agent on {targetIds.length} selected node{targetIds.length === 1 ? "" : "s"} to
                        the build this panel ships. Agents already on it are skipped; the built-in node updates with the panel.
                    </DialogDescription>
                </DialogHeader>

                {!result && !running && (
                    <p className="text-sm text-muted-foreground">
                        Agents will restart and may briefly disconnect. A failure on one node will not stop the others.
                    </p>
                )}

                {running && (
                    <div role="status" className="flex items-center gap-3 rounded-lg bg-blue-500/10 p-3 text-sm">
                        <Loader2 className="h-5 w-5 shrink-0 animate-spin text-blue-500" />
                        <div>
                            <p className="font-medium">Updating agents on {runningIds.length} node{runningIds.length === 1 ? "" : "s"}…</p>
                            <p className="text-muted-foreground">This may take several minutes. Keep this page open until results appear.</p>
                        </div>
                    </div>
                )}

                {error && (
                    <div role="alert" className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">
                        <p className="font-medium">{error}</p>
                        <p className="mt-1">Some agents may have accepted the update. Check node status before retrying.</p>
                    </div>
                )}

                {targetIds.length > 500 && (
                    <p role="alert" className="text-sm text-destructive">Select at most 500 nodes per update.</p>
                )}

                {result && !running && (
                    <div role="status" className="space-y-1">
                        <p className="text-sm font-medium">
                            {result.succeeded} / {result.total} succeeded{failedIds.length > 0 && ` · ${failedIds.length} failed`}
                        </p>
                        <p className="text-xs text-muted-foreground">
                            Successful nodes accepted the update or already have the server&apos;s agent version.
                        </p>
                    </div>
                )}

                <ul aria-label="Selected nodes" className="max-h-[min(35vh,320px)] overflow-y-auto rounded-lg border divide-y">
                    {displayedIds.map((id) => {
                        const node = nodesById.get(id)
                        const outcome = result?.results[String(id)]
                        const isRunning = runningIds.includes(id)
                        return (
                            <li key={id} className="flex items-start gap-2 p-3 text-sm">
                                {isRunning ? <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-blue-500" />
                                    : outcome?.success ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-500" />
                                    : outcome ? <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                                    : <Server className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />}
                                <div className="min-w-0 flex-1">
                                    <p className="break-words font-medium">{node?.name || `Node #${id}`}</p>
                                    {node?.ip && <p className="text-xs font-mono text-muted-foreground">{node.ip}</p>}
                                    {outcome && !outcome.success && !isRunning && (
                                        <p className="mt-1 break-words text-xs text-destructive">{outcome.error || "Agent update failed"}</p>
                                    )}
                                </div>
                                <Badge variant="outline" className="shrink-0">
                                    {isRunning ? "In progress" : outcome ? (outcome.success ? "Success" : "Failed") : "Selected"}
                                </Badge>
                            </li>
                        )
                    })}
                </ul>

                <DialogFooter className="gap-2 sm:gap-0">
                    <Button variant="ghost" onClick={onClose} disabled={running}>
                        {result || error ? "Done" : "Cancel"}
                    </Button>
                    {running ? (
                        <Button disabled><Loader2 className="mr-2 h-4 w-4 animate-spin" />Updating…</Button>
                    ) : result ? (
                        failedIds.length > 0 && <Button onClick={() => void run(failedIds)}>Retry {failedIds.length} failed</Button>
                    ) : (
                        <Button onClick={() => void run(targetIds)} disabled={targetIds.length === 0 || targetIds.length > 500}>
                            {error ? "Retry update" : `Update ${targetIds.length} agent${targetIds.length === 1 ? "" : "s"}`}
                        </Button>
                    )}
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
