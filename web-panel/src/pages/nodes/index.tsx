import { useState, useCallback, useEffect, useMemo, useRef, type MouseEvent } from "react"
import { useNavigate } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { ArrowLeftRight, Loader2, Plus, Server, ShieldAlert, SquareCheck, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
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
import { useConfirm } from "@/components/ui/confirm-dialog"
import { DeployAgentDialog } from "@/components/deploy-agent-dialog"
import { UpdateAgentDialog } from "@/components/update-agent-dialog"
import { MigrationDialog } from "@/components/node/migration-dialog"
import { BulkNodeActionsBar } from "@/components/node/bulk-node-actions-bar"
import { WipeConfirmBody } from "@/components/node/wipe-confirm-body"
import { AddNodeKindPicker } from "@/components/node/add-node-kind-picker"
import { AddMtprotoNodeDialog } from "@/components/mtproto/add-mtproto-node-dialog"
import { useEventsStatus } from "@/components/providers/events-provider"
import { FleetTiles } from "@/components/nodes-list/fleet-tiles"
import { NodeCard, NodeCardSkeleton } from "@/components/nodes-list/node-card"
import { NodesTable, NodeRowsPhone } from "@/components/nodes-list/nodes-table"
import { MaintenanceDialog } from "@/components/nodes-list/maintenance-dialog"
import { ViewSwitch, type NodesView } from "@/components/nodes-list/view-switch"
import type { NodeActions } from "@/components/nodes-list/node-menu"
import { buildView, DEFAULT_SORT, firstDir, orderViews, type NodeView, type Sort, type SortKey } from "@/components/nodes-list/facts"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useLocalStorage } from "@/hooks/use-local-storage"
import { useNodes, useNodesStatsBulk, useNodesStatsHistoryBulk, useNodesDigest, useCreateNode, useDeleteNode, useCheckNodeHealth, useMtprotoNodes } from "@/lib/queries"
import { useAgentUpdates } from "@/lib/queries/use-bulk-agent-update"
import { queryKeys } from "@/lib/queries/keys"
import { wipeNode } from "@/lib/api/nuke"
import { setNodeMaintenance } from "@/lib/api/maintenance"
import { getApiBaseUrl } from "@/lib/config"
import { nodeSchema } from "@/lib/validations/node-schema"
import { isLiteNode } from "@/lib/node-kind"
import type { Node, NodeStats } from "@/lib/types"
import { cn } from "@/lib/utils"
import { toast } from "@/lib/toast"
import { useNodesStore } from "@/store/nodes-store"

/** Re-render on an interval so durations and "last reading" stay current. */
function useNow(intervalMs: number) {
    const [now, setNow] = useState(() => Date.now())
    useEffect(() => {
        const id = setInterval(() => setNow(Date.now()), intervalMs)
        return () => clearInterval(id)
    }, [intervalMs])
    return now
}

function Reconnecting({ className }: { className?: string }) {
    return (
        <span role="status" className={cn("inline-flex items-center gap-1.5 whitespace-nowrap text-[13px] text-muted-foreground", className)}>
            <Loader2 className="size-3.5 animate-spin" aria-hidden /> Reconnecting…
        </span>
    )
}

