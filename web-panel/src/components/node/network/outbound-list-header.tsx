import { cn } from "@/lib/utils"
import { Checkbox } from "@/components/ui/checkbox"
import { OUTBOUND_GRID } from "./outbound-grid"
import { SortableHead, PlainHead, type SortDir } from "./sortable-head"

export type OutboundSortField = "name" | "usage" | "traffic" | "test"

interface OutboundListHeaderProps {
    allSelected: boolean
    someSelected: boolean
    onToggleAll: () => void
    sortField: OutboundSortField | null
    sortDir: SortDir
    onSort: (field: OutboundSortField) => void
}

export function OutboundListHeader({
    allSelected, someSelected, onToggleAll, sortField, sortDir, onSort,
}: OutboundListHeaderProps) {
    const sort = { sortField, sortDir, onSort }
    return (
        <div className={cn(OUTBOUND_GRID, "h-9 pl-2 pr-3 bg-muted/50 rounded-t-xl")}>
            <span />
            <Checkbox
                checked={allSelected ? true : someSelected ? "indeterminate" : false}
                onCheckedChange={onToggleAll}
                aria-label="Select all outbounds"
            />
            <span />
            <SortableHead field="name" {...sort}>Name</SortableHead>
            <PlainHead>Protocol</PlainHead>
            <SortableHead field="usage" align="right" {...sort}>Used by</SortableHead>
            <SortableHead field="traffic" align="right" {...sort}>Traffic</SortableHead>
            <SortableHead field="test" align="right" {...sort}>Test</SortableHead>
            <span />
        </div>
    )
}
