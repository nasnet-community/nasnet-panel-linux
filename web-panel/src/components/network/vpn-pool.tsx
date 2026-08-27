import { useState } from "react"
import { ChevronRight, Plus, ShieldCheck, ShieldOff } from "lucide-react"
import { toast } from "sonner"
import {
    DndContext,
    closestCenter,
    KeyboardSensor,
    PointerSensor,
    useSensor,
    useSensors,
    type DragEndEvent,
} from "@dnd-kit/core"
import { SortableContext, verticalListSortingStrategy, arrayMove } from "@dnd-kit/sortable"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { VpnAddDialog } from "@/components/network/vpn-add-dialog"
import { PoolStrategyControl } from "@/components/network/vpn-pool-strategy"
import { PoolRow, SortablePoolRow, type PoolRowProps } from "@/components/network/vpn-pool-row"
import { useDeleteVPNProfile, useSetPoolOrder, useSetVPNTransport } from "@/lib/queries/use-network"
import { cn } from "@/lib/utils"
import type {
    PoolStrategy,
    TunnelStatus,
    VPNProfile,
    VPNUplink,
} from "@/lib/types/network"
import type { TunnelHealth } from "@/lib/types/health"

interface Props {
    profiles: VPNProfile[] | undefined
    loading: boolean
    tunnels: TunnelStatus[]
    health: TunnelHealth[]
    poolLossPct: number | undefined
    poolRTTms: number | undefined
    /** The secondary uplinks the pool can ride. */
    uplinks: VPNUplink[]
    strategy: PoolStrategy
    /** The tunnel carrying alone, when the strategy runs one at a time. */
    carrier?: string
    /** A change is already waiting to be kept, so a second one must wait. */
    armed: boolean
    busy: boolean
    onEnable: (id: number) => void
    onDisable: (id: number) => void
}

/** Answering, or not yet judged: either can take traffic. */
function usable(h: TunnelHealth | undefined): boolean {
    return h === undefined || h.verdict === "up" || h.verdict === "degraded" || h.verdict === ""
}

