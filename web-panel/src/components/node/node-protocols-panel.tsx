import { Switch } from "@/components/ui/switch"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useNodeProtocols, useToggleNodeProtocol } from "@/lib/queries"
import { coreProtocolMeta, type NodeProtocolState } from "@/lib/types/cores"
import { CoreProtocolIcon } from "@/components/cores/core-protocol-badge"
import { cn } from "@/lib/utils"

/** Number of protocols currently turned off, for the trigger badge. */
export function countDisabledProtocols(protocols: NodeProtocolState[]): number {
    return protocols.filter((p) => !p.enabled).length
}

/**
 * Per-node protocol on/off list. Turning a protocol
 * off tears down its listeners and blocks new tunnels of that type on the node
 * (dependencies stay installed); re-enabling restores it. Enforcement is
 * server-side — this is the admin control surface.
 *
 * Rendered as the body of NodeProtocolsDialog; it owns no card chrome or heading.
 */
export function NodeProtocolsPanel({ nodeId }: { nodeId: number }) {
    const { data: protocols = [], isLoading } = useNodeProtocols(nodeId)
    const toggle = useToggleNodeProtocol(nodeId)
    const confirm = useConfirm()

    const onToggle = async (p: NodeProtocolState) => {
        // Disabling a protocol that still has live tunnels tears their listeners
        // down — confirm first. Enabling never needs a prompt.
        if (p.enabled && p.inbound_count > 0) {
            const plural = p.inbound_count === 1 ? "" : "s"
            const ok = await confirm({
                title: `Disable ${coreProtocolMeta(p.protocol).label} on this node?`,
                description: `${p.inbound_count} tunnel${plural} on this node will stop accepting connections and subscriptions using ${p.inbound_count === 1 ? "it" : "them"} lose access until you re-enable. Nothing is deleted.`,
                variant: "warning",
                confirmLabel: "Disable protocol",
            })
            if (!ok) return
        }
        toggle.mutate(p.protocol)
    }

    if (isLoading) {
        return <div className="py-2 text-sm text-muted-foreground">Loading protocols…</div>
    }

    return (
        <div className="divide-y rounded-lg border">
            {protocols.map((p) => (
                <ProtocolRow key={p.protocol} state={p} pending={toggle.isPending} onToggle={onToggle} />
            ))}
        </div>
    )
}

function ProtocolRow({
    state: p,
    pending,
    onToggle,
}: {
    state: NodeProtocolState
    pending: boolean
    onToggle: (p: NodeProtocolState) => void
}) {
    const meta = coreProtocolMeta(p.protocol)
    return (
        <div
            className={cn(
                "flex items-center gap-3 px-3 py-2.5",
                !p.enabled && "bg-muted/30",
            )}
        >
            <CoreProtocolIcon
                protocol={p.protocol}
                size="sm"
                className={cn(!p.enabled && "opacity-50 grayscale")}
            />
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{meta.label}</span>
                    {p.inbound_count > 0 && (
                        <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">
                            {p.inbound_count} tunnel{p.inbound_count === 1 ? "" : "s"}
                        </span>
                    )}
                </div>
                <p className="truncate text-xs text-muted-foreground">{meta.oneLiner}</p>
            </div>
            <span
                className={cn(
                    "text-xs font-medium",
                    p.enabled
                        ? "text-emerald-600 dark:text-emerald-400"
                        : "text-muted-foreground",
                )}
            >
                {p.enabled ? "Enabled" : "Disabled"}
            </span>
            <Switch
                checked={p.enabled}
                disabled={pending}
                onCheckedChange={() => onToggle(p)}
                aria-label={`Toggle ${meta.label}`}
            />
        </div>
    )
}
