import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Monitor, Server, ArrowRight, AlertTriangle } from "lucide-react"
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
import { listNodes, listNodeInbounds, migrateAccount, Account } from "@/lib/admin-api"

const schema = z.object({
    targetNodeId: z.string().min(1, "Target node is required"),
    targetInboundId: z.string().min(1, "Target inbound is required"),
})

interface AccountMigrationDialogProps {
    account: Account | null
    sourceNodeId: number
    open: boolean
    onOpenChange: (open: boolean) => void
    onSuccess?: () => void
}

export function AccountMigrationDialog({
    account,
    sourceNodeId,
    open,
    onOpenChange,
    onSuccess
}: AccountMigrationDialogProps) {
    const [isSubmitting, setIsSubmitting] = useState(false)
    const queryClient = useQueryClient()

    // Fetch potential target nodes
    const { data: nodes = [] } = useQuery({
        queryKey: ["nodes-migration", sourceNodeId],
        queryFn: async () => {
            const res = await listNodes()
            if (res.success && res.data) {
                // Same-node + different-inbound is a valid migration.
                return res.data
            }
            return []
        },
        enabled: open && !!account,
    })

    const form = useForm<z.infer<typeof schema>>({
        resolver: zodResolver(schema),
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

    const handleSubmit = async (values: z.infer<typeof schema>) => {
        if (!account) return

        setIsSubmitting(true)
        try {
            const res = await migrateAccount(
                account.id,
                parseInt(values.targetInboundId)
            )

            if (res.success) {
                toast.success("Account migrated successfully")
                onOpenChange(false)
                onSuccess?.()
                // Invalidate keys
                queryClient.invalidateQueries({ queryKey: ["accounts"] })
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

    if (!account) return null

    const sourceNodeName = nodes.find(n => n.id === sourceNodeId)?.name || "Current Node"
    const targetNodeName = nodes.find(n => n.id.toString() === targetNodeId)?.name || "Target Node"

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[500px]">
                {/* ... Header ... */}
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <Server className="w-5 h-5 text-blue-500" />
                        Migrate Account
                    </DialogTitle>
                    <DialogDescription>
                        Transfer <strong>{account.email}</strong> to another node/inbound.
                    </DialogDescription>
                </DialogHeader>

                <Alert variant="default" className="bg-blue-500/10 text-blue-600 border-blue-500/20">
                    <Monitor className="h-4 w-4" />
                    <AlertTitle>Live Transfer</AlertTitle>
                    <AlertDescription>
                        The user will be immediately provisioned on the new node and removed from the current one.
                    </AlertDescription>
                </Alert>

                <div className="flex items-center justify-between p-4 bg-muted/50 rounded-lg border my-2">
                    <div className="flex flex-col items-center gap-1">
                        <div className="w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-800 flex items-center justify-center border">
                            <Server className="w-5 h-5 text-muted-foreground" />
                        </div>
                        <span className="text-xs font-medium text-muted-foreground">Source</span>
                        <span className="text-sm font-semibold truncate max-w-[100px]">{sourceNodeName}</span>
                    </div>

                    <div className="flex flex-col items-center justify-center px-4">
                        <ArrowRight className="w-5 h-5 text-muted-foreground/50" />
                    </div>

                    <div className="flex flex-col items-center gap-1">
                        <div className="w-10 h-10 rounded-full bg-blue-100 dark:bg-blue-900/30 flex items-center justify-center border border-blue-200 dark:border-blue-800">
                            <Monitor className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                        </div>
                        <span className="text-xs font-medium text-muted-foreground">Target</span>
                        <span className="text-sm font-semibold truncate max-w-[100px]">{targetNodeName}</span>
                    </div>
                </div>

                <Form {...form}>
                    <form className="space-y-4">
                        <FormField
                            control={form.control}
                            name="targetNodeId"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Destination Node</FormLabel>
                                    <Select
                                        onValueChange={(val) => {
                                            field.onChange(val)
                                            form.setValue("targetInboundId", "")
                                        }}
                                        defaultValue={field.value}
                                    >
                                        <FormControl>
                                            <SelectTrigger>
                                                <SelectValue placeholder="Select a node" />
                                            </SelectTrigger>
                                        </FormControl>
                                        <SelectContent>
                                            {nodes.map((node) => (
                                                <SelectItem key={node.id} value={node.id.toString()}>
                                                    {node.name} <span className="text-muted-foreground ml-1">({node.ip})</span>
                                                </SelectItem>
                                            ))}
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
                                    <FormLabel>Inbound</FormLabel>
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
                                                    {inbound.tag} <span className="text-muted-foreground text-xs ml-1">({inbound.protocol}:{inbound.port})</span>
                                                </SelectItem>
                                            ))}
                                            {targetInbounds.length === 0 && targetNodeId && !isLoadingInbounds && (
                                                <div className="p-2 text-sm text-center text-muted-foreground">
                                                    No inbounds found
                                                </div>
                                            )}
                                        </SelectContent>
                                    </Select>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <DialogFooter className="gap-2 sm:gap-0 mt-4">
                            <Button
                                type="button"
                                variant="outline"
                                onClick={() => onOpenChange(false)}
                            >
                                Cancel
                            </Button>
                            <Button
                                type="button"
                                onClick={form.handleSubmit(handleSubmit, (errors) => {
                                    toast.error("Please select both a target node and inbound")
                                })}
                                disabled={isSubmitting}
                            >
                                {isSubmitting ? "Migrating..." : "Start Migration"}
                            </Button>
                        </DialogFooter>
                    </form>
                </Form>
            </DialogContent>
        </Dialog>
    )
}
