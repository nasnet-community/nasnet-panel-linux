import { useMemo, useState } from "react"
import { type UseFormReturn } from "react-hook-form"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import { SockoptForm } from "@/components/shared/sockopt-form"
import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { FinalMaskEditor } from "@/components/shared/finalmask-editor"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
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
    const [showProxy, setShowProxy] = useState(!!proxy.tag)
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
        <div className="space-y-6">
            {/* Mux */}
            <div className="space-y-4">
                <div className="flex items-center justify-between">
                    <div>
                        <h4 className="text-sm font-medium">Mux (Multiplexing)</h4>
                        <p className="text-xs text-muted-foreground">Multiplex connections over a single TCP link</p>
                    </div>
                    <Switch
                        checked={mux.enabled ?? false}
                        onCheckedChange={(checked) => updateMux({ enabled: checked })}
                    />
                </div>
                {mux.enabled && (
                    <div className="space-y-4 rounded-md border p-4 bg-muted/20">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
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
                                <SelectTrigger className="w-full">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="reject">Reject</SelectItem>
                                    <SelectItem value="allow">Allow</SelectItem>
                                    <SelectItem value="skip">Skip</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                )}
            </div>

            {/* Proxy Chaining */}
            <div className="space-y-4">
                <button
                    type="button"
                    className="text-xs text-primary hover:underline"
                    onClick={() => {
                        setShowProxy(!showProxy)
                        if (showProxy) updateProxy({ tag: "", transportLayer: false })
                    }}
                >
                    {showProxy ? "Hide" : "Show"} Proxy Chaining
                </button>
                {showProxy && (
                    <div className="space-y-4 rounded-md border p-4 bg-muted/20">
                        <div className="space-y-2">
                            <Label>Outbound Tag</Label>
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
                                <p className="text-xs text-muted-foreground italic">No other outbounds available to chain to</p>
                            )}
                            <p className="text-xs text-muted-foreground">Forward traffic through another outbound</p>
                        </div>
                        <div className="flex items-center space-x-2">
                            <Switch
                                id="transportLayer"
                                checked={proxy.transportLayer ?? false}
                                onCheckedChange={(c) => updateProxy({ transportLayer: c })}
                            />
                            <Label htmlFor="transportLayer">Transport Layer</Label>
                        </div>
                        <p className="text-xs text-muted-foreground">
                            When enabled, proxies raw TCP through the target outbound&apos;s transport instead of the proxy protocol layer
                        </p>
                    </div>
                )}
            </div>

            {/* Socket Options */}
            <div className="space-y-4">
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
