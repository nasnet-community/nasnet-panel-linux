import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { useSetPoolStrategy } from "@/lib/queries/use-network"
import { POOL_STRATEGIES, strategyLabel } from "@/lib/vpn-labels"
import type { PoolStrategy } from "@/lib/types/network"

/** One choice for the whole pool, applied on click. */
export function PoolStrategyControl({
    strategy,
    disabled,
}: {
    strategy: PoolStrategy
    disabled: boolean
}) {
    const set = useSetPoolStrategy()
    const selected = POOL_STRATEGIES.find((s) => s.value === strategy) ?? POOL_STRATEGIES[0]

    function choose(next: PoolStrategy) {
        if (next === strategy) return
        set.mutate(next, {
            onSuccess: () => toast.success(`Traffic now uses the pool: ${strategyLabel(next)}`),
            onError: (e) =>
                toast.error(e instanceof Error ? e.message : "Failed to change how the pool is used"),
        })
    }

    return (
        <div className="space-y-1.5">
            <p className="text-text-secondary text-sm font-medium">How traffic uses these VPNs</p>
            <div
                className="divide-border border-border flex w-fit divide-x overflow-hidden rounded-md border"
                role="group"
                aria-label="How traffic uses these VPNs"
            >
                {POOL_STRATEGIES.map((s) => (
                    <button
                        key={s.value}
                        type="button"
                        aria-pressed={s.value === strategy}
                        disabled={disabled || set.isPending}
                        onClick={() => choose(s.value)}
                        className={cn(
                            "px-3 py-1.5 text-sm transition-colors disabled:opacity-50",
                            s.value === strategy
                                ? "bg-surface-3 text-text-primary font-medium"
                                : "text-text-tertiary hover:bg-surface-2",
                        )}
                    >
                        {s.label}
                    </button>
                ))}
            </div>
            <p className="text-text-tertiary max-w-xl text-xs">{selected.blurb}</p>
        </div>
    )
}
