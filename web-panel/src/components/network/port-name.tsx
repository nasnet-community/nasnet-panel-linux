import { useState } from "react"
import { Check, Pencil, X } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useSetInterfaceLabel } from "@/lib/queries/use-network"
import { cn } from "@/lib/utils"
import type { NetworkInterfaceView } from "@/lib/types/network"

/** Where the name sits: a second line under the interface name in the ports
 *  table, or the heading of the port's own card. */
type Variant = "row" | "title"

/** The operator's name for a port. Same pencil-then-input shape the connected
 *  devices list uses, so one habit covers both. */
export function PortName({
    iface,
    variant = "row",
}: {
    iface: NetworkInterfaceView
    variant?: Variant
}) {
    const save = useSetInterfaceLabel()
    const [editing, setEditing] = useState(false)
    const [draft, setDraft] = useState("")

    async function commit() {
        const label = draft.trim()
        try {
            await save.mutateAsync({ key: iface.key, label })
            setEditing(false)
            toast.success(label ? "Name saved" : "Name removed")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to save the name")
        }
    }

    if (editing) {
        return (
            <div className="flex items-center gap-1">
                <Input
                    autoFocus
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") void commit()
                        if (e.key === "Escape") setEditing(false)
                    }}
                    placeholder="Name this port"
                    aria-label={`Name for ${iface.if_name}`}
                    className="h-7 max-w-[13rem] text-sm"
                    maxLength={63}
                />
                <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    aria-label="Save the name"
                    onClick={() => void commit()}
                >
                    <Check className="h-3.5 w-3.5" />
                </Button>
                <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    aria-label="Discard"
                    onClick={() => setEditing(false)}
                >
                    <X className="h-3.5 w-3.5" />
                </Button>
            </div>
        )
    }

    // As a title the name is the port's identity, so an unnamed port shows its
    // interface name rather than the word "Unnamed".
    const display =
        variant === "title" ? (
            <span
                className={cn(
                    "truncate text-base font-medium",
                    !iface.label && "font-mono",
                )}
            >
                {iface.label || iface.if_name}
            </span>
        ) : iface.label ? (
            <span className="text-text-secondary truncate text-xs">{iface.label}</span>
        ) : (
            <span className="text-text-tertiary text-xs">Unnamed</span>
        )

    return (
        <div className="group/name flex min-w-0 items-center gap-1.5">
            {display}
            <Button
                size="icon"
                variant="ghost"
                aria-label={iface.label ? `Rename ${iface.if_name}` : `Name ${iface.if_name}`}
                className="h-6 w-6 shrink-0 opacity-0 transition-opacity group-hover/name:opacity-100 focus-visible:opacity-100"
                onClick={() => {
                    setDraft(iface.label)
                    setEditing(true)
                }}
            >
                <Pencil className="h-3 w-3" />
            </Button>
        </div>
    )
}
