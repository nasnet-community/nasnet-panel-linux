import { useState, useMemo } from "react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Label } from "@/components/ui/label"
import { AlertTriangle, Loader2 } from "lucide-react"
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { Inbound, Node } from "@/lib/types"
import { useMigrateInbound } from "@/lib/queries/use-nodes"

interface MigrateInboundDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    sourceInbound: Inbound
    allNodes: Node[]
    allInbounds: Map<number, Inbound[]>
    accountCount: number
}

export function MigrateInboundDialog({
    open,
    onOpenChange,
    sourceInbound,
    allNodes,
    allInbounds,
    accountCount,
}: MigrateInboundDialogProps) {
    const [targetInboundId, setTargetInboundId] = useState<string>("")
    const migrateMutation = useMigrateInbound()

    const targetOptions = useMemo(() => {
        const options: { nodeId: number; nodeName: string; inbounds: Inbound[] }[] = []
        for (const node of allNodes) {
            if (!node.is_active) continue
            const nodeInbounds = (allInbounds.get(node.id) || []).filter(
                (inb) => inb.id !== sourceInbound.id && !inb.is_disabled
            )
            if (nodeInbounds.length > 0) {
                options.push({ nodeId: node.id, nodeName: node.name, inbounds: nodeInbounds })
            }
        }
        return options
    }, [allNodes, allInbounds, sourceInbound.id])

    const selectedTarget = useMemo(() => {
        if (!targetInboundId) return null
        const id = parseInt(targetInboundId)
        for (const group of targetOptions) {
            const found = group.inbounds.find((inb) => inb.id === id)
            if (found) return found
        }
        return null
    }, [targetInboundId, targetOptions])

    const protocolMismatch = selectedTarget && selectedTarget.protocol?.toLowerCase() !== sourceInbound.protocol?.toLowerCase()

    const handleMigrate = () => {
        if (!targetInboundId) return
        migrateMutation.mutate(
            { sourceInboundId: sourceInbound.id, targetInboundId: parseInt(targetInboundId) },
            { onSuccess: () => onOpenChange(false) }
        )
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Migrate Inbound</DialogTitle>
                    <DialogDescription>
                        Move all accounts from this inbound to another.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="rounded-md border p-3 space-y-1">
                        <Label className="text-xs text-muted-foreground">Source</Label>
                        <div className="flex items-center gap-2">
                            <span className="font-medium">{sourceInbound.tag}</span>
                            <Badge variant="outline">{sourceInbound.protocol}</Badge>
                            <Badge variant="secondary">:{sourceInbound.port}</Badge>
                        </div>
                        <p className="text-xs text-muted-foreground">{accountCount} accounts</p>
                    </div>

                    <div className="space-y-2">
                        <Label>Target Inbound</Label>
                        <Select value={targetInboundId} onValueChange={setTargetInboundId}>
                            <SelectTrigger>
                                <SelectValue placeholder="Select target inbound..." />
                            </SelectTrigger>
                            <SelectContent>
                                {targetOptions.map((group) => (
                                    <SelectGroup key={group.nodeId}>
                                        <SelectLabel>{group.nodeName}</SelectLabel>
                                        {group.inbounds.map((inb) => (
                                            <SelectItem key={inb.id} value={String(inb.id)}>
                                                <span className="flex items-center gap-2">
                                                    {inb.tag}
                                                    <Badge variant="outline" className="text-[10px] h-4 px-1">
                                                        {inb.protocol}
                                                    </Badge>
                                                    :{inb.port}
                                                </span>
                                            </SelectItem>
                                        ))}
                                    </SelectGroup>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    {protocolMismatch && (
                        <div className="flex items-start gap-2 rounded-md border border-yellow-500/30 bg-yellow-500/10 p-3 text-sm">
                            <AlertTriangle className="w-4 h-4 text-yellow-500 mt-0.5 shrink-0" />
                            <span>
                                Protocol mismatch: <strong>{sourceInbound.protocol}</strong> →{" "}
                                <strong>{selectedTarget?.protocol}</strong>. Flow and encryption will be adjusted.
                            </span>
                        </div>
                    )}
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleMigrate}
                        disabled={!targetInboundId || migrateMutation.isPending}
                    >
                        {migrateMutation.isPending && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                        Migrate
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
