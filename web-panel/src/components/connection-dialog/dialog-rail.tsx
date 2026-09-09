import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"

// Fixed set of tabs. Tabs never disappear: one that does not apply to the
// current protocol is dimmed with an explanatory summary instead, so the
// user always knows where a setting lives.
export interface RailItem {
    id: string
    label: string
    icon: LucideIcon
    /** One-line read-only summary of what the tab currently holds. */
    summary: string
    errorCount: number
    disabled?: boolean
}

interface DialogRailProps {
    items: RailItem[]
    activeId: string
    onChange: (id: string) => void
}

export function DialogRail({ items, activeId, onChange }: DialogRailProps) {
    return (
        <nav className="flex w-52 shrink-0 flex-col gap-0.5 border-r p-2">
            {items.map((item) => {
                const Icon = item.icon
                const isActive = item.id === activeId
                const hasErrors = item.errorCount > 0
                return (
                    <button
                        key={item.id}
                        type="button"
                        onClick={() => onChange(item.id)}
                        aria-current={isActive ? "page" : undefined}
                        className={cn(
                            "flex min-h-[52px] w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
                            isActive
                                ? "bg-accent text-accent-foreground"
                                : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                            item.disabled && !isActive && "opacity-50",
                        )}
                    >
                        <Icon className="mt-0.5 h-4 w-4 shrink-0" />
                        <span className="flex min-w-0 flex-1 flex-col">
                            <span className={cn("text-sm font-medium leading-5", hasErrors && "text-status-danger")}>
                                {item.label}
                            </span>
                            <span className={cn(
                                "truncate text-xs leading-4",
                                isActive ? "text-muted-foreground" : "text-text-tertiary",
                            )}>
                                {item.summary}
                            </span>
                        </span>
                        {hasErrors && (
                            <span className="mt-[3px] grid h-[18px] min-w-[18px] shrink-0 place-items-center rounded-full bg-status-danger-soft px-1.5 text-[11px] font-semibold leading-4 text-status-danger">
                                {item.errorCount}
                            </span>
                        )}
                    </button>
                )
            })}
        </nav>
    )
}
