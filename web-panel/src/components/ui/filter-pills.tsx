import { cn } from "@/lib/utils"

export interface FilterPill<T extends string> {
    label: string
    value: number
    filter: T
    activeColor: string
}

interface FilterPillsProps<T extends string> {
    pills: FilterPill<T>[]
    activeFilter: T
    defaultFilter: T
    onFilterChange: (filter: T) => void
}

export function FilterPills<T extends string>({
    pills,
    activeFilter,
    defaultFilter,
    onFilterChange,
}: FilterPillsProps<T>) {
    return (
        <div className="flex items-center gap-1.5 flex-wrap">
            {pills.map((pill) => {
                const isActive = activeFilter === pill.filter
                return (
                    <button
                        key={pill.filter}
                        onClick={() => onFilterChange(isActive ? defaultFilter : pill.filter)}
                        className={cn(
                            "inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border transition-all",
                            isActive
                                ? pill.activeColor
                                : "border-border/50 hover:border-border text-muted-foreground hover:text-foreground"
                        )}
                    >
                        <span className="font-bold tabular-nums">{pill.value}</span>
                        <span>{pill.label}</span>
                    </button>
                )
            })}
        </div>
    )
}
