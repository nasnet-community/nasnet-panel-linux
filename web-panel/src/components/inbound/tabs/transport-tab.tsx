import { type UseFormReturn } from "react-hook-form"
import { type InboundFormData } from "@/lib/validations/inbound-schema"
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
import { RAW_ONLY_PROTOCOLS, type SectionVisibility } from "../use-inbound-form"

const REALITY_NETWORKS = ["tcp", "xhttp", "splithttp", "grpc"]

interface TransportTabProps {
    form: UseFormReturn<InboundFormData>
    sectionVisibility: SectionVisibility
}

// Network, its transport settings and the security layer on one page. They
// used to be three tabs, two of which only appeared after choices made on the
// third.
export function TransportTab({ form, sectionVisibility }: TransportTabProps) {
    const protocol = form.watch("protocol")
    const network = form.watch("network")
    const security = form.watch("security")
    const transportSettings = form.watch("transport_settings")
    const tlsSettings = form.watch("tls_settings")
    const realitySettings = form.watch("reality_settings")

    const rawOnly = RAW_ONLY_PROTOCOLS.includes(protocol)
    const networkOptions = rawOnly
        ? NETWORK_TYPES.filter((n) => n.value === "tcp")
        : security === "reality"
            ? NETWORK_TYPES.filter((n) => REALITY_NETWORKS.includes(n.value))
            : NETWORK_TYPES
    const securityOptions = rawOnly
        ? SECURITY_TYPES.filter((s) => s.value !== "reality")
        : SECURITY_TYPES

    const networkLabel = shortNetworkLabel(network)

    if (protocol === "wireguard") {
        return (
            <Callout>
                WireGuard carries its own encryption and runs straight over UDP. There are no network or TLS settings for it.
            </Callout>
        )
    }

    return (
        <div className="space-y-7">
            {protocol === "hysteria2" && (
                <Callout>
                    Hysteria2 runs over QUIC with TLS built in. Only the certificate settings below apply.
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
                                    <Select value={field.value} onValueChange={field.onChange} disabled={rawOnly}>
                                        <FormControl>
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                        </FormControl>
                                        <SelectContent>
                                            {networkOptions.map((n) => (
                                                <SelectItem key={n.value} value={n.value}>{n.label}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                    <FormDescription className="text-xs">
                                        {rawOnly
                                            ? `${protocol} reads raw bytes, so it only runs over TCP.`
                                            : "How bytes travel: raw TCP, WebSocket, gRPC, XHTTP…"}
                                    </FormDescription>
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
                                        value={field.value}
                                        onValueChange={(val) => {
                                            field.onChange(val)
                                            if (val === "reality" && !REALITY_NETWORKS.includes(form.getValues("network"))) {
                                                form.setValue("network", "tcp", { shouldDirty: true })
                                            }
                                        }}
                                    >
                                        <FormControl>
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                        </FormControl>
                                        <SelectContent>
                                            {securityOptions.map((s) => (
                                                <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                    <FormDescription className="text-xs">
                                        {rawOnly ? "TLS termination is fine here; Reality is not." : "Reality only runs over TCP, XHTTP or gRPC."}
                                    </FormDescription>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                    </div>
                    {security === "none" && !rawOnly && (
                        <Callout tone="warning">
                            No TLS or Reality on this listener. Keep it only if a proxy in front terminates TLS or the protocol encrypts on its own.
                        </Callout>
                    )}
                </FormSection>
            )}

            {sectionVisibility.transportSettings && (
                <FormSection title={`${networkLabel} settings`}>
                    <TransportForm
                        network={network || "tcp"}
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
                    />
                    <NestedErrors errors={form.formState.errors.tls_settings} />
                </FormSection>
            )}

            {sectionVisibility.security && security === "reality" && (
                <FormSection title="Reality">
                    <RealityForm
                        settings={realitySettings}
                        onChange={(s) => form.setValue("reality_settings", s, { shouldDirty: true })}
                        isInbound
                    />
                    <NestedErrors errors={form.formState.errors.reality_settings} />
                </FormSection>
            )}
        </div>
    )
}
