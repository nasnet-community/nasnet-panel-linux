import { useState } from "react"
import { ShieldAlert, TriangleAlert } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { VpnProfiles } from "@/components/network/vpn-profiles"
import { VpnStatusCard } from "@/components/network/vpn-status-card"
import { useActivateVPN, useDeactivateVPN, useVPNProfiles, useVPNStatus } from "@/lib/queries/use-network"
import { verdictsFromError } from "@/lib/api/network"
import type { Verdict } from "@/lib/types/network"

interface Props {
    /** A change is already armed, so a second one must wait for it. */
    armed: boolean
    onApplied: () => void
}

export function VpnTab({ armed, onApplied }: Props) {
    const profiles = useVPNProfiles()
    const status = useVPNStatus()
    const activate = useActivateVPN()
    const deactivate = useDeactivateVPN()
    const [verdicts, setVerdicts] = useState<Verdict[]>([])

    async function run(fn: () => Promise<unknown>) {
        setVerdicts([])
        try {
            await fn()
            onApplied()
        } catch (err) {
            setVerdicts(verdictsFromError(err))
        }
    }

    const st = status.data
    const on = st?.active_profile_id != null

    return (
        <div className="space-y-4">
            {/* The kill switch is not a setting, so it is stated rather than
                offered — and what it is doing right now differs by state. */}
            {!on ? (
                <Alert variant="warning">
                    <ShieldAlert className="h-4 w-4" />
                    <AlertDescription>
                        Nothing goes out over the secondary uplink until a VPN is turned on. That
                        uplink never carries traffic in the open, so with no VPN it carries nothing.
                    </AlertDescription>
                </Alert>
            ) : (
                !st?.connected && (
                    <Alert variant="warning">
                        <TriangleAlert className="h-4 w-4" />
                        <AlertDescription>
                            The tunnel is not answering, so traffic bound for the secondary uplink
                            is being dropped. It resumes on its own when the tunnel comes back.
                        </AlertDescription>
                    </Alert>
                )
            )}

            {status.isError && (
                <Alert variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        The VPN status could not be read. {status.error?.message}
                    </AlertDescription>
                </Alert>
            )}

            {verdicts.map((v) => (
                <Alert key={v.rule + v.message} variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        <span className="text-text-tertiary font-mono text-xs">{v.rule}</span>{" "}
                        {v.message}
                    </AlertDescription>
                </Alert>
            ))}

            <VpnStatusCard status={st} loading={status.isLoading} />

            <VpnProfiles
                profiles={profiles.data}
                loading={profiles.isLoading}
                armed={armed}
                busy={activate.isPending || deactivate.isPending}
                onActivate={(id) => void run(() => activate.mutateAsync(id))}
                onDeactivate={() => void run(() => deactivate.mutateAsync())}
            />
        </div>
    )
}
