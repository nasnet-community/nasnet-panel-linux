import { useCallback, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { HiOutlineRefresh } from "react-icons/hi"
import { RotateCw, Upload, X as XIcon, Cpu, Download } from "lucide-react"
import { useConfirm } from "@/components/ui/confirm-dialog"
import {
    useBulkRestartNodes,
    useBulkPushNodeConfig,
    useBulkCheckNodeHealth,
    useNodes,
} from "@/lib/queries"
import { isLocalNode } from "@/lib/node-kind"
import { useNodesStore } from "@/store/nodes-store"
import { BulkXrayVersionDialog } from "@/components/node/bulk-xray-version-dialog"
import { BulkAgentUpdateDialog } from "@/components/node/bulk-agent-update-dialog"

// BulkNodeActionsBar is a floating bar rendered only while at least one
// node is selected. It reads selection from the Zustand store directly so
// the parent page doesn't need to re-render when the selection changes.
export function BulkNodeActionsBar() {
    const selectedIds = useNodesStore((s) => s.selectedIds)
    const clearSelection = useNodesStore((s) => s.clearSelection)
    const confirm = useConfirm()

    const restartMut = useBulkRestartNodes()
    const pushMut = useBulkPushNodeConfig()
    const healthMut = useBulkCheckNodeHealth()
    const [xrayDialogOpen, setXrayDialogOpen] = useState(false)
    const [agentTargets, setAgentTargets] = useState<number[] | null>(null)

    const count = selectedIds.size
    const anyPending = restartMut.isPending || pushMut.isPending || healthMut.isPending || agentTargets !== null

    const ids = useCallback(() => Array.from(selectedIds), [selectedIds])
    // The built-in node updates with the panel; restarting it drops LAN clients.
    const { data: nodes } = useNodes(count > 0)
    const localId = nodes?.find(isLocalNode)?.id
    const includesLocal = localId !== undefined && selectedIds.has(localId)
    const agentIds = useCallback(() => Array.from(selectedIds).filter((id) => id !== localId), [selectedIds, localId])
    const agentDisabled = anyPending || (includesLocal && count === 1)
    const agentTitle = includesLocal ? "Built in, updates with the panel" : undefined

    const onRestart = useCallback(async () => {
        const ok = await confirm({
            title: "Restart Xray on selected nodes?",
            description: `This will restart Xray on ${count} node${count === 1 ? "" : "s"}. Active connections may briefly drop.${includesLocal ? " LAN clients drop for a few seconds." : ""}`,
            confirmLabel: "Restart",
            variant: "warning",
        })
        if (!ok) return
        restartMut.mutate(ids(), { onSuccess: () => clearSelection() })
    }, [confirm, count, restartMut, ids, clearSelection, includesLocal])

    const onPush = useCallback(async () => {
        const ok = await confirm({
            title: "Push config to selected nodes?",
            description: `This will regenerate and push the Xray config to ${count} node${count === 1 ? "" : "s"}.`,
            confirmLabel: "Push",
        })
        if (!ok) return
        pushMut.mutate(ids(), { onSuccess: () => clearSelection() })
    }, [confirm, count, pushMut, ids, clearSelection])

    const onHealth = useCallback(() => {
        healthMut.mutate(ids())
    }, [healthMut, ids])

    if (count === 0 && agentTargets === null) return null

    return (
        <>
            {count > 0 && <div className="fixed bottom-[100px] md:bottom-6 left-1/2 -translate-x-1/2 z-50 w-max max-w-[calc(100vw-2rem)] animate-in slide-in-from-bottom-4 fade-in duration-300">
                {/* Desktop */}
                <div className="hidden md:flex flex-wrap justify-center items-center gap-2 bg-background/95 backdrop-blur-sm border rounded-xl shadow-2xl px-4 py-3">
                    <Badge variant="secondary" className="font-mono text-sm px-3">
                        {count} selected
                    </Badge>
                    <div className="h-6 w-px bg-border" />
                    <Button variant="outline" size="sm" onClick={onHealth} disabled={anyPending}>
                        <HiOutlineRefresh className={healthMut.isPending ? "w-3.5 h-3.5 mr-1.5 animate-spin" : "w-3.5 h-3.5 mr-1.5"} />
                        Health Check
                    </Button>
                    <Button variant="outline" size="sm" onClick={onPush} disabled={anyPending}>
                        <Upload className={pushMut.isPending ? "w-3.5 h-3.5 mr-1.5 animate-pulse" : "w-3.5 h-3.5 mr-1.5"} />
                        Push Config
                    </Button>
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setXrayDialogOpen(true)}
                        disabled={anyPending}
                        className="text-indigo-600 hover:text-indigo-700 border-indigo-500/30 hover:border-indigo-500/50"
                    >
                        <Cpu className="w-3.5 h-3.5 mr-1.5" />
                        Update Xray
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => setAgentTargets(agentIds())} disabled={agentDisabled} title={agentTitle}>
                        <Download className="w-3.5 h-3.5 mr-1.5" />
                        Update Agent
                    </Button>
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={onRestart}
                        disabled={anyPending}
                        className="text-amber-600 hover:text-amber-700 border-amber-500/30 hover:border-amber-500/50"
                    >
                        <RotateCw className={restartMut.isPending ? "w-3.5 h-3.5 mr-1.5 animate-spin" : "w-3.5 h-3.5 mr-1.5"} />
                        Restart Xray
                    </Button>
                    <div className="h-6 w-px bg-border" />
                    <Button variant="ghost" size="sm" onClick={clearSelection} disabled={anyPending}>
                        <XIcon className="w-3.5 h-3.5 mr-1.5" />
                        Clear
                    </Button>
                </div>

                {/* Mobile */}
                <div className="flex md:hidden flex-wrap justify-center items-center gap-1.5 bg-background/95 backdrop-blur-sm border rounded-xl shadow-2xl px-3 py-2.5">
                    <Badge variant="secondary" className="font-mono text-xs px-2">
                        {count}
                    </Badge>
                    <div className="h-5 w-px bg-border" />
                    <Button variant="outline" size="icon" className="h-8 w-8" onClick={onHealth} disabled={anyPending} aria-label="Health check">
                        <HiOutlineRefresh className={healthMut.isPending ? "w-3.5 h-3.5 animate-spin" : "w-3.5 h-3.5"} />
                    </Button>
                    <Button variant="outline" size="icon" className="h-8 w-8" onClick={onPush} disabled={anyPending} aria-label="Push config">
                        <Upload className="w-3.5 h-3.5" />
                    </Button>
                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 text-indigo-600 border-indigo-500/30"
                        onClick={() => setXrayDialogOpen(true)}
                        disabled={anyPending}
                        aria-label="Update Xray version"
                    >
                        <Cpu className="w-3.5 h-3.5" />
                    </Button>
                    <Button variant="outline" size="icon" className="h-8 w-8" onClick={() => setAgentTargets(agentIds())} disabled={agentDisabled} title={agentTitle} aria-label="Update agents">
                        <Download className="w-3.5 h-3.5" />
                    </Button>
                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 text-amber-600 border-amber-500/30"
                        onClick={onRestart}
                        disabled={anyPending}
                        aria-label="Restart Xray"
                    >
                        <RotateCw className="w-3.5 h-3.5" />
                    </Button>
                    <div className="h-5 w-px bg-border" />
                    <Button variant="ghost" size="icon" className="h-8 w-8" onClick={clearSelection} disabled={anyPending} aria-label="Clear selection">
                        <XIcon className="w-3.5 h-3.5" />
                    </Button>
                </div>
            </div>}

            {agentTargets !== null && (
                <BulkAgentUpdateDialog selectedIds={agentTargets} onClose={() => setAgentTargets(null)} />
            )}

            <BulkXrayVersionDialog
                open={xrayDialogOpen}
                onOpenChange={setXrayDialogOpen}
                selectedIds={ids()}
            />
        </>
    )
}
