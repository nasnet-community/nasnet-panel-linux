import { useState } from "react"
import { Check, CircleAlert, Loader2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { moveAgentPort, moveNodeAddress, switchNodeConnectMode, type AgentMove } from "@/lib/api/nodes"
import { AddressPrompt } from "@/components/settings-page/address-prompt"
import { toast } from "@/lib/toast"
import type { Node } from "@/lib/types"
import { cn } from "@/lib/utils"

type Step = { id: NonNullable<AgentMove["step"]>; label: string }

const PORT_STEPS: Step[] = [
    { id: "open", label: "Opening the new port on the node" },
    { id: "switch", label: "Reconnecting on the new port" },
    { id: "close", label: "Closing the old port" },
]
const MODE_STEPS: Step[] = [
    { id: "write", label: "Changing the agent's connection settings" },
    { id: "connect", label: "Waiting for the agent to connect back" },
    { id: "confirm", label: "Confirming the new mode" },
]

/** The steps of a move, marked once it has ended. */
function MoveSteps({ steps, result }: { steps: Step[]; result: AgentMove | null }) {
    const stop = result?.step ? steps.findIndex(s => s.id === result.step) : -1
    return (
        <ol className="space-y-2.5">
            {steps.map((s, i) => {
                const state = !result ? "pending"
                    : stop < 0 ? "done"
                    : i < stop ? "done"
                    : i === stop ? (result.moved ? "warn" : "failed")
                    : "skipped"
                return (
                    <li key={s.id} className={cn("flex items-center gap-2.5 text-sm", state === "pending" || state === "skipped" ? "text-muted-foreground" : "text-foreground")}>
                        <span className={cn(
                            "flex size-5 shrink-0 items-center justify-center rounded-full border",
                            state === "done" && "border-emerald-500 bg-emerald-500 text-white",
                            state === "failed" && "border-red-500 bg-red-500 text-white",
                            state === "warn" && "border-amber-500 bg-amber-500 text-white",
                            (state === "pending" || state === "skipped") && "border-border",
                        )}>
                            {state === "done" && <Check className="size-3" aria-hidden />}
                            {state === "failed" && <X className="size-3" aria-hidden />}
                            {state === "warn" && <CircleAlert className="size-3" aria-hidden />}
                        </span>
                        <span className={cn(state === "skipped" && "line-through")}>{s.label}</span>
                    </li>
                )
            })}
        </ol>
    )
}

function useMove(run: () => Promise<AgentMove>, onDone: (r: AgentMove) => void) {
    const [busy, setBusy] = useState(false)
    const [result, setResult] = useState<AgentMove | null>(null)
    // a run passed here wins over the one the hook was made with
    const start = async (now?: () => Promise<AgentMove>) => {
        setBusy(true)
        setResult(null)
        try {
            const r = await (now ?? run)()
            setResult(r)
            if (r.moved && !r.step) toast.success(r.message)
            else if (r.moved) toast.warning(r.message)
            else toast.error(r.message)
            onDone(r)
        } catch (err) {
            const message = err instanceof Error ? err.message : "The move didn't run"
            toast.error(message)
        } finally {
            setBusy(false)
        }
    }
    return { busy, result, start, reset: () => setResult(null) }
}

async function unwrap(p: Promise<{ success: boolean; data?: AgentMove; error?: string }>): Promise<AgentMove> {
    const res = await p
    if (!res.success || !res.data) throw new Error(res.error || "The move didn't run")
    return res.data
}

function portProblem(raw: string, node: Pick<Node, "agent_port" | "api_port">): string {
    const v = raw.trim()
    if (!v) return "Enter a port number."
    if (!/^\d+$/.test(v)) return "Use a port from 1 to 65535."
    const n = Number(v)
    if (n < 1 || n > 65535) return "Use a port from 1 to 65535."
    if (n === node.agent_port) return "The agent already uses this port."
    if (n === node.api_port) return "That's Xray's API port."
    return ""
}

/** Moves the agent to another port: the old one stays open until the panel is in on the new one. */
export function AgentPortDialog({ node, open, onOpenChange, onMoved }: {
    node: Node
    open: boolean
    onOpenChange: (open: boolean) => void
    onMoved: () => void
}) {
    const [value, setValue] = useState("")
    const [touched, setTouched] = useState(false)
    const problem = portProblem(value, node)
    const move = useMove(() => unwrap(moveAgentPort(node.id, Number(value.trim()))), r => { if (r.moved) onMoved() })
    const ended = move.result !== null
    const close = (o: boolean) => {
        if (move.busy) return
        if (!o) { setValue(""); setTouched(false); move.reset() }
        onOpenChange(o)
    }

    return (
        <Dialog open={open} onOpenChange={close}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{move.busy || ended ? `Moving the agent to port ${value.trim()}` : "Change the agent port"}</DialogTitle>
                    <DialogDescription>
                        The node opens the new port beside port {node.agent_port}. The panel switches only once it connects there; if it can't, nothing changes.
                    </DialogDescription>
                </DialogHeader>
                {move.busy || ended ? (
                    <div className="space-y-4">
                        {move.busy && <p className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" aria-hidden />This takes up to half a minute.</p>}
                        <MoveSteps steps={PORT_STEPS} result={move.result} />
                        {move.result && <p className={cn("rounded-md px-3 py-2 text-sm", move.result.moved && !move.result.step ? "bg-emerald-500/10" : move.result.moved ? "bg-amber-500/10" : "bg-red-500/10")}>{move.result.message}</p>}
                        <DialogFooter><Button type="button" onClick={() => close(false)} disabled={move.busy}>{ended ? "Done" : "Moving…"}</Button></DialogFooter>
                    </div>
                ) : (
                    <form className="space-y-4" onSubmit={e => { e.preventDefault(); setTouched(true); if (!problem) void move.start() }}>
                        <div className="space-y-1.5">
                            <Label htmlFor="agent-port">New port</Label>
                            <Input
                                id="agent-port" type="number" inputMode="numeric" min={1} max={65535} placeholder={String(node.agent_port)}
                                value={value} onChange={e => setValue(e.target.value)} onBlur={() => setTouched(true)}
                                aria-invalid={touched && !!value && !!problem} aria-describedby="agent-port-hint" className="h-11 font-mono sm:h-9" autoFocus
                            />
                            <p id="agent-port-hint" className={touched && value && problem ? "text-xs text-red-500" : "text-xs text-muted-foreground"}>
                                {touched && value && problem ? problem : "Allow it in any firewall in front of the server first."}
                            </p>
                        </div>
                        <DialogFooter>
                            <Button type="button" variant="outline" onClick={() => close(false)}>Cancel</Button>
                            <Button type="submit" disabled={!!problem}>Change port</Button>
                        </DialogFooter>
                    </form>
                )}
            </DialogContent>
        </Dialog>
    )
}

/** Switches between direct and reverse; the agent goes back by itself if it can't connect under the new mode. */
export function ConnectModeDialog({ node, open, onOpenChange, onSwitched }: {
    node: Node
    open: boolean
    onOpenChange: (open: boolean) => void
    onSwitched: () => void
}) {
    const target = (node.connect_mode || "direct") === "direct" ? "reverse" : "direct"
    const move = useMove(() => unwrap(switchNodeConnectMode(node.id, target)), r => { if (r.moved) onSwitched() })
    const ended = move.result !== null
    const close = (o: boolean) => {
        if (move.busy) return
        if (!o) move.reset()
        onOpenChange(o)
    }

    return (
        <Dialog open={open} onOpenChange={close}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{move.busy || ended ? `Switching to ${target} mode` : `Switch to ${target} mode`}</DialogTitle>
                    <DialogDescription>
                        {target === "reverse"
                            ? "The agent connects to the panel, so the node needs no open port. The panel must be reachable at its address for agents."
                            : `The panel connects to the agent on port ${node.agent_port}, so that port must be reachable.`}
                    </DialogDescription>
                </DialogHeader>
                {move.busy || ended ? (
                    <div className="space-y-4">
                        {move.busy && <p className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" aria-hidden />The agent restarts, so this can take a couple of minutes.</p>}
                        <MoveSteps steps={MODE_STEPS} result={move.result} />
                        {move.result && <p className={cn("rounded-md px-3 py-2 text-sm", move.result.moved ? "bg-emerald-500/10" : "bg-red-500/10")}>{move.result.message}</p>}
                        <DialogFooter><Button type="button" onClick={() => close(false)} disabled={move.busy}>{ended ? "Done" : "Switching…"}</Button></DialogFooter>
                    </div>
                ) : (
                    <>
                        <p className="rounded-md bg-muted/60 px-3 py-2 text-xs text-muted-foreground">
                            The agent restarts under the new mode. If the panel can't reach it there within a few minutes, it goes back to {node.connect_mode || "direct"} mode by itself.
                        </p>
                        <DialogFooter>
                            <Button type="button" variant="outline" onClick={() => close(false)}>Cancel</Button>
                            <Button type="button" onClick={() => void move.start()}>Switch to {target}</Button>
                        </DialogFooter>
                    </>
                )}
            </DialogContent>
        </Dialog>
    )
}

const ADDRESS_STEPS: Step[] = [
    { id: "reach", label: "Reaching the agent at the new address" },
    { id: "certificate", label: "Giving the agent a certificate for the new address" },
    { id: "switch", label: "Switching the panel over" },
]

/** Asks for a node's new address, then moves the panel there once the node answers as itself. */
export function AddressChange({ node, open, onOpenChange, onMoved }: {
    node: Node
    open: boolean
    onOpenChange: (open: boolean) => void
    onMoved: () => void
}) {
    const [target, setTarget] = useState("")
    const direct = (node.connect_mode || "direct") === "direct"
    const move = useMove(() => unwrap(moveNodeAddress(node.id, target)), r => { if (r.moved) onMoved() })
    const begin = (address: string) => {
        setTarget(address)
        void move.start(() => unwrap(moveNodeAddress(node.id, address)))
    }
    const running = move.busy || move.result !== null
    const close = () => { if (!move.busy) { move.reset(); setTarget("") } }

    return (
        <>
            <AddressPrompt
                open={open}
                onOpenChange={onOpenChange}
                current={node.ip}
                description={direct
                    ? "The panel tries the new address first and switches only if the agent answers there as this node."
                    : "The agent connects to the panel, so the address only goes into users' links."}
                warning="Users' links use this address unless an inbound sets its own, so their apps need the new links."
                onSubmit={begin}
            />
            <Dialog open={running} onOpenChange={o => { if (!o) close() }}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle>Moving {node.name} to {target}</DialogTitle>
                        <DialogDescription>Nothing changes unless the node answers at the new address.</DialogDescription>
                    </DialogHeader>
                    <div className="space-y-4">
                        {move.busy && <p className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" aria-hidden />This takes up to half a minute.</p>}
                        {direct && <MoveSteps steps={ADDRESS_STEPS} result={move.result} />}
                        {move.result && <p className={cn("rounded-md px-3 py-2 text-sm", move.result.moved ? "bg-emerald-500/10" : "bg-red-500/10")}>{move.result.message}</p>}
                        <DialogFooter><Button type="button" onClick={close} disabled={move.busy}>{move.result ? "Done" : "Moving…"}</Button></DialogFooter>
                    </div>
                </DialogContent>
            </Dialog>
        </>
    )
}
