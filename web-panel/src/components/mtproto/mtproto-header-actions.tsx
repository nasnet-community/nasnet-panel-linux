import { Package, Play, RotateCw, Send, Square, Wrench } from "lucide-react"
import { HiOutlineDotsVertical } from "react-icons/hi"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useMtprotoNode } from "@/lib/queries"
import type { Node } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useMtprotoActions } from "./mtproto-actions"

const LINK_DOWN = "Needs the link to the node"
const STOPPED = "The proxy is stopped: start it first"

/** The header's actions on an MTProto node: Restart, Update when one is due, and ⋮ for the rest. */
export function MtprotoHeaderActions({ node, hideButtons }: { node: Node; hideButtons?: boolean }) {
    const { data: r, refetch } = useMtprotoNode(node.id)
    const a = useMtprotoActions(node, r, () => void refetch())
    const agentDue = !!r?.updates?.agent
    const proxyDue = !!r?.updates?.telemt
    const stopped = !!r?.proxy_stopped
    const off = !a.linkUp
    // a disabled button shows no tooltip, so its wrapper carries the reason
    const wide = cn("hidden", !hideButtons && "md:inline-flex")
    const narrow = cn(!hideButtons && "md:hidden")
    const restartWhy = off ? LINK_DOWN : stopped ? STOPPED : undefined

    return (
        <>
            <span className={wide} title={restartWhy}>
                <Button size="sm" variant="outline" className="h-8 px-3" disabled={!!restartWhy || a.restarting} onClick={() => void a.restartProxy()}>
                    <RotateCw className={cn("mr-2 size-3.5", a.restarting && "animate-spin")} />
                    Restart proxy
                </Button>
            </span>
            {agentDue && (
                <span className={wide} title={off ? LINK_DOWN : undefined}>
                    <Button size="sm" variant="outline" className="h-8 px-3" disabled={off} onClick={() => void a.updateAgent()}>
                        <Package className="mr-2 size-3.5" />Update agent
                        <span className="ml-1.5 size-1.5 rounded-full bg-amber-500" aria-label="update available" />
                    </Button>
                </span>
            )}
            {proxyDue && (
                <span className={wide} title={off ? LINK_DOWN : undefined}>
                    <Button size="sm" variant="outline" className="h-8 px-3" disabled={off} onClick={() => void a.updateProxy()}>
                        <Send className="mr-2 size-3.5" />Update proxy
                        <span className="ml-1.5 size-1.5 rounded-full bg-amber-500" aria-label="update available" />
                    </Button>
                </span>
            )}
            <DropdownMenu>
                <DropdownMenuTrigger asChild>
                    <Button variant="outline" size="sm" aria-label="Node actions" className="h-8 px-2.5">
                        <HiOutlineDotsVertical className="size-4" />
                    </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-52">
                    <DropdownMenuItem className={narrow} disabled={!!restartWhy} onClick={() => void a.restartProxy()}>
                        <RotateCw className="mr-2 size-4" /> Restart proxy
                    </DropdownMenuItem>
                    {agentDue && (
                        <DropdownMenuItem className={narrow} disabled={off} onClick={() => void a.updateAgent()}>
                            <Package className="mr-2 size-4" /> Update agent<span className="ml-auto text-[11px] text-amber-500">new</span>
                        </DropdownMenuItem>
                    )}
                    {proxyDue && (
                        <DropdownMenuItem className={narrow} disabled={off} onClick={() => void a.updateProxy()}>
                            <Send className="mr-2 size-4" /> Update proxy<span className="ml-auto text-[11px] text-amber-500">new</span>
                        </DropdownMenuItem>
                    )}
                    <DropdownMenuSeparator className={narrow} />
                    {stopped ? (
                        <DropdownMenuItem disabled={off || !!a.toggling} onClick={a.startProxy}>
                            <Play className="mr-2 size-4" /> Start proxy
                        </DropdownMenuItem>
                    ) : (
                        <DropdownMenuItem disabled={off || !!a.toggling} onClick={() => void a.stopProxy()} className="text-red-500 focus:text-red-500">
                            <Square className="mr-2 size-4" /> Stop proxy
                        </DropdownMenuItem>
                    )}
                    <DropdownMenuItem onClick={a.repair}>
                        <Wrench className="mr-2 size-4" /> Repair…
                    </DropdownMenuItem>
                </DropdownMenuContent>
            </DropdownMenu>
            {a.dialogs}
        </>
    )
}
