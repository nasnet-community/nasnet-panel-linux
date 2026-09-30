import type { UseFormReturn } from "react-hook-form"
import { Server, ArrowRightLeft } from "lucide-react"
import { cn } from "@/lib/utils"
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
    const egress = form.watch("egress_mode") ?? "direct"

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

            {/* Egress */}
            <div className="space-y-1.5">
                <span className="text-sm font-medium">Egress</span>
                <div className="grid grid-cols-2 gap-2">
                    <EgressOption
                        active={egress === "direct"}
                        onClick={() => form.setValue("egress_mode", "direct", { shouldDirty: true })}
                        title="Direct"
                        subtitle={builtIn ? "From this box" : "Straight out of the node"}
                    />
                    <EgressOption
                        active={egress === "xray"}
                        onClick={() => form.setValue("egress_mode", "xray", { shouldDirty: true })}
                        title="Through Xray"
                        subtitle="Route via the node's Xray rules and outbounds"
                    />
                </div>
                <div className="flex items-start gap-2 rounded-md border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
                    <ArrowRightLeft className="mt-0.5 size-4 shrink-0" />
                    <span>
                        {egress === "xray"
                            ? "Connections leave through the node's Xray routing rules and outbounds. Addresses on the node and its networks are still refused, and the tunnel still counts its users' traffic."
                            : builtIn
                                ? "From this box, through the VPN pool when router mode is on. Addresses on the box and its networks are refused."
                                : "Straight out of the node. Addresses on the node and its networks are refused."}
                    </span>
                </div>
            </div>
        </div>
    )
}

function EgressOption({
    active,
    onClick,
    title,
    subtitle,
}: {
    active: boolean
    onClick: () => void
    title: string
    subtitle: string
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className={cn(
                "rounded-lg border p-3 text-left transition-colors",
                active
                    ? "border-primary bg-primary/5 ring-1 ring-primary/30"
                    : "hover:border-foreground/20 hover:bg-accent",
            )}
        >
            <div className="text-sm font-medium">{title}</div>
            <div className="text-xs text-muted-foreground">{subtitle}</div>
        </button>
    )
}
