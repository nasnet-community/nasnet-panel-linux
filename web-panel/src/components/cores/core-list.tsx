import { Cable, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState } from "@/components/ui/empty-state"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { CoreInbound } from "@/lib/types/cores"
import { CoreCard } from "./core-card"

function CardSkeleton() {
    return (
        <div className="flex flex-col gap-3 rounded-xl border bg-card p-4 shadow-sm">
            <div className="flex items-center gap-3">
                <Skeleton className="size-9 rounded-lg" />
                <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-24" />
                    <Skeleton className="h-3 w-32" />
                </div>
            </div>
            <div className="flex gap-2">
                <Skeleton className="h-5 w-16 rounded-full" />
                <Skeleton className="h-5 w-14 rounded-full" />
            </div>
            <Skeleton className="h-8 w-full" />
        </div>
    )
}

export function CoreList({
    inbounds,
    isLoading,
    hasAny,
    onOpen,
    onEdit,
    onCreate,
    warnings,
    setupRequired,
}: {
    inbounds: CoreInbound[]
    isLoading: boolean
    /** whether the node has any tunnels at all (before filtering) */
    hasAny: boolean
    onOpen: (inbound: CoreInbound) => void
    onEdit: (inbound: CoreInbound) => void
    onCreate: () => void
    warnings?: Record<number, string>
    /** protocols whose binary the node is missing */
    setupRequired?: Set<string>
}) {
    if (isLoading) {
        return (
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                {Array.from({ length: 6 }).map((_, i) => (
                    <CardSkeleton key={i} />
                ))}
            </div>
        )
    }

    if (!hasAny) {
        return (
            <div className="rounded-xl border border-dashed">
                <EmptyState
                    icon={Cable}
                    title="No tunnels yet"
                    description="Stand up an SSH or MTProto tunnel on this node."
                />
                <div className="flex justify-center pb-10">
                    <Button onClick={onCreate}>
                        <Plus className="size-4" />
                        New tunnel
                    </Button>
                </div>
            </div>
        )
    }

    if (inbounds.length === 0) {
        return (
            <EmptyState
                icon={Cable}
                title="No matching tunnels"
                description="No tunnels match the current search or protocol filter."
            />
        )
    }

    // CoreCard always renders a Tooltip (the Settings button + push-failed status
    // chip), so the grid needs a TooltipProvider ancestor. Provide it here — the
    // sole renderer of CoreCard — so every render site (per-node tab, All Tunnels)
    // is covered without each having to remember to wrap.
    return (
        <TooltipProvider delayDuration={200}>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                {inbounds.map((inbound) => (
                    <CoreCard
                        key={inbound.id}
                        inbound={inbound}
                        onOpen={onOpen}
                        onEdit={onEdit}
                        lastWarning={warnings?.[inbound.id]}
                        setupRequired={setupRequired?.has(inbound.protocol)}
                    />
                ))}
            </div>
        </TooltipProvider>
    )
}
