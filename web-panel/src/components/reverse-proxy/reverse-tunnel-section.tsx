import { useState } from "react"
import {
    HiOutlinePlus,
    HiOutlineDotsHorizontal,
    HiOutlineCog,
    HiOutlineDocumentText,
    HiOutlineRefresh,
    HiOutlineTrash,
    HiOutlineArrowDown,
    HiOutlineArrowUp,
} from "react-icons/hi"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { CountryFlag } from "@/components/ui/country-flag"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { cn, formatBytes, formatUptime } from "@/lib/utils"
import { useDeleteReverseTunnel, useRotateReverseTunnel } from "@/lib/queries/use-reverse-tunnels"
import type { Inbound, Node, ReverseTunnel } from "@/lib/types"
import { ReverseTunnelDialog, type ReverseTunnelMode } from "./reverse-tunnel-dialog"
import { ReverseTunnelPeerDialog } from "./reverse-tunnel-peer-dialog"

interface ReverseTunnelSectionProps {
    nodeId: number
    nodes: Node[]
    inbounds: Inbound[]
    tunnels: ReverseTunnel[]
    onAddHandBuilt: () => void
}

// Lists the reverse tunnels this node is in, as portal or bridge.
export function ReverseTunnelSection({ nodeId, nodes, inbounds, tunnels, onAddHandBuilt }: ReverseTunnelSectionProps) {
    const confirm = useConfirm()
    const deleteTunnel = useDeleteReverseTunnel()
    const rotateTunnel = useRotateReverseTunnel()
    const [dialog, setDialog] = useState<{ open: boolean; mode: ReverseTunnelMode; tunnel: ReverseTunnel | null }>({
        open: false,
        mode: "nodes",
        tunnel: null,
    })
    const [peerId, setPeerId] = useState<number | null>(null)
    const nodeById = new Map(nodes.map((n) => [n.id, n]))

    const handleDelete = async (t: ReverseTunnel) => {
        const ok = await confirm({
            title: "Delete reverse tunnel",
            description: <>Delete <span className="font-semibold">{t.name}</span>? Both nodes restart Xray, and routing to <span className="font-mono">{t.portal_tag}</span> stops working.</>,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (ok) deleteTunnel.mutate(t.id)
    }

    const handleRotate = async (t: ReverseTunnel) => {
        const external = t.kind === "external_bridge"
        const ok = await confirm({
            title: "Rotate tunnel id",
            description: external
                ? "The tunnel gets a new id. The external bridge stays down until it runs the new config."
                : "The tunnel gets a new id, and both nodes restart Xray.",
            confirmLabel: "Rotate",
        })
        if (ok) rotateTunnel.mutate(t.id)
    }

    return (
        <div>
            <div className="flex items-center justify-between mb-4 gap-3">
                <div>
                    <h3 className="text-lg font-semibold">Reverse tunnels</h3>
                    <p className="text-sm text-muted-foreground">A bridge dials its portal, and the portal sends traffic out through the bridge's internet</p>
                </div>
                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <Button size="sm">
                            <HiOutlinePlus className="w-4 h-4 mr-2" />
                            Add
                        </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => setDialog({ open: true, mode: "nodes", tunnel: null })}>
                            Tunnel to another node
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => setDialog({ open: true, mode: "external", tunnel: null })}>
                            Tunnel to an external server
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onClick={onAddHandBuilt}>Hand-built entry</DropdownMenuItem>
                    </DropdownMenuContent>
                </DropdownMenu>
            </div>

            <div className="rounded-2xl border bg-card/50 backdrop-blur-sm border-white/5 overflow-hidden">
                {tunnels.length === 0 ? (
                    <div className="flex flex-col items-center justify-center py-12 text-muted-foreground gap-1 text-sm">
                        <p>No reverse tunnels.</p>
                        <p className="text-xs">Add one to let this node's users exit through another node, or the reverse.</p>
                    </div>
                ) : (
                    <div className="divide-y divide-border/50">
                        {tunnels.map((t) => {
                            const portal = t.portal_node_id != null ? nodeById.get(t.portal_node_id) : undefined
                            const bridge = t.bridge_node_id != null ? nodeById.get(t.bridge_node_id) : undefined
                            const status = tunnelStatus(t, portal)
                            const isPortal = t.portal_node_id === nodeId
                            return (
                                <div key={t.id} className="group flex flex-col sm:flex-row sm:items-center gap-3 px-4 py-3">
                                    <div className="flex items-start gap-3 min-w-0 flex-1">
                                        <span className={cn("mt-1.5 h-2 w-2 shrink-0 rounded-full", STATUS_DOT[status.tone])} />
                                        <div className="min-w-0 space-y-1">
                                            <div className="flex items-center gap-2 flex-wrap">
                                                <span className="font-medium truncate">{t.name}</span>
                                                <Badge variant="outline" className="text-[11px] py-0">
                                                    {isPortal ? "Portal" : "Bridge"}
                                                </Badge>
                                            </div>
                                            <div className="flex items-center gap-1.5 text-xs text-muted-foreground flex-wrap">
                                                <EndLabel name={portal?.name ?? t.portal_node_name} code={portal?.country_code} external={t.portal_node_id == null} />
                                                {t.portal_inbound_tag && <span className="font-mono">· {t.portal_inbound_tag}</span>}
                                                <span aria-hidden>→</span>
                                                <EndLabel name={bridge?.name ?? t.bridge_node_name} code={bridge?.country_code} external={t.bridge_node_id == null} />
                                            </div>
                                            <p className="text-xs text-muted-foreground">
                                                {status.text}
                                                {isPortal && (
                                                    <> · route to <span className="font-mono text-foreground/80">{t.portal_tag}</span></>
                                                )}
                                            </p>
                                        </div>
                                    </div>
                                    <div className="flex items-center gap-4 sm:justify-end pl-5 sm:pl-0">
                                        <div className="flex items-center gap-3 text-xs font-mono tabular-nums text-muted-foreground" title="Received from the bridge / sent to the bridge">
                                            <span className="inline-flex items-center gap-1"><HiOutlineArrowDown className="w-3 h-3" />{formatBytes(t.from_bridge)}</span>
                                            <span className="inline-flex items-center gap-1"><HiOutlineArrowUp className="w-3 h-3" />{formatBytes(t.to_bridge)}</span>
                                        </div>
                                        <DropdownMenu>
                                            <DropdownMenuTrigger asChild>
                                                <Button variant="ghost" size="icon-sm" aria-label={`Actions for ${t.name}`} className="text-muted-foreground hover:text-foreground">
                                                    <HiOutlineDotsHorizontal className="w-4 h-4" />
                                                </Button>
                                            </DropdownMenuTrigger>
                                            <DropdownMenuContent align="end">
                                                <DropdownMenuItem onClick={() => setDialog({ open: true, mode: t.kind === "nodes" ? "nodes" : "external", tunnel: t })}>
                                                    <HiOutlineCog className="w-4 h-4 mr-2" />
                                                    Edit
                                                </DropdownMenuItem>
                                                {t.kind !== "nodes" && (
                                                    <DropdownMenuItem onClick={() => setPeerId(t.id)}>
                                                        <HiOutlineDocumentText className="w-4 h-4 mr-2" />
                                                        {t.kind === "external_bridge" ? "Bridge config" : "Portal client"}
                                                    </DropdownMenuItem>
                                                )}
                                                {t.kind !== "external_portal" && (
                                                    <DropdownMenuItem onClick={() => handleRotate(t)}>
                                                        <HiOutlineRefresh className="w-4 h-4 mr-2" />
                                                        Rotate id
                                                    </DropdownMenuItem>
                                                )}
                                                <DropdownMenuSeparator />
                                                <DropdownMenuItem onClick={() => handleDelete(t)} className="text-red-500 focus:text-red-500">
                                                    <HiOutlineTrash className="w-4 h-4 mr-2" />
                                                    Delete
                                                </DropdownMenuItem>
                                            </DropdownMenuContent>
                                        </DropdownMenu>
                                    </div>
                                </div>
                            )
                        })}
                    </div>
                )}
            </div>

            <ReverseTunnelDialog
                open={dialog.open}
                onOpenChange={(open) => !open && setDialog((d) => ({ ...d, open: false }))}
                mode={dialog.mode}
                tunnel={dialog.tunnel}
                nodeId={nodeId}
                nodes={nodes}
                inbounds={inbounds}
                onCreated={(t) => t.kind !== "nodes" && setPeerId(t.id)}
            />
            <ReverseTunnelPeerDialog tunnelId={peerId} onClose={() => setPeerId(null)} />
        </div>
    )
}

