import { type UseFormReturn } from "react-hook-form"
import { type InboundFormData } from "@/lib/validations/inbound-schema"
import { TLSForm } from "@/components/shared/tls-form"
import { RealityForm } from "@/components/shared/reality-form"

interface SecurityTabProps {
    form: UseFormReturn<InboundFormData>
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
                />
            )}
            {security === "reality" && (
                <RealityForm
                    settings={realitySettings}
                    onChange={(s) => form.setValue("reality_settings", s, { shouldDirty: true })}
                    isInbound
                />
            )}
        </div>
    )
}
