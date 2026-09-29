import { cn } from "@/lib/utils"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { CoreInbound } from "@/lib/types/cores"

export type CoreStatus =
    | "live"
    | "disabled"
    | "push-failed"
    | "provisioning"
    | "setup-required"

const STATUS_META: Record<
    CoreStatus,
    { label: string; dot: string; text: string; ring: string; pulse?: boolean }
> = {
    live: {
        label: "Live",
        dot: "bg-emerald-500",
        text: "text-emerald-600 dark:text-emerald-400",
        ring: "border-emerald-500/20 bg-emerald-500/10",
    },
    disabled: {
        label: "Disabled",
        dot: "bg-muted-foreground",
        text: "text-muted-foreground",
        ring: "border-border bg-muted/50",
    },
    "push-failed": {
        label: "Push failed",
        dot: "bg-red-500",
        text: "text-red-600 dark:text-red-400",
        ring: "border-red-500/20 bg-red-500/10",
    },
    provisioning: {
        label: "Provisioning",
        dot: "bg-amber-500",
        text: "text-amber-600 dark:text-amber-400",
        ring: "border-amber-500/20 bg-amber-500/10",
        pulse: true,
    },
    "setup-required": {
        label: "Setup required",
        dot: "bg-muted-foreground",
        text: "text-muted-foreground",
        ring: "border-dashed border-border bg-transparent",
    },
}

/**
 * Derive the base tunnel status from what the list endpoint returns.
 * `push-failed` comes from a mutation's reconcile warning and
 * `setup-required` from the node's dependency probe; callers pass those in.
 */
export function deriveCoreStatus(inbound: CoreInbound): CoreStatus {
    return inbound.is_disabled ? "disabled" : "live"
}

export function CoreStatusChip({
    status,
    tooltip,
    className,
}: {
    status: CoreStatus
    tooltip?: string
    className?: string
}) {
    const m = STATUS_META[status]
    const chip = (
        <span
            className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium",
                m.ring,
                m.text,
                className,
            )}
        >
            <span className={cn("size-1.5 rounded-full", m.dot, m.pulse && "animate-pulse")} />
            {m.label}
        </span>
    )
    if (!tooltip) return chip
    return (
        <Tooltip>
            <TooltipTrigger asChild>{chip}</TooltipTrigger>
            <TooltipContent>{tooltip}</TooltipContent>
        </Tooltip>
    )
}
