import { useState, useCallback, memo, useEffect, useMemo, useRef } from "react"
import { PageHeader } from "@/components/shared/page-header"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    HiOutlineServer,
    HiOutlinePlus,
    HiOutlineRefresh,
    HiOutlineTrash,
    HiOutlinePencil,
    HiOutlineDotsVertical,
    HiOutlineTerminal,
} from "react-icons/hi"
import {
    ShieldAlert,
    Trash2,
    ArrowLeftRight,
    Cpu,
    Network,
    Loader2,
    Server,
    Globe,
    ShieldOffIcon,
} from "lucide-react"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { DeployAgentDialog } from "@/components/deploy-agent-dialog"
import { NodeDetailSheet } from "@/components/node/node-detail-sheet"
import { MetricBar } from "@/components/ui/metric-bar"
import { Sparkline } from "@/components/ui/sparkline"
import type { Node } from "@/lib/types"
import { cn, formatBytes, formatSpeed } from "@/lib/utils"
import { toast } from "@/lib/toast"
import { MigrationDialog } from "@/components/node/migration-dialog"
import { Link } from "react-router"
import { useNodes, useNodesStatsBulk, useNodesStatsHistoryBulk, sparklineFromBulk, useCreateNode, useDeleteNode, useCheckNodeHealth, useNodeAggregateStats } from "@/lib/queries"
import { queryKeys } from "@/lib/queries/keys"
import type { NodeStats } from "@/lib/types"
import { useNodesStore } from "@/store/nodes-store"
import { useQueryClient } from "@tanstack/react-query"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { nodeSchema } from "@/lib/validations/node-schema"
import { AutoRefreshControl } from "@/components/ui/auto-refresh-control"
import { Checkbox } from "@/components/ui/checkbox"
import { BulkNodeActionsBar } from "@/components/node/bulk-node-actions-bar"
import { WipeConfirmBody } from "@/components/node/wipe-confirm-body"
import { wipeNode } from "@/lib/api/nuke"
import { LocalNodeBadge } from "@/components/node/local-node-badge"
import { isLocalNode, nodeAddress, outageReasonLabel } from "@/lib/node-kind"
import { useChartPalette } from "@/lib/design/palette"

// ─── Compact Node Card ────────────────────────────────────────────

// Stable-reference empty array for Sparkline props when history hasn't loaded.
// Using a shared constant avoids creating a new [] on each render, which would
// defeat the memo() wrapper on CompactNodeCard.
/** Compact "3h" / "2d" style age, for the card's last-outage hint. */
function relativeSince(iso: string): string {
    const sec = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000))
    if (sec < 60) return `${sec}s ago`
    const min = Math.floor(sec / 60)
    if (min < 60) return `${min}m ago`
    const h = Math.floor(min / 60)
    if (h < 24) return `${h}h ago`
    const d = Math.floor(h / 24)
    return `${d}d ago`
}

const EMPTY_SERIES: number[] = []

