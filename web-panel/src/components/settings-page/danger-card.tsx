import { useId, type ReactNode } from "react"
import { TriangleAlert, type LucideIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/** The page's last card: actions that act on the server and can't be taken back. */
export function DangerCard({ children, note = "These act on the server and can't be taken back.", title = "Danger zone" }: { children: ReactNode; note?: string; title?: string }) {
    const titleId = useId()
    return (
        <section aria-labelledby={titleId} className="overflow-hidden rounded-xl border border-red-500/30 bg-card">
            <header className="px-4 pb-1 pt-3.5">
                <h3 id={titleId} className="flex items-center gap-2 text-sm font-semibold text-red-500">
                    <TriangleAlert className="size-4" aria-hidden />{title}
                </h3>
                <p className="mt-0.5 text-xs text-muted-foreground">{note}</p>
            </header>
            <div className="divide-y divide-border/40 px-4">{children}</div>
        </section>
    )
}

const TONES = {
    muted: { tile: "bg-muted text-muted-foreground", button: "" },
    warn: { tile: "bg-amber-500/10 text-amber-500", button: "border-amber-500/40 text-amber-500 hover:bg-amber-500/10 hover:text-amber-500" },
    danger: { tile: "bg-red-500/10 text-red-500", button: "border-red-500/40 text-red-500 hover:bg-red-500/10 hover:text-red-500" },
}

export function DangerRow({ icon: Icon, tone, title, text, why, disabled, button, onClick }: {
    icon: LucideIcon
    tone: keyof typeof TONES
    title: string
    text: string
    /** why the button is off, or what it will ask for */
    why?: string
    disabled?: boolean
    button: string
    onClick: () => void
}) {
    return (
        <div className="flex items-start gap-3 py-3">
            <span className={cn("mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md", TONES[tone].tile)}>
                <Icon className="size-3.5" aria-hidden />
            </span>
            <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{title}</p>
                <p className="mt-0.5 text-xs text-muted-foreground">{text}</p>
                {why && <p className="mt-1 text-xs text-foreground/80">{why}</p>}
            </div>
            <Button type="button" size="sm" variant="outline" onClick={onClick} disabled={disabled} className={cn("h-8 shrink-0", TONES[tone].button)}>
                {button}
            </Button>
        </div>
    )
}
