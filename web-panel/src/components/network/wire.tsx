import { cn } from "@/lib/utils"

/** One segment of the path a packet takes into the network.
 *  live — open, dashes drift toward the device
 *  off  — deliberately closed, dormant
 *  cut  — delivery fails here, and the mark is the diagnosis */
export type WireState = "live" | "off" | "cut"

export function Wire({
    state = "off",
    arrow = false,
    className,
}: {
    state?: WireState
    arrow?: boolean
    className?: string
}) {
    if (state === "cut") {
        return (
            <span
                className={cn("flex min-w-6 flex-1 items-center gap-1", className)}
                role="img"
                aria-label="blocked here"
            >
                <span className="bg-status-warning/40 h-px flex-1" />
                <span className="text-status-warning text-xs font-medium">✕</span>
                <span className="bg-status-warning/40 h-px flex-1" />
            </span>
        )
    }
    return (
        <span
            aria-hidden
            className={cn(
                "flex min-w-4 flex-1 items-center",
                state === "live" ? "text-status-success/85" : "text-text-disabled",
                className,
            )}
        >
            {state === "live" ? (
                <span className="wire-live bg-status-success/30 h-px flex-1" />
            ) : (
                <span className="border-border-strong h-0 flex-1 border-t border-dashed" />
            )}
            {arrow && (
                <span className="h-0 w-0 border-y-[3px] border-l-[5px] border-y-transparent border-l-current" />
            )}
        </span>
    )
}

/** The identity of a rule: the port it opens, styled like a label on hardware. */
export function PortSocket({
    className,
    children,
}: {
    className?: string
    children: React.ReactNode
}) {
    return (
        <span
            className={cn(
                "bg-surface-3 border-border shrink-0 rounded-md border px-2 py-0.5 font-mono text-xs font-medium tabular-nums",
                className,
            )}
        >
            {children}
        </span>
    )
}

/** An unconnected path — the drawing for "nothing listens here yet". */
export function WireEmpty({
    from,
    to,
    title,
    description,
}: {
    from: string
    to: string
    title: string
    description: string
}) {
    return (
        <div className="py-10 text-center">
            <div className="text-text-tertiary mx-auto flex w-full max-w-72 items-center gap-2.5 font-mono text-xs select-none">
                <span>{from}</span>
                <Wire state="off" />
                <span>{to}</span>
            </div>
            <p className="mt-5 text-sm font-medium">{title}</p>
            <p className="text-text-secondary mx-auto mt-1 max-w-sm text-sm">{description}</p>
        </div>
    )
}
