import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

/** Uppercase field label used across the rail, figures and card titles. */
export function FieldLabel({ children, className, as: As = "span", htmlFor }: { children: ReactNode; className?: string; as?: "span" | "label" | "h3"; htmlFor?: string }) {
    return (
        <As htmlFor={htmlFor} className={cn("text-[12px] font-semibold uppercase tracking-wider text-muted-foreground", className)}>
            {children}
        </As>
    )
}

export interface SegmentOption<V extends string> {
    value: V
    label: ReactNode
    title?: string
    disabled?: boolean
}

/** Muted track with the active option raised, like the tab list. */
export function Segmented<V extends string>({
    value,
    options,
    onChange,
    size = "sm",
    ariaLabel,
    className,
}: {
    value: V
    options: SegmentOption<V>[]
    onChange: (v: V) => void
    size?: "sm" | "lg"
    ariaLabel?: string
    className?: string
}) {
    return (
        <div role="group" aria-label={ariaLabel} className={cn("inline-flex shrink-0 items-center gap-0.5 rounded-lg bg-muted p-[3px]", className)}>
            {options.map(o => (
                <button
                    key={o.value}
                    type="button"
                    aria-pressed={value === o.value}
                    title={o.title}
                    disabled={o.disabled}
                    onClick={() => onChange(o.value)}
                    className={cn(
                        "inline-flex items-center gap-1.5 whitespace-nowrap rounded-md font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-40",
                        size === "lg" ? "h-9 px-3 text-sm" : "h-6 px-2.5 text-xs",
                        value === o.value ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
                    )}
                >
                    {o.label}
                </button>
            ))}
        </div>
    )
}

/** Label over a large mono figure. */
export function Figure({ label, value, suffix }: { label: string; value: ReactNode; suffix?: ReactNode }) {
    return (
        <div className="flex min-w-0 flex-col gap-1.5">
            <FieldLabel>{label}</FieldLabel>
            <span className="flex items-baseline gap-1.5">
                <span className="font-mono text-[22px] font-bold leading-[1.1] tracking-tight tabular-nums sm:text-[28px]">{value}</span>
                {suffix && <span className="font-mono text-sm text-muted-foreground tabular-nums">{suffix}</span>}
            </span>
        </div>
    )
}

export function Kbd({ children }: { children: ReactNode }) {
    return <kbd className="rounded border border-border bg-muted px-1.5 py-px font-mono text-[12px] text-muted-foreground">{children}</kbd>
}

/** Rounded pill for scope chips in the summary card. */
export function Pill({ children, className }: { children: ReactNode; className?: string }) {
    return <span className={cn("inline-flex h-6 items-center gap-1.5 rounded-full bg-muted px-2.5 text-xs font-medium", className)}>{children}</span>
}
