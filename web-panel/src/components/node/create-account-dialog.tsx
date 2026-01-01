import { useState, useEffect } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import * as z from "zod"
import { v4 as uuidv4 } from "uuid"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    Form,
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { HiOutlineRefresh } from "react-icons/hi"
import { useNodeInbounds } from "@/lib/queries/use-nodes"
import { useCreateAccount } from "@/lib/queries/use-accounts"
import { toast } from "sonner"

const formSchema = z.object({
    inbound_id: z.string().min(1, "Please select an inbound"),
    email: z.string().email("Invalid email address"),
    uuid: z.string().min(36, "UUID is required"),
    flow: z.string().optional(),
})

interface CreateAccountDialogProps {
    nodeId: number
    open: boolean
    onOpenChange: (open: boolean) => void
    onSuccess?: () => void
}

export function CreateAccountDialog({ nodeId, open, onOpenChange, onSuccess }: CreateAccountDialogProps) {
    const { data: inbounds, isLoading: isLoadingInbounds } = useNodeInbounds(nodeId)
    const createMutation = useCreateAccount(nodeId)

    const form = useForm<z.infer<typeof formSchema>>({
        resolver: zodResolver(formSchema),
        defaultValues: {
            inbound_id: "",
            email: "",
            uuid: "",
            flow: "",
        },
    })

    // Generate UUID on open if empty
    useEffect(() => {
        if (open && !form.getValues("uuid")) {
            form.setValue("uuid", uuidv4())
        }
    }, [open, form])

    function onSubmit(values: z.infer<typeof formSchema>) {
        createMutation.mutate({
            inbound_id: parseInt(values.inbound_id),
            email: values.email,
            uuid: values.uuid,
            flow: values.flow,
        }, {
            onSuccess: () => {
                form.reset()
                form.setValue("uuid", uuidv4()) // Reset UUID for next time
                onOpenChange(false)
                onSuccess?.()
            }
        })
    }

    const generateUUID = (e: React.MouseEvent) => {
        e.preventDefault()
        form.setValue("uuid", uuidv4())
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[425px]">
                <DialogHeader>
                    <DialogTitle>Create Account</DialogTitle>
                    <DialogDescription>
                        Add a new manual account to this node.
                    </DialogDescription>
                </DialogHeader>

                <Form {...form}>
                    <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
                        <FormField
                            control={form.control}
                            name="inbound_id"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Inbound</FormLabel>
                                    <FormControl>
                                        <select
                                            className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                                            value={field.value}
                                            onChange={field.onChange}
                                            disabled={isLoadingInbounds}
                                        >
                                            <option value="" disabled>Select inbound</option>
                                            {inbounds?.map((inbound) => (
                                                <option key={inbound.id} value={inbound.id.toString()}>
                                                    {inbound.tag} ({inbound.protocol}:{inbound.port})
                                                </option>
                                            ))}
                                        </select>
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="email"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Email</FormLabel>
                                    <FormControl>
                                        <Input placeholder="user@example.com" {...field} />
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="uuid"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>UUID</FormLabel>
                                    <div className="flex gap-2">
                                        <FormControl>
                                            <Input placeholder="UUID" {...field} />
                                        </FormControl>
                                        <Button
                                            variant="outline"
                                            size="icon"
                                            onClick={generateUUID}
                                            type="button"
                                            title="Generate Info"
                                            aria-label="Generate Info"
                                        >
                                            <HiOutlineRefresh className="h-4 w-4" />
                                        </Button>
                                    </div>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="flow"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Flow (Optional)</FormLabel>
                                    <FormControl>
                                        <Input placeholder="xtls-rprx-vision" {...field} />
                                    </FormControl>
                                    <FormDescription>
                                        Required for some VLESS configurations (Reality).
                                    </FormDescription>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <DialogFooter>
                            <Button type="submit" disabled={createMutation.isPending}>
                                {createMutation.isPending ? "Creating..." : "Create Account"}
                            </Button>
                        </DialogFooter>
                    </form>
                </Form>
            </DialogContent>
        </Dialog>
    )
}
