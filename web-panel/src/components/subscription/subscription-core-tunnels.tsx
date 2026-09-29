import { useMemo, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, X } from "lucide-react"
import { toast } from "@/lib/toast"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useAllCoreInbounds, useNodes } from "@/lib/queries"
import { coreProtocolMeta, type CoreProtocol } from "@/lib/types/cores"
import { CoreProtocolIcon } from "@/components/cores/core-protocol-badge"
import {
    listSubscriptionCoreAccounts,
    assignSubscriptionToCoreInbound,
    unassignSubscriptionFromCoreInbound,
} from "@/lib/api/subscriptions"
import { SectionHeader } from "./sections/section-header"

/**
 * SubscriptionCoreTunnels — assign/unassign SSH and MTProto tunnels to a
 * subscription. Assigning provisions a tunnel user keyed by the subscription's
 * email and reconciles the node, so the tunnel accepts it.
 */
export function SubscriptionCoreTunnels({ subscriptionId }: { subscriptionId: number }) {
    const qc = useQueryClient()
    const confirm = useConfirm()
    const [busy, setBusy] = useState<number | null>(null)

    const queryKey = ["core-accounts", "subscription", subscriptionId]
    const { data: accounts = [], isLoading } = useQuery({
        queryKey,
        queryFn: async () => {
            const res = await listSubscriptionCoreAccounts(subscriptionId)
            if (!res.success) throw new Error(res.error || "Failed")
            return res.data?.accounts ?? []
        },
    })
    const { data: allCore = [] } = useAllCoreInbounds()
    const { data: nodes = [] } = useNodes()
    const nodeName = useMemo(() => new Map(nodes.map((n) => [n.id, n.name])), [nodes])

    const assignedIds = useMemo(() => new Set(accounts.map((a) => a.core_inbound_id)), [accounts])
    const available = useMemo(
        () => allCore.filter((ci) => !assignedIds.has(ci.id) && !ci.is_disabled),
        [allCore, assignedIds],
    )

    const refetch = () => qc.invalidateQueries({ queryKey })

    const assign = async (coreInboundId: number) => {
        setBusy(coreInboundId)
        try {
            const res = await assignSubscriptionToCoreInbound(subscriptionId, coreInboundId)
            if (res.success) {
                toast.success("Tunnel assigned")
                refetch()
            } else {
                toast.error(res.error || "Failed to assign tunnel")
            }
        } finally {
            setBusy(null)
        }
    }

    const unassign = async (coreInboundId: number, label: string) => {
        const ok = await confirm({
            title: "Remove tunnel?",
            description: `This removes the ${label} tunnel from this subscription (deletes the user on the node).`,
            confirmLabel: "Remove",
            variant: "destructive",
        })
        if (!ok) return
        setBusy(coreInboundId)
        try {
            const res = await unassignSubscriptionFromCoreInbound(subscriptionId, coreInboundId)
            if (res.success) {
                toast.success("Tunnel removed")
                refetch()
            } else {
                toast.error(res.error || "Failed to remove tunnel")
            }
        } finally {
            setBusy(null)
        }
    }

    return (
        <div className="space-y-2">
            <SectionHeader
                tone="default"
                right={
                    available.length > 0 ? (
                        <Select
                            value=""
                            onValueChange={(v) => assign(Number(v))}
                            disabled={busy !== null}
                        >
                            <SelectTrigger className="h-6 w-[150px] text-xs">
                                <SelectValue placeholder="+ Assign tunnel" />
                            </SelectTrigger>
                            <SelectContent>
                                {available.map((ci) => {
                                    const meta = safeMeta(ci.protocol)
                                    return (
                                        <SelectItem key={ci.id} value={String(ci.id)}>
                                            {(meta?.label ?? ci.protocol)} · :{ci.port} ·{" "}
                                            {nodeName.get(ci.node_id) ?? `Node #${ci.node_id}`}
                                        </SelectItem>
                                    )
                                })}
                            </SelectContent>
                        </Select>
                    ) : undefined
                }
            >
                Tunnels {!isLoading && `(${accounts.length})`}
            </SectionHeader>

            {isLoading ? (
                <div className="h-8 animate-pulse rounded-md bg-muted/20" />
            ) : accounts.length === 0 ? (
                <p className="py-1 text-sm text-muted-foreground">No tunnels assigned</p>
            ) : (
                <div className="space-y-1">
                    {accounts.map((a) => {
                        const meta = safeMeta(a.protocol)
                        const label = `${meta?.label ?? a.protocol} :${a.port}`
                        return (
                            <div
                                key={a.core_user_id}
                                className="group flex items-center justify-between rounded-md px-2 py-1.5 hover:bg-muted/50"
                            >
                                <div className="flex min-w-0 items-center gap-2">
                                    <CoreProtocolIcon protocol={a.protocol as CoreProtocol} />
                                    <span className="truncate text-sm font-medium">{meta?.label ?? a.protocol}</span>
                                    <Badge variant="outline" className="h-4 shrink-0 px-1 text-[11px]">
                                        :{a.port}
                                    </Badge>
                                    <span className="shrink-0 text-[12px] text-muted-foreground">
                                        {nodeName.get(a.node_id) ?? `Node #${a.node_id}`}
                                    </span>
                                    {!a.enabled && (
                                        <Badge variant="outline" className="h-4 shrink-0 px-1 text-[11px] text-amber-500">
                                            disabled
                                        </Badge>
                                    )}
                                </div>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-6 w-6 shrink-0 p-0 text-muted-foreground hover:text-destructive"
                                    disabled={busy === a.core_inbound_id}
                                    onClick={() => unassign(a.core_inbound_id, label)}
                                    aria-label={`Remove ${label}`}
                                >
                                    {busy === a.core_inbound_id ? (
                                        <Loader2 className="h-3 w-3 animate-spin" />
                                    ) : (
                                        <X className="h-3 w-3" />
                                    )}
                                </Button>
                            </div>
                        )
                    })}
                </div>
            )}
        </div>
    )
}

function safeMeta(protocol: string) {
    try {
        return coreProtocolMeta(protocol as CoreProtocol)
    } catch {
        return undefined
    }
}
