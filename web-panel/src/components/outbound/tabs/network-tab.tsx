import { type UseFormReturn, useWatch } from "react-hook-form"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import { NETWORK_TYPES, SECURITY_TYPES } from "@/lib/types"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
    FormControl,
    FormField,
    FormItem,
    FormLabel,
} from "@/components/ui/form"

const REALITY_NETWORKS = ['tcp', 'xhttp', 'splithttp', 'grpc']

interface NetworkTabProps {
    form: UseFormReturn<OutboundFormData>
}

export function NetworkTab({ form }: NetworkTabProps) {
    const security = useWatch({ control: form.control, name: "security" })
    const filteredNetworks = security === "reality" ? NETWORK_TYPES.filter(n => REALITY_NETWORKS.includes(n.value)) : NETWORK_TYPES

    return (
        <div className="space-y-6">
            {security === "reality" && (
                <p className="text-xs text-amber-500">Reality only supports TCP, XHTTP, and gRPC networks.</p>
            )}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <FormField
                    control={form.control}
                    name="network"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Network</FormLabel>
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
                                    {filteredNetworks.map((n) => (
                                        <SelectItem key={n.value} value={n.value}>
                                            {n.label}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
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
                                    if (val === "reality") {
                                        const currentNetwork = form.getValues("network")
                                        if (!currentNetwork || !REALITY_NETWORKS.includes(currentNetwork)) {
                                            form.setValue("network", "tcp")
                                        }
                                    }
                                }}
                            >
                                <FormControl>
                                    <SelectTrigger className="w-full">
                                        <SelectValue />
                                    </SelectTrigger>
                                </FormControl>
                                <SelectContent>
                                    {SECURITY_TYPES.map((s) => (
                                        <SelectItem key={s.value} value={s.value}>
                                            {s.label}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </FormItem>
                    )}
                />
            </div>
        </div>
    )
}