export default function NodesPage() {
    const navigate = useNavigate()
    const queryClient = useQueryClient()
    const isMobile = useIsMobile()
    const now = useNow(15_000)

    // Live from the event stream; poll only while it's down.
    const live = useEventsStatus() === "connected"
    const everLive = useRef(false)
    if (live) everLive.current = true
    const reconnecting = !live && everLive.current

    // Edits the stream doesn't announce (maintenance, Xray stopped) still
    // arrive within a minute.
    const { data: nodes = [], isLoading } = useNodes(true, { poll: live ? 60_000 : 15_000 })
    // MTProto nodes have no stats sweep; one call covers them all.
    const onlineIds = useMemo(() => nodes.filter((n) => n.is_online && !isLiteNode(n)).map((n) => n.id), [nodes])
    const { data: mtprotoSummaries } = useMtprotoNodes(nodes.some(isLiteNode))
    const { data: bulkStats } = useNodesStatsBulk(onlineIds, onlineIds.length > 0, live ? 60_000 : 15_000)
    const { data: history } = useNodesStatsHistoryBulk(onlineIds, 15, onlineIds.length > 0)
    const { data: digests } = useNodesDigest(nodes.length > 0)
    const { data: agentDue } = useAgentUpdates(nodes.length > 0)

    const createNodeMutation = useCreateNode()
    const deleteNodeMutation = useDeleteNode()
    const checkHealthMutation = useCheckNodeHealth()

    const [view, setView] = useLocalStorage<NodesView>("nodes:view", "cards")
    const [sort, setSort] = useLocalStorage<Sort>("nodes:list-sort", DEFAULT_SORT)

    const views = useMemo(() => nodes.map((n) => {
        const key = String(n.id)
        // A node that just went down keeps the readings the browser already had.
        const reading = bulkStats?.[key]?.stats ?? queryClient.getQueryData<NodeStats>(queryKeys.nodeStats(n.id))
        const mtproto = isLiteNode(n) ? mtprotoSummaries?.find((s) => s.node_id === n.id) : undefined
        return buildView(n, reading, digests?.[key], !!agentDue?.[key], now, mtproto)
    }), [nodes, bulkStats, digests, agentDue, now, queryClient, mtprotoSummaries])

    const cardsOrder = useMemo(() => orderViews(views), [views])
    const listOrder = useMemo(() => orderViews(views, sort), [views, sort])
    const shown = view === "list" ? listOrder : cardsOrder

    const {
        addDialogOpen, openAddDialog, closeAddDialog,
        deployDialogOpen, openDeployDialog, closeDeployDialog,
        selectedDeployNodeId, setCheckingHealth,
        newNode, updateNewNode, resetNewNode,
        selectMode, selectedIds, lastSelectedId,
        startSelecting, stopSelecting, toggleSelected, selectRange, setSelected,
    } = useNodesStore()

    // Selection belongs to this visit; coming back starts fresh.
    useEffect(() => () => stopSelecting(), [stopSelecting])

    // Esc leaves select mode, unless a field or dialog has the key.
    useEffect(() => {
        if (!selectMode) return
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape" || e.defaultPrevented) return
            const el = document.activeElement
            if (el && (el.closest("input, textarea, [role=dialog], [role=menu]"))) return
            stopSelecting()
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [selectMode, stopSelecting])

    const shownIds = useMemo(() => shown.map((v) => v.node.id), [shown])

    const onActivate = useCallback((v: NodeView, e: MouseEvent) => {
        const id = v.node.id
        if (selectMode || e.shiftKey) {
            if (!selectMode) startSelecting(id)
            else if (e.shiftKey && lastSelectedId != null) selectRange(lastSelectedId, id, shownIds)
            else toggleSelected(id)
            return
        }
        if (e.metaKey || e.ctrlKey || e.button === 1) {
            window.open(`${getApiBaseUrl()}/nodes/${id}`, "_blank", "noopener")
            return
        }
        navigate(`/nodes/${id}`)
    }, [selectMode, lastSelectedId, shownIds, startSelecting, selectRange, toggleSelected, navigate])

    const onToggle = useCallback((v: NodeView) => toggleSelected(v.node.id), [toggleSelected])
    const onLongPress = useCallback((v: NodeView) => {
        if (selectMode) toggleSelected(v.node.id)
        else startSelecting(v.node.id)
    }, [selectMode, toggleSelected, startSelecting])

    const onSort = (key: SortKey) => {
        setSort((s) => (s.key === key ? { key, dir: s.dir === "asc" ? "desc" : "asc" } : { key, dir: firstDir(key) }))
    }

    const confirm = useConfirm()
    const [mtprotoDialogOpen, setMtprotoDialogOpen] = useState(false)
    // The add dialog opens on a choice of node kind.
    const [addKind, setAddKind] = useState<"choose" | "full">("choose")
    const startAdd = () => {
        setAddKind("choose")
        openAddDialog()
    }
    const addMTProtoNode = () => {
        closeAddDialog()
        setAddKind("choose")
        setMtprotoDialogOpen(true)
    }
    const [migrationNode, setMigrationNode] = useState<Node | null>(null)
    const [deleteConflictNode, setDeleteConflictNode] = useState<Node | null>(null)
    const [maintenanceNode, setMaintenanceNode] = useState<Node | null>(null)
    const [maintenancePending, setMaintenancePending] = useState(false)
    const [agentNode, setAgentNode] = useState<Node | null>(null)

    // ── Wipe ──────────────────────────────────────────────────────────
    // The ref is read by the onConfirm closure to get the latest checkbox value.
    // WipeConfirmBody owns the visual state; it writes back here via onAlsoRemoveChange.
    const alsoRemoveHubRecordRef = useRef(false)

    const handleWipeNode = useCallback(async (node: Node) => {
        alsoRemoveHubRecordRef.current = false
        await confirm({
            variant: "warning",
            title: "Wipe node data",
            typeToConfirm: node.name,
            confirmLabel: "Wipe node",
            cancelLabel: "Cancel",
            description: (
                <WipeConfirmBody
                    nodeName={node.name}
                    onAlsoRemoveChange={(v) => { alsoRemoveHubRecordRef.current = v }}
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
    }, [confirm, queryClient])

    const handleCheckHealth = useCallback((nodeId: number) => {
        setCheckingHealth(nodeId)
        checkHealthMutation.mutate(nodeId, { onSettled: () => setCheckingHealth(null) })
    }, [checkHealthMutation, setCheckingHealth])

    const handleDeleteNode = useCallback(async (node: Node) => {
        const confirmed = await confirm({
            title: "Delete node",
            description: `Delete "${node.name}"? This can't be undone.`,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (!confirmed) return
        deleteNodeMutation.mutate({ id: node.id }, {
            onError: (error: unknown) => {
                if ((error as { code?: string })?.code === "NODE_HAS_CHILDREN") setDeleteConflictNode(node)
            },
        })
    }, [confirm, deleteNodeMutation])

    const setMaintenance = useCallback(async (node: Node, enabled: boolean, message: string) => {
        setMaintenancePending(true)
        try {
            const r = await setNodeMaintenance(node.id, { enabled, message })
            if (!r.success) {
                toast.error(r.error || "Couldn't change maintenance")
                return
            }
            toast.success(enabled ? `${node.name} is in maintenance` : `Maintenance ended on ${node.name}`)
            setMaintenanceNode(null)
            queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            queryClient.invalidateQueries({ queryKey: ["maintenance"] })
        } catch (e) {
            toast.error(`Couldn't change maintenance: ${(e as Error).message}`)
        } finally {
            setMaintenancePending(false)
        }
    }, [queryClient])

    const actions: NodeActions = useMemo(() => ({
        checkConnection: (n) => handleCheckHealth(n.id),
        deploy: (n) => openDeployDialog(n.id),
        updateAgent: (n) => setAgentNode(n),
        startMaintenance: (n) => setMaintenanceNode(n),
        endMaintenance: (n) => void setMaintenance(n, false, ""),
        wipe: (n) => void handleWipeNode(n),
        remove: (n) => void handleDeleteNode(n),
    }), [handleCheckHealth, openDeployDialog, setMaintenance, handleWipeNode, handleDeleteNode])

    const handleForceDelete = () => {
        if (!deleteConflictNode) return
        deleteNodeMutation.mutate({ id: deleteConflictNode.id, force: true }, {
            onSuccess: () => setDeleteConflictNode(null),
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
            onSuccess: (created) => { closeAddDialog(); resetNewNode(); if (created?.id) openDeployDialog(created.id) },
        })
    }

    const toggleSelectMode = () => (selectMode ? stopSelecting() : startSelecting())
    const empty = !isLoading && nodes.length === 0
    const selectedSet: ReadonlySet<number> = selectedIds

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            {/* Phone header */}
            <div className="flex items-center justify-between gap-2 px-4 py-2 md:hidden">
                <div className="grid">
                    <h1 className="text-xl font-bold tracking-tight">Nodes</h1>
                    {reconnecting && <Reconnecting className="text-xs" />}
                </div>
                <div className="flex items-center gap-0.5">
                    {!empty && <ViewSwitch value={view} onChange={setView} iconsOnly />}
                    {!empty && (
                        <Button variant="ghost" size="icon" className={cn("size-10", selectMode && "bg-muted")} onClick={toggleSelectMode} aria-pressed={selectMode} aria-label={selectMode ? "Done selecting" : "Select nodes"}>
                            <SquareCheck className="size-[18px]" />
                        </Button>
                    )}
                    <Button variant="ghost" size="icon" className="size-10" onClick={startAdd} aria-label="Add node">
                        <Plus className="size-[18px]" />
                    </Button>
                </div>
            </div>

            {/* Desktop header */}
            <div className="hidden items-center justify-between gap-4 md:flex">
                <div className="flex min-w-0 items-baseline gap-3.5">
                    <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">Nodes</h1>
                    {reconnecting && <Reconnecting />}
                </div>
                <div className="flex shrink-0 items-center gap-2">
                    {!empty && <ViewSwitch value={view} onChange={setView} />}
                    {selectMode && (
                        <Button variant="ghost" size="sm" onClick={() => setSelected(shownIds)}>Select all</Button>
                    )}
                    {!empty && (
                        <Button variant="outline" size="sm" onClick={toggleSelectMode} aria-pressed={selectMode} className={cn(selectMode && "bg-muted")}>
                            <SquareCheck className="size-3.5" />
                            {selectMode ? "Done" : "Select"}
                        </Button>
                    )}
                    <Button onClick={startAdd}>
                        <Plus className="size-4" />
                        Add node
                    </Button>
                </div>
            </div>

            {empty ? (
                <div className="grid justify-items-center gap-3 rounded-2xl border border-dashed px-6 py-16 text-center">
                    <Server className="size-9 text-muted-foreground/60" aria-hidden />
                    <h2 className="text-base font-semibold">No nodes yet</h2>
                    <p className="max-w-[42ch] text-sm text-muted-foreground">
                        A node is a server that runs the NasNet agent and carries your users' traffic. Add one, and the next step installs the agent.
                    </p>
                    <Button onClick={startAdd}><Plus className="size-4" />Add node</Button>
                </div>
            ) : (
                <>
                    <FleetTiles views={views} history={history} now={now} compact={isMobile} loading={isLoading} />

                    <div className="pb-24 md:pb-20">
                        {view === "list" ? (
                            isMobile ? (
                                <NodeRowsPhone
                                    views={shown}
                                    now={now}
                                    selectMode={selectMode}
                                    selected={selectedSet}
                                    onActivate={onActivate}
                                    onToggle={onToggle}
                                    onLongPress={onLongPress}
                                />
                            ) : (
                                <NodesTable
                                    views={shown}
                                    now={now}
                                    loading={isLoading}
                                    sort={sort}
                                    onSort={onSort}
                                    actions={actions}
                                    selectMode={selectMode}
                                    selected={selectedSet}
                                    onToggle={onToggle}
                                    onToggleAll={(all) => (all ? setSelected(shownIds) : setSelected([]))}
                                    onActivate={onActivate}
                                />
                            )
                        ) : (
                            // Two across up to four nodes, three from five, by the grid's own width.
                            <div className="@container">
                            <div className={cn("grid grid-cols-1 gap-3 md:gap-4 @min-[700px]:grid-cols-2", nodes.length > 4 && "@min-[1040px]:grid-cols-3")}>
                                {isLoading
                                    ? Array.from({ length: 2 }, (_, i) => <NodeCardSkeleton key={i} />)
                                    : shown.map((v) => (
                                        <NodeCard
                                            key={v.node.id}
                                            view={v}
                                            now={now}
                                            actions={actions}
                                            selectMode={selectMode}
                                            selected={selectedSet.has(v.node.id)}
                                            compact={isMobile}
                                            onActivate={onActivate}
                                            onToggle={onToggle}
                                            onLongPress={onLongPress}
                                        />
                                    ))}
                            </div>
                            </div>
                        )}
                    </div>
                </>
            )}

            <AddMtprotoNodeDialog
                open={mtprotoDialogOpen}
                onOpenChange={setMtprotoDialogOpen}
                onBack={() => {
                    setMtprotoDialogOpen(false)
                    startAdd()
                }}
                onInstalled={(id) => {
                    queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
                    navigate(`/nodes/${id}`)
                }}
            />

            {/* Add Node Dialog */}
            <Dialog open={addDialogOpen} onOpenChange={(open) => {
                if (open) return openAddDialog()
                closeAddDialog()
                setAddKind("choose")
            }}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Add node</DialogTitle>
                        <DialogDescription>
                            {addKind === "choose" ? "Pick what the node will run." : "A remote machine that runs the NasNet agent."}
                        </DialogDescription>
                    </DialogHeader>
                    {addKind === "choose" ? (
                        <AddNodeKindPicker onPick={(kind) => (kind === "full" ? setAddKind("full") : addMTProtoNode())} />
                    ) : (
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
                    )}
                    {addKind === "full" && (
                        <DialogFooter>
                            <Button variant="ghost" onClick={() => setAddKind("choose")}>Back</Button>
                            <Button onClick={handleAddNode} disabled={createNodeMutation.isPending}>
                                {createNodeMutation.isPending ? "Adding..." : "Add and deploy"}
                            </Button>
                        </DialogFooter>
                    )}
                </DialogContent>
            </Dialog>

            <DeployAgentDialog
                nodeId={selectedDeployNodeId}
                open={deployDialogOpen}
                onOpenChange={(open) => open ? undefined : closeDeployDialog()}
            />

            <MaintenanceDialog
                node={maintenanceNode}
                pending={maintenancePending}
                onConfirm={(message) => maintenanceNode && void setMaintenance(maintenanceNode, true, message)}
                onOpenChange={(open) => !open && setMaintenanceNode(null)}
            />

            {agentNode && (
                <UpdateAgentDialog
                    nodeId={agentNode.id}
                    nodeName={agentNode.name}
                    currentVersion={bulkStats?.[String(agentNode.id)]?.stats?.agent_version || agentNode.agent_version}
                    open={!!agentNode}
                    onOpenChange={(open) => !open && setAgentNode(null)}
                    onSuccess={() => queryClient.invalidateQueries({ queryKey: queryKeys.nodes })}
                />
            )}

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
