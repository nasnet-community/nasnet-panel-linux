import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { Link } from "react-router"
import { Info, Network, RefreshCw, TriangleAlert, Waypoints } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/ui/empty-state"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { HealthStrip } from "@/components/router/health-strip"
import { LanTab } from "@/pages/router/lan-tab"
import { PortForwardsTab } from "@/pages/router/port-forwards-tab"
import { VpnTab } from "@/pages/router/vpn-tab"
import { useEventListener } from "@/components/providers/events-provider"
import { ApplyDialog } from "@/components/network/apply-dialog"
import { ArmedChangeBar } from "@/components/network/armed-change-bar"
import { RoleBays } from "@/components/network/role-bays"
import {
    buildAssignRequest,
    InterfaceTable,
    type RoleChoice,
} from "@/components/network/interface-table"
import {
    useApplyNetworkChange,
    useLAN,
    useNetworkInterfaces,
    useNetworkState,
    usePlanNetworkChange,
    usePortForwards,
} from "@/lib/queries/use-network"
import { remainingSeconds } from "@/lib/api/network"
import { uncoveredWarnings } from "@/lib/network-labels"
import { queryKeys } from "@/lib/queries/keys"
import { cn } from "@/lib/utils"
import { useQueryClient } from "@tanstack/react-query"
import type { AssignRoleRequest, NetworkInterfaceView } from "@/lib/types/network"

const NETWORK_EVENTS = new Set([
    "interface.added",
    "interface.removed",
    "interface.link_changed",
    "wan.up",
    "wan.down",
    "wan.failover",
    "wan.failover_lost",
    "wan.failover_restored",
    "wan.force_state",
    "wan.apply_rolled_back",
    "wan.lease_warning",
    "vpn.up",
    "vpn.down",
    "vpn.degraded",
    "vpn.pool_changed",
    "wan.applied",
])

function useFreshness(updatedAt: number | undefined) {
    const [label, setLabel] = useState("")
    useEffect(() => {
        if (!updatedAt) {
            setLabel("")
            return
        }
        const tick = () => {
            const s = Math.floor((Date.now() - updatedAt) / 1000)
            if (s < 5) setLabel("just now")
            else if (s < 60) setLabel(`${s}s ago`)
            else setLabel(`${Math.floor(s / 60)}m ago`)
        }
        tick()
        const id = setInterval(tick, 5000)
        return () => clearInterval(id)
    }, [updatedAt])
    return label
}

/** A live figure or state dot next to a tab label, so closed tabs still tell. */
function TabMeta({ children }: { children: ReactNode }) {
    return (
        <span className="text-text-tertiary group-data-[state=active]:text-text-secondary text-xs tabular-nums transition-colors">
            {children}
        </span>
    )
}

function PageSkeleton() {
    return (
        <div className="space-y-6">
            <Skeleton className="h-16 w-full max-w-lg" />
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                {[0, 1, 2, 3].map((i) => (
                    <Skeleton key={i} className="h-28 w-full" />
                ))}
            </div>
            <Skeleton className="h-64 w-full" />
        </div>
    )
}

