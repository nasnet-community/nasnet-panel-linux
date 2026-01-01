import { type UseFormReturn } from "react-hook-form"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import { TransportForm } from "@/components/shared/transport-form"

interface TransportTabProps {
    form: UseFormReturn<OutboundFormData>
}

export function TransportTab({ form }: TransportTabProps) {
    const network = form.watch("network")
    const settings = form.watch("transport_settings")

    return (
        <div className="space-y-4">
            <TransportForm
                network={network || "tcp"}
                settings={settings}
                onChange={(s) => form.setValue("transport_settings", s, { shouldDirty: true })}
            />
        </div>
    )
}
