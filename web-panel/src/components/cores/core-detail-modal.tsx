import { Settings2, Activity, Users as UsersIcon, Trash2 } from "lucide-react"
import { Dialog, DialogContent } from "@/components/ui/dialog"
import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useCoreInboundStatus, useDeleteCoreInbound } from "@/lib/queries"
import { coreProtocolMeta, type CoreInbound } from "@/lib/types/cores"
import type { Node } from "@/lib/types"
import { CoreProtocolIcon } from "./core-protocol-badge"
import { CoreStatusChip, deriveCoreStatus } from "./core-status-chip"
import { CoreUsersPanel } from "./core-users-panel"

export interface CoreDetailModalProps {
    nodeId: number
    node: Node
    inbound: CoreInbound | null
    open: boolean
    onOpenChange: (open: boolean) => void
    onEdit: (inbound: CoreInbound) => void
}

function DetailHeader({
    inbound,
    onEdit,
    onDelete,
}: {
    inbound: CoreInbound
    onEdit: (inbound: CoreInbound) => void
    onDelete: () => void
}) {
    const meta = coreProtocolMeta(inbound.protocol)
    return (
        // pr-10 reserves space for the dialog's absolute top-right close (X) so
        // the action buttons don't collide with it.
        <div className="flex items-start gap-3 pr-10">
            <CoreProtocolIcon protocol={inbound.protocol} size="lg" />
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                    <h2 className="truncate text-lg font-semibold">{meta.label}</h2>
                    <span className="font-mono text-sm text-muted-foreground">:{inbound.port}</span>
                </div>
                {inbound.remark && (
                    <p className="truncate text-sm text-muted-foreground">{inbound.remark}</p>
                )}
                <div className="mt-2 flex flex-wrap items-center gap-2">
                    <CoreStatusChip status={deriveCoreStatus(inbound)} />
                </div>
            </div>
            <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={() => onEdit(inbound)}>
                    <Settings2 className="size-4" />
                    Edit
                </Button>
                <Button
                    variant="outline"
                    size="sm"
                    className="text-muted-foreground hover:border-red-500/40 hover:text-red-500"
                    onClick={onDelete}
                    aria-label="Delete tunnel"
                >
                    <Trash2 className="size-4" />
                </Button>
            </div>
        </div>
    )
}

function LiveStatusRow({ inbound, online }: { inbound: CoreInbound; online: boolean }) {
    const { data, isError } = useCoreInboundStatus(inbound.id, online && !inbound.is_disabled)
    if (!online || isError || !data) return null
    return (
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1 rounded-lg border bg-card px-3 py-2 text-xs">
            <span className="flex items-center gap-1.5">
                <Activity className={data.running ? "size-3.5 text-emerald-500" : "size-3.5 text-muted-foreground"} />
                <span className="font-medium">{data.running ? "Running" : "Stopped"}</span>
                {data.version && <span className="text-muted-foreground">· {data.version}</span>}
            </span>
            <span className="flex items-center gap-1.5 text-muted-foreground">
                <UsersIcon className="size-3.5" />
                <span className="font-medium text-foreground">{data.live_sessions}</span> live ·{" "}
                <span className="font-medium text-foreground">{data.distinct_devices}</span> devices
            </span>
            {data.detail && <span className="basis-full text-muted-foreground">{data.detail}</span>}
        </div>
    )
}

function DetailBody({
    inbound,
    node,
    nodeId,
}: {
    inbound: CoreInbound
    node: Node
    nodeId: number
}) {
    return (
        <div className="space-y-4">
            <LiveStatusRow inbound={inbound} online={node.is_online} />
            {/* No host: the hub puts each node's public address in the links. */}
            <CoreUsersPanel
                inboundId={inbound.id}
                nodeId={nodeId}
                protocol={inbound.protocol}
                hostOnline={node.is_online}
            />
        </div>
    )
}

export function CoreDetailModal({
    nodeId,
    node,
    inbound,
    open,
    onOpenChange,
    onEdit,
}: CoreDetailModalProps) {
    const isMobile = useIsMobile()
    const confirm = useConfirm()
    const del = useDeleteCoreInbound(nodeId)
    if (!inbound) return null
    const meta = coreProtocolMeta(inbound.protocol)

    const handleDelete = async () => {
        const ok = await confirm({
            title: "Delete tunnel?",
            description: `This removes the ${meta.label} tunnel on port ${inbound.port} and tears it down on the node. This cannot be undone.`,
            confirmLabel: "Delete tunnel",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await del.mutateAsync(inbound.id)
            onOpenChange(false)
        } catch {
            /* error toast handled by the mutation */
        }
    }

    if (isMobile) {
        return (
            <Sheet open={open} onOpenChange={onOpenChange}>
                <SheetContent side="bottom" className="flex h-[92dvh] flex-col rounded-t-xl p-0">
                    <div className="border-b p-4">
                        <SheetTitle className="sr-only">
                            {meta.label} tunnel on port {inbound.port}
                        </SheetTitle>
                        <SheetDescription className="sr-only">
                            Manage users and client config for this tunnel.
                        </SheetDescription>
                        <DetailHeader inbound={inbound} onEdit={onEdit} onDelete={handleDelete} />
                    </div>
                    <div className="flex-1 overflow-y-auto p-4">
                        <DetailBody inbound={inbound} node={node} nodeId={nodeId} />
                    </div>
                </SheetContent>
            </Sheet>
        )
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="flex max-h-[92vh] max-w-2xl flex-col gap-0 p-0">
                <div className="border-b p-5">
                    <DetailHeader inbound={inbound} onEdit={onEdit} onDelete={handleDelete} />
                </div>
                <div className="flex-1 overflow-y-auto p-5">
                    <DetailBody inbound={inbound} node={node} nodeId={nodeId} />
                </div>
            </DialogContent>
        </Dialog>
    )
}
