import { useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Layers, Loader2, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Stat } from "@/components/node/network/section-stat"
import { poolAPI, type OutboundPool, type PoolSettings } from "@/lib/api/outbound-pools"
import { queryKeys } from "@/lib/queries/keys"
import { PoolForm } from "./pool-form"
import { PoolCard } from "./pool-card"
import { relativeTime } from "./pool-display"
import type { BalancingRule, Outbound, ReverseTunnelTarget } from "@/lib/types"
import { toast } from "@/lib/toast"

export function NodePools({ nodeId, outbounds, balancers, tunnels, local = false }: { nodeId: number, outbounds: Outbound[], balancers: BalancingRule[], tunnels?: ReverseTunnelTarget[], local?: boolean }) {
    const queryClient = useQueryClient()
    const confirm = useConfirm()
    const key = ["outbound-pools", nodeId]
    const query = useQuery({ queryKey: key, queryFn: () => poolAPI.list(nodeId), refetchInterval: query => query.state.data?.some(p => p.active_run_id) ? 3000 : 15000 })
    const pools = useMemo(() => query.data ?? [], [query.data])
    const [editing, setEditing] = useState<OutboundPool | "new" | null>(null)
    const mutation = useMutation({ mutationFn: (action: () => Promise<unknown>) => action(), onSuccess: () => { void queryClient.invalidateQueries({ queryKey: key }) }, onError: error => toast.error(error.message) })
    const generation = pools.map(p => `${p.id}:${p.applied_revision}:${p.last_run?.state}:${p.active_count}`).join("|")
    useEffect(() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.nodeOutbounds(nodeId) })
        void queryClient.invalidateQueries({ queryKey: queryKeys.nodeBalancingRules(nodeId) })
    }, [generation, nodeId, queryClient])
    const members = useMemo(() => {
        const byPool = new Map<number, Outbound[]>()
        for (const o of outbounds) if (o.managed_pool_id && !o.is_disabled) byPool.set(o.managed_pool_id, [...byPool.get(o.managed_pool_id) ?? [], o])
        return byPool
    }, [outbounds])
    const summary = useMemo(() => {
        const finished = pools.map(p => p.last_run?.finished_at).filter(Boolean) as string[]
        return {
            members: pools.reduce((n, p) => n + p.active_count, 0),
            onFallback: pools.filter(p => p.last_run && !p.active_count).length,
            scanning: pools.filter(p => p.active_run_id).length,
            lastScan: finished.sort().at(-1) ?? null,
        }
    }, [pools])
    const save = async (settings: PoolSettings) => {
        try {
            await mutation.mutateAsync(() => poolAPI.save(nodeId, editing && editing !== "new" ? editing.id : undefined, settings))
            setEditing(null); toast.success(settings.enabled ? "Subscription pool saved. Scheduled scanning is enabled." : "Subscription pool saved with its schedule paused.")
        } catch { /* The mutation displays the error. */ }
    }
    const remove = async (pool: OutboundPool) => {
        if (await confirm({ title: `Delete ${pool.name}?`, description: "This removes its pool members. The balancer, fallback and routing rules remain.", confirmLabel: "Delete subscription pool", variant: "destructive" })) mutation.mutate(() => poolAPI.remove(nodeId, pool.id))
    }
    return <TooltipProvider delayDuration={200}>
        <div className="space-y-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="min-w-0 space-y-1.5">
                    <h3 className="text-lg font-semibold leading-tight">Subscription pools</h3>
                    {pools.length > 0 ? <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 text-sm">
                        <Stat value={String(pools.length)} label={pools.length === 1 ? "subscription pool" : "subscription pools"} />
                        <Stat value={String(summary.members)} label={summary.members === 1 ? "active member" : "active members"} tone={summary.members ? undefined : "muted"} />
                        {summary.onFallback > 0 && <Stat value={String(summary.onFallback)} label="on fallback" tone="warn" />}
                        {summary.scanning > 0 && <Stat value={String(summary.scanning)} label="scanning" />}
                        {(summary.lastScan || !summary.scanning) && <span className="text-muted-foreground whitespace-nowrap">{summary.lastScan ? `scanned ${relativeTime(summary.lastScan).toLowerCase()}` : "never scanned"}</span>}
                    </div> : <p className="text-sm text-muted-foreground">Scanned upstream configs that feed a balancer.</p>}
                </div>
                <Button size="sm" className="self-start" onClick={() => setEditing("new")}><Plus className="w-4 h-4 mr-2" />Create subscription pool</Button>
            </div>
            {query.isLoading && <Card className="p-8 flex gap-2 items-center justify-center text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />Loading subscription pools…</Card>}
            {query.isError && <Card className="p-5 border-destructive/30"><p className="text-sm text-destructive">{query.error.message}</p><Button variant="outline" size="sm" className="mt-3" onClick={() => void query.refetch()}>Try again</Button></Card>}
            {!query.isLoading && !query.isError && !pools.length && <Card className="py-12 px-6 text-center border-dashed"><div className="mx-auto mb-4 grid size-12 place-items-center rounded-xl bg-muted"><Layers className="size-6 text-muted-foreground" /></div><h3 className="font-medium">Build your first subscription pool</h3><p className="text-sm text-muted-foreground mt-2 max-w-sm mx-auto">Combine subscription URLs and config lists, scan them from this node, and send healthy members to a balancer.</p><Button className="mt-5" onClick={() => setEditing("new")}>Add sources</Button></Card>}
            {pools.map((pool, index) => <PoolCard
                key={pool.id}
                nodeId={nodeId}
                pool={pool}
                members={members.get(pool.id) ?? []}
                defaultOpen={index === 0}
                pending={mutation.isPending}
                onRun={refresh => mutation.mutate(() => poolAPI.run(nodeId, pool.id, refresh))}
                onCancel={() => mutation.mutate(() => poolAPI.cancel(nodeId, pool.id))}
                onEdit={() => setEditing(pool)}
                onToggleSchedule={() => mutation.mutate(() => poolAPI.save(nodeId, pool.id, { ...pool, enabled: !pool.enabled, fallback_tag: pool.balancer.fallback_tag }))}
                onDelete={() => void remove(pool)}
            />)}
            {editing && <PoolForm key={editing === "new" ? "new" : editing.id} pool={editing === "new" ? undefined : editing} balancers={balancers} outbounds={outbounds} tunnels={tunnels} maxThreads={local ? 32 : 2000} pending={mutation.isPending} onClose={() => setEditing(null)} onSave={save} />}
        </div>
    </TooltipProvider>
}
