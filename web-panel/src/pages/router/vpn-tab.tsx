import { useState } from "react"
import { toast } from "sonner"
import { ShieldAlert, TriangleAlert } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { VpnPoolCard } from "@/components/network/vpn-pool-card"
import { VpnPoolTable } from "@/components/network/vpn-pool-table"
import {
    useDisableVPNProfile,
    useEnableVPNProfile,
    useVPNProfiles,
    useVPNStatus,
} from "@/lib/queries/use-network"
import { useRouterHealth } from "@/lib/queries/use-router-health"
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
    const health = useRouterHealth()
    const enable = useEnableVPNProfile()
    const disable = useDisableVPNProfile()
    const [verdicts, setVerdicts] = useState<Verdict[]>([])

    async function run(fn: () => Promise<{ verdicts?: Verdict[] } | unknown>, done: string) {
        setVerdicts([])
        try {
            const res = await fn()
            // Warnings arrive with a 200; the error path never sees them.
            const ok = res as { verdicts?: Verdict[] } | undefined
            setVerdicts(ok?.verdicts ?? [])
            toast.success(done)
            onApplied()
        } catch (err) {
            // Verdicts render as alerts; anything else would vanish silently.
            const vs = verdictsFromError(err)
            setVerdicts(vs)
            if (vs.length === 0) {
                toast.error(err instanceof Error ? err.message : "The change did not apply")
            }
        }
    }

    function name(id: number): string {
        return profiles.data?.find((p) => p.id === id)?.name ?? "The VPN"
    }

    const st = status.data
    const tunnels = st?.tunnels ?? []
    // Unread status is not an empty pool: claiming either way would be a guess.
    const known = !status.isLoading && !status.isError && !!st
    const on = tunnels.length > 0
    const connected = tunnels.some((t) => t.connected)

    return (
        <div className="space-y-4">
            {/* The kill switch is not a setting, so it is stated rather than
                offered — and what it is doing right now differs by state. */}
            {known && !on ? (
                <Alert variant="warning">
                    <ShieldAlert className="h-4 w-4" />
                    <AlertDescription>
                        Nothing goes out over the secondary uplink until a VPN is turned on. That
                        uplink never carries traffic in the open, so with no VPN it carries nothing.
                    </AlertDescription>
                </Alert>
            ) : (
                known &&
                !connected && (
                    <Alert variant="warning">
                        <TriangleAlert className="h-4 w-4" />
                        <AlertDescription>
                            None of the pool&apos;s tunnels are answering, so traffic bound for the
                            secondary uplink is being dropped. It resumes on its own when one comes
                            back.
                        </AlertDescription>
                    </Alert>
                )
            )}

            {profiles.isError && (
                <Alert variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        The VPN profiles could not be read. {profiles.error?.message}
                    </AlertDescription>
                </Alert>
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

            <VpnPoolCard status={st} loading={status.isLoading} />

            <VpnPoolTable
                profiles={profiles.data}
                loading={profiles.isLoading}
                tunnels={tunnels}
                health={health.data?.vpn?.tunnels ?? []}
                armed={armed}
                busy={enable.isPending || disable.isPending}
                onEnable={(id) =>
                    void run(() => enable.mutateAsync(id), `${name(id)} added to the pool`)
                }
                onDisable={(id) =>
                    void run(() => disable.mutateAsync(id), `${name(id)} removed from the pool`)
                }
            />
        </div>
    )
}
