import { ToggleRight } from "lucide-react"
import { Dialog, DialogContent, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { NodeProtocolsPanel } from "@/components/node/node-protocols-panel"

const TITLE = "Protocols on this node"
const BLURB =
    "Turn a whole protocol off to tear down its listeners and block new tunnels of that type — without uninstalling anything. Re-enabling restores it."

export interface NodeProtocolsDialogProps {
    nodeId: number
    open: boolean
    onOpenChange: (open: boolean) => void
}

function Header() {
    return (
        // pr-10 reserves space for the dialog's absolute top-right close (X).
        <div className="flex items-start gap-3 pr-10">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                <ToggleRight className="size-5 text-muted-foreground" aria-hidden />
            </div>
            <div className="min-w-0">
                <h2 className="text-base font-semibold">{TITLE}</h2>
                <p className="mt-0.5 text-xs text-muted-foreground">{BLURB}</p>
            </div>
        </div>
    )
}

/** Modal wrapper around the per-node protocol on/off matrix. */
export function NodeProtocolsDialog({ nodeId, open, onOpenChange }: NodeProtocolsDialogProps) {
    const isMobile = useIsMobile()

    if (isMobile) {
        return (
            <Sheet open={open} onOpenChange={onOpenChange}>
                <SheetContent side="bottom" className="flex h-[92dvh] flex-col rounded-t-xl p-0">
                    <div className="border-b p-4">
                        <SheetTitle className="sr-only">{TITLE}</SheetTitle>
                        <SheetDescription className="sr-only">{BLURB}</SheetDescription>
                        <Header />
                    </div>
                    <div className="flex-1 overflow-y-auto p-4">
                        <NodeProtocolsPanel nodeId={nodeId} />
                    </div>
                </SheetContent>
            </Sheet>
        )
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="flex max-h-[92vh] max-w-2xl flex-col gap-0 p-0">
                <div className="border-b p-5">
                    <DialogTitle className="sr-only">{TITLE}</DialogTitle>
                    <DialogDescription className="sr-only">{BLURB}</DialogDescription>
                    <Header />
                </div>
                <div className="flex-1 overflow-y-auto p-5">
                    <NodeProtocolsPanel nodeId={nodeId} />
                </div>
                <div className="flex justify-end border-t p-4">
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Done
                    </Button>
                </div>
            </DialogContent>
        </Dialog>
    )
}
