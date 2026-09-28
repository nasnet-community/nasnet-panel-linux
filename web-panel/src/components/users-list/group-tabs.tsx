import { cn } from "@/lib/utils"
import type { UserGroup, UserGroupCounts } from "@/lib/types"
import { GROUPS } from "./groups"

interface GroupPickerProps {
    value: UserGroup
    counts?: UserGroupCounts
    onChange: (group: UserGroup) => void
}

export function GroupTabs({ value, counts, onChange }: GroupPickerProps) {
    return (
        <div role="tablist" aria-label="User groups" className="flex gap-7 overflow-x-auto border-b border-border/60 no-scrollbar">
            {GROUPS.map((g) => {
                const selected = g.id === value
                const count = counts?.[g.id]
                return (
                    <button
                        key={g.id}
                        type="button"
                        role="tab"
                        aria-selected={selected}
                        onClick={() => onChange(g.id)}
                        className={cn(
                            "-mb-px flex shrink-0 items-baseline gap-2 border-b-2 pb-2.5 text-sm font-medium outline-none transition-colors focus-visible:text-foreground",
                            selected ? "border-foreground text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
                            count === 0 && !selected && "text-muted-foreground/60",
                        )}
                    >
                        {g.label}
                        <span className={cn("font-mono text-[12.5px] tabular-nums", selected ? "text-text-secondary" : "text-muted-foreground/80")}>{count ?? "–"}</span>
                    </button>
                )
            })}
        </div>
    )
}

export function GroupChips({ value, counts, onChange }: GroupPickerProps) {
    return (
        <div role="tablist" aria-label="User groups" className="-mx-4 flex gap-2 overflow-x-auto px-4 no-scrollbar">
            {GROUPS.map((g) => {
                const selected = g.id === value
                return (
                    <button
                        key={g.id}
                        type="button"
                        role="tab"
                        aria-selected={selected}
                        onClick={() => onChange(g.id)}
                        className={cn(
                            "flex h-9 shrink-0 items-center gap-1.5 rounded-full border px-3.5 text-[13.5px] font-medium",
                            selected ? "border-foreground bg-foreground text-background" : "border-border text-text-secondary",
                        )}
                    >
                        {g.label}
                        <span className={cn("font-mono text-xs tabular-nums", selected ? "text-background/70" : "text-muted-foreground")}>{counts?.[g.id] ?? "–"}</span>
                    </button>
                )
            })}
        </div>
    )
}
