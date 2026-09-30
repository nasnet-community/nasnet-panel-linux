import { useMemo, useState } from "react"
import { Search, Cable, Server, WifiOff } from "lucide-react"
import { toast } from "@/lib/toast"
import { Input } from "@/components/ui/input"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"
import type { Node } from "@/lib/types"
import { LocalNodeBadge } from "@/components/node/local-node-badge"
import { isLocalNode, nodeAddress } from "@/lib/node-kind"
import { useAllCoreInbounds, useNodeDeps, useNodes } from "@/lib/queries"
import { useCoresStore, type CoreProtocolFilter } from "@/store/cores-store"
import type { CoreInbound } from "@/lib/types/cores"
import { filterAndSortInbounds } from "@/components/cores/filter-sort"
import { CoreList } from "@/components/cores/core-list"
import { CoreDetailModal } from "@/components/cores/core-detail-modal"
import { CreateCoreDialog } from "@/components/cores/create-core-dialog"

const PROTOCOL_TABS: { value: CoreProtocolFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "ssh", label: "SSH" },
    { value: "mtproto", label: "MTProto" },
]

interface NodeGroup {
    nodeId: number
    node?: Node
    inbounds: CoreInbound[]
}

/**
 * Cross-node "All Tunnels" overview — every SSH/MTProto inbound across every node in
 * one place. Cards carry no node identity, so tunnels are grouped under a
 * per-node header. Detail/edit reuse the same modal/dialog as the per-node tab;
 * creation stays node-scoped (it needs a node context), so this page is
 * detail/edit-only.
 */
