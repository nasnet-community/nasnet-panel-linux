import { useMemo, useState } from "react"
import { type UseFormReturn } from "react-hook-form"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import { SockoptForm } from "@/components/shared/sockopt-form"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { FinalMaskEditor } from "@/components/shared/finalmask-editor"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Disclosure, FormSection, SwitchRow } from "@/components/connection-dialog/section"
import type { MuxSettings, ProxySettingsConfig, FinalMask, Outbound } from "@/lib/types"

interface AdvancedTabProps {
    form: UseFormReturn<OutboundFormData>
    allOutbounds?: Outbound[]
    currentTag?: string
}

export function AdvancedTab({ form, allOutbounds = [], currentTag }: AdvancedTabProps) {
    const sockopt = form.watch("sockopt_settings")
    const mux = (form.watch("mux_settings") || {}) as MuxSettings
    const proxy = (form.watch("proxy_settings") || {}) as ProxySettingsConfig
    const finalmask = (form.watch("finalmask") || {}) as FinalMask
    const sendThrough = form.watch("send_through") || ""
    const [showFinalMask, setShowFinalMask] = useState(!!finalmask.tcp || !!finalmask.udp || !!finalmask.quicParams)

    const chainableOutbounds = useMemo(() =>
        allOutbounds.filter(o => o.tag !== currentTag && !o.is_disabled),
        [allOutbounds, currentTag]
    )

    const updateMux = (updates: Partial<MuxSettings>) => {
        form.setValue("mux_settings", { ...mux, ...updates }, { shouldDirty: true })
    }

    const updateProxy = (updates: Partial<ProxySettingsConfig>) => {
        form.setValue("proxy_settings", { ...proxy, ...updates }, { shouldDirty: true })
    }

    return (
        <div className="space-y-7">
            <FormSection title="Mux (multiplexing)">
                <SwitchRow
                    label="Enable Mux"
                    help="Multiplex connections over a single TCP link."
                    checked={mux.enabled ?? false}
                    onCheckedChange={(checked) => updateMux({ enabled: checked })}
                />
                {mux.enabled && (
                    <div className="space-y-4 rounded-md border bg-muted/20 p-4">
                        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                            <div className="space-y-2">
                                <Label>Concurrency</Label>
                                <Input
                                    type="number"
                                    placeholder="8"
                                    value={mux.concurrency ?? ""}
                                    onChange={(e) => updateMux({ concurrency: parseInt(e.target.value) || 0 })}
                                />
                                <p className="text-xs text-muted-foreground">-1 to 1024</p>
                            </div>
                            <div className="space-y-2">
                                <Label>XUDP Concurrency</Label>
                                <Input
                                    type="number"
                                    placeholder="8"
                                    value={mux.xudpConcurrency ?? ""}
                                    onChange={(e) => updateMux({ xudpConcurrency: parseInt(e.target.value) || 0 })}
                                />
                            </div>
                        </div>
                        <div className="space-y-2">
                            <Label>XUDP Proxy UDP443</Label>
                            <Select
                                value={mux.xudpProxyUDP443 || "reject"}
                                onValueChange={(v) => updateMux({ xudpProxyUDP443: v })}
                            >
                                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="reject">Reject</SelectItem>
                                    <SelectItem value="allow">Allow</SelectItem>
                                    <SelectItem value="skip">Skip</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                )}
            </FormSection>

            <FormSection title="Routing">
                <Disclosure
                    title="Proxy chaining"
                    summary={proxy.tag ? `via ${proxy.tag}` : "off"}
                    defaultOpen={!!proxy.tag}
                    onOpenChange={(open) => { if (!open && proxy.tag) updateProxy({ tag: "", transportLayer: false }) }}
                >
                    <div className="space-y-2">
                        <Label>Outbound tag</Label>
                        {chainableOutbounds.length > 0 ? (
                            <Select
                                value={proxy.tag || ""}
                                onValueChange={(v) => updateProxy({ tag: v })}
                            >
                                <SelectTrigger className="w-full">
                                    <SelectValue placeholder="Select an outbound" />
                                </SelectTrigger>
                                <SelectContent>
                                    {chainableOutbounds.map((o) => (
                                        <SelectItem key={o.tag} value={o.tag}>
                                            {o.tag}{o.remark ? ` (${o.remark})` : ""} — {o.protocol}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        ) : (
                            <p className="text-xs italic text-muted-foreground">No other outbounds available to chain to</p>
                        )}
                        <p className="text-xs text-muted-foreground">Forward this outbound's traffic through another outbound. Collapsing clears it.</p>
                    </div>
                    <SwitchRow
                        id="transportLayer"
                        label="Transport layer"
                        help="Proxy raw TCP through the target outbound's transport instead of the proxy protocol layer."
                        checked={proxy.transportLayer ?? false}
                        onCheckedChange={(c) => updateProxy({ transportLayer: c })}
                    />
                </Disclosure>

                <div className="space-y-2">
                    <Label htmlFor="send-through">Send through <span className="text-xs font-normal text-text-tertiary">optional</span></Label>
                    <Input
                        id="send-through"
                        placeholder="0.0.0.0"
                        value={sendThrough}
                        onChange={(e) => form.setValue("send_through", e.target.value, { shouldDirty: true })}
                    />
                    <p className="text-xs text-muted-foreground">Local address to bind outgoing connections to. Leave empty for the default route.</p>
                </div>
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
