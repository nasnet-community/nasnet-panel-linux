import type { ReactNode } from "react"
import { ChevronRight } from "lucide-react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"

// ─── Type roles ─────────────────────────────────────────────────────
//
// Four roles for the entire Starlink tab. Before this the tab rendered ten
// distinct sizes (7px through 30px) across three font families for what were
// semantically the same four things — a card title was 9px mono in the status
// strip, 11px sans in the cards and 12px in the drawer. Anything here that
// needs a size not in this list is a design bug, not a new role.

export const T = {
    /** Card titles, column labels, legend keys. */
    eyebrow: "text-[11px] leading-5 font-medium uppercase tracking-[0.08em] text-muted-foreground/70",
    /** The one number a card exists to show. */
    value: "text-[28px] leading-8 font-bold tracking-[-0.02em] tabular-nums",
    /** The unit that trails a value. */
    unit: "text-[13px] leading-4 font-medium text-muted-foreground",
    /** Key-value rows, badges, inline facts. */
    body: "text-xs leading-4",
    /** Deltas, axis ticks, footnotes. The floor — nothing renders smaller. */
    meta: "text-[11px] leading-4 text-muted-foreground",
} as const

// ─── Card shell ─────────────────────────────────────────────────────

/**
 * The one card surface on the tab. `p-4` deliberately overrides the shadcn
 * Card's `py-6`: carrying both is what left the KPI row sitting 41px inset
 * while every neighbouring card sat at 17px.
 */
export const cardShell =
    "relative h-full gap-3 overflow-hidden rounded-2xl border-border bg-card/50 p-4 backdrop-blur-sm"

interface StarlinkCardProps {
    title: string
    /**
     * Right-hand header slot, for a card that does NOT open a drawer. On a
     * card that does, the chevron owns that spot — the two must never both
     * appear, or the slot stops meaning anything.
     */
    aside?: ReactNode
    onClick?: () => void
    ariaLabel?: string
    className?: string
    bodyClassName?: string
    children: ReactNode
}

export function StarlinkCard({
    title, aside, onClick, ariaLabel, className, bodyClassName, children,
}: StarlinkCardProps) {
    const interactive = !!onClick

    return (
        <Card
            {...(interactive
                ? {
                    role: "button",
                    tabIndex: 0,
                    "aria-label": ariaLabel ?? `${title}. Open detail.`,
                    onClick,
                    onKeyDown: (e: React.KeyboardEvent) => {
                        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick() }
                    },
                }
                : {})}
            className={cn(
                cardShell,
                interactive && "group cursor-pointer transition-colors hover:bg-card/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                className,
            )}
        >
            <div className="flex h-5 items-center justify-between gap-2">
                <p className={T.eyebrow}>{title}</p>
                {interactive
                    ? <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground/50 transition-colors group-hover:text-muted-foreground" aria-hidden />
                    : aside}
            </div>
            <div className={cn("flex flex-col gap-3", bodyClassName)}>{children}</div>
        </Card>
    )
}

// ─── Segmented control ──────────────────────────────────────────────

interface SegmentedControlProps<V extends string> {
    value: V
    options: readonly { value: V; label: string; ariaLabel?: string }[]
    onChange: (value: V) => void
    /** lg is the 44px touch target; use it wherever the tab is on a phone. */
    size?: "sm" | "lg"
    className?: string
}

export function SegmentedControl<V extends string>({
    value, options, onChange, size = "sm", className,
}: SegmentedControlProps<V>) {
    const lg = size === "lg"

    return (
        <div
            className={cn(
                "flex w-max shrink-0 items-center gap-0.5 rounded-[10px] bg-muted/30 p-0.5",
                lg ? "h-11 rounded-xl" : "h-7",
                className,
            )}
        >
            {options.map((opt) => (
                <button
                    key={opt.value}
                    type="button"
                    aria-pressed={value === opt.value}
                    aria-label={opt.ariaLabel}
                    onClick={() => onChange(opt.value)}
                    className={cn(
                        "flex shrink-0 items-center whitespace-nowrap rounded-lg font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                        lg ? "h-10 px-3.5 text-[13px]" : "h-6 px-2.5 text-xs",
                        value === opt.value
                            ? "bg-foreground text-background"
                            : "text-muted-foreground hover:text-foreground",
                    )}
                >
                    {opt.label}
                </button>
            ))}
        </div>
    )
}

// ─── Key-value row ──────────────────────────────────────────────────

export function KeyValue({ label, children }: { label: string; children: ReactNode }) {
    return (
        <div className="flex items-center justify-between gap-3 text-xs leading-4">
            <span className="shrink-0 whitespace-nowrap text-muted-foreground">{label}</span>
            <span className="min-w-0 truncate text-right font-medium tabular-nums">{children}</span>
        </div>
    )
}