export default function NetworkPage() {
    const qc = useQueryClient()
    const state = useNetworkState()
    const interfaces = useNetworkInterfaces()
    const plan = usePlanNetworkChange()
    const apply = useApplyNetworkChange()
    // Fetched here so the tab labels can carry live meta; the tabs share the cache.
    const lan = useLAN()
    const forwards = usePortForwards()

    const [dialogOpen, setDialogOpen] = useState(false)
    const [pending, setPending] = useState<AssignRoleRequest | null>(null)
    const debounce = useRef<ReturnType<typeof setTimeout> | null>(null)

    const refresh = useCallback(() => {
        void qc.invalidateQueries({ queryKey: queryKeys.network })
    }, [qc])

    // Debounced so a burst of link events is one refetch.
    useEventListener(
        useCallback(
            (type: string) => {
                if (!NETWORK_EVENTS.has(type)) return
                if (debounce.current) clearTimeout(debounce.current)
                debounce.current = setTimeout(refresh, 400)
            },
            [refresh],
        ) as never,
    )

    const altOrigin = useMemo(() => {
        const lan = state.data?.uplinks?.find((u) => u.slot === "domestic")
        const addr = lan?.addrs?.[0]?.split("/")[0]
        return addr ? `${window.location.protocol}//${addr}:${window.location.port}` : ""
    }, [state.data])

    const freshness = useFreshness(state.dataUpdatedAt)

    // Router mode off 404s every route, so an error means "hide the section".
    if (state.isError) {
        return (
            <EmptyState
                icon={Network}
                title="Router mode is not enabled"
                description="This box is running as a VPN panel only. Router mode is enabled by nasnet-tool at install time."
            />
        )
    }

    if (state.isLoading || interfaces.isLoading) {
        return <PageSkeleton />
    }

    function onAssign(iface: NetworkInterfaceView, choice: RoleChoice) {
        const req = buildAssignRequest(interfaces.data ?? [], iface, choice)
        setPending(req)
        plan.mutate(req)
        setDialogOpen(true)
    }

    const rows = interfaces.data ?? []
    const uplinkCount = state.data?.uplinks?.length ?? 0
    const setupPending = !!state.data && !state.data.takeover_done
    const armed =
        !!state.data?.pending_plan_id && remainingSeconds(state.data.confirm_deadline_unix) > 0
    const warnings = uncoveredWarnings(state.data?.warnings)

    return (
        <div className="mx-auto max-w-6xl space-y-6">
            <div className="flex flex-wrap items-start justify-between gap-4">
                <div className="max-w-2xl">
                    <h1 className="text-3xl font-semibold">Router</h1>
                    <p className="text-text-secondary text-base">
                        Changes preview first and revert if you lose access.
                    </p>
                </div>
                <div className="flex items-center gap-3">
                    {freshness && (
                        <span className="text-text-tertiary text-sm tabular-nums">
                            updated {freshness}
                        </span>
                    )}
                    <Button variant="outline" size="sm" asChild>
                        <Link to="/router/flow">
                            <Waypoints className="mr-1.5 h-3.5 w-3.5" />
                            Traffic flow
                        </Link>
                    </Button>
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={refresh}
                        disabled={state.isFetching || interfaces.isFetching}
                    >
                        <RefreshCw
                            className={cn(
                                "mr-1.5 h-3.5 w-3.5",
                                (state.isFetching || interfaces.isFetching) && "animate-spin",
                            )}
                        />
                        Refresh
                    </Button>
                </div>
            </div>

            {armed && state.data && (
                <ArmedChangeBar
                    planId={state.data.pending_plan_id}
                    deadlineUnix={state.data.confirm_deadline_unix}
                    altOrigin={altOrigin}
                    onSettled={refresh}
                />
            )}

            {setupPending && (
                <Alert variant="info">
                    <Info className="h-4 w-4" />
                    <AlertTitle>Finish setup</AlertTitle>
                    <AlertDescription className="text-text-secondary">
                        nasnet is not managing this network yet. Assign a domestic uplink and a LAN
                        port to hand routing over.
                    </AlertDescription>
                </Alert>
            )}

            {/* One advisory at a time: during setup the empty bays say this already. */}
            {!setupPending && uplinkCount < 2 && (
                <Alert variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        {uplinkCount === 0
                            ? "No uplink assigned — traffic has nowhere to go. Assign the port your ISP plugs into."
                            : "One uplink assigned — no failover and no split routing. Add a secondary uplink to get both."}
                    </AlertDescription>
                </Alert>
            )}

            {warnings.map((w) => (
                <Alert key={w} variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>{w}</AlertDescription>
                </Alert>
            ))}

            {/* First thing an operator checks; lives above the tabs. */}
            {!setupPending && uplinkCount > 0 && <HealthStrip />}

            <Tabs defaultValue="ports" className="space-y-6">
                <TabsList variant="line">
                    <TabsTrigger value="ports">
                        Ports
                        {/* Same filter as the table, so the count matches what opens. */}
                        <TabMeta>{rows.filter((r) => r.assignable).length}</TabMeta>
                    </TabsTrigger>
                    <TabsTrigger value="lan">
                        Local network
                        {lan.data && !lan.data.enabled && (
                            <TabMeta>
                                <span className="text-status-warning">off</span>
                            </TabMeta>
                        )}
                    </TabsTrigger>
                    <TabsTrigger value="vpn">
                        VPN
                        {state.data?.vpn.active && (
                            <span
                                aria-hidden
                                className={cn(
                                    "size-1.5 rounded-full",
                                    state.data.vpn.connected
                                        ? "bg-status-success"
                                        : "bg-status-danger",
                                )}
                            />
                        )}
                    </TabsTrigger>
                    <TabsTrigger value="forwards">
                        Port forwards
                        {(forwards.data?.length ?? 0) > 0 && (
                            <TabMeta>{forwards.data!.length}</TabMeta>
                        )}
                    </TabsTrigger>
                </TabsList>

                <TabsContent value="ports" className="mt-0 space-y-6">
                    <section className="space-y-3">
                        <div className="flex items-baseline justify-between gap-4">
                            <h2 className="text-base font-medium">Roles</h2>
                            <p className="text-text-tertiary text-sm">
                                Dashed bays are unassigned
                            </p>
                        </div>
                        <RoleBays interfaces={rows} state={state.data} />
                    </section>

                    <Card>
                        <CardHeader>
                            <CardTitle>Ports</CardTitle>
                            <CardDescription>
                                Picking a role opens a review step (nothing changes until you apply)
                            </CardDescription>
                        </CardHeader>
                        <CardContent>
                            <InterfaceTable
                                interfaces={rows}
                                onAssign={onAssign}
                                disabled={apply.isPending || armed}
                                uplinks={state.data?.uplinks}
                            />
                        </CardContent>
                    </Card>
                </TabsContent>

                <TabsContent value="lan" className="mt-0">
                    <LanTab state={state.data} armed={armed} onApplied={refresh} />
                </TabsContent>

                <TabsContent value="vpn" className="mt-0">
                    <VpnTab armed={armed} onApplied={refresh} />
                </TabsContent>

                <TabsContent value="forwards" className="mt-0">
                    <PortForwardsTab state={state.data} interfaces={rows} />
                </TabsContent>
            </Tabs>

            <ApplyDialog
                open={dialogOpen}
                onOpenChange={setDialogOpen}
                plan={plan.data ?? null}
                planning={plan.isPending}
                planError={plan.error?.message ?? null}
                applied={apply.data ?? null}
                applyError={apply.error?.message ?? null}
                altOrigin={altOrigin}
                onApply={(confirmed) => pending && apply.mutate({ ...pending, confirmed })}
                onDone={() => {
                    plan.reset()
                    apply.reset()
                    setPending(null)
                    refresh()
                }}
            />
        </div>
    )
}
