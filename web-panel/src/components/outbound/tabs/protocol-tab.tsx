import { type UseFormReturn } from "react-hook-form"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import type { DNSOutboundSettings } from "@/lib/types"
import { FreedomForm } from "@/components/shared/protocol-forms/freedom-form"
import { BlackholeForm } from "@/components/shared/protocol-forms/blackhole-form"
import { VMessOutboundForm } from "@/components/shared/protocol-forms/vmess-outbound-form"
import { VLESSForm } from "@/components/shared/protocol-forms/vless-form"
import { TrojanOutboundForm } from "@/components/shared/protocol-forms/trojan-outbound-form"
import { ShadowsocksForm } from "@/components/shared/protocol-forms/shadowsocks-form"
import { SOCKSForm } from "@/components/shared/protocol-forms/socks-form"
import { HTTPForm } from "@/components/shared/protocol-forms/http-form"
import { WireGuardForm } from "@/components/shared/protocol-forms/wireguard-form"
import { DNSOutboundForm } from "@/components/shared/protocol-forms/dns-outbound-form"
import { LoopbackForm } from "@/components/shared/protocol-forms/loopback-form"
import { HysteriaForm } from "@/components/shared/protocol-forms/hysteria-form"

interface ProtocolTabProps {
    form: UseFormReturn<OutboundFormData>
}

export function ProtocolTab({ form }: ProtocolTabProps) {
    const protocol = form.watch("protocol")
    const network = form.watch("network")
    const security = form.watch("security")

    return (
        <div className="space-y-4">
            {protocol === "freedom" && (
                <FreedomForm
                    settings={form.watch("freedom_settings")}
                    onChange={(s) => form.setValue("freedom_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "blackhole" && (
                <BlackholeForm
                    settings={form.watch("blackhole_settings")}
                    onChange={(s) => form.setValue("blackhole_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "vless" && (
                <VLESSForm
                    data={form.watch("vless_settings") || {}}
                    onChange={(s) => form.setValue("vless_settings", s, { shouldDirty: true })}
                    mode="outbound"
                    network={network}
                    security={security}
                />
            )}
            {protocol === "vmess" && (
                <VMessOutboundForm
                    settings={form.watch("vmess_settings")}
                    onChange={(s) => form.setValue("vmess_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "trojan" && (
                <TrojanOutboundForm
                    settings={form.watch("trojan_settings")}
                    onChange={(s) => form.setValue("trojan_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "shadowsocks" && (
                <ShadowsocksForm
                    settings={form.watch("shadowsocks_settings")}
                    onChange={(s) => form.setValue("shadowsocks_settings", s, { shouldDirty: true })}
                    isOutbound
                />
            )}
            {protocol === "socks" && (
                <SOCKSForm
                    settings={form.watch("socks_settings")}
                    onChange={(s) => form.setValue("socks_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "http" && (
                <HTTPForm
                    settings={form.watch("http_settings")}
                    onChange={(s) => form.setValue("http_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "wireguard" && (
                <WireGuardForm
                    settings={form.watch("wireguard_settings")}
                    onChange={(s) => form.setValue("wireguard_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "dns" && (
                <DNSOutboundForm
                    settings={form.watch("dns_settings") as DNSOutboundSettings | undefined}
                    onChange={(s) => form.setValue("dns_settings", s as OutboundFormData["dns_settings"], { shouldDirty: true })}
                />
            )}
            {protocol === "loopback" && (
                <LoopbackForm
                    settings={form.watch("loopback_settings")}
                    onChange={(s) => form.setValue("loopback_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "hysteria2" && (
                <HysteriaForm
                    settings={form.watch("hysteria_settings")}
                    onChange={(s) => form.setValue("hysteria_settings", s, { shouldDirty: true })}
                    isOutbound
                />
            )}
        </div>
    )
}
