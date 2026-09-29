import type { ReactNode } from "react"
import { ChevronRight } from "lucide-react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import { T, cardShell } from "../node-ui"

// Literal class names so Tailwind keeps them.
const GLOW = {
    blue: "hover:shadow-blue-500/10",
    purple: "hover:shadow-purple-500/10",
    teal: "hover:shadow-teal-500/10",
    indigo: "hover:shadow-indigo-500/10",
    orange: "hover:shadow-orange-500/10",
    emerald: "hover:shadow-emerald-500/10",
    cyan: "hover:shadow-cyan-500/10",
    sky: "hover:shadow-sky-500/10",
} as const

export type CardGlow = keyof typeof GLOW

interface OverviewCardProps {
    /** Tint of the hover shadow; every card lifts on hover. */
    glow?: CardGlow
    /** Omit for a card whose own controls head it (the charts card). */
    title?: string
    /** Right side of the header: a count, a total, a control. */
    aside?: ReactNode
    /** Makes the whole card a link-like button (header shows a chevron). */
    onOpen?: () => void
    openLabel?: string
    className?: string
    children: ReactNode
}

/** The one card surface on the Overview tab. */
export function OverviewCard({ title, aside, onOpen, openLabel, glow, className, children }: OverviewCardProps) {
    const hover = cn("transition-shadow duration-300 hover:shadow-lg", glow && GLOW[glow])
    const body = (
        <>
            {title && (
                <div className="flex min-h-5 items-center justify-between gap-2">
                    <h2 className={cn(T.eyebrow, "truncate")}>{title}</h2>
                    {onOpen ? <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground/50 transition-colors group-hover:text-muted-foreground" aria-hidden /> : aside}
                </div>
            )}
            {children}
        </>
    )
    if (onOpen) {
        return (
            <Card
                role="button"
                tabIndex={0}
                aria-label={openLabel ?? title ?? "Open"}
                onClick={onOpen}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onOpen() } }}
                className={cn(cardShell, hover, "group cursor-pointer transition-[box-shadow,background-color] hover:bg-card/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}
            >
                {body}
            </Card>
        )
    }
    return <Card className={cn(cardShell, hover, className)}>{body}</Card>
}

/** Label/value row used across the stat cards. */
export function StatRow({ label, children, className }: { label: ReactNode; children: ReactNode; className?: string }) {
    return (
        <div className={cn("flex min-h-6 items-center justify-between gap-3 text-xs leading-4", className)}>
            <span className="shrink-0 whitespace-nowrap text-muted-foreground">{label}</span>
            <span className="flex min-w-0 items-center justify-end gap-1.5 text-right font-semibold tabular-nums">{children}</span>
        </div>
    )
}

export function RowDivider() {
    return <div className="my-1.5 h-px bg-border/60" />
}