const CompactNodeCard = memo(function CompactNodeCard({
    node,
    stats,
    isStatsLoading,
    sparkline,
    onCheckHealth,
    onDelete,
    onWipe,
    onDeploy,
    onSelect,
    onToggleSelect,
    onRangeSelect,
}: {
    node: Node
    stats?: NodeStats
    isStatsLoading: boolean
    sparkline: { cpu: number[]; memory: number[] }
    onCheckHealth: (nodeId: number) => void
    onDelete: (node: Node) => void
    onWipe: (node: Node) => void
    onDeploy: (nodeId: number) => void
    onSelect: (node: Node) => void
    onToggleSelect: (nodeId: number) => void
    onRangeSelect: (nodeId: number) => void
}) {
    // Subscribe to exactly the slices this card cares about. Zustand
    // compares each returned primitive, so unaffected cards skip the
    // render when selection state changes elsewhere.
    const isSelected = useNodesStore((s) => s.selectedIds.has(node.id))
    const selectionMode = useNodesStore((s) => s.selectedIds.size > 0)
    const isChecking = useNodesStore((s) => s.checkingHealthId === node.id)

    const cpuHistory = sparkline.cpu.length > 0 ? sparkline.cpu : EMPTY_SERIES

    const isPending = new Date(node.last_check).getFullYear() < 2000
    const local = isLocalNode(node)
    const palette = useChartPalette()

    // While selection mode is active, tapping the card toggles selection
    // instead of opening the detail sheet. Shift-click always extends a
    // range. Ctrl/Meta-click toggles (matches file-manager convention).
    const handleCardClick = (e: React.MouseEvent) => {
        if (e.shiftKey) {
            e.preventDefault()
            onRangeSelect(node.id)
            return
        }
        if (e.ctrlKey || e.metaKey || selectionMode) {
            e.preventDefault()
            onToggleSelect(node.id)
            return
        }
        onSelect(node)
    }

    return (
        <div
            onClick={handleCardClick}
            className={cn(
                "group relative rounded-lg border bg-card/80 backdrop-blur-sm p-4 sm:p-5 lg:p-6 cursor-pointer",
                "transition-all duration-200 hover:shadow-md",
                isSelected ? "border-primary ring-2 ring-primary/30" : "border-border/50 hover:border-border",
                !node.is_online && !isSelected && "opacity-75"
            )}
        >
            {/* Selection checkbox — visible in selection mode or on hover. */}
            <div
                className={cn(
                    "absolute top-3 left-3 z-10 transition-opacity",
                    selectionMode || isSelected ? "opacity-100" : "opacity-0 group-hover:opacity-100",
                )}
                onClick={(e) => { e.stopPropagation(); onToggleSelect(node.id) }}
            >
                <Checkbox
                    checked={isSelected}
                    aria-label={`Select ${node.name}`}
                    onClick={(e) => e.stopPropagation()}
                    onCheckedChange={() => onToggleSelect(node.id)}
                    className="bg-background/80 backdrop-blur-sm"
                />
            </div>
            {/* Header row */}
            <div className={cn(
                "flex items-start justify-between gap-3 mb-4 md:mb-5 transition-[padding]",
                (selectionMode || isSelected) && "pl-7"
            )}>
                <div className="flex items-center gap-2.5 md:gap-3 min-w-0">
                    <div className={cn(
                        "w-2.5 h-2.5 md:w-3 md:h-3 rounded-full shrink-0",
                        isPending ? "bg-amber-500 animate-pulse" : node.is_online ? "bg-emerald-500" : "bg-red-500"
                    )} />
                    <h3 className="text-sm md:text-base lg:text-lg font-semibold truncate">{node.name}</h3>
                    {/* Last outage: for a node that's down this is when it went
                        down; for one that's up it's the most recent blip. Reads
                        from the stamped column, so it survives event pruning. */}
                    {node.last_disconnected_at && (
                        <span
                            className="text-[11px] md:text-[12px] text-muted-foreground/70 font-mono shrink-0"
                            title={`Last disconnected ${new Date(node.last_disconnected_at).toLocaleString()}${
                                node.last_offline_reason ? ` — ${outageReasonLabel(node.last_offline_reason, isLocalNode(node))}` : ""
                            }`}
                        >
                            {node.is_online ? "last down " : "down "}
                            {relativeSince(node.last_disconnected_at)}
                        </span>
                    )}
                    {local && <LocalNodeBadge />}
                </div>
                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7 md:h-8 md:w-8 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
                            onClick={(e) => e.stopPropagation()}
                            aria-label="Node actions"
                        >
                            <HiOutlineDotsVertical className="w-4 h-4 md:w-5 md:h-5" />
                        </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-44 md:w-48">
                        <DropdownMenuLabel className="text-xs md:text-sm">Actions</DropdownMenuLabel>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem asChild className="md:text-[14px]">
                            <Link to={`/nodes/${node.id}`} onClick={(e) => e.stopPropagation()}>
                                <HiOutlinePencil className="w-3.5 h-3.5 md:w-4 md:h-4 mr-2" /> Edit
                            </Link>
                        </DropdownMenuItem>
                        {!node.is_online && !local && (
                            <DropdownMenuItem className="md:text-[14px]" onClick={(e) => { e.preventDefault(); e.stopPropagation(); onDeploy(node.id) }}>
                                <HiOutlineTerminal className="w-3.5 h-3.5 md:w-4 md:h-4 mr-2 text-blue-500" /> Deploy
                            </DropdownMenuItem>
                        )}
                        <DropdownMenuItem className="md:text-[14px]" onClick={(e) => { e.preventDefault(); e.stopPropagation(); onCheckHealth(node.id) }} disabled={isChecking}>
                            <HiOutlineRefresh className={cn("w-3.5 h-3.5 md:w-4 md:h-4 mr-2", isChecking && "animate-spin")} /> Health Check
                        </DropdownMenuItem>
                        {!local && (
                            <>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem onClick={(e) => { e.preventDefault(); e.stopPropagation(); onWipe(node) }} className="text-amber-600 focus:text-amber-700 focus:bg-amber-500/10 md:text-[14px]">
                                    <ShieldOffIcon className="w-3.5 h-3.5 md:w-4 md:h-4 mr-2" /> Wipe…
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={(e) => { e.preventDefault(); e.stopPropagation(); onDelete(node) }} className="text-red-500 focus:text-red-500 md:text-[14px]">
                                    <HiOutlineTrash className="w-3.5 h-3.5 md:w-4 md:h-4 mr-2" /> Delete
                                </DropdownMenuItem>
                            </>
                        )}
                    </DropdownMenuContent>
                </DropdownMenu>
            </div>

            {/* IP & Location */}
            <div className="flex items-center gap-2 md:gap-3 mb-4 md:mb-5 text-[12px] md:text-sm text-muted-foreground">
                <span className={cn(!local && "font-mono")}>{nodeAddress(node)}</span>
                {local ? (
                    <>
                        <span className="text-border">|</span>
                        <span>Built in</span>
                    </>
                ) : (
                    <>
                        <span className="text-border">|</span>
                        <span className="capitalize">{node.connect_mode}</span>
                    </>
                )}
                {node.country_code && (
                    <>
                        <span className="text-border">|</span>
                        <span>{node.country_code}</span>
                    </>
                )}
                {node.datacenter && (
                    <>
                        <span className="text-border">|</span>
                        <span className="truncate">{node.datacenter}</span>
                    </>
                )}
            </div>

            {/* Metrics (for online agent nodes) */}
            {node.is_online && stats?.stats_unavailable && (
                <p role="status" className="text-xs text-amber-500">Stats unavailable · showing last readings</p>
            )}
            {node.is_online && !stats && !isStatsLoading && (
                <p role="status" className="text-xs text-muted-foreground">Stats unavailable · retrying automatically</p>
            )}
            {node.is_online && stats ? (
                <div className="space-y-3 md:space-y-4 lg:space-y-5">
                    {/* Resource bars */}
                    <div className="grid grid-cols-3 gap-3 md:gap-4 lg:gap-5">
                        <MetricBar value={stats.cpu_percent ?? 0} label="CPU" size="sm" />
                        <MetricBar value={stats.memory_percent ?? 0} label="RAM" size="sm" />
                        <MetricBar value={stats.disk_percent ?? 0} label="Disk" size="sm" />
                    </div>
                    {/* Sparkline + stats row */}
                    <div className="flex items-center justify-between pt-2 md:pt-3 border-t border-border/30">
                        <div className="flex items-center gap-3 md:gap-4 text-[12px] md:text-[14px] text-muted-foreground">
                            <span>↑{formatSpeed(stats.up_speed ?? 0)}</span>
                            <span>↓{formatSpeed(stats.down_speed ?? 0)}</span>
                            {stats.online_users !== undefined && (
                                <span className="text-foreground font-medium">{stats.online_users} online</span>
                            )}
                        </div>
                        {cpuHistory.length > 2 && (
                            <div className="w-16 md:w-20 lg:w-24">
                                <Sparkline data={cpuHistory} height={20} color={palette.success} />
                            </div>
                        )}
                    </div>
                </div>
            ) : node.is_online && isStatsLoading ? (
                <div className="space-y-3 md:space-y-4">
                    <div className="grid grid-cols-3 gap-3 md:gap-4 lg:gap-5">
                        {[1, 2, 3].map((i) => <Skeleton key={i} className="h-6 md:h-7 w-full" />)}
                    </div>
                    <Skeleton className="h-5 md:h-6 w-full" />
                </div>
            ) : !node.is_online ? (
                <div className="flex items-center justify-between pt-1 md:pt-2">
                    <span className="text-[12px] md:text-[14px] text-muted-foreground">{isPending ? "Never connected" : `Last seen: ${new Date(node.last_check).toLocaleString()}`}</span>
                    {!local && (
                        <Button
                            variant="ghost"
                            size="sm"
                            className="text-[11px] md:text-xs h-6 md:h-8 px-2 md:px-3"
                            onClick={(e) => { e.stopPropagation(); onDeploy(node.id) }}
                        >
                            <HiOutlineTerminal className="w-3 h-3 md:w-4 md:h-4 mr-1 md:mr-1.5" /> Deploy
                        </Button>
                    )}
                </div>
            ) : null}
        </div>
    )
})

