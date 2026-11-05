import { useEffect, useId, useState } from "react"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import type { Node } from "@/lib/types"

/** Asks for the notice users see, then starts maintenance on one node. */
export function MaintenanceDialog({
    node,
    pending,
    onConfirm,
    onOpenChange,
}: {
    node: Node | null
    pending: boolean
    onConfirm: (message: string) => void
    onOpenChange: (open: boolean) => void
}) {
    const id = useId()
    const [message, setMessage] = useState("")
    useEffect(() => { if (node) setMessage(node.maintenance_message ?? "") }, [node])

    return (
        <Dialog open={!!node} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Start maintenance on {node?.name}</DialogTitle>
                    <DialogDescription>
                        Users on this node see a notice in their subscription page and the bot. Buying, renewing and new configs are paused for them.
                    </DialogDescription>
                </DialogHeader>
                <div className="space-y-2">
                    <Label htmlFor={id}>Message for users <span className="font-normal text-muted-foreground">(optional)</span></Label>
                    <Textarea
                        id={id}
                        dir="auto"
                        rows={3}
                        value={message}
                        onChange={(e) => setMessage(e.target.value)}
                        placeholder="Leave empty for the standard notice"
                    />
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>Cancel</Button>
                    <Button onClick={() => onConfirm(message.trim())} disabled={pending}>
                        {pending && <Loader2 className="size-4 animate-spin" />}
                        Start maintenance
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
