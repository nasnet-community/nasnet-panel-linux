import React from "react"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Button } from "@/components/ui/button"
import { HiOutlinePencil, HiOutlineTrash, HiOutlineDuplicate, HiOutlineDotsVertical, HiOutlineInformationCircle } from "react-icons/hi"
import { cn } from "@/lib/utils"
import type { HostWithRelations } from "@/lib/types"

interface InfoHostCardProps {
    host: HostWithRelations
    isSelected: boolean
    isMultiSelectMode: boolean
    onToggleSelect: () => void
    onEdit: () => void
    onDelete: () => void
    onDuplicate: () => void
    onToggle: () => void
    onCtrlClick: () => void
}

export const InfoHostCard = React.memo(function InfoHostCard({
    host,
    isSelected,
    isMultiSelectMode,
    onToggleSelect,
    onEdit,
    onDelete,
    onDuplicate,
    onToggle,
    onCtrlClick,
}: InfoHostCardProps) {
    function handleClick(e: React.MouseEvent) {
        if ((e.target as HTMLElement).closest('[role="checkbox"]') ||
            (e.target as HTMLElement).closest('button') ||
            (e.target as HTMLElement).closest('[data-slot]')) return

        if (isMultiSelectMode) {
            onToggleSelect()
            return
        }

        if (e.metaKey || e.ctrlKey) {
            onCtrlClick()
            return
        }

        onEdit()
    }

    return (
        <div
            onClick={handleClick}
            className={cn(
                "group relative border rounded-lg p-3 border-l-4 border-l-amber-500 transition-all cursor-pointer",
                host.is_disabled ? "opacity-50 bg-muted/30" : "bg-card hover:shadow-sm",
                isSelected && "ring-2 ring-primary"
            )}
        >
            {/* Multi-select checkbox overlay */}
            {isMultiSelectMode && (
                <div className="absolute top-2 left-6 z-10">
                    <Checkbox checked={isSelected} onCheckedChange={onToggleSelect} />
                </div>
            )}

            {/* Header: INFO badge + Switch + Menu */}
            <div className="flex items-start justify-between gap-2">
                <Badge variant="outline" className="text-[10px] h-5 px-1.5 bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-950 dark:text-amber-300 dark:border-amber-800">
                    <HiOutlineInformationCircle className="h-3 w-3 mr-0.5" />
                    INFO
                </Badge>
                <div className="flex items-center gap-1 shrink-0">
                    <Switch
                        checked={!host.is_disabled}
                        onCheckedChange={onToggle}
                        className="scale-75"
                    />
                    {/* Desktop three-dot menu */}
                    <div className="hidden md:block opacity-0 group-hover:opacity-100 transition-opacity">
                        <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                                <Button variant="ghost" size="icon" className="h-7 w-7">
                                    <HiOutlineDotsVertical className="h-4 w-4" />
                                </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                                <DropdownMenuItem onClick={onEdit}>
                                    <HiOutlinePencil className="h-4 w-4 mr-2" />
                                    Edit
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={onDuplicate}>
                                    <HiOutlineDuplicate className="h-4 w-4 mr-2" />
                                    Duplicate
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={onDelete} className="text-destructive focus:text-destructive">
                                    <HiOutlineTrash className="h-4 w-4 mr-2" />
                                    Delete
                                </DropdownMenuItem>
                            </DropdownMenuContent>
                        </DropdownMenu>
                    </div>
                </div>
            </div>

            {/* Remark template */}
            {host.remark && (
                <div className="text-xs text-muted-foreground mt-2 font-mono truncate">
                    &ldquo;{host.remark}&rdquo;
                </div>
            )}

            {/* Priority */}
            <div className="flex items-center gap-2 mt-2">
                <Badge variant="outline" className="text-[10px] h-5 px-1.5">
                    P:{host.priority}
                </Badge>
            </div>
        </div>
    )
})
