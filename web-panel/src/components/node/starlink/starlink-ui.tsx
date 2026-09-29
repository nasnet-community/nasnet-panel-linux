import type { ReactNode } from "react"
import { ChevronRight } from "lucide-react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"

export { T, cardShell, SegmentedControl, KeyValue } from "../node-ui"
import { T, cardShell } from "../node-ui"

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