export function VpnPool({
    profiles,
    loading,
    tunnels,
    health,
    poolLossPct,
    poolRTTms,
    uplinks,
    strategy,
    carrier,
    armed,
    busy,
    onEnable,
    onDisable,
}: Props) {
    const del = useDeleteVPNProfile()
    const order = useSetPoolOrder()
    const transport = useSetVPNTransport()
    const confirm = useConfirm()
    const [adding, setAdding] = useState(false)
    const [editing, setEditing] = useState<VPNProfile | null>(null)
    const [openRow, setOpenRow] = useState<number | null>(null)
    // The order as dragged, held until the server's copy agrees.
    const [dragged, setDragged] = useState<number[] | null>(null)
    const sensors = useSensors(
        useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
        useSensor(KeyboardSensor),
    )

    const rows = profiles ?? []
    const statusById = new Map(tunnels.map((t) => [t.profile_id, t]))
    const healthById = new Map(health.map((h) => [h.profile_id, h]))
    const on = rows.filter((p) => p.enabled).sort((a, b) => a.priority - b.priority || a.id - b.id)
    const serverIds = on.map((p) => p.id)
    // Dropped once the server agrees, or the pool changes under us. Resetting
    // in render, not an effect: an effect paints the stale order for a frame.
    const sameMembers =
        dragged?.length === serverIds.length && !!dragged?.every((id) => serverIds.includes(id))
    if (dragged && (!sameMembers || dragged.join() === serverIds.join())) setDragged(null)
    const idOrder = dragged ?? serverIds
    const ordered = idOrder
        .map((id) => on.find((p) => p.id === id))
        .filter((p): p is VPNProfile => !!p)
    const off = rows.filter((p) => !p.enabled)

    const chain = strategy === "order" && on.length > 1
    // The chain's next healthy standby: the one that would take over.
    const nextUpId = chain
        ? ordered.find(
              (p) => statusById.get(p.id)?.if_name !== carrier && usable(healthById.get(p.id)),
          )?.id
        : undefined

    const carrying = ordered.filter(
        (p) => statusById.get(p.id)?.in_pool && usable(healthById.get(p.id)),
    )

    if (loading) return <Skeleton className="h-64 w-full" />

    function carryingNow(p: VPNProfile): boolean {
        return p.enabled && (statusById.get(p.id)?.in_pool ?? false)
    }

    async function turnOn(p: VPNProfile) {
        const ok = await confirm({
            title: `Add ${p.name} to the pool`,
            description:
                strategy === "spread"
                    ? "Traffic starts riding this VPN as soon as it joins. " +
                      "You get 90 seconds to keep the change before it reverts itself."
                    : "It joins at the end of the list and waits its turn. " +
                      "You get 90 seconds to keep the change before it reverts itself.",
            confirmLabel: "Turn it on",
            icon: <ShieldCheck className="h-5 w-5" />,
        })
        if (ok) onEnable(p.id)
    }

    async function turnOff(p: VPNProfile) {
        const last = on.length === 1
        const ok = await confirm({
            title: `Remove ${p.name} from the pool`,
            description: last
                ? "This is the last VPN. Traffic bound for the secondary uplink will be " +
                  "dropped rather than sent in the open. Nothing falls back to the other uplink. " +
                  "You get 90 seconds to keep the change before it reverts itself."
                : carryingNow(p)
                  ? "It is the one carrying traffic right now, so the others take it over. " +
                    "You get 90 seconds to keep the change before it reverts itself."
                  : "Its flows move to the remaining VPNs. " +
                    "You get 90 seconds to keep the change before it reverts itself.",
            confirmLabel: "Turn it off",
            variant: "warning",
            icon: <ShieldOff className="h-5 w-5" />,
        })
        if (ok) onDisable(p.id)
    }

    async function remove(p: VPNProfile) {
        const ok = await confirm({
            title: `Delete ${p.name}`,
            description: "The stored config and its keys are removed. Nothing else changes.",
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await del.mutateAsync(p.id)
            toast.success("VPN deleted")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to delete the VPN")
        }
    }

    /** Every drop commits: one drag is the whole edit. */
    function commitOrder(ids: number[], movedId: number) {
        const before = idOrder
        const name = on.find((p) => p.id === movedId)?.name ?? "The VPN"
        setDragged(ids)
        order.mutate(ids, {
            onSuccess: () => toast.success(`${name} moved to position ${ids.indexOf(movedId) + 1}`),
            onError: (e) => {
                setDragged(before)
                toast.error(e instanceof Error ? e.message : "Failed to save the order")
            },
        })
    }

    function onDragEnd(e: DragEndEvent) {
        const { active, over } = e
        if (!over || active.id === over.id) return
        const from = idOrder.indexOf(Number(active.id))
        const to = idOrder.indexOf(Number(over.id))
        if (from === -1 || to === -1) return
        commitOrder(arrayMove(idOrder, from, to), Number(active.id))
    }

    function move(p: VPNProfile, direction: "up" | "down") {
        const from = idOrder.indexOf(p.id)
        const to = direction === "up" ? from - 1 : from + 1
        if (from === -1 || to < 0 || to >= idOrder.length) return
        commitOrder(arrayMove(idOrder, from, to), p.id)
    }

    function commitTransport(p: VPNProfile, uplinkKey: string) {
        const label = uplinks.find((u) => u.key === uplinkKey)?.label
        transport.mutate(
            { id: p.id, uplinkKey },
            {
                onSuccess: () =>
                    toast.success(
                        label ? `${p.name} pinned to ${label}` : `${p.name} set to automatic`,
                    ),
                onError: (e) =>
                    toast.error(e instanceof Error ? e.message : "Failed to change the uplink"),
            },
        )
    }

    function rowProps(p: VPNProfile, index: number): PoolRowProps {
        return {
            profile: p,
            status: statusById.get(p.id),
            health: healthById.get(p.id),
            uplinks,
            chain,
            position: index,
            first: index === 0,
            last: index === ordered.length - 1,
            nextUp: p.id === nextUpId,
            open: openRow === p.id,
            armed,
            busy,
            pinBusy: transport.isPending,
            orderBusy: order.isPending,
            onOpenChange: (o) => setOpenRow(o ? p.id : null),
            onMove: (d) => move(p, d),
            onToggle: (o) => void (o ? turnOn(p) : turnOff(p)),
            onPin: (key) => commitTransport(p, key),
            onEdit: () => setEditing(p),
            onDelete: () => void remove(p),
        }
    }

    // One sentence for the pool, in the words of the chosen strategy.
    function verdict(): { text: string; tone: string } {
        if (rows.length === 0) return { text: "No VPN yet", tone: "bg-text-disabled" }
        if (on.length === 0) return { text: "No VPN is turned on", tone: "bg-text-disabled" }
        if (carrying.length === 0)
            return { text: "No VPN is carrying traffic", tone: "bg-status-danger" }
        if (strategy === "spread")
            return {
                text: `${carrying.length} of ${on.length} sharing the traffic`,
                tone: carrying.length === on.length ? "bg-status-success" : "bg-status-warning",
            }
        const lead = carrying[0]
        const rtt = healthById.get(lead.id)?.median_rtt_ms
        if (strategy === "fastest" && rtt) {
            return { text: `${lead.name} is carrying, fastest at ${rtt} ms`, tone: "bg-status-success" }
        }
        const standby = on.length - 1
        return {
            text: standby > 0
                ? `${lead.name} is carrying, ${standby} on standby`
                : `${lead.name} is carrying`,
            tone: "bg-status-success",
        }
    }

    const v = verdict()
    const stats =
        poolLossPct !== undefined && poolRTTms !== undefined && on.length > 0
            ? `${poolLossPct}% loss · ${poolRTTms} ms`
            : null

    return (
        <Card className="overflow-hidden pb-0">
            <CardHeader className="pb-4">
                <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
                    <h2 className="flex items-center gap-2.5 text-lg font-medium">
                        <span aria-hidden className={cn("h-2 w-2 shrink-0 rounded-full", v.tone)} />
                        {v.text}
                    </h2>
                    <div className="flex items-center gap-4">
                        {stats && (
                            <span className="text-text-secondary text-sm tabular-nums">{stats}</span>
                        )}
                        <Button size="sm" onClick={() => setAdding(true)}>
                            <Plus className="mr-1.5 h-3.5 w-3.5" />
                            Add a VPN
                        </Button>
                    </div>
                </div>

                {/* One VPN is not a choice between VPNs. */}
                {on.length > 1 && (
                    <div className="pt-3">
                        <PoolStrategyControl strategy={strategy} disabled={armed || busy} />
                    </div>
                )}
            </CardHeader>

            <CardContent className="px-0 pb-0">
                {rows.length === 0 ? (
                    <div className="px-6 pb-10 text-center">
                        <p className="text-text-primary font-medium">
                            Add your first VPN
                        </p>
                        <p className="text-text-tertiary mx-auto mt-1 max-w-md text-sm">
                            Turn one on and foreign traffic has a way out. Until then it has none.
                        </p>
                        <Button className="mt-4" onClick={() => setAdding(true)}>
                            <Plus className="mr-1.5 h-3.5 w-3.5" />
                            Add a VPN
                        </Button>
                    </div>
                ) : (
                    <div className="border-border/60 border-t">
                        {chain ? (
                            <DndContext
                                sensors={sensors}
                                collisionDetection={closestCenter}
                                onDragEnd={onDragEnd}
                            >
                                <SortableContext items={idOrder} strategy={verticalListSortingStrategy}>
                                    {ordered.map((p, i) => (
                                        <SortablePoolRow key={p.id} {...rowProps(p, i)} />
                                    ))}
                                </SortableContext>
                            </DndContext>
                        ) : (
                            ordered.map((p, i) => <PoolRow key={p.id} {...rowProps(p, i)} />)
                        )}
                    </div>
                )}

                {off.length > 0 && (
                    <details
                        className="border-border/60 border-t"
                        // An empty pool should still show what to turn on.
                        open={on.length === 0}
                    >
                        <summary className="text-text-tertiary hover:text-text-secondary flex cursor-pointer list-none items-center gap-2 px-4 py-3 text-sm [&::-webkit-details-marker]:hidden">
                            <ChevronRight
                                aria-hidden
                                className="h-3.5 w-3.5 transition-transform [details[open]_&]:rotate-90"
                            />
                            Not in use ({off.length})
                        </summary>
                        <div className="border-border/60 border-t">
                            {off.map((p) => (
                                <PoolRow key={p.id} {...rowProps(p, -1)} />
                            ))}
                        </div>
                    </details>
                )}
            </CardContent>

            <VpnAddDialog open={adding} onOpenChange={setAdding} />
            {/* Keyed: the dialog seeds its fields at mount, so it needs a remount. */}
            <VpnAddDialog
                key={editing?.id ?? "none"}
                open={editing !== null}
                onOpenChange={(o) => !o && setEditing(null)}
                profile={editing}
            />
        </Card>
    )
}