function EndLabel({ name, code, external }: { name?: string; code?: string; external: boolean }) {
    if (external) return <span className="italic">External</span>
    return (
        <span className="inline-flex items-center gap-1">
            {code && <CountryFlag code={code} className="h-3 w-4 rounded-[2px]" />}
            {name ?? "Unknown node"}
        </span>
    )
}

const STATUS_DOT = {
    good: "bg-emerald-500",
    bad: "bg-red-500",
    muted: "bg-muted-foreground/40",
} as const

// tunnelStatus says what the portal last saw of the bridge.
function tunnelStatus(t: ReverseTunnel, portal?: Node): { tone: keyof typeof STATUS_DOT; text: string } {
    if (t.kind === "external_portal") return { tone: "muted", text: "Status isn't visible: the portal runs outside this panel" }
    if (portal && !portal.is_online) return { tone: "muted", text: "Unknown: the portal node is offline" }
    const lasted = t.state_since
        ? ` for ${formatUptime(Math.max(0, Math.floor((Date.now() - new Date(t.state_since).getTime()) / 1000)))}`
        : ""
    if (t.state === "connected") {
        return { tone: "good", text: `Connected${lasted}${t.bridge_ip ? ` from ${t.bridge_ip}` : ""}` }
    }
    if (t.state === "down") return { tone: "bad", text: `Down${lasted}` }
    return { tone: "muted", text: "Waiting for the bridge to connect" }
}
