import { Link } from "react-router"
import { Activity, Copy, Download, MoreVertical, ShieldOff, Terminal, Trash2, Wrench } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { Node } from "@/lib/types"
import { nodeAddress } from "@/lib/node-kind"
import { cn, copyToClipboard } from "@/lib/utils"
import { toast } from "@/lib/toast"
import { useNodesStore } from "@/store/nodes-store"
import type { NodeView } from "./facts"

export interface NodeActions {
    checkConnection: (node: Node) => void
    deploy: (node: Node) => void
    updateAgent: (node: Node) => void
    startMaintenance: (node: Node) => void
    endMaintenance: (node: Node) => void
    wipe: (node: Node) => void
    remove: (node: Node) => void
}

/** The ⋯ menu on a card or row; always visible, so touch can reach it. */
export function NodeMenu({ view, actions, className }: { view: NodeView; actions: NodeActions; className?: string }) {
    const { node, status, local, lite } = view
    // Only a server we install the agent on can be deployed to, wiped or deleted from here.
    const remote = !local && !lite
    const checking = useNodesStore((s) => s.checkingHealthId === node.id)
    const copyIP = async () => {
        if (await copyToClipboard(node.ip)) toast.success("IP copied", { description: node.ip })
        else toast.error("Couldn't copy the IP")
    }
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button
                    variant="ghost"
                    size="icon"
                    className={cn("size-8 shrink-0 text-muted-foreground hover:text-foreground", className)}
                    aria-label={`Actions for ${node.name}`}
                >
                    <MoreVertical className="size-4" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52">
                <DropdownMenuItem className="gap-2" disabled={checking} onSelect={() => actions.checkConnection(node)}>
                    <Activity className="size-4" /> {checking ? "Checking…" : "Check connection"}
                </DropdownMenuItem>
                {nodeAddress(node) === node.ip && (
                    <DropdownMenuItem className="gap-2" onSelect={copyIP}>
                        <Copy className="size-4" /> Copy IP
                    </DropdownMenuItem>
                )}
                {remote && (status === "down" || status === "pending") && (
                    <DropdownMenuItem className="gap-2" onSelect={() => actions.deploy(node)}>
                        <Terminal className="size-4" /> Deploy agent
                    </DropdownMenuItem>
                )}
                {remote && view.agentDue && node.is_online && (
                    <DropdownMenuItem className="gap-2" onSelect={() => actions.updateAgent(node)}>
                        <Download className="size-4" /> Update agent
                    </DropdownMenuItem>
                )}
                {node.maintenance_mode ? (
                    <DropdownMenuItem className="gap-2" onSelect={() => actions.endMaintenance(node)}>
                        <Wrench className="size-4" /> End maintenance
                    </DropdownMenuItem>
                ) : (
                    <DropdownMenuItem className="gap-2" onSelect={() => actions.startMaintenance(node)}>
                        <Wrench className="size-4" /> Start maintenance…
                    </DropdownMenuItem>
                )}
                {lite && (
                    <>
                        <DropdownMenuSeparator />
                        {/* An MTProto node is removed from its own page, which can ask for the SSH login. */}
                        <DropdownMenuItem asChild className="gap-2 text-red-500 focus:bg-red-500/10 focus:text-red-500">
                            <Link to={`/nodes/${node.id}`}>
                                <Trash2 className="size-4 text-current" /> Remove…
                            </Link>
                        </DropdownMenuItem>
                    </>
                )}
                {remote && (
                    <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onSelect={() => actions.wipe(node)} className="gap-2 text-amber-600 focus:bg-amber-500/10 focus:text-amber-700 dark:text-amber-500">
                            <ShieldOff className="size-4 text-current" /> Wipe…
                        </DropdownMenuItem>
                        <DropdownMenuItem onSelect={() => actions.remove(node)} className="gap-2 text-red-500 focus:bg-red-500/10 focus:text-red-500">
                            <Trash2 className="size-4 text-current" /> Delete…
                        </DropdownMenuItem>
                    </>
                )}
            </DropdownMenuContent>
        </DropdownMenu>
    )
}
