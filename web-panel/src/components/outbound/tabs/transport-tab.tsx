import { type UseFormReturn } from "react-hook-form"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import { NETWORK_TYPES, SECURITY_TYPES } from "@/lib/types"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import { TransportForm } from "@/components/shared/transport-form"
import { TLSForm } from "@/components/shared/tls-form"
import { RealityForm } from "@/components/shared/reality-form"
import { Callout, FormSection } from "@/components/connection-dialog/section"
import { NestedErrors } from "@/components/connection-dialog/error-summary"
import { shortNetworkLabel } from "@/components/connection-dialog/labels"
import type { SectionVisibility } from "../use-outbound-form"

const REALITY_NETWORKS = ["tcp", "xhttp", "splithttp", "grpc"]

interface TransportTabProps {
    form: UseFormReturn<OutboundFormData>
    sectionVisibility: SectionVisibility
}

// Network, its transport settings and the security layer on one page,
// mirroring the remote server's inbound.
export function TransportTab({ form, sectionVisibility }: TransportTabProps) {
    const protocol = form.watch("protocol")
    const network = form.watch("network") || "tcp"
    const security = protocol === "hysteria2" ? "tls" : (form.watch("security") || "none")
    const transportSettings = form.watch("transport_settings")
    const tlsSettings = form.watch("tls_settings")
    const realitySettings = form.watch("reality_settings")

    const networkOptions = security === "reality"
        ? NETWORK_TYPES.filter((n) => REALITY_NETWORKS.includes(n.value))
        : NETWORK_TYPES
    const networkLabel = shortNetworkLabel(network)

    if (!sectionVisibility.network && protocol !== "hysteria2") {
        return (
            <Callout>
                {protocol === "wireguard"
                    ? "WireGuard carries its own encryption and runs straight over UDP. There are no network or TLS settings for it."
                    : "This outbound does not dial a remote server, so there are no network or TLS settings."}
            </Callout>
        )
    }

    return (
        <div className="space-y-7">
            {protocol === "hysteria2" && (
                <Callout>
                    Hysteria2 runs over QUIC with TLS built in. Only the TLS settings below apply.
                </Callout>
            )}

            {sectionVisibility.network && (
                <FormSection title="Stream">
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                        <FormField
                            control={form.control}
                            name="network"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Network</FormLabel>
                                    <Select value={field.value || "tcp"} onValueChange={field.onChange}>
                                        <FormControl>
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                        </FormControl>
                                        <SelectContent>
                                            {networkOptions.map((n) => (
                                                <SelectItem key={n.value} value={n.value}>{n.label}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                    <FormDescription className="text-xs">Must match the remote inbound.</FormDescription>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                        <FormField
                            control={form.control}
                            name="security"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Security</FormLabel>
                                    <Select
                                        value={field.value || "none"}
                                        onValueChange={(val) => {
                                            field.onChange(val)
                                            if (val === "reality" && !REALITY_NETWORKS.includes(form.getValues("network") || "tcp")) {
                                                form.setValue("network", "tcp", { shouldDirty: true })
                                            }
                                        }}
                                    >
                                        <FormControl>
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                        </FormControl>
                                        <SelectContent>
                                            {SECURITY_TYPES.map((s) => (
                                                <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                    <FormDescription className="text-xs">Reality only runs over TCP, XHTTP or gRPC.</FormDescription>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                    </div>
                </FormSection>
            )}

            {sectionVisibility.transportSettings && (
                <FormSection title={`${networkLabel} settings`}>
                    <TransportForm
                        network={network}
                        settings={transportSettings}
                        onChange={(s) => form.setValue("transport_settings", s, { shouldDirty: true })}
                    />
                </FormSection>
            )}

            {sectionVisibility.security && security === "tls" && (
                <FormSection title="TLS">
                    <TLSForm
                        settings={tlsSettings}
                        onChange={(s) => form.setValue("tls_settings", s, { shouldDirty: true })}
                        isOutbound
                    />
                    <NestedErrors errors={form.formState.errors.tls_settings} />
                </FormSection>
            )}

            {sectionVisibility.security && security === "reality" && (
                <FormSection title="Reality">
                    <RealityForm
                        settings={realitySettings}
                        onChange={(s) => form.setValue("reality_settings", s, { shouldDirty: true })}
                    />
                    <NestedErrors errors={form.formState.errors.reality_settings} />
                </FormSection>
            )}
        </div>
    )
}
