import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

export const TAG_TONE = {
    ok: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
    warn: "border-amber-500/35 bg-amber-500/10 text-amber-700 dark:text-amber-400",
    bad: "border-red-500/35 bg-red-500/10 text-red-600 dark:text-red-400",
    muted: "border-border bg-muted/50 text-muted-foreground",
}

export function Tag({ kind, children, className }: { kind: keyof typeof TAG_TONE; children: ReactNode; className?: string }) {
    return <span className={cn("inline-flex h-[22px] shrink-0 items-center gap-1 rounded-md border px-2 text-[12px] font-medium", TAG_TONE[kind], className)}>{children}</span>
}
