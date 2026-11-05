import { LayoutGrid, List } from "lucide-react"
import { cn } from "@/lib/utils"

export type NodesView = "cards" | "list"

const OPTIONS = [
    { value: "cards", label: "Cards", icon: LayoutGrid },
    { value: "list", label: "List", icon: List },
] as const

/** Cards | List; icons only on the phone. */
export function ViewSwitch({ value, onChange, iconsOnly }: { value: NodesView; onChange: (v: NodesView) => void; iconsOnly?: boolean }) {
    return (
        <div role="group" aria-label="View" className={cn("flex shrink-0 items-center gap-0.5 rounded-[10px] bg-muted/70 p-0.5", iconsOnly ? "h-10" : "h-8")}>
            {OPTIONS.map(({ value: v, label, icon: Icon }) => (
                <button
                    key={v}
                    type="button"
                    aria-pressed={value === v}
                    aria-label={label}
                    title={iconsOnly ? label : undefined}
                    onClick={() => onChange(v)}
                    className={cn(
                        "flex h-full items-center gap-1.5 rounded-lg text-[13px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                        iconsOnly ? "px-2.5" : "px-2.5",
                        value === v ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground",
                    )}
                >
                    <Icon className="size-3.5" aria-hidden />
                    {!iconsOnly && label}
                </button>
            ))}
        </div>
    )
}
