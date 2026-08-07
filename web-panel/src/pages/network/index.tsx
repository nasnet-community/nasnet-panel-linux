import { useCallback, useMemo, useRef, useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/ui/empty-state"
import { Skeleton } from "@/components/ui/skeleton"
import { useEventListener } from "@/components/providers/events-provider"
import { ApplyDialog } from "@/components/network/apply-dialog"
import { InterfaceTable, type RoleChoice } from "@/components/network/interface-table"
import {
    useApplyNetworkChange,
    useNetworkInterfaces,
    useNetworkState,
    usePlanNetworkChange,
} from "@/lib/queries/use-network"
import { queryKeys } from "@/lib/queries/keys"
import { useQueryClient } from "@tanstack/react-query"
import type { AssignRoleRequest, NetworkInterfaceView } from "@/lib/types/network"

const NETWORK_EVENTS = new Set([
    "interface.added",
    "interface.removed",
    "interface.link_changed",
    "wan.up",
    "wan.down",
    "wan.failover",
    "wan.apply_rolled_back",
    "wan.lease_warning",
])

export default function NetworkPage() {
    const qc = useQueryClient()
    const state = useNetworkState()
    const interfaces = useNetworkInterfaces()
    const plan = usePlanNetworkChange()
    const apply = useApplyNetworkChange()

    const [dialogOpen, setDialogOpen] = useState(false)
    const [pending, setPending] = useState<AssignRoleRequest | null>(null)
    const debounce = useRef<ReturnType<typeof setTimeout> | null>(null)

    // Debounced so a burst of link events is one refetch.
    useEventListener(
        useCallback(
            (type: string) => {
                if (!NETWORK_EVENTS.has(type)) return
                if (debounce.current) clearTimeout(debounce.current)
                debounce.current = setTimeout(() => {
                    void qc.invalidateQueries({ queryKey: queryKeys.network })
                }, 400)
            },
            [qc],
        ) as never,
    )

    const altOrigin = useMemo(() => {
        const lan = state.data?.uplinks.find((u) => u.slot === "domestic")
        const addr = lan?.addrs?.[0]?.split("/")[0]
        return addr ? `${window.location.protocol}//${addr}:${window.location.port}` : ""
    }, [state.data])

    // Router mode off 404s every route, so an error means "hide the section".
    if (state.isError) {
        return (
            <EmptyState
                title="Router mode is not enabled"
                description="This box is running as a VPN panel only. Router mode is enabled by nasnet-tool at install time."
            />
        )
    }

    if (state.isLoading || interfaces.isLoading) {
        return <Skeleton className="h-64 w-full" />
    }

    function onAssign(iface: NetworkInterfaceView, choice: RoleChoice) {
        const req: AssignRoleRequest = {
            interface_id: iface.id,
            role: choice.role,
            slot: choice.slot,
        }
        setPending(req)
        plan.mutate(req)
        setDialogOpen(true)
    }

    return (
        <div className="space-y-6">
            <div>
                <h1 className="text-2xl font-semibold">Network</h1>
                <p className="text-muted-foreground text-sm">
                    Assign each interface a role. Changes apply behind a 90-second confirmation and
                    revert themselves if you lose access.
                </p>
            </div>

            {state.data && !state.data.takeover_done && (
                <Alert>
                    <AlertDescription>
                        Network not managed by nasnet yet — assign roles to finish setup.
                    </AlertDescription>
                </Alert>
            )}

            {state.data?.warnings.map((w) => (
                <Alert key={w}>
                    <AlertDescription>{w}</AlertDescription>
                </Alert>
            ))}

            <Card>
                <CardHeader>
                    <CardTitle>Interfaces</CardTitle>
                    <CardDescription>
                        WireGuard and hysteria2 inbounds are reachable on the domestic uplink only.
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <InterfaceTable
                        interfaces={interfaces.data ?? []}
                        onAssign={onAssign}
                        disabled={apply.isPending}
                    />
                </CardContent>
            </Card>

            <ApplyDialog
                open={dialogOpen}
                onOpenChange={setDialogOpen}
                plan={plan.data ?? null}
                planning={plan.isPending}
                applied={apply.data ?? null}
                altOrigin={altOrigin}
                onApply={() => pending && apply.mutate(pending)}
                onDone={() => {
                    plan.reset()
                    apply.reset()
                    setPending(null)
                    void qc.invalidateQueries({ queryKey: queryKeys.network })
                }}
            />
        </div>
    )
}
