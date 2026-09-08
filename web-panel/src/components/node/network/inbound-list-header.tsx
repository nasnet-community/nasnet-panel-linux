import { cn } from "@/lib/utils"
import { HiOutlineSortAscending, HiOutlineSortDescending, HiOutlineSelector } from "react-icons/hi"
import { Checkbox } from "@/components/ui/checkbox"
import { INBOUND_GRID, EXPIRED_CELL } from "./inbound-grid"

export type InboundSortField = "name" | "clients" | "traffic" | "expired"
export type SortDir = "asc" | "desc"

interface InboundListHeaderProps {
    allSelected: boolean
    someSelected: boolean
    onToggleAll: () => void
    sortField: InboundSortField | null
    sortDir: SortDir
    onSort: (field: InboundSortField) => void
}

function Sortable({ field, children, align = "left", sortField, sortDir, onSort }: {
    field: InboundSortField
    children: React.ReactNode
    align?: "left" | "right"
    sortField: InboundSortField | null
    sortDir: SortDir
    onSort: (field: InboundSortField) => void
}) {
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

export function InboundListHeader({
    allSelected, someSelected, onToggleAll, sortField, sortDir, onSort,
}: InboundListHeaderProps) {
    return (
        <div className={cn(INBOUND_GRID, "h-9 pl-2 pr-3 bg-muted/50 rounded-t-xl")}>
            <span />
            <Checkbox
                checked={allSelected ? true : someSelected ? "indeterminate" : false}
                onCheckedChange={onToggleAll}
                aria-label="Select all inbounds"
            />
            <span />
            <Sortable field="name" sortField={sortField} sortDir={sortDir} onSort={onSort}>Name</Sortable>
            <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Protocol</span>
            <Sortable field="clients" align="right" sortField={sortField} sortDir={sortDir} onSort={onSort}>Clients</Sortable>
            <Sortable field="traffic" align="right" sortField={sortField} sortDir={sortDir} onSort={onSort}>Traffic</Sortable>
            <div className={EXPIRED_CELL}>
                <Sortable field="expired" align="right" sortField={sortField} sortDir={sortDir} onSort={onSort}>Expired</Sortable>
            </div>
            <span />
        </div>
    )
}
