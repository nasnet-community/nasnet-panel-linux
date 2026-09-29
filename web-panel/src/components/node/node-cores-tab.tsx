import { useMemo, useState } from "react"
import { Plus, Search, Cable, WifiOff, ToggleRight } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import { useCoreInbounds, useNodeDeps, useNodeProtocols } from "@/lib/queries"
import { useCoresStore, type CoreProtocolFilter } from "@/store/cores-store"
import { type CoreInbound } from "@/lib/types/cores"
import { filterAndSortInbounds } from "@/components/cores/filter-sort"
import { CoreList } from "@/components/cores/core-list"
import { CoreDetailModal } from "@/components/cores/core-detail-modal"
import { CreateCoreDialog } from "@/components/cores/create-core-dialog"
import { NodeProtocolsDialog } from "@/components/cores/node-protocols-dialog"
import { countDisabledProtocols } from "@/components/node/node-protocols-panel"
import { TunnelDependencies } from "@/components/node/node-setup-tab"

const PROTOCOL_TABS: { value: CoreProtocolFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "ssh", label: "SSH" },
    { value: "mtproto", label: "MTProto" },
]

export function NodeCoresTab({ nodeId, node }: { nodeId: number; node: Node }) {
    const { data: inbounds = [], isLoading } = useCoreInbounds(nodeId)
    // Shares the cache entry with the panel inside the dialog — this only powers
    // the trigger badge so a disabled protocol stays visible without opening it.
    const { data: protocols = [] } = useNodeProtocols(nodeId)
    const { search, setSearch, protocolFilter, setProtocolFilter, sortBy, sortDir, setSort } =
        useCoresStore()

    const [detailInbound, setDetailInbound] = useState<CoreInbound | null>(null)
    const [detailOpen, setDetailOpen] = useState(false)
    const [createOpen, setCreateOpen] = useState(false)
    const [editInbound, setEditInbound] = useState<CoreInbound | null>(null)
    const [protocolsOpen, setProtocolsOpen] = useState(false)

    const offCount = countDisabledProtocols(protocols)

    // Same cache entry as the Dependencies section below.
    const { data: depReports = [] } = useNodeDeps(nodeId, node.is_online)
    const setupRequired = useMemo(
        () => new Set(depReports.filter((r) => !r.ready).map((r) => r.core_type)),
        [depReports],
    )

    const stats = useMemo(() => {
        const live = inbounds.filter((i) => !i.is_disabled).length
        const disabled = inbounds.filter((i) => i.is_disabled).length
        const users = inbounds.reduce((n, i) => n + (i.users?.length ?? 0), 0)
        return { live, disabled, users }
    }, [inbounds])

    const filtered = useMemo(
        () => filterAndSortInbounds(inbounds, { protocolFilter, search, sortBy, sortDir }),
        [inbounds, protocolFilter, search, sortBy, sortDir],
    )

    const openDetail = (inbound: CoreInbound) => {
        setDetailInbound(inbound)
        setDetailOpen(true)
    }
    const openEdit = (inbound: CoreInbound) => {
        setEditInbound(inbound)
        setCreateOpen(true)
    }
    const openCreate = () => {
        setEditInbound(null)
        setCreateOpen(true)
    }

    return (
        <div className="space-y-5">
            {/* Header */}
            <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                        <h2 className="text-lg font-semibold">Tunnels</h2>
                        {inbounds.length > 0 && (
                            <div className="flex items-center gap-3 text-xs tabular-nums">
                                <Stat dotClass="bg-emerald-500" label="live" value={stats.live} />
                                {stats.disabled > 0 && (
                                    <Stat
                                        dotClass="bg-muted-foreground"
                                        label="disabled"
                                        value={stats.disabled}
                                    />
                                )}
                                <div className="flex items-center gap-1 text-muted-foreground">
                                    <Cable className="size-3.5" />
                                    <span className="font-medium text-foreground">{stats.users}</span>
                                    {stats.users === 1 ? "user" : "users"}
                                </div>
                            </div>
                        )}
                    </div>
                    <p className="text-sm text-muted-foreground">
                        SSH and MTProto servers running alongside Xray.
                    </p>
                </div>
                <div className="flex items-center gap-2">
                    <Button variant="outline" onClick={() => setProtocolsOpen(true)}>
                        <ToggleRight className="size-4" />
                        Protocols
                        {offCount > 0 && (
                            <Badge variant="warning" className="ml-1 px-1.5 py-0 text-[11px]">
                                {offCount}
                            </Badge>
                        )}
                    </Button>
                    <Button onClick={openCreate}>
                        <Plus className="size-4" />
                        New tunnel
                    </Button>
                </div>
            </div>

            {/* Offline banner */}
            {!node.is_online && (
                <div className="flex items-center gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-4 py-2.5 text-sm text-amber-600 dark:text-amber-400">
                    <WifiOff className="size-4 shrink-0" />
                    Node is offline — tunnels still list, but client configs can't be fetched until it reconnects.
                </div>
            )}

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

            {/* Grid */}
            <CoreList
                inbounds={filtered}
                isLoading={isLoading}
                hasAny={inbounds.length > 0}
                onOpen={openDetail}
                onEdit={openEdit}
                onCreate={openCreate}
                setupRequired={setupRequired}
            />

            {/* What the protocols need installed on this node */}
            <TunnelDependencies nodeId={nodeId} node={node} />

            {/* Per-node protocol enable/disable */}
            <NodeProtocolsDialog
                nodeId={nodeId}
                open={protocolsOpen}
                onOpenChange={setProtocolsOpen}
            />

            {/* Detail modal */}
            <CoreDetailModal
                nodeId={nodeId}
                node={node}
                inbound={detailInbound}
                open={detailOpen}
                onOpenChange={setDetailOpen}
                onEdit={(inbound) => {
                    setDetailOpen(false)
                    openEdit(inbound)
                }}
            />

            {/* Create / edit dialog */}
            <CreateCoreDialog
                nodeId={nodeId}
                node={node}
                open={createOpen}
                onOpenChange={setCreateOpen}
                editInbound={editInbound}
            />
        </div>
    )
}

function Stat({
    dotClass,
    label,
    value,
}: {
    dotClass: string
    label: string
    value: number
}) {
    return (
        <div className="flex items-center gap-1">
            <span className={cn("size-1.5 rounded-full", dotClass)} />
            <span className="font-medium text-foreground">{value}</span>
            <span className="text-muted-foreground">{label}</span>
        </div>
    )
}