function NodeCardSkeleton() {
    return (
        <div className="rounded-lg border border-border/50 p-4 sm:p-5 lg:p-6 space-y-4 md:space-y-5">
            <div className="flex items-center gap-2.5 md:gap-3">
                <Skeleton className="w-2.5 h-2.5 md:w-3 md:h-3 rounded-full" />
                <Skeleton className="h-4 md:h-5 lg:h-6 w-28 md:w-36 lg:w-40" />
            </div>
            <Skeleton className="h-3 md:h-4 lg:h-5 w-36 md:w-48 lg:w-56" />
            <div className="grid grid-cols-3 gap-3 md:gap-4 lg:gap-5">
                {[1, 2, 3].map((i) => <Skeleton key={i} className="h-6 md:h-7 w-full" />)}
            </div>
            <Skeleton className="h-4 md:h-5 lg:h-6 w-full" />
        </div>
    )
}

// ─── Main Page ────────────────────────────────────────────────────

export default function NodesPage() {
    const queryClient = useQueryClient()
    const { data: rawNodes, isLoading, isRefetching, dataUpdatedAt } = useNodes(true, { poll: true })
    // The built-in node first, then the order the panel returns.
    const nodes = useMemo(
        () => [...(rawNodes ?? [])].sort((a, b) => Number(isLocalNode(b)) - Number(isLocalNode(a))),
        [rawNodes],
    )
    const { data: aggregateStats } = useNodeAggregateStats()
    const createNodeMutation = useCreateNode()
    const deleteNodeMutation = useDeleteNode()
    const checkHealthMutation = useCheckNodeHealth()

    // Batch-fetch stats for all online nodes in one request (replaces per-card useNodeStats).
    // Memoized so the identity stays stable across re-renders that don't change
    // the online set — without it, every render would re-alloc the array, and
    // dependent useCallbacks downstream would churn their identity too.
    const onlineIds = useMemo(
        () => nodes.filter((n) => n.is_online).map((n) => n.id),
        [nodes],
    )
    const { data: bulkStats, isLoading: isBulkStatsLoading } = useNodesStatsBulk(
        onlineIds,
        onlineIds.length > 0,
    )

    // Bulk history (one request instead of N per-card sparkline queries).
    // Parent passes the per-node slice down as a prop; the selector
    // keeps object identity stable so memo'd cards don't re-render on
    // refetches that return unchanged data.
    const { data: bulkHistory } = useNodesStatsHistoryBulk(onlineIds, 15, onlineIds.length > 0)

    const {
        addDialogOpen, openAddDialog, closeAddDialog,
        deployDialogOpen, openDeployDialog, closeDeployDialog,
        selectedDeployNodeId, setCheckingHealth,
        newNode, updateNewNode, resetNewNode,
        selectedIds, lastSelectedId, toggleSelected, selectRange, setSelected, clearSelection,
    } = useNodesStore()

    const selectionMode = selectedIds.size > 0

    // Esc clears the selection — quick escape hatch so a misclick never
    // blocks the workflow. Skip while a dialog or input is active (best
    // effort: we check document.activeElement tag).
    useEffect(() => {
        if (!selectionMode) return
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return
            const tag = (document.activeElement?.tagName ?? "").toLowerCase()
            if (tag === "input" || tag === "textarea") return
            clearSelection()
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [selectionMode, clearSelection])

    const orderedNodeIds = useMemo(() => nodes.map((n) => n.id), [nodes])
    const handleToggleSelect = useCallback((nodeId: number) => {
        toggleSelected(nodeId)
    }, [toggleSelected])

    // Read ordered ids + anchor via refs so the callback identity stays stable
    // across selection changes — otherwise memo'd cards would re-render every
    // time the user selects anything (handleRangeSelect prop would flip).
    const lastSelectedIdRef = useRef(lastSelectedId)
    lastSelectedIdRef.current = lastSelectedId
    const orderedNodeIdsRef = useRef(orderedNodeIds)
    orderedNodeIdsRef.current = orderedNodeIds
    const handleRangeSelect = useCallback((nodeId: number) => {
        const anchor = lastSelectedIdRef.current ?? nodeId
        selectRange(anchor, nodeId, orderedNodeIdsRef.current)
    }, [selectRange])

    const allSelected = nodes.length > 0 && selectedIds.size === nodes.length
    const handleToggleSelectAll = useCallback(() => {
        if (allSelected) {
            clearSelection()
        } else {
            setSelected(nodes.map(n => n.id))
        }
    }, [allSelected, clearSelection, setSelected, nodes])

    const confirm = useConfirm()
    const [migrationNode, setMigrationNode] = useState<Node | null>(null)
    const [deleteConflictNode, setDeleteConflictNode] = useState<Node | null>(null)
    const [selectedNode, setSelectedNode] = useState<Node | null>(null)

    // ── Wipe state ────────────────────────────────────────────────────────────
    // The ref is read by the onConfirm closure to get the latest checkbox value.
    // WipeConfirmBody owns the visual state; it writes back here via onAlsoRemoveChange.
    const alsoRemoveHubRecordRef = useRef(false)

    const updateAlsoRemoveHubRecord = (v: boolean) => {
        alsoRemoveHubRecordRef.current = v
    }

    const handleWipeNode = useCallback(async (node: Node) => {
        // Reset ref before opening (WipeConfirmBody resets its own visual state on mount)
        alsoRemoveHubRecordRef.current = false

        const confirmed = await confirm({
            variant: "warning",
            title: "Wipe node data",
            typeToConfirm: node.name,
            confirmLabel: "Wipe node",
            cancelLabel: "Cancel",
            description: (
                <WipeConfirmBody
                    nodeName={node.name}
                    onAlsoRemoveChange={updateAlsoRemoveHubRecord}
                />
            ),
            onConfirm: async () => {
                const alsoRemove = alsoRemoveHubRecordRef.current
                const result = await wipeNode(node.id, { alsoRemoveHubRecord: alsoRemove })
                if (result.error) {
                    toast.error(`Wipe error: ${result.error}`, { duration: 8000 })
                    throw new Error(result.error)
                }
                if (alsoRemove) {
                    toast.success(`${node.name} wiped and removed from the panel`)
                    queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
                } else {
                    toast.success(`${node.name} wiped successfully`)
                }
            },
        })
        void confirmed
    }, [confirm, queryClient])

    const handleCheckHealth = useCallback((nodeId: number) => {
        setCheckingHealth(nodeId)
        checkHealthMutation.mutate(nodeId, {
            onSettled: () => setCheckingHealth(null)
        })
    }, [checkHealthMutation, setCheckingHealth])

    const handleDeleteNode = useCallback(async (node: Node) => {
        const confirmed = await confirm({
            title: "Delete Node",
            description: `Are you sure you want to delete "${node.name}"? This action cannot be undone.`,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (!confirmed) return

        deleteNodeMutation.mutate({ id: node.id }, {
            onError: (error: any) => {
                if (error?.code === "NODE_HAS_CHILDREN") {
                    setDeleteConflictNode(node)
                }
            }
        })
    }, [confirm, deleteNodeMutation])

    const handleForceDelete = () => {
        if (!deleteConflictNode) return
        deleteNodeMutation.mutate({ id: deleteConflictNode.id, force: true }, {
            onSuccess: () => setDeleteConflictNode(null)
        })
    }

    const handleMigrate = () => {
        if (!deleteConflictNode) return
        setMigrationNode(deleteConflictNode)
        setDeleteConflictNode(null)
    }

    const handleAddNode = () => {
        const result = nodeSchema.safeParse(newNode)
        if (!result.success) {
            toast.error(result.error.issues[0].message)
            return
        }

        createNodeMutation.mutate({
            name: newNode.name,
            ip: newNode.ip,
            api_port: parseInt(newNode.api_port),
            country_code: newNode.country,
            datacenter: newNode.datacenter,
            agent_port: parseInt(newNode.agent_port),
            connect_mode: newNode.connect_mode,
        }, {
            // Straight on to the deploy step for the node just added.
            onSuccess: (created) => { closeAddDialog(); resetNewNode(); if (created?.id) openDeployDialog(created.id) }
        })
    }

    const handleRefresh = useCallback(() => {
        // Scoped refresh: list + bulk stats + bulk history only. Avoids
        // blasting inbound / outbound / routing / config-diff caches on
        // every auto-refresh tick.
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeStatsBulkAll(), exact: false })
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeStatsHistoryBulkAll(), exact: false })
    }, [queryClient])

    const handleDeployFromSheet = useCallback(() => {
        if (selectedNode) {
            const nodeId = selectedNode.id
            setSelectedNode(null)
            openDeployDialog(nodeId)
        }
    }, [selectedNode, openDeployDialog])

    const onlineCount = nodes.filter(n => n.is_online).length
    const totalTraffic = aggregateStats?.totalTraffic ?? 0
    const avgCpu = aggregateStats?.avgCpu ?? 0
    const totalConnections = aggregateStats?.totalConnections ?? 0

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            {/* Mobile Compact Header */}
            <div className="md:hidden flex items-center justify-between px-4 py-2.5">
                <div className="flex items-baseline gap-2">
                    <h1 className="text-xl font-bold tracking-tight">Nodes</h1>
                    <span className="text-sm text-muted-foreground">
                        <span className="text-emerald-500">{onlineCount}</span>/{nodes.length}
                    </span>
                </div>
                <div className="flex items-center gap-1">
                    <Button variant="ghost" size="icon" className="h-9 w-9"
                        onClick={() => handleRefresh()}
                        disabled={isRefetching} aria-label="Refresh">
                        <HiOutlineRefresh className={cn("w-4.5 h-4.5", isRefetching && "animate-spin")} />
                    </Button>
                    <Button variant="ghost" size="icon" className="h-9 w-9"
                        onClick={openAddDialog} aria-label="Add node">
                        <HiOutlinePlus className="w-4.5 h-4.5" />
                    </Button>
                </div>
            </div>

            {/* Header */}
            <div className="hidden md:block">
                <PageHeader
                    title="Nodes"
                    description="This router and the servers it manages"
                    actions={
                        <>
                            <AutoRefreshControl isRefreshing={isRefetching} dataUpdatedAt={dataUpdatedAt} />
                            <Button onClick={openAddDialog}>
                                <HiOutlinePlus className="w-4 h-4 mr-2" />
                                Add node
                            </Button>
                        </>
                    }
                />
            </div>

            {/* Aggregate Stats Bar */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 md:gap-4 lg:gap-5">
                <div className="rounded-lg border border-border/50 bg-card/50 p-3 md:p-4 lg:p-5">
                    <div className="flex items-center gap-2 text-muted-foreground mb-1 md:mb-2 text-[11px] sm:text-[12px] md:text-xs">
                        <Server className="w-3.5 h-3.5 md:w-4 md:h-4" />
                        <span className="uppercase tracking-wider font-medium">Nodes</span>
                    </div>
                    {isLoading ? <Skeleton className="h-6 md:h-7 lg:h-8 w-16 md:w-20" /> : (
                        <p className="text-lg md:text-xl lg:text-2xl font-bold">
                            <span className="text-emerald-500">{onlineCount}</span>
                            <span className="text-muted-foreground font-normal text-sm md:text-base"> / {nodes.length}</span>
                        </p>
                    )}
                </div>
                <div className="rounded-lg border border-border/50 bg-card/50 p-3 md:p-4 lg:p-5">
                    <div className="flex items-center gap-2 text-muted-foreground mb-1 md:mb-2 text-[11px] sm:text-[12px] md:text-xs">
                        <Globe className="w-3.5 h-3.5 md:w-4 md:h-4" />
                        <span className="uppercase tracking-wider font-medium">Total Traffic</span>
                    </div>
                    <p className="text-lg md:text-xl lg:text-2xl font-bold">{formatBytes(totalTraffic)}</p>
                </div>
                <div className="rounded-lg border border-border/50 bg-card/50 p-3 md:p-4 lg:p-5">
                    <div className="flex items-center gap-2 text-muted-foreground mb-1 md:mb-2 text-[11px] sm:text-[12px] md:text-xs">
                        <Cpu className="w-3.5 h-3.5 md:w-4 md:h-4" />
                        <span className="uppercase tracking-wider font-medium">Avg CPU</span>
                    </div>
                    <p className={cn(
                        "text-lg md:text-xl lg:text-2xl font-bold",
                        avgCpu > 80 ? "text-red-500" : avgCpu > 50 ? "text-amber-500" : "text-emerald-500"
                    )}>{avgCpu.toFixed(0)}%</p>
                </div>
                <div className="rounded-lg border border-border/50 bg-card/50 p-3 md:p-4 lg:p-5">
                    <div className="flex items-center gap-2 text-muted-foreground mb-1 md:mb-2 text-[11px] sm:text-[12px] md:text-xs">
                        <Network className="w-3.5 h-3.5 md:w-4 md:h-4" />
                        <span className="uppercase tracking-wider font-medium">Connections</span>
                    </div>
                    <p className="text-lg md:text-xl lg:text-2xl font-bold">{totalConnections.toLocaleString()}</p>
                </div>
            </div>

            {/* Nodes Grid */}
            <div className="space-y-2">
                {!isLoading && nodes.length > 0 && (
                    <div className="flex items-center justify-between gap-3">
                        <label className="flex w-fit cursor-pointer items-center gap-2 text-xs text-muted-foreground transition-colors hover:text-foreground">
                            <Checkbox
                                checked={allSelected}
                                onCheckedChange={handleToggleSelectAll}
                                aria-label={allSelected ? "Deselect all nodes" : "Select all nodes"}
                            />
                            <span>{allSelected ? "Deselect all" : "Select all"}</span>
                        </label>
                        {selectionMode && (
                            <span className="text-xs tabular-nums text-muted-foreground">
                                {selectedIds.size} of {nodes.length} selected
                            </span>
                        )}
                    </div>
                )}
                <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
                    {isLoading ? (
                        Array.from({ length: 6 }).map((_, i) => <NodeCardSkeleton key={i} />)
                    ) : nodes.length > 0 ? (
                        nodes.map((node) => (
                            <CompactNodeCard
                                key={node.id}
                                node={node}
                                stats={bulkStats?.[String(node.id)]?.stats}
                                isStatsLoading={node.is_online && isBulkStatsLoading && !bulkStats?.[String(node.id)]}
                                sparkline={sparklineFromBulk(bulkHistory, node.id)}
                                onCheckHealth={handleCheckHealth}
                                onDelete={handleDeleteNode}
                                onWipe={handleWipeNode}
                                onDeploy={openDeployDialog}
                                onSelect={setSelectedNode}
                                onToggleSelect={handleToggleSelect}
                                onRangeSelect={handleRangeSelect}
                            />
                        ))
                    ) : (
                        <Card className="col-span-full">
                            <CardContent className="flex flex-col items-center justify-center py-12">
                                <HiOutlineServer className="w-12 h-12 text-muted-foreground/50 mb-4" />
                                <p className="text-muted-foreground mb-4">No nodes configured</p>
                                <Button onClick={openAddDialog}>
                                    <HiOutlinePlus className="w-4 h-4 mr-2" /> Add Your First Node
                                </Button>
                            </CardContent>
                        </Card>
                    )}
                </div>
            </div>

            {/* Node Detail Sheet */}
            <NodeDetailSheet
                node={selectedNode}
                open={!!selectedNode}
                onOpenChange={(open) => !open && setSelectedNode(null)}
                onDeploy={selectedNode ? handleDeployFromSheet : undefined}
            />

            {/* Add Node Dialog */}
            <Dialog open={addDialogOpen} onOpenChange={(open) => open ? openAddDialog() : closeAddDialog()}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Add node</DialogTitle>
                        <DialogDescription>A remote machine that runs the NasNet agent.</DialogDescription>
                    </DialogHeader>
                    <div className="space-y-4 py-4">
                        <div className="space-y-2">
                            <Label htmlFor="name">Name</Label>
                            <Input id="name" placeholder="Frankfurt VPS" value={newNode.name} onChange={(e) => updateNewNode({ name: e.target.value })} />
                        </div>
                        <div className="space-y-2">
                            <Label htmlFor="ip">Node address</Label>
                            <Input id="ip" className="font-mono" placeholder="203.0.113.24" value={newNode.ip} onChange={(e) => updateNewNode({ ip: e.target.value })} />
                        </div>
                        <fieldset className="space-y-2">
                            <legend className="text-sm font-medium">How the panel reaches it</legend>
                            {([
                                { value: "direct", label: "Direct", hint: "This router dials the node's agent port. Works from behind NAT." },
                                { value: "reverse", label: "Reverse", hint: "The node dials this panel. Needs reverse connections on and a panel address the node can reach." },
                            ] as const).map((m) => (
                                <label
                                    key={m.value}
                                    className={cn(
                                        "flex cursor-pointer gap-3 rounded-lg border p-3 transition-colors",
                                        newNode.connect_mode === m.value ? "border-primary ring-2 ring-primary/15" : "hover:bg-muted/40",
                                    )}
                                >
                                    <input
                                        type="radio"
                                        name="connect_mode"
                                        value={m.value}
                                        checked={newNode.connect_mode === m.value}
                                        onChange={() => updateNewNode({ connect_mode: m.value })}
                                        className="mt-0.5 accent-primary"
                                    />
                                    <span className="space-y-0.5">
                                        <span className="block text-sm font-medium">{m.label}</span>
                                        <span className="block text-xs text-muted-foreground">{m.hint}</span>
                                    </span>
                                </label>
                            ))}
                        </fieldset>
                        <div className="space-y-2">
                            <Label htmlFor="agent_port">Agent port</Label>
                            <Input id="agent_port" type="number" className="w-32 font-mono" placeholder="9090" value={newNode.agent_port} onChange={(e) => updateNewNode({ agent_port: e.target.value })} />
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={closeAddDialog}>Cancel</Button>
                        <Button onClick={handleAddNode} disabled={createNodeMutation.isPending}>
                            {createNodeMutation.isPending ? "Adding..." : "Add and deploy"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <DeployAgentDialog
                nodeId={selectedDeployNodeId}
                open={deployDialogOpen}
                onOpenChange={(open) => open ? undefined : closeDeployDialog()}
            />

            {/* Delete Conflict Dialog */}
            <Dialog open={!!deleteConflictNode} onOpenChange={(open) => !open && setDeleteConflictNode(null)}>
                <DialogContent className="sm:max-w-lg">
                    <DialogHeader className="text-left">
                        <DialogTitle className="flex items-center gap-2 pr-6">
                            <ShieldAlert aria-hidden="true" className="h-5 w-5 shrink-0 text-destructive" />
                            Cannot Delete Node
                        </DialogTitle>
                        <DialogDescription>
                            Move this node’s active accounts before deleting it.
                        </DialogDescription>
                    </DialogHeader>

                    <div className="flex min-w-0 items-center gap-3 rounded-lg border border-border/50 bg-muted/20 px-3 py-2.5">
                        <Server aria-hidden="true" className="h-4 w-4 shrink-0 text-muted-foreground" />
                        <div className="min-w-0 flex-1">
                            <p className="truncate text-sm font-medium" title={deleteConflictNode?.name}>{deleteConflictNode?.name}</p>
                            <p className="truncate font-mono text-xs text-muted-foreground">{deleteConflictNode?.ip}</p>
                        </div>
                        <Badge variant="outline" className="shrink-0 text-[11px] font-medium text-muted-foreground">Active accounts</Badge>
                    </div>

                    <div className="flex items-start gap-3 py-1">
                        <ArrowLeftRight aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                        <div className="space-y-1">
                            <p className="text-sm font-medium">Migrate accounts first</p>
                            <p className="text-sm leading-relaxed text-muted-foreground">
                                Transfer accounts to another node, then return here to delete this one.
                            </p>
                        </div>
                    </div>

                    <DialogFooter className="gap-2 border-t border-border/40 pt-4 sm:justify-between sm:space-x-0">
                        <Button variant="ghost" onClick={handleForceDelete} disabled={deleteNodeMutation.isPending} className="text-destructive hover:bg-destructive/10 hover:text-destructive">
                            {deleteNodeMutation.isPending ? <Loader2 aria-hidden="true" className="h-4 w-4 animate-spin" /> : <Trash2 aria-hidden="true" className="h-4 w-4" />}
                            {deleteNodeMutation.isPending ? "Deleting…" : "Force Delete"}
                        </Button>
                        <div className="flex flex-col-reverse gap-2 sm:flex-row">
                            <Button variant="outline" onClick={() => setDeleteConflictNode(null)} disabled={deleteNodeMutation.isPending}>Keep Node</Button>
                            <Button onClick={handleMigrate} disabled={deleteNodeMutation.isPending}>
                                <ArrowLeftRight aria-hidden="true" className="h-4 w-4" />
                                Migrate Accounts
                            </Button>
                        </div>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <MigrationDialog
                sourceNode={migrationNode}
                open={!!migrationNode}
                onOpenChange={(open) => !open && setMigrationNode(null)}
                onSuccess={() => { toast.success("Accounts migrated. You can now safe delete the node."); setMigrationNode(null) }}
            />

            <BulkNodeActionsBar />
        </div>
    )
}
