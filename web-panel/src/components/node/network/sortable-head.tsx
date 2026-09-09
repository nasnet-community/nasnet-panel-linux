import { cn } from "@/lib/utils"
import { HiOutlineSortAscending, HiOutlineSortDescending, HiOutlineSelector } from "react-icons/hi"

export type SortDir = "asc" | "desc"

interface SortableHeadProps<F extends string> {
    field: F
    sortField: F | null
    sortDir: SortDir
    onSort: (field: F) => void
    align?: "left" | "right"
    children: React.ReactNode
}

// One column head that sorts. Generic over the field union so each list keeps
// its own set of sortable columns without re-typing the button.
export function SortableHead<F extends string>({
    field, sortField, sortDir, onSort, align = "left", children,
}: SortableHeadProps<F>) {
    const active = sortField === field
    const Icon = !active ? HiOutlineSelector : sortDir === "asc" ? HiOutlineSortAscending : HiOutlineSortDescending
    return (
        <button
            type="button"
            onClick={() => onSort(field)}
            aria-label={`Sort by ${field}`}
            className={cn(
                "flex items-center gap-1 text-[11px] font-medium uppercase tracking-wide transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-sm",
                align === "right" && "justify-end w-full",
                active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
        >
            {children}
            <Icon className={cn("w-3 h-3", !active && "opacity-40")} />
        </button>
    )
}

// A column head that does not sort, styled to sit beside the ones that do.
export function PlainHead({ children, align = "left" }: { children: React.ReactNode; align?: "left" | "right" }) {
    return (
        <span className={cn(
            "block text-[11px] font-medium uppercase tracking-wide text-muted-foreground",
            align === "right" && "text-right",
        )}>
            {children}
        </span>
    )
}