export default function TunnelsPage() {
    const { data: inbounds = [], isLoading } = useAllCoreInbounds()
    const { data: nodes = [] } = useNodes()
    const { search, setSearch, protocolFilter, setProtocolFilter, sortBy, sortDir, setSort } =
        useCoresStore()
    const [nodeFilter, setNodeFilter] = useState<"all" | number>("all")

    const [detailInbound, setDetailInbound] = useState<CoreInbound | null>(null)
    const [detailOpen, setDetailOpen] = useState(false)
    const [editInbound, setEditInbound] = useState<CoreInbound | null>(null)
    const [editOpen, setEditOpen] = useState(false)

    const nodeMap = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes])

    const stats = useMemo(() => {
        const live = inbounds.filter((i) => !i.is_disabled).length
        const disabled = inbounds.filter((i) => i.is_disabled).length
        const users = inbounds.reduce((n, i) => n + (i.users?.length ?? 0), 0)
        const nodeIds = new Set(inbounds.map((i) => i.node_id))
        return { live, disabled, users, nodes: nodeIds.size }
    }, [inbounds])

    // Filter (protocol + search) then group by node, ordered by node name.
    const groups = useMemo<NodeGroup[]>(() => {
        const scoped =
            nodeFilter === "all" ? inbounds : inbounds.filter((i) => i.node_id === nodeFilter)
        const filtered = filterAndSortInbounds(scoped, { protocolFilter, search, sortBy, sortDir })
        const byNode = new Map<number, CoreInbound[]>()
        for (const inbound of filtered) {
            const list = byNode.get(inbound.node_id) ?? []
            list.push(inbound)
            byNode.set(inbound.node_id, list)
        }
        return [...byNode.entries()]
            .map(([nodeId, list]) => ({ nodeId, node: nodeMap.get(nodeId), inbounds: list }))
            .sort((a, b) => {
                const an = a.node?.name ?? `Node #${a.nodeId}`
                const bn = b.node?.name ?? `Node #${b.nodeId}`
                return an.localeCompare(bn)
            })
    }, [inbounds, nodeFilter, protocolFilter, search, sortBy, sortDir, nodeMap])

    // Nodes that actually host tunnels — the only useful entries for the filter.
    const nodesWithTunnels = useMemo(() => {
        const ids = new Set(inbounds.map((i) => i.node_id))
        return nodes
            .filter((n) => ids.has(n.id))
            .sort((a, b) => a.name.localeCompare(b.name))
    }, [inbounds, nodes])

    // The detail modal / edit dialog both need the inbound's node. If it can't be
    // resolved (e.g. a node was deleted out from under an orphaned inbound), we
    // refuse to open rather than crash on node.ip / node.is_online.
    const openDetail = (inbound: CoreInbound) => {
        if (!nodeMap.get(inbound.node_id)) {
            toast.error("This tunnel's node no longer exists.")
            return
        }
        setDetailInbound(inbound)
        setDetailOpen(true)
    }
    const openEdit = (inbound: CoreInbound) => {
        if (!nodeMap.get(inbound.node_id)) {
            toast.error("This tunnel's node no longer exists.")
            return
        }
        setEditInbound(inbound)
        setEditOpen(true)
    }

    const detailNode = detailInbound ? nodeMap.get(detailInbound.node_id) : undefined
    const editNode = editInbound ? nodeMap.get(editInbound.node_id) : undefined

    const hasAny = inbounds.length > 0

    return (
        <div className="space-y-5">
            {/* Header */}
            <div>
                <h1 className="text-2xl font-semibold">Tunnels</h1>
                <p className="text-sm text-muted-foreground">
                    Every SSH and MTProto tunnel across all nodes. Create tunnels from a node's
                    Tunnels tab.
                </p>
            </div>

            {/* Stat row */}
            <div className="flex flex-wrap gap-x-6 gap-y-2 rounded-lg border bg-card px-4 py-3 text-sm">
                <Stat dotClass="bg-emerald-500" label="Live" value={stats.live} />
                <Stat dotClass="bg-muted-foreground" label="Disabled" value={stats.disabled} />
                <div className="flex items-center gap-1.5 text-muted-foreground">
                    <Server className="size-4" />
                    <span className="font-medium text-foreground">{stats.nodes}</span> nodes
                </div>
                <div className="ml-auto flex items-center gap-1.5 text-muted-foreground">
                    <Cable className="size-4" />
                    <span className="font-medium text-foreground">{stats.users}</span> users total
                </div>
            </div>

            {/* Toolbar */}
            <div className="flex flex-wrap items-center gap-2">
                <div className="relative min-w-[200px] flex-1">
                    <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        placeholder="Search protocol, port, remark, email…"
                        className="pl-8"
                    />
                </div>
                <Select
                    value={nodeFilter === "all" ? "all" : String(nodeFilter)}
                    onValueChange={(v) => setNodeFilter(v === "all" ? "all" : Number(v))}
                >
                    <SelectTrigger className="w-[170px]">
                        <SelectValue placeholder="All nodes" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">All nodes</SelectItem>
                        {nodesWithTunnels.map((n) => (
                            <SelectItem key={n.id} value={String(n.id)}>
                                {n.name}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <div className="flex items-center gap-1 rounded-lg border bg-card p-0.5">
                    {PROTOCOL_TABS.map((t) => (
                        <button
                            key={t.value}
                            type="button"
                            onClick={() => setProtocolFilter(t.value)}
                            className={cn(
                                "rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
                                protocolFilter === t.value
                                    ? "bg-primary/10 text-primary"
                                    : "text-muted-foreground hover:text-foreground",
                            )}
                        >
                            {t.label}
                        </button>
                    ))}
                </div>
                <Select
                    value={`${sortBy}:${sortDir}`}
                    onValueChange={(v) => {
                        const [field, dir] = v.split(":") as [typeof sortBy, typeof sortDir]
                        setSort(field, dir)
                    }}
                >
                    <SelectTrigger className="w-[150px]">
                        <SelectValue placeholder="Sort" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="protocol:asc">Protocol A→Z</SelectItem>
                        <SelectItem value="port:asc">Port ↑</SelectItem>
                        <SelectItem value="port:desc">Port ↓</SelectItem>
                        <SelectItem value="users:desc">Most users</SelectItem>
                        <SelectItem value="status:asc">Live first</SelectItem>
                    </SelectContent>
                </Select>
            </div>

            {/* Grouped grid */}
            {!hasAny ? (
                <CoreList
                    inbounds={[]}
                    isLoading={isLoading}
                    hasAny={false}
                    onOpen={openDetail}
                    onEdit={openEdit}
                    onCreate={() => {}}
                />
            ) : groups.length === 0 ? (
                <div className="rounded-lg border border-dashed py-16 text-center text-sm text-muted-foreground">
                    No tunnels match the current filters.
                </div>
            ) : (
                <div className="space-y-8">
                    {groups.map((group) => (
                        <NodeGroupSection
                            key={group.nodeId}
                            group={group}
                            onOpen={openDetail}
                            onEdit={openEdit}
                        />
                    ))}
                </div>
            )}

            {/* Detail modal — only mounts once a node is resolved. */}
            {detailNode && (
                <CoreDetailModal
                    nodeId={detailNode.id}
                    node={detailNode}
                    inbound={detailInbound}
                    open={detailOpen}
                    onOpenChange={setDetailOpen}
                    onEdit={(inbound) => {
                        setDetailOpen(false)
                        openEdit(inbound)
                    }}
                />
            )}

            {/* Edit dialog — edit-only on the overview (no create). */}
            {editNode && (
                <CreateCoreDialog
                    nodeId={editNode.id}
                    node={editNode}
                    open={editOpen}
                    onOpenChange={setEditOpen}
                    editInbound={editInbound}
                />
            )}
        </div>
    )
}

/** One node's tunnels; flags the protocols whose binary that node is missing. */
function NodeGroupSection({
    group,
    onOpen,
    onEdit,
}: {
    group: NodeGroup
    onOpen: (inbound: CoreInbound) => void
    onEdit: (inbound: CoreInbound) => void
}) {
    const { data: reports = [] } = useNodeDeps(group.nodeId, !!group.node?.is_online)
    const setupRequired = useMemo(
        () => new Set(reports.filter((r) => !r.ready).map((r) => r.core_type)),
        [reports],
    )
    return (
        <section className="space-y-3">
            <NodeGroupHeader group={group} />
            <CoreList
                inbounds={group.inbounds}
                isLoading={false}
                hasAny
                onOpen={onOpen}
                onEdit={onEdit}
                onCreate={() => {}}
                setupRequired={setupRequired}
            />
        </section>
    )
}

function NodeGroupHeader({ group }: { group: NodeGroup }) {
    const { node, nodeId, inbounds } = group
    const name = node?.name ?? `Node #${nodeId}`
    const online = node?.is_online ?? false
    return (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b pb-2">
            <Server className="size-4 text-muted-foreground" />
            <span className="font-semibold">{name}</span>
            {isLocalNode(node) && <LocalNodeBadge />}
            {node ? (
                <span
                    className={cn(
                        "inline-flex items-center gap-1 text-xs",
                        online ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground",
                    )}
                >
                    {online ? (
                        <span className="size-1.5 rounded-full bg-emerald-500" />
                    ) : (
                        <WifiOff className="size-3" />
                    )}
                    {online ? "Online" : "Offline"}
                </span>
            ) : (
                <span className="text-xs text-amber-600 dark:text-amber-400">node not found</span>
            )}
            {node?.country_code && (
                <span className="text-xs text-muted-foreground">{node.country_code}</span>
            )}
            {node?.ip && <span className="font-mono text-xs text-muted-foreground">{nodeAddress(node)}</span>}
            <span className="ml-auto text-xs text-muted-foreground">
                {inbounds.length} tunnel{inbounds.length === 1 ? "" : "s"}
            </span>
        </div>
    )
}

function Stat({ dotClass, label, value }: { dotClass: string; label: string; value: number }) {
    return (
        <div className="flex items-center gap-1.5">
            <span className={cn("size-2 rounded-full", dotClass)} />
            <span className="font-medium text-foreground">{value}</span>
            <span className="text-muted-foreground">{label}</span>
        </div>
    )
}
