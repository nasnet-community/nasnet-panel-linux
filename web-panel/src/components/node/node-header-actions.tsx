import { useState } from "react"
import { Download, Globe, Loader2, Package, Play, RotateCw, Square, SquareTerminal, Trash2 } from "lucide-react"
import { HiOutlineDotsVertical } from "react-icons/hi"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { GeofilesDialog } from "@/components/node/geofiles-dialog"
import { DeployAgentDialog } from "@/components/deploy-agent-dialog"
import { useAgentUpdate } from "@/components/node/use-agent-update"
import { clearNodeSSHLogs, restartNodeSSH, restartXrayProcess, startXrayProcess, stopXrayProcess } from "@/lib/api/nodes"
import { isLocalNode } from "@/lib/node-kind"
import { toast } from "@/lib/toast"
import type { Node, NodeStats } from "@/lib/types"
import { cn } from "@/lib/utils"

const OFFLINE = "Needs the node online"

/** The header's actions on a full node: Restart, Update when one is due, and ⋮ for the rest. */
export function NodeHeaderActions({ node, stats, hideButtons, onRefresh, onUpdateAgent }: {
    node: Node
    stats?: NodeStats | null
    /** the terminal workspace keeps everything in the menu */
    hideButtons?: boolean
    onRefresh: () => void
    onUpdateAgent: () => void
}) {
    const confirm = useConfirm()
    const [busy, setBusy] = useState<string | null>(null)
    const [geofilesOpen, setGeofilesOpen] = useState(false)
    const [deployOpen, setDeployOpen] = useState(false)
    const remote = !isLocalNode(node)
    const update = useAgentUpdate(node)
    const due = remote && update.due
    const online = node.is_online
    // unknown until the agent reports; then only the action that fits is offered
    const running = stats?.xray_status ? stats.xray_status === "running" : undefined
    const wide = cn("hidden", !hideButtons && "md:inline-flex")
    const narrow = cn(!hideButtons && "md:hidden")

    const run = async (what: string, done: string, action: () => Promise<{ success: boolean; error?: string }>) => {
        setBusy(what)
        try {
            const res = await action()
            if (!res.success) throw new Error(res.error || `Couldn't ${what.toLowerCase()}`)
            toast.success(done)
            onRefresh()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : `Couldn't ${what.toLowerCase()}`)
        } finally {
            setBusy(null)
        }
    }
    const restart = () => void run("Restart Xray", "Xray restarted", () => restartXrayProcess(node.id))
    const stop = async () => {
        if (!await confirm({
            title: `Stop Xray on ${node.name}?`,
            description: "Its users are cut off until you start Xray again.",
            confirmLabel: "Stop Xray",
            variant: "destructive",
        })) return
        await run("Stop Xray", "Xray stopped", () => stopXrayProcess(node.id))
    }

    return (
        <>
            <span className={wide} title={online ? undefined : OFFLINE}>
                <Button size="sm" variant="outline" className="h-8 px-3" onClick={restart} disabled={!online || busy !== null}>
                    {busy === "Restart Xray" ? <Loader2 className="mr-2 size-3.5 animate-spin" /> : <RotateCw className="mr-2 size-3.5" />}
                    Restart Xray
                </Button>
            </span>
            {due && (
                <span className={wide} title={online ? undefined : OFFLINE}>
                    <Button size="sm" variant="outline" className="h-8 px-3" onClick={onUpdateAgent} disabled={!online}>
                        <Package className="mr-2 size-3.5" />Update agent
                        <span className="ml-1.5 size-1.5 rounded-full bg-amber-500" aria-label="update available" />
                    </Button>
                </span>
            )}
            <DropdownMenu>
                <DropdownMenuTrigger asChild>
                    <Button variant="outline" size="sm" aria-label="Node actions" className="h-8 px-2.5" disabled={busy !== null}>
                        {busy ? <Loader2 className="size-4 animate-spin" /> : <HiOutlineDotsVertical className="size-4" />}
                    </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-56">
                    <DropdownMenuItem className={narrow} disabled={!online} onSelect={restart}>
                        <RotateCw className="mr-2 size-4" /> Restart Xray
                    </DropdownMenuItem>
                    {due && (
                        <DropdownMenuItem className={narrow} disabled={!online} onSelect={onUpdateAgent}>
                            <Package className="mr-2 size-4" /> Update agent<span className="ml-auto text-[11px] text-amber-500">new</span>
                        </DropdownMenuItem>
                    )}
                    <DropdownMenuSeparator className={narrow} />
                    {running !== false && (
                        <DropdownMenuItem disabled={!online} onSelect={() => void stop()} className="text-red-500 focus:text-red-500">
                            <Square className="mr-2 size-4" /> Stop Xray
                        </DropdownMenuItem>
                    )}
                    {running !== true && (
                        <DropdownMenuItem disabled={!online} onSelect={() => void run("Start Xray", "Xray started", () => startXrayProcess(node.id))}>
                            <Play className="mr-2 size-4" /> Start Xray
                        </DropdownMenuItem>
                    )}
                    <DropdownMenuItem disabled={!online} onSelect={() => setGeofilesOpen(true)}>
                        <Globe className="mr-2 size-4" /> Update geofiles…
                    </DropdownMenuItem>
                    {!node.is_stealth && <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem disabled={!online} onSelect={() => void run("Restart SSH", "SSH restarted", () => restartNodeSSH(node.id))}>
                            <SquareTerminal className="mr-2 size-4" /> Restart SSH
                        </DropdownMenuItem>
                        <DropdownMenuItem disabled={!online} onSelect={() => void run("Clear SSH logs", "SSH logs cleared", () => clearNodeSSHLogs(node.id))}>
                            <Trash2 className="mr-2 size-4" /> Clear SSH logs
                        </DropdownMenuItem>
                    </>}
                    {remote && <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onSelect={() => setDeployOpen(true)}>
                            <Download className="mr-2 size-4" /> Reinstall agent…
                        </DropdownMenuItem>
                    </>}
                </DropdownMenuContent>
            </DropdownMenu>
            <GeofilesDialog nodeId={node.id} isOnline={online} onRefresh={onRefresh} open={geofilesOpen} onOpenChange={setGeofilesOpen} />
            {remote && <DeployAgentDialog nodeId={node.id} open={deployOpen} onOpenChange={setDeployOpen} />}
        </>
    )
}
