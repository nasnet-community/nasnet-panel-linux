import { ChevronRight, Send, Server } from "lucide-react"

export type AddNodeKind = "full" | "mtproto"

const KINDS = [
    { kind: "full", icon: Server, label: "Full node", hint: "Runs the NasNet agent and Xray. You install it with a script." },
    { kind: "mtproto", icon: Send, label: "MTProto node", hint: "A Telegram proxy on your VPS. Its traffic leaves through this panel." },
] as const

/** The first step of Add node: what the new node will run. */
export function AddNodeKindPicker({ onPick }: { onPick: (kind: AddNodeKind) => void }) {
    return (
        <div className="space-y-2 py-2">
            {KINDS.map((o) => (
                <button
                    key={o.kind}
                    type="button"
                    onClick={() => onPick(o.kind)}
                    className="flex w-full items-center gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/30"
                >
                    <o.icon aria-hidden="true" className="h-5 w-5 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 space-y-0.5">
                        <span className="block text-sm font-medium">{o.label}</span>
                        <span className="block text-xs text-muted-foreground">{o.hint}</span>
                    </span>
                    <ChevronRight aria-hidden="true" className="h-4 w-4 shrink-0 text-muted-foreground" />
                </button>
            ))}
        </div>
    )
}
