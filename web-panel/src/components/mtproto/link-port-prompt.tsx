import { useState } from "react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import type { MtprotoNode } from "@/lib/api/mtproto"
import { linkPortProblem } from "./mtproto-labels"

export function LinkPortPrompt({ open, onOpenChange, r, onSubmit }: {
    open: boolean
    onOpenChange: (open: boolean) => void
    r: Pick<MtprotoNode, "hub_port" | "capture_port" | "ssh_port">
    onSubmit: (port: number) => void
}) {
    const [value, setValue] = useState("")
    const [touched, setTouched] = useState(false)
    const problem = linkPortProblem(value, r)
    const shown = touched && value !== "" ? problem : ""

    return (
        <Dialog
            open={open}
            onOpenChange={(next) => {
                if (!next) {
                    setValue("")
                    setTouched(false)
                }
                onOpenChange(next)
            }}
        >
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Change the link port</DialogTitle>
                    <DialogDescription>
                        The node opens the new port beside port {r.hub_port}. The panel switches only once it connects there; if it can't, nothing changes.
                    </DialogDescription>
                </DialogHeader>
                <form
                    className="space-y-4"
                    onSubmit={(e) => {
                        e.preventDefault()
                        setTouched(true)
                        if (problem) return
                        onSubmit(Number(value.trim()))
                        setValue("")
                        setTouched(false)
                    }}
                >
                    <div className="space-y-1.5">
                        <Label htmlFor="mtproto-link-port">New port</Label>
                        <Input
                            id="mtproto-link-port"
                            type="number"
                            inputMode="numeric"
                            min={1}
                            max={65535}
                            placeholder={String(r.hub_port)}
                            value={value}
                            onChange={(e) => setValue(e.target.value)}
                            onBlur={() => setTouched(true)}
                            aria-invalid={!!shown}
                            aria-describedby="mtproto-link-port-hint"
                            className="h-11 font-mono sm:h-9"
                            autoFocus
                        />
                        <p id="mtproto-link-port-hint" className={shown ? "text-xs text-red-500" : "text-xs text-muted-foreground"}>
                            {shown || "Allow it in any firewall in front of the VPS first. Users keep their links: this isn't the proxy's port."}
                        </p>
                    </div>
                    <DialogFooter>
                        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
                        <Button type="submit" disabled={!!problem}>Change port</Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    )
}
