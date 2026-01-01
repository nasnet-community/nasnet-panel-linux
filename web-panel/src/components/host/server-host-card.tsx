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
import { HiOutlinePencil, HiOutlineTrash, HiOutlineDuplicate, HiOutlineDotsVertical } from "react-icons/hi"
import { cn } from "@/lib/utils"
import type { HostWithRelations } from "@/lib/types"

interface ServerHostCardProps {
    host: HostWithRelations
    isSelected: boolean
    isMultiSelectMode: boolean
    onToggleSelect: () => void
    onEdit: () => void
    onDelete: () => void
    onDuplicate: () => void
    onToggle: () => void
    onTagClick: (tag: string) => void
    onCtrlClick: () => void
}

function getSecurityBadge(security: string): { label: string; variant: "default" | "secondary" | "outline" | "success" } {
    switch (security?.toLowerCase()) {
        case "tls":
            return { label: "TLS", variant: "success" }
        case "reality":
            return { label: "Reality", variant: "default" }
        case "none":
            return { label: "None", variant: "outline" }
        default:
            return { label: security || "Inherit", variant: "outline" }
    }
}

export const ServerHostCard = React.memo(function ServerHostCard({
    host,
    isSelected,
    isMultiSelectMode,
    onToggleSelect,
    onEdit,
    onDelete,
    onDuplicate,
    onToggle,
    onTagClick,
    onCtrlClick,
}: ServerHostCardProps) {
    const securityBadge = getSecurityBadge(host.security)
    const protocol = host.inbound?.protocol?.toUpperCase() ?? ""

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
                "group relative border rounded-lg p-4 border-l-4 border-l-blue-500 transition-all cursor-pointer",
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

            {/* Header: Remark + Switch + Menu */}
            <div className="flex items-start justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0 flex-1">
                    <span className="font-medium text-sm truncate">
                        {host.remark || "(no remark)"}
                    </span>
                </div>
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

            {/* Badges: protocol, security, priority */}
            <div className="flex items-center gap-1.5 flex-wrap mt-2">
                {protocol && (
                    <Badge variant="secondary" className="text-[10px] h-5 px-1.5 font-mono">
                        {protocol}
                    </Badge>
                )}
                <Badge variant={securityBadge.variant} className="text-[10px] h-5 px-1.5">
                    {securityBadge.label}
                </Badge>
                <Badge variant="outline" className="text-[10px] h-5 px-1.5">
                    P:{host.priority}
                </Badge>
            </div>

            {/* Address:port */}
            <div className="text-xs text-muted-foreground mt-1.5 font-mono">
                {host.address || "Inherit"}{host.port != null && `:${host.port}`}
            </div>

            {/* Node + Inbound */}
            {host.inbound && (
                <div className="text-xs text-muted-foreground mt-1.5">
                    {host.inbound.node?.name || `Node ${host.inbound.node_id}`}
                    <span className="mx-1 text-muted-foreground/40">&bull;</span>
                    {host.inbound.tag}
                </div>
            )}

            {/* Tags */}
            {(host.tags || []).length > 0 && (
                <div className="flex flex-wrap gap-1 mt-2">
                    {host.tags.map((tag) => (
                        <button key={tag} onClick={(e) => { e.stopPropagation(); onTagClick(tag) }}>
                            <Badge variant="secondary" className="text-[10px] h-4 px-1 cursor-pointer hover:bg-primary/20">
                                #{tag}
                            </Badge>
                        </button>
                    ))}
                </div>
            )}
        </div>
    )
})
