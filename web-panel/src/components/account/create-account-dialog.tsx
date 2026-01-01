import { useState, useEffect } from "react"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Badge } from "@/components/ui/badge"
import { HiOutlineRefresh, HiOutlineServer, HiOutlineCheck, HiOutlineGlobeAlt, HiOutlineLightningBolt } from "react-icons/hi"
import { createAccountManual, listNodes, listNodeInbounds } from "@/lib/admin-api"
import { generateUUID } from "@/lib/utils"
import { useQueryClient } from "@tanstack/react-query"
import { queryKeys } from "@/lib/queries/keys"
import { useAccountsStore } from "@/store/accounts-store"
import { toast } from "sonner"
import type { Node, Inbound } from "@/lib/types"
import { cn } from "@/lib/utils"

interface CreateAccountDialogProps {
    open?: boolean
    onOpenChange?: (open: boolean) => void
    onSuccess?: () => void
}

interface NodeWithInbounds extends Node {
    inbounds: Inbound[]
}

export function CreateAccountDialog({ open: propOpen, onOpenChange: propOnOpenChange, onSuccess }: CreateAccountDialogProps) {
    const queryClient = useQueryClient()
    const store = useAccountsStore()
    const open = propOpen ?? store.createDialog.open
    const onOpenChange = propOnOpenChange ?? ((v: boolean) => { if (!v) store.closeCreateDialog() })
    const [email, setEmail] = useState("")
    const [uuid, setUuid] = useState("")
    const [selectedInboundId, setSelectedInboundId] = useState<number | null>(null)
    const [isLoading, setIsLoading] = useState(false)
    const [isSubmitting, setIsSubmitting] = useState(false)

    // Data state
    const [nodes, setNodes] = useState<NodeWithInbounds[]>([])

    // Load data when dialog opens
    useEffect(() => {
        if (open) {
            loadData()
            if (!uuid) generateUuid()
        } else {
            // Reset form on close (optional, maybe better to keep state? defaulting to reset for clean slate)
            // setEmail("")
            // setSelectedInboundId(null)
        }
    }, [open])

    const generateUuid = () => {
        setUuid(generateUUID())
    }

    const loadData = async () => {
        setIsLoading(true)
        try {
            // 1. Fetch nodes
            const nodesRes = await listNodes()
            if (!nodesRes.success || !nodesRes.data) {
                toast.error("Failed to load nodes")
                return
            }

            // 2. Fetch inbounds for all nodes parallel
            const nodesData = nodesRes.data
            const nodesWithInbounds: NodeWithInbounds[] = []

            await Promise.all(nodesData.map(async (node) => {
                const inboundsRes = await listNodeInbounds(node.id)
                if (inboundsRes.success && inboundsRes.data) {
                    nodesWithInbounds.push({
                        ...node,
                        inbounds: inboundsRes.data
                    })
                } else {
                    nodesWithInbounds.push({
                        ...node,
                        inbounds: []
                    })
                }
            }))

            setNodes(nodesWithInbounds)
        } catch (error) {
            toast.error("Failed to load server data")
        } finally {
            setIsLoading(false)
        }
    }

    const handleSubmit = async () => {
        if (!email || !uuid || !selectedInboundId) {
            toast.error("Please fill in all fields and select a server")
            return
        }

        setIsSubmitting(true)
        try {
            const res = await createAccountManual(selectedInboundId, email, uuid)

            if (res.success) {
                toast.success("Account created successfully")
                onOpenChange(false)

                // Reset form
                setEmail("")
                setSelectedInboundId(null)
                generateUuid()

                // Trigger refresh in parent
                if (onSuccess) onSuccess()

                // Invalidate query
                queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
            } else {
                toast.error(res.error || "Failed to create account")
            }
        } catch (error) {
            toast.error("An unexpected error occurred")
        } finally {
            setIsSubmitting(false)
        }
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-2xl max-h-[95vh] flex flex-col p-0 gap-0 overflow-hidden">
                <DialogHeader className="p-6 pb-2">
                    <DialogTitle className="text-2xl font-bold">Create New Account</DialogTitle>
                    <DialogDescription>
                        Manually provision a generic account on a specific server.
                    </DialogDescription>
                </DialogHeader>

                <div className="flex-1 overflow-y-auto p-6 pt-2 space-y-6">
                    {/* User Details Section */}
                    <div className="space-y-4">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="space-y-2">
                                <Label htmlFor="email">Email / Username</Label>
                                <Input
                                    id="email"
                                    placeholder="user@example.com"
                                    value={email}
                                    onChange={(e) => setEmail(e.target.value)}
                                    autoFocus
                                />
                            </div>
                            <div className="space-y-2">
                                <Label htmlFor="uuid">UUID</Label>
                                <div className="flex gap-2">
                                    <Input
                                        id="uuid"
                                        value={uuid}
                                        onChange={(e) => setUuid(e.target.value)}
                                        className="font-mono text-sm"
                                    />
                                    <Button size="icon" variant="outline" onClick={generateUuid} title="Generate new UUID" aria-label="Generate new UUID">
                                        <HiOutlineRefresh className="w-4 h-4" />
                                    </Button>
                                </div>
                            </div>
                        </div>
                    </div>

                    <div className="border-t" />

                    {/* Server Selection Section */}
                    <div className="space-y-3">
                        <div className="flex items-center justify-between">
                            <Label className="text-base font-semibold">Select Server & Inbound</Label>
                            {isLoading && <span className="text-xs text-muted-foreground animate-pulse">Loading servers...</span>}
                        </div>

                        <ScrollArea className="h-[300px] pr-4 -mr-4">
                            <div className="space-y-3 pb-2">
                                {isLoading ? (
                                    Array.from({ length: 3 }).map((_, i) => (
                                        <Skeleton key={i} className="h-24 w-full rounded-xl" />
                                    ))
                                ) : nodes.length === 0 ? (
                                    <div className="flex flex-col items-center justify-center py-8 text-muted-foreground border-2 border-dashed rounded-xl">
                                        <HiOutlineServer className="w-8 h-8 mb-2 opacity-50" />
                                        <p>No nodes available</p>
                                    </div>
                                ) : (
                                    nodes.map((node) => (
                                        <div key={node.id} className="border rounded-xl overflow-hidden bg-card/50">
                                            {/* Node Header */}
                                            <div className="px-4 py-3 bg-muted/30 border-b flex items-center justify-between">
                                                <div className="flex items-center gap-3">
                                                    <div className={cn(
                                                        "w-8 h-8 rounded-lg flex items-center justify-center",
                                                        node.is_online ? "bg-emerald-500/10 text-emerald-500" : "bg-red-500/10 text-red-500"
                                                    )}>
                                                        <HiOutlineServer className="w-5 h-5" />
                                                    </div>
                                                    <div>
                                                        <h4 className="font-medium text-sm leading-none">{node.name}</h4>
                                                        <span className="text-xs text-muted-foreground font-mono">{node.ip}</span>
                                                    </div>
                                                </div>
                                                <Badge variant={node.is_online ? "outline" : "secondary"} className="text-[10px] h-5">
                                                    {node.is_online ? "Online" : "Offline"}
                                                </Badge>
                                            </div>

                                            {/* Inbounds List */}
                                            <div className="p-2 space-y-1">
                                                {node.inbounds.length > 0 ? (
                                                    node.inbounds.map((inbound) => {
                                                        const isSelected = selectedInboundId === inbound.id
                                                        return (
                                                            <div
                                                                key={inbound.id}
                                                                onClick={() => setSelectedInboundId(inbound.id)}
                                                                className={cn(
                                                                    "group flex items-center justify-between p-3 rounded-lg cursor-pointer transition-all border",
                                                                    isSelected
                                                                        ? "bg-primary/5 border-primary ring-1 ring-primary/20"
                                                                        : "bg-transparent border-transparent hover:bg-muted hover:border-border"
                                                                )}
                                                            >
                                                                <div className="flex items-center gap-3">
                                                                    <div className={cn(
                                                                        "w-4 h-4 rounded-full border flex items-center justify-center transition-colors",
                                                                        isSelected ? "border-primary bg-primary text-primary-foreground" : "border-muted-foreground/30"
                                                                    )}>
                                                                        {isSelected && <HiOutlineCheck className="w-2.5 h-2.5" />}
                                                                    </div>
                                                                    <div className="flex flex-col">
                                                                        <span className="text-sm font-medium flex items-center gap-2">
                                                                            {inbound.tag}
                                                                        </span>
                                                                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                                                            <span className="uppercase">{inbound.protocol}</span>
                                                                            <span>•</span>
                                                                            <span>Port {inbound.port}</span>
                                                                        </div>
                                                                    </div>
                                                                </div>

                                                                <div className="flex items-center gap-2 opacity-100 sm:opacity-0 sm:group-hover:opacity-100 transition-opacity">
                                                                    <Badge variant="secondary" className="text-[10px] bg-background">
                                                                        {inbound.protocol}
                                                                    </Badge>
                                                                </div>
                                                            </div>
                                                        )
                                                    })
                                                ) : (
                                                    <div className="p-4 text-center text-xs text-muted-foreground italic">
                                                        No inbounds configured on this node
                                                    </div>
                                                )}
                                            </div>
                                        </div>
                                    ))
                                )}
                            </div>
                        </ScrollArea>
                    </div>
                </div>

                <DialogFooter className="p-6 border-t bg-muted/10">
                    <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
                        Cancel
                    </Button>
                    <Button onClick={handleSubmit} disabled={isSubmitting || !selectedInboundId}>
                        {isSubmitting ? (
                            <>
                                <HiOutlineRefresh className="w-4 h-4 mr-2 animate-spin" />
                                Creating...
                            </>
                        ) : (
                            <>
                                <HiOutlineLightningBolt className="w-4 h-4 mr-2" />
                                Create Account
                            </>
                        )}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
