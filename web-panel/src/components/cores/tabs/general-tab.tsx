import type { UseFormReturn } from "react-hook-form"
import { Server, ArrowRightLeft } from "lucide-react"
import { Input } from "@/components/ui/input"
import {
    FormField,
    FormItem,
    FormLabel,
    FormControl,
    FormMessage,
} from "@/components/ui/form"
import type { CoreFormData } from "@/lib/validations/core-schema"

export function GeneralTab({
    form,
    nodeName,
    builtIn,
}: {
    form: UseFormReturn<CoreFormData>
    nodeName: string
    /** the tunnel lives on the panel's built-in node */
    builtIn: boolean
}) {

    return (
        <div className="space-y-4">
            {/* Node (read-only — set by the route) */}
            <div className="space-y-1.5">
                <span className="text-sm font-medium">Node</span>
                <div className="flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2 text-sm">
                    <Server className="size-4 text-muted-foreground" />
                    <span className="font-medium">{nodeName}</span>
                </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
                <FormField
                    control={form.control}
                    name="port"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Port</FormLabel>
                            <FormControl>
                                <Input
                                    type="number"
                                    min={1}
                                    max={65535}
                                    value={field.value ?? 0}
                                    onChange={(e) => field.onChange(Number(e.target.value))}
                                />
                            </FormControl>
                            <FormMessage />
                        </FormItem>
                    )}
                />
                <FormField
                    control={form.control}
                    name="listen"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Listen address</FormLabel>
                            <FormControl>
                                <Input
                                    value={field.value ?? ""}
                                    onChange={field.onChange}
                                    placeholder="0.0.0.0"
                                    className="font-mono"
                                />
                            </FormControl>
                            <FormMessage />
                        </FormItem>
                    )}
                />
            </div>

            <FormField
                control={form.control}
                name="remark"
                render={({ field }) => (
                    <FormItem>
                        <FormLabel>Remark</FormLabel>
                        <FormControl>
                            <Input
                                value={field.value ?? ""}
                                onChange={field.onChange}
                                placeholder="A label to recognise this tunnel"
                            />
                        </FormControl>
                        <FormMessage />
                    </FormItem>
                )}
            />

            {/* Egress (read-only: tunnels dial out from the node) */}
            <div className="space-y-1.5">
                <span className="text-sm font-medium">Egress</span>
                <div className="flex items-start gap-2 rounded-md border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
                    <ArrowRightLeft className="mt-0.5 size-4 shrink-0" />
                    <span>
                        {builtIn
                            ? "From this box, through the VPN pool when router mode is on. Addresses on the box and its networks are refused."
                            : "Straight out of the node. Addresses on the node and its networks are refused."}
                    </span>
                </div>
            </div>
        </div>
    )
}
