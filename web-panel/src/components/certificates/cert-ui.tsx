import type { ReactNode } from "react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import { toneBar, type Tone } from "./cert-helpers"

/** Same four type roles as the Starlink tab. */
export const T = {
    eyebrow: "text-[12px] leading-5 font-medium uppercase tracking-[0.08em] text-muted-foreground/70",
    body: "text-xs leading-4",
    meta: "text-[12px] leading-4 text-muted-foreground",
} as const

export const cardShell = "relative gap-3 overflow-hidden rounded-2xl border-border bg-card/50 p-4 backdrop-blur-sm"

export function CertCard({ title, aside, className, children }: { title: string; aside?: ReactNode; className?: string; children: ReactNode }) {
    return (
        <Card className={cn(cardShell, className)}>
            <div className="flex min-h-7 items-center justify-between gap-2">
                <h2 className={T.eyebrow}>{title}</h2>
                {aside}
            </div>
            {children}
        </Card>
    )
}

export function ExpiryBar({ pct, tone }: { pct: number; tone: Tone }) {
    return (
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted/60" aria-hidden>
            <div className={cn("h-full rounded-full", toneBar[tone])} style={{ width: `${pct}%` }} />
        </div>
    )
}
