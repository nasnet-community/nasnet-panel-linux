import { useState, useMemo, useCallback } from "react"
import { Loader2, Plus, Minus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogFooter,
} from "@/components/ui/dialog"
import {
    Tabs,
    TabsList,
    TabsTrigger,
    TabsContent,
} from "@/components/ui/tabs"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { InboundTreeSelector } from "./inbound-tree-selector"
import { useBulkManageInbounds, useBulkInboundSummary } from "@/lib/queries/use-subscriptions"
import { useQuery } from "@tanstack/react-query"
import { listNodes } from "@/lib/api/nodes"
import type { Inbound } from "@/lib/types"

// ==================== Types ====================

interface BulkManageInboundsDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    selectedSubscriptionIds: number[]
    onSuccess?: () => void
}

// ==================== Component ====================

export function BulkManageInboundsDialog({
    open,
    onOpenChange,
    selectedSubscriptionIds,
    onSuccess,
}: BulkManageInboundsDialogProps) {
    const confirm = useConfirm()
    const [activeTab, setActiveTab] = useState<"add" | "remove">("add")
    const [addSelectedIds, setAddSelectedIds] = useState<Set<number>>(new Set())
    const [removeSelectedIds, setRemoveSelectedIds] = useState<Set<number>>(new Set())

    // Fetch nodes with inbounds
    const { data: nodesData, isLoading: nodesLoading } = useQuery({
        queryKey: ["nodes"],
        queryFn: async () => {
            const res = await listNodes()
            if (!res.success) throw new Error(res.error || "Failed to load nodes")
            return res.data!
        },
        enabled: open,
    })

    // Fetch inbound summary for selected subscriptions
    const { data: summaryData, isLoading: summaryLoading } = useBulkInboundSummary(
        open ? selectedSubscriptionIds : []
    )

    const bulkManage = useBulkManageInbounds()

    const nodes = nodesData ?? []
    const totalSubs = summaryData?.total_subscriptions ?? selectedSubscriptionIds.length
    const inboundCounts: Record<string, number> = summaryData?.inbound_counts
        ? Object.fromEntries(
            Object.entries(summaryData.inbound_counts).map(([k, v]) => [k, v])
        )
        : {}

    // Inbounds on ALL selected subs → disable in Add tab
    const fullyAssignedIds = useMemo(() => {
        const ids = new Set<number>()
        for (const [id, count] of Object.entries(inboundCounts)) {
            if (count === totalSubs) ids.add(Number(id))
        }
        return ids
    }, [inboundCounts, totalSubs])

    // Inbounds on ANY selected sub → shown in Remove tab
    const presentInboundIds = useMemo(() => {
        return new Set(Object.keys(inboundCounts).map(Number))
    }, [inboundCounts])

    // Annotations for Remove tab
    const removeAnnotations = useMemo(() => {
        const map = new Map<number, string>()
        for (const [id, count] of Object.entries(inboundCounts)) {
            map.set(Number(id), `on ${count}/${totalSubs} selected`)
        }
        return map
    }, [inboundCounts, totalSubs])

    // ---- Add tab toggle handlers ----
    const handleToggleAddInbound = useCallback((inboundId: number) => {
        setAddSelectedIds((prev) => {
            const next = new Set(prev)
            if (next.has(inboundId)) {
                next.delete(inboundId)
            } else {
                next.add(inboundId)
            }
            return next
        })
    }, [])

    const handleToggleAddNode = useCallback(
        (nodeId: number, inbounds: Inbound[], selectAll: boolean) => {
            setAddSelectedIds((prev) => {
                const next = new Set(prev)
                const eligible = inbounds
                    .filter((ib) => !fullyAssignedIds.has(ib.id))
                    .map((ib) => ib.id)
                if (selectAll) {
                    eligible.forEach((id) => next.add(id))
                } else {
                    eligible.forEach((id) => next.delete(id))
                }
                return next
            })
        },
        [fullyAssignedIds]
    )

    // ---- Remove tab toggle handlers ----
    const handleToggleRemoveInbound = useCallback((inboundId: number) => {
        setRemoveSelectedIds((prev) => {
            const next = new Set(prev)
            if (next.has(inboundId)) {
                next.delete(inboundId)
            } else {
                next.add(inboundId)
            }
            return next
        })
    }, [])

    const handleToggleRemoveNode = useCallback(
        (nodeId: number, inbounds: Inbound[], selectAll: boolean) => {
            setRemoveSelectedIds((prev) => {
                const next = new Set(prev)
                const eligible = inbounds
                    .filter((ib) => presentInboundIds.has(ib.id))
                    .map((ib) => ib.id)
                if (selectAll) {
                    eligible.forEach((id) => next.add(id))
                } else {
                    eligible.forEach((id) => next.delete(id))
                }
                return next
            })
        },
        [presentInboundIds]
    )

    // ---- Reset state ----
    const resetState = useCallback(() => {
        setAddSelectedIds(new Set())
        setRemoveSelectedIds(new Set())
        setActiveTab("add")
    }, [])

    const handleClose = useCallback(
        (open: boolean) => {
            if (!open) resetState()
            onOpenChange(open)
        },
        [onOpenChange, resetState]
    )

    // ---- Apply ----
    const canApply =
        addSelectedIds.size > 0 || removeSelectedIds.size > 0

    const summaryText = useMemo(() => {
        const parts: string[] = []
        if (addSelectedIds.size > 0) {
            parts.push(`add ${addSelectedIds.size} inbound${addSelectedIds.size !== 1 ? "s" : ""}`)
        }
        if (removeSelectedIds.size > 0) {
            parts.push(`remove ${removeSelectedIds.size} inbound${removeSelectedIds.size !== 1 ? "s" : ""}`)
        }
        if (parts.length === 0) {
            return `Select inbounds to add or remove across ${selectedSubscriptionIds.length} subscription${selectedSubscriptionIds.length !== 1 ? "s" : ""}`
        }
        return `Will ${parts.join(" and ")} across ${selectedSubscriptionIds.length} subscription${selectedSubscriptionIds.length !== 1 ? "s" : ""}`
    }, [addSelectedIds.size, removeSelectedIds.size, selectedSubscriptionIds.length])

    const handleApply = useCallback(async () => {
        const confirmed = await confirm({
            title: "Apply Inbound Changes",
            description: summaryText,
            confirmLabel: "Apply",
        })
        if (!confirmed) return

        bulkManage.mutate(
            {
                subscription_ids: selectedSubscriptionIds,
                add_inbound_ids: Array.from(addSelectedIds),
                remove_inbound_ids: Array.from(removeSelectedIds),
            },
            {
                onSuccess: () => {
                    resetState()
                    onOpenChange(false)
                    onSuccess?.()
                },
            }
        )
    }, [
        confirm,
        summaryText,
        bulkManage,
        selectedSubscriptionIds,
        addSelectedIds,
        removeSelectedIds,
        resetState,
        onOpenChange,
        onSuccess,
    ])

    const isLoading = nodesLoading || summaryLoading

    return (
        <Dialog open={open} onOpenChange={handleClose}>
            <DialogContent className="max-w-2xl max-h-[90vh] flex flex-col">
                <DialogHeader>
                    <DialogTitle>Manage Inbounds</DialogTitle>
                    <DialogDescription>
                        Add or remove inbounds across {selectedSubscriptionIds.length} selected subscription{selectedSubscriptionIds.length !== 1 ? "s" : ""}
                    </DialogDescription>
                </DialogHeader>

                {isLoading ? (
                    <div className="flex items-center justify-center py-12">
                        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                    </div>
                ) : (
                    <Tabs
                        value={activeTab}
                        onValueChange={(v) => setActiveTab(v as "add" | "remove")}
                        className="flex flex-col flex-1 min-h-0"
                    >
                        <TabsList className="w-full shrink-0">
                            <TabsTrigger value="add" className="flex-1 gap-1.5">
                                <Plus className="h-3.5 w-3.5" />
                                Add Inbounds
                                {addSelectedIds.size > 0 && (
                                    <Badge variant="secondary" className="ml-1 h-4 px-1.5 text-xs">
                                        {addSelectedIds.size}
                                    </Badge>
                                )}
                            </TabsTrigger>
                            <TabsTrigger value="remove" className="flex-1 gap-1.5">
                                <Minus className="h-3.5 w-3.5" />
                                Remove Inbounds
                                {removeSelectedIds.size > 0 && (
                                    <Badge variant="secondary" className="ml-1 h-4 px-1.5 text-xs">
                                        {removeSelectedIds.size}
                                    </Badge>
                                )}
                            </TabsTrigger>
                        </TabsList>

                        <TabsContent value="add" className="flex-1 overflow-y-auto mt-2">
                            <InboundTreeSelector
                                nodes={nodes}
                                selectedInboundIds={addSelectedIds}
                                onToggleInbound={handleToggleAddInbound}
                                onToggleNode={handleToggleAddNode}
                                disabledInboundIds={fullyAssignedIds}
                                disabledLabel="Already on all selected"
                            />
                        </TabsContent>

                        <TabsContent value="remove" className="flex-1 overflow-y-auto mt-2">
                            <InboundTreeSelector
                                nodes={nodes}
                                selectedInboundIds={removeSelectedIds}
                                onToggleInbound={handleToggleRemoveInbound}
                                onToggleNode={handleToggleRemoveNode}
                                filterInboundIds={presentInboundIds}
                                inboundAnnotations={removeAnnotations}
                            />
                        </TabsContent>
                    </Tabs>
                )}

                <DialogFooter className="flex-col gap-2 border-t pt-4 sm:flex-row sm:items-center sm:justify-between">
                    <p className="text-sm text-muted-foreground">{summaryText}</p>
                    <div className="flex gap-2">
                        <Button
                            variant="outline"
                            onClick={() => handleClose(false)}
                            disabled={bulkManage.isPending}
                        >
                            Cancel
                        </Button>
                        <Button
                            onClick={handleApply}
                            disabled={!canApply || bulkManage.isPending}
                        >
                            {bulkManage.isPending && (
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                            )}
                            Apply
                        </Button>
                    </div>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
