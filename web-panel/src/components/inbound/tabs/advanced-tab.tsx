import { useState } from "react"
import { type UseFormReturn } from "react-hook-form"
import { type InboundFormData } from "@/lib/validations/inbound-schema"
import { SockoptForm } from "@/components/shared/sockopt-form"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { FinalMaskEditor } from "@/components/shared/finalmask-editor"
import { FormSection, SwitchRow } from "@/components/connection-dialog/section"
import type { FinalMask } from "@/lib/types"

interface AdvancedTabProps {
    form: UseFormReturn<InboundFormData>
}

export function AdvancedTab({ form }: AdvancedTabProps) {
    const sniffing = form.watch("sniffing_settings")
    const sockopt = form.watch("sockopt_settings")
    const finalmask = (form.watch("finalmask") || {}) as FinalMask
    const [showFinalMask, setShowFinalMask] = useState(!!finalmask.tcp || !!finalmask.udp || !!finalmask.quicParams)

    const updateSniffing = (updates: Record<string, unknown>) => {
        form.setValue("sniffing_settings", {
            ...sniffing,
            ...updates,
        }, { shouldDirty: true })
    }

    return (
        <div className="space-y-7">
            <FormSection title="Sniffing">
                <SwitchRow
                    label="Enable sniffing"
                    help="Detect the real destination for domain-based routing."
                    checked={sniffing?.enabled ?? true}
                    onCheckedChange={(checked) => updateSniffing({ enabled: checked })}
                />

                {sniffing?.enabled && (
                    <div className="space-y-4 border-l-2 border-muted pl-4">
                        <div className="flex flex-col gap-4 md:flex-row md:items-center md:gap-6">
                            <div className="flex items-center gap-2">
                                <Switch
                                    id="sniffing-route"
                                    checked={sniffing?.routeOnly ?? false}
                                    onCheckedChange={(checked) => updateSniffing({ routeOnly: checked })}
                                />
                                <Label htmlFor="sniffing-route" className="text-sm">Route only</Label>
                            </div>
                            <div className="flex items-center gap-2">
                                <Switch
                                    id="sniffing-metadata"
                                    checked={sniffing?.metadataOnly ?? false}
                                    onCheckedChange={(checked) => updateSniffing({ metadataOnly: checked })}
                                />
                                <Label htmlFor="sniffing-metadata" className="text-sm">Metadata only</Label>
                            </div>
                        </div>
                        <div className="space-y-2">
                            <Label>Excluded domains</Label>
                            <Textarea
                                placeholder={"example.com\nads.example.com"}
                                value={(sniffing?.domainsExcluded || []).join("\n")}
                                onChange={(e) =>
                                    updateSniffing({
                                        domainsExcluded: e.target.value.split("\n").filter(Boolean),
                                    })
                                }
                                rows={3}
                            />
                            <p className="text-xs text-muted-foreground">One per line. These keep their original destination.</p>
                        </div>
                    </div>
                )}
            </FormSection>

            <FormSection title="Socket options">
                <SockoptForm
                    data={sockopt}
                    onChange={(s) => form.setValue("sockopt_settings", s, { shouldDirty: true })}
                />
            </FormSection>

            <FormSection title="Packet masking (FinalMask)">
                <SwitchRow
                    label="Mask packet headers"
                    help="Anti-detection for TCP, UDP and QUIC packet shapes."
                    checked={showFinalMask}
                    onCheckedChange={(checked) => {
                        setShowFinalMask(checked)
                        // null (not undefined) so the merge-bind update path nils the
                        // backend pointer; undefined vanishes from the payload and the
                        // node keeps the old mask.
                        if (!checked) form.setValue("finalmask", null, { shouldDirty: true })
                    }}
                />
                {showFinalMask && (
                    <FinalMaskEditor
                        value={finalmask}
                        onChange={(next) => form.setValue("finalmask", next, { shouldDirty: true })}
                    />
                )}
            </FormSection>
        </div>
    )
}
