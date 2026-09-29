// Dispatches the Configuration tab to the bespoke per-protocol form. Each form
// is a controlled {value,onChange} component editing the protocol's settings
// slot; the create/edit dialog bridges that slot to react-hook-form.
import type { CoreProtocol, CoreSettings, SshSettings, MtprotoSettings } from "@/lib/types/cores"
import { SshForm } from "./ssh-form"
import { MtprotoForm } from "./mtproto-form"

export function ProtocolConfigForm({
    protocol,
    value,
    onChange,
    builtIn,
    errors,
}: {
    protocol: CoreProtocol
    value: CoreSettings
    onChange: (next: CoreSettings) => void
    /** the tunnel lives on the panel's built-in node */
    builtIn: boolean
    /** field-name → message, extracted from the settings slot's zod errors */
    errors?: Record<string, string>
}) {
    const change = onChange as (v: CoreSettings) => void
    switch (protocol) {
        case "ssh":
            return <SshForm value={value as SshSettings} onChange={change as (v: SshSettings) => void} />
        case "mtproto":
            return (
                <MtprotoForm
                    value={value as MtprotoSettings}
                    onChange={change as (v: MtprotoSettings) => void}
                    builtIn={builtIn}
                    errors={errors}
                />
            )
        default:
            return null
    }
}
