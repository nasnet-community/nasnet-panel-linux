import { useState } from "react"
import { TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const HOST = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/i
const IPV6 = /^[0-9a-f:.]+$/i

function addressProblem(raw: string, current: string): string {
    const v = raw.trim()
    if (!v) return "Enter an address."
    if (v.toLowerCase() === current.toLowerCase()) return "That's the current address."
    if (!(HOST.test(v) || (v.includes(":") && IPV6.test(v)))) return "Use an IP address or a host name."
    return ""
}

/** Asks for a node's new address; the move itself checks the node answers there. */
export function AddressPrompt({ open, onOpenChange, current, description, warning, onSubmit }: {
    open: boolean
    onOpenChange: (open: boolean) => void
    current: string
    description: string
    /** what changes for users */
    warning: string
    onSubmit: (address: string) => void
}) {
    const [value, setValue] = useState("")
    const [touched, setTouched] = useState(false)
    const problem = addressProblem(value, current)
    const shown = touched && value.trim() !== "" ? problem : ""
    const close = (o: boolean) => {
        if (!o) { setValue(""); setTouched(false) }
        onOpenChange(o)
    }

    return (
        <Dialog open={open} onOpenChange={close}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Change the address</DialogTitle>
                    <DialogDescription>{description}</DialogDescription>
                </DialogHeader>
                <p className="flex items-start gap-2 rounded-md bg-amber-500/10 px-3 py-2 text-xs">
                    <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-amber-500" aria-hidden />{warning}
                </p>
                <form
                    className="space-y-4"
                    onSubmit={e => {
                        e.preventDefault()
                        setTouched(true)
                        if (problem) return
                        onSubmit(value.trim())
                        close(false)
                    }}
                >
                    <div className="space-y-1.5">
                        <Label htmlFor="node-new-address">New address</Label>
                        <Input
                            id="node-new-address" value={value} placeholder="IP address or host name" autoComplete="off" spellCheck={false}
                            onChange={e => setValue(e.target.value)} onBlur={() => setTouched(true)}
                            aria-invalid={!!shown} aria-describedby="node-new-address-hint" className="h-11 font-mono sm:h-9" autoFocus
                        />
                        <p id="node-new-address-hint" className={shown ? "text-xs text-red-500" : "text-xs text-muted-foreground"}>
                            {shown || "With a host name, the next move only needs a DNS change."}
                        </p>
                    </div>
                    <DialogFooter>
                        <Button type="button" variant="outline" onClick={() => close(false)}>Cancel</Button>
                        <Button type="submit" disabled={!!problem}>Change address</Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    )
}
