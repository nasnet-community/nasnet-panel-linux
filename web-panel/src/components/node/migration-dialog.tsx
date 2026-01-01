import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Monitor, Server, ArrowRight, AlertTriangle, AlertCircle } from "lucide-react"
import { toast } from "sonner"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import * as z from "zod"

import { Button } from "@/components/ui/button"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import {
    Form,
    FormControl,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { listNodes, listNodeInbounds, migrateNode } from "@/lib/admin-api"
import { Node } from "@/lib/types"

const transformSchema = z.object({
    targetNodeId: z.string().min(1, "Target node is required"),
    targetInboundId: z.string().min(1, "Target inbound is required"),
})

interface MigrationDialogProps {
    sourceNode: Node | null
    open: boolean
    onOpenChange: (open: boolean) => void
    onSuccess?: () => void
}

export function MigrationDialog({
    sourceNode,
    open,
    onOpenChange,
    onSuccess
}: MigrationDialogProps) {
    const [isSubmitting, setIsSubmitting] = useState(false)

    // Fetch potential target nodes (exclude source node)
    const { data: nodes = [] } = useQuery({
        queryKey: ["nodes-migration", sourceNode?.id],
        queryFn: async () => {
            const res = await listNodes()
            if (res.success && res.data) {
                // Filter out the source node
                return res.data.filter(n => n.id !== sourceNode?.id)
            }
            return []
        },
        enabled: open && !!sourceNode,
    })

    const form = useForm<z.infer<typeof transformSchema>>({
        resolver: zodResolver(transformSchema),
    })

    const targetNodeId = form.watch("targetNodeId")

    // Fetch inbounds for the selected target node
    const { data: targetInbounds = [], isLoading: isLoadingInbounds } = useQuery({
        queryKey: ["node-inbounds", targetNodeId],
        queryFn: async () => {
            if (!targetNodeId) return []
            const res = await listNodeInbounds(parseInt(targetNodeId))
            if (res.success && res.data) {
                return res.data
            }
            return []
        },
        enabled: !!targetNodeId,
    })

    const handleSubmit = async (values: z.infer<typeof transformSchema>) => {
        if (!sourceNode) return

        setIsSubmitting(true)
        try {
            const res = await migrateNode(
                sourceNode.id,
                parseInt(values.targetNodeId),
                parseInt(values.targetInboundId)
            )

            if (res.success) {
                toast.success("Migration queued successfully")
                onOpenChange(false)
                onSuccess?.()
            } else {
                toast.error(res.error || "Migration failed")
            }
        } catch (error) {
            toast.error("An unexpected error occurred")
            console.error(error)
        } finally {
            setIsSubmitting(false)
        }
    }

    if (!sourceNode) return null

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[500px]">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <Server className="w-5 h-5 text-blue-500" />
                        Migrate Accounts
                    </DialogTitle>
                    <DialogDescription>
                        Move all accounts from <strong>{sourceNode.name}</strong> to another node.
                    </DialogDescription>
                </DialogHeader>

                <Alert variant="default" className="bg-amber-500/10 text-amber-600 border-amber-500/20">
                    <AlertTriangle className="h-4 w-4" />
                    <AlertTitle>Important</AlertTitle>
                    <AlertDescription>
                        This will update the configuration for all users on the source node. They will be moved to the new node immediately.
                    </AlertDescription>
                </Alert>

                <div className="flex items-center justify-between p-4 bg-muted/50 rounded-lg border">
                    <div className="flex flex-col items-center gap-1">
                        <div className="w-10 h-10 rounded-full bg-red-100 flex items-center justify-center text-red-600 border border-red-200">
                            <Server className="w-5 h-5" />
                        </div>
                        <span className="text-xs font-medium text-muted-foreground">Source</span>
                        <span className="text-sm font-semibold">{sourceNode.name}</span>
                    </div>

                    <ArrowRight className="w-5 h-5 text-muted-foreground/50" />

                    <div className="flex flex-col items-center gap-1">
                        <div className="w-10 h-10 rounded-full bg-emerald-100 flex items-center justify-center text-emerald-600 border border-emerald-200">
                            <Monitor className="w-5 h-5" />
                        </div>
                        <span className="text-xs font-medium text-muted-foreground">Target</span>
                        <span className="text-sm font-semibold">
                            {nodes.find(n => n.id.toString() === targetNodeId)?.name || "Select Node"}
                        </span>
                    </div>
                </div>

                <Form {...form}>
                    <form onSubmit={form.handleSubmit(handleSubmit)} className="space-y-4">
                        <FormField
                            control={form.control}
                            name="targetNodeId"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Target Node</FormLabel>
                                    <Select
                                        onValueChange={(val) => {
                                            field.onChange(val)
                                            form.setValue("targetInboundId", "") // Reset inbound when node changes
                                        }}
                                        defaultValue={field.value}
                                    >
                                        <FormControl>
                                            <SelectTrigger>
                                                <SelectValue placeholder="Select a destination node" />
                                            </SelectTrigger>
                                        </FormControl>
                                        <SelectContent>
                                            {nodes.map((node) => (
                                                <SelectItem key={node.id} value={node.id.toString()}>
                                                    {node.name} ({node.ip})
                                                </SelectItem>
                                            ))}
                                            {nodes.length === 0 && (
                                                <div className="p-2 text-sm text-center text-muted-foreground">
                                                    No other nodes available
                                                </div>
                                            )}
                                        </SelectContent>
                                    </Select>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="targetInboundId"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Target Inbound</FormLabel>
                                    <Select
                                        onValueChange={field.onChange}
                                        defaultValue={field.value}
                                        disabled={!targetNodeId || isLoadingInbounds}
                                    >
                                        <FormControl>
                                            <SelectTrigger>
                                                <SelectValue placeholder={isLoadingInbounds ? "Loading inbounds..." : "Select an inbound"} />
                                            </SelectTrigger>
                                        </FormControl>
                                        <SelectContent>
                                            {targetInbounds.map((inbound) => (
                                                <SelectItem key={inbound.id} value={inbound.id.toString()}>
                                                    {inbound.remark} ({inbound.protocol} - {inbound.port})
                                                </SelectItem>
                                            ))}
                                            {targetInbounds.length === 0 && targetNodeId && !isLoadingInbounds && (
                                                <div className="p-2 text-sm text-center text-muted-foreground">
                                                    No inbounds found on this node
                                                </div>
                                            )}
                                        </SelectContent>
                                    </Select>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <DialogFooter className="gap-2 sm:gap-0">
                            <Button
                                type="button"
                                variant="outline"
                                onClick={() => onOpenChange(false)}
                            >
                                Cancel
                            </Button>
                            <Button type="submit" disabled={isSubmitting}>
                                {isSubmitting ? "Migrating..." : "Start Migration"}
                            </Button>
                        </DialogFooter>
                    </form>
                </Form>
            </DialogContent>
        </Dialog>
    )
}
