import { type UseFormReturn } from "react-hook-form"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import { TLSForm } from "@/components/shared/tls-form"
import { RealityForm } from "@/components/shared/reality-form"

interface SecurityTabProps {
    form: UseFormReturn<OutboundFormData>
}

export function SecurityTab({ form }: SecurityTabProps) {
    const security = form.watch("security")
    const tlsSettings = form.watch("tls_settings")
    const realitySettings = form.watch("reality_settings")

    return (
        <div className="space-y-4">
            {security === "tls" && (
                <TLSForm
                    settings={tlsSettings}
                    onChange={(s) => form.setValue("tls_settings", s, { shouldDirty: true })}
                    isOutbound
                />
            )}
            {security === "reality" && (
                <RealityForm
                    settings={realitySettings}
                    onChange={(s) => form.setValue("reality_settings", s, { shouldDirty: true })}
                />
            )}
        </div>
    )
}
