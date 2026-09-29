import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

// Shared by the node tabs (Overview, Starlink) and the Certificates page, so
// they read as one product: four type roles, one card surface, one control.

// ─── Type roles ─────────────────────────────────────────────────────
//
// Four roles for the node tabs. Before this the tab rendered ten
// distinct sizes (7px through 30px) across three font families for what were
// semantically the same four things — a card title was 9px mono in the status
// strip, 11px sans in the cards and 12px in the drawer. Anything here that
// needs a size not in this list is a design bug, not a new role.

export const T = {
    /** Card titles, column labels, legend keys. */
    eyebrow: "text-[12px] leading-5 font-medium uppercase tracking-[0.08em] text-muted-foreground/70",
    /** The one number a card exists to show. */
    value: "text-[30px] leading-8 font-bold tracking-[-0.02em] tabular-nums",
    /** The unit that trails a value. */
    unit: "text-[14px] leading-4 font-medium text-muted-foreground",
    /** Key-value rows, badges, inline facts. */
    body: "text-xs leading-4",
    /** Deltas, axis ticks, footnotes. The floor — nothing renders smaller. */
    meta: "text-[12px] leading-4 text-muted-foreground",
} as const

// ─── Card shell ─────────────────────────────────────────────────────

/**
 * The one card surface on the tab. `p-4` deliberately overrides the shadcn
 * Card's `py-6`: carrying both is what left the KPI row sitting 41px inset
 * while every neighbouring card sat at 17px.
 */
export const cardShell =
    "relative h-full gap-3 overflow-hidden rounded-2xl border-border bg-card/50 p-4 backdrop-blur-sm"

// ─── Segmented control ──────────────────────────────────────────────

interface SegmentedControlProps<V extends string> {
    value: V
    options: readonly { value: V; label: string; ariaLabel?: string; disabled?: boolean; title?: string }[]
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
                    disabled={opt.disabled}
                    title={opt.title}
                    onClick={() => onChange(opt.value)}
                    className={cn(
                        "flex shrink-0 items-center whitespace-nowrap rounded-lg font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                        lg ? "h-10 px-3.5 text-[14px]" : "h-6 px-2.5 text-xs",
                        value === opt.value
                            ? "bg-foreground text-background"
                            : "text-muted-foreground hover:text-foreground",
                        "disabled:pointer-events-none disabled:opacity-35",
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
