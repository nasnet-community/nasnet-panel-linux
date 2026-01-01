import { type UseFormReturn } from "react-hook-form"
import { type InboundFormData } from "@/lib/validations/inbound-schema"
import { INBOUND_PRESETS, type InboundPreset } from "@/lib/presets/inbound-presets"
import { INBOUND_PROTOCOLS } from "@/lib/types"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
    Form,
    FormControl,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
    FormDescription,
} from "@/components/ui/form"
import { cn } from "@/lib/utils"
import { motion } from "framer-motion"
import { useState } from "react"

interface GeneralTabProps {
    form: UseFormReturn<InboundFormData>
    mode: "create" | "edit"
    onApplyPreset: (presetId: string) => void
}

export function GeneralTab({ form, mode, onApplyPreset }: GeneralTabProps) {
    const [selectedPreset, setSelectedPreset] = useState<string | null>(null)
    const protocol = form.watch("protocol")

    const handlePresetClick = (preset: InboundPreset) => {
        setSelectedPreset(preset.id)
        onApplyPreset(preset.id)
    }

    return (
        <div className="space-y-6">
            {/* Presets grid - create mode only */}
            {mode === "create" && (
                <div className="space-y-3">
                    <h3 className="text-sm font-medium text-muted-foreground">Quick Start Templates</h3>
                    <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
                        {INBOUND_PRESETS.map((preset) => {
                            const Icon = preset.icon
                            const isSelected = selectedPreset === preset.id
                            return (
                                <motion.button
                                    key={preset.id}
                                    type="button"
                                    whileTap={{ scale: 0.97 }}
                                    animate={isSelected ? { scale: [1, 1.02, 1] } : {}}
                                    transition={{ duration: 0.15 }}
                                    onClick={() => handlePresetClick(preset)}
                                    className={cn(
                                        "flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors",
                                        isSelected
                                            ? "border-primary bg-primary/5 ring-1 ring-primary/20"
                                            : "hover:bg-accent/50"
                                    )}
                                >
                                    <div className="flex items-center gap-2">
                                        <Icon className="h-4 w-4 text-muted-foreground" />
                                        <span className="text-sm font-medium">{preset.name}</span>
                                    </div>
                                    <p className="text-[11px] text-muted-foreground leading-tight">
                                        {preset.description}
                                    </p>
                                </motion.button>
                            )
                        })}
                    </div>
                </div>
            )}

            {/* Tag & Remark */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
                <FormField
                    control={form.control}
                    name="tag"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Tag *</FormLabel>
                            <FormControl>
                                <Input
                                    placeholder="vless-tcp-reality"
                                    {...field}
                                />
                            </FormControl>
                            <FormDescription className="text-xs">
                                Unique identifier for this inbound
                            </FormDescription>
                            <FormMessage />
                        </FormItem>
                    )}
                />
                <FormField
                    control={form.control}
                    name="remark"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Remark</FormLabel>
                            <FormControl>
                                <Input
                                    placeholder="Germany Fast"
                                    {...field}
                                    value={field.value || ""}
                                />
                            </FormControl>
                        </FormItem>
                    )}
                />
            </div>

            {/* Listen, Port, Protocol */}
            <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
                <FormField
                    control={form.control}
                    name="listen"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Listen</FormLabel>
                            <FormControl>
                                <Input
                                    placeholder="0.0.0.0"
                                    {...field}
                                    value={field.value || ""}
                                />
                            </FormControl>
                        </FormItem>
                    )}
                />
                <FormField
                    control={form.control}
                    name="port"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Port *</FormLabel>
                            <FormControl>
                                <Input
                                    type="number"
                                    placeholder="443"
                                    {...field}
                                    value={field.value || ""}
                                    onChange={(e) => {
                                        // NaN for non-integer input so zod flags it (no silent truncation)
                                        field.onChange(e.target.valueAsNumber)
                                    }}
                                />
                            </FormControl>
                            <FormMessage />
                        </FormItem>
                    )}
                />
                <FormField
                    control={form.control}
                    name="protocol"
                    render={({ field }) => (
                        <FormItem className="col-span-2 md:col-span-1">
                            <FormLabel>Protocol</FormLabel>
                            <Select
                                value={field.value}
                                onValueChange={field.onChange}
                            >
                                <FormControl>
                                    <SelectTrigger className="w-full">
                                        <SelectValue />
                                    </SelectTrigger>
                                </FormControl>
                                <SelectContent>
                                    {INBOUND_PROTOCOLS.map((p) => (
                                        <SelectItem key={p.value} value={p.value}>
                                            {p.label}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </FormItem>
                    )}
                />
            </div>

            {/* Port Range */}
            <FormField
                control={form.control}
                name="port_range"
                render={({ field }) => (
                    <FormItem>
                        <FormLabel>Port range (optional)</FormLabel>
                        <FormControl>
                            <Input
                                placeholder="e.g. 1000-2000 or 80,443,8080"
                                {...field}
                                value={field.value || ""}
                            />
                        </FormControl>
                        <FormDescription className="text-xs">
                            Listener binds this range/list; the Port above is still used for client links.
                        </FormDescription>
                        <FormMessage />
                    </FormItem>
                )}
            />

            {/* Client Management Info */}
            {["vless", "vmess", "trojan"].includes(protocol) && (
                <div className="text-center py-4 text-muted-foreground rounded-lg border bg-muted/20">
                    <p className="font-medium">{protocol?.toUpperCase()} uses dynamic client management.</p>
                    <p className="text-sm mt-1">Users are added via the subscription system.</p>
                </div>
            )}
        </div>
    )
}
