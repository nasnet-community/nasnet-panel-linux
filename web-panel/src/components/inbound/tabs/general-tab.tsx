import { type UseFormReturn } from "react-hook-form"
import { type InboundFormData } from "@/lib/validations/inbound-schema"
import { INBOUND_PRESETS } from "@/lib/presets/inbound-presets"
import { INBOUND_PROTOCOLS } from "@/lib/types"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
    FormControl,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
    FormDescription,
} from "@/components/ui/form"
import { TemplatePicker, type TemplateItem } from "@/components/connection-dialog/template-picker"
import { Callout, FormSection } from "@/components/connection-dialog/section"

const TEMPLATE_ITEMS: TemplateItem[] = INBOUND_PRESETS.map((p) => ({
    id: p.id,
    name: p.name,
    tokens: p.tokens,
    description: p.description,
}))

const Optional = () => <span className="text-xs font-normal text-text-tertiary">optional</span>

interface GeneralTabProps {
    form: UseFormReturn<InboundFormData>
    mode: "create" | "edit"
    appliedPresetId: string | null
    onApplyPreset: (presetId: string) => void
}

export function GeneralTab({ form, mode, appliedPresetId, onApplyPreset }: GeneralTabProps) {
    const protocol = form.watch("protocol")

    return (
        <div className="space-y-6">
            {mode === "create" && (
                <TemplatePicker
                    items={TEMPLATE_ITEMS}
                    appliedId={appliedPresetId}
                    onApply={onApplyPreset}
                    visibleCount={8}
                    sourceHint="from XTLS/Xray-examples"
                />
            )}

            <FormSection title="Identity">
                <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
                    <FormField
                        control={form.control}
                        name="tag"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Tag <span className="text-text-tertiary">*</span></FormLabel>
                                <FormControl>
                                    <Input placeholder="vless-de-fra" {...field} />
                                </FormControl>
                                <FormMessage />
                                <FormDescription className="text-xs">
                                    Unique on this node. Used in routing rules and client links.
                                </FormDescription>
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
                                    <Input placeholder="Frankfurt · Hetzner" {...field} value={field.value || ""} />
                                </FormControl>
                                <FormDescription className="text-xs">
                                    Shown to you in lists. Clients never see it.
                                </FormDescription>
                            </FormItem>
                        )}
                    />
                </div>
            </FormSection>

            <FormSection title="Protocol">
                <FormField
                    control={form.control}
                    name="protocol"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Protocol</FormLabel>
                            <Select value={field.value} onValueChange={field.onChange}>
                                <FormControl>
                                    <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                </FormControl>
                                <SelectContent>
                                    {INBOUND_PROTOCOLS.map((p) => (
                                        <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <FormDescription className="text-xs">
                                Changes which settings appear under Protocol and Transport.
                            </FormDescription>
                        </FormItem>
                    )}
                />
            </FormSection>

            <FormSection title="Listener">
                <div className="grid grid-cols-2 gap-4 md:grid-cols-[minmax(0,1fr)_148px_minmax(0,1.4fr)]">
                    <FormField
                        control={form.control}
                        name="listen"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Listen</FormLabel>
                                <FormControl>
                                    <Input placeholder="0.0.0.0" {...field} value={field.value || ""} />
                                </FormControl>
                                <FormDescription className="text-xs">IP or unix socket path.</FormDescription>
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="port"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Port <span className="text-text-tertiary">*</span></FormLabel>
                                <FormControl>
                                    <Input
                                        type="number"
                                        placeholder="443"
                                        {...field}
                                        value={field.value || ""}
                                        onChange={(e) => {
                                            // valueAsNumber is NaN for anything that isn't a clean
                                            // integer (e.g. "443abc"), letting zod flag it instead
                                            // of silently truncating.
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
                        name="port_range"
                        render={({ field }) => (
                            <FormItem className="col-span-2 md:col-span-1">
                                <FormLabel>Port range <Optional /></FormLabel>
                                <FormControl>
                                    <Input placeholder="1000-2000 or 80,443,8080" {...field} value={field.value || ""} />
                                </FormControl>
                                <FormDescription className="text-xs">
                                    Binds this range too; Port above stays in client links.
                                </FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                </div>
            </FormSection>

            {["vless", "vmess", "trojan"].includes(protocol) && (
                <Callout>
                    {protocol.toUpperCase()} clients are managed by the subscription system. There is nothing to add here.
                </Callout>
            )}
        </div>
    )
}
