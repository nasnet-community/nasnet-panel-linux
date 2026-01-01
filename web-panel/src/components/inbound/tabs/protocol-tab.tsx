import { type UseFormReturn } from "react-hook-form"
import { type InboundFormData } from "@/lib/validations/inbound-schema"
import { VLESSForm } from "@/components/shared/protocol-forms/vless-form"
import { ShadowsocksForm } from "@/components/shared/protocol-forms/shadowsocks-form"
import { WireGuardForm } from "@/components/shared/protocol-forms/wireguard-form"
import { HTTPForm } from "@/components/shared/protocol-forms/http-form"
import { SOCKSForm } from "@/components/shared/protocol-forms/socks-form"
import { DokodemoForm } from "@/components/shared/protocol-forms/dokodemo-form"
import { HysteriaForm } from "@/components/shared/protocol-forms/hysteria-form"
import { FallbacksForm } from "@/components/shared/protocol-forms/fallbacks-form"

interface ProtocolTabProps {
    form: UseFormReturn<InboundFormData>
}

export function ProtocolTab({ form }: ProtocolTabProps) {
    const protocol = form.watch("protocol")
    const network = form.watch("network")
    const security = form.watch("security")

    return (
        <div className="space-y-4">
            {protocol === "vless" && (
                <VLESSForm
                    data={form.watch("vless_settings") || {}}
                    onChange={(s) => form.setValue("vless_settings", s, { shouldDirty: true })}
                    mode="inbound"
                    network={network}
                    security={security}
                />
            )}
            {protocol === "trojan" && (
                <FallbacksForm
                    fallbacks={(form.watch("trojan_settings") as any)?.fallbacks || []}
                    onChange={(fallbacks) => {
                        const current = (form.getValues as any)("trojan_settings") || {}
                        form.setValue("trojan_settings" as any, { ...current, fallbacks }, { shouldDirty: true })
                    }}
                />
            )}
            {protocol === "shadowsocks" && (
                <ShadowsocksForm
                    settings={form.watch("shadowsocks_settings")}
                    onChange={(s) => form.setValue("shadowsocks_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "wireguard" && (
                <WireGuardForm
                    settings={form.watch("wireguard_settings")}
                    onChange={(s) => form.setValue("wireguard_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "http" && (
                <HTTPForm
                    settings={form.watch("http_settings")}
                    onChange={(s) => form.setValue("http_settings", s, { shouldDirty: true })}
                />
            )}
            {(protocol === "socks" || protocol === "mixed") && (
                <SOCKSForm
                    settings={form.watch("socks_settings")}
                    onChange={(s) => form.setValue("socks_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "dokodemo-door" && (
                <DokodemoForm
                    settings={form.watch("dokodemo_settings")}
                    onChange={(s) => form.setValue("dokodemo_settings", s, { shouldDirty: true })}
                />
            )}
            {protocol === "hysteria2" && (
                <HysteriaForm
                    settings={form.watch("hysteria_settings")}
                    onChange={(s) => form.setValue("hysteria_settings", s, { shouldDirty: true })}
                />
            )}
        </div>
    )
}
