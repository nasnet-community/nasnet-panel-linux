import { useState } from "react"
import { type UseFormReturn } from "react-hook-form"
import { type InboundFormData } from "@/lib/validations/inbound-schema"
import { SockoptForm } from "@/components/shared/sockopt-form"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { FinalMaskEditor } from "@/components/shared/finalmask-editor"
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
        <div className="space-y-6">
            {/* Sniffing Settings */}
            <div className="space-y-4">
                <h4 className="text-sm font-medium text-muted-foreground">Sniffing</h4>
                <div className="flex items-center justify-between">
                    <div className="space-y-0.5">
                        <Label>Enable Sniffing</Label>
                        <p className="text-xs text-muted-foreground">
                            Detect protocol for better routing
                        </p>
                    </div>
                    <Switch
                        checked={sniffing?.enabled ?? true}
                        onCheckedChange={(checked) => updateSniffing({ enabled: checked })}
                    />
                </div>

                {sniffing?.enabled && (
                    <div className="flex flex-col md:flex-row md:items-center gap-4 md:gap-6 pl-4 border-l-2 border-muted">
                        <div className="flex items-center gap-2">
                            <Switch
                                id="sniffing-route"
                                checked={sniffing?.routeOnly ?? false}
                                onCheckedChange={(checked) => updateSniffing({ routeOnly: checked })}
                            />
                            <Label htmlFor="sniffing-route" className="text-sm">Route Only</Label>
                        </div>
                        <div className="flex items-center gap-2">
                            <Switch
                                id="sniffing-metadata"
                                checked={sniffing?.metadataOnly ?? false}
                                onCheckedChange={(checked) => updateSniffing({ metadataOnly: checked })}
                            />
                            <Label htmlFor="sniffing-metadata" className="text-sm">Metadata Only</Label>
                        </div>
                    </div>
                )}
                {sniffing?.enabled && (
                    <div className="space-y-2 pl-4 border-l-2 border-muted">
                        <Label>Excluded Domains</Label>
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
                        <p className="text-xs text-muted-foreground">Domains excluded from sniffing override, one per line</p>
                    </div>
                )}
            </div>

            {/* Socket Options */}
            <div className="space-y-4 border-t pt-4">
                <h4 className="text-sm font-medium text-muted-foreground">Socket Options</h4>
                <SockoptForm
                    data={sockopt}
                    onChange={(s) => form.setValue("sockopt_settings", s, { shouldDirty: true })}
                />
            </div>

            {/* FinalMask */}
            <div className="space-y-4 border-t pt-4">
                <div className="flex items-center justify-between">
                    <div>
                        <h4 className="text-sm font-medium text-muted-foreground">Packet Masking (FinalMask)</h4>
                        <p className="text-xs text-muted-foreground">Mask packet headers for anti-detection</p>
                    </div>
                    <Switch
                        checked={showFinalMask}
                        onCheckedChange={(checked) => {
                            setShowFinalMask(checked)
                            if (!checked) form.setValue("finalmask", null, { shouldDirty: true })
                        }}
                    />
                </div>
                {showFinalMask && (
                    <FinalMaskEditor
                        value={finalmask}
                        onChange={(next) => form.setValue("finalmask", next, { shouldDirty: true })}
                    />
                )}
            </div>
        </div>
    )
}
