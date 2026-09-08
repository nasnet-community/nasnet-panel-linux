import { cn, formatBytes, copyToClipboard } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
    HiChevronRight,
    HiOutlineCog,
    HiOutlineSwitchHorizontal,
    HiOutlineTrash,
    HiOutlineBan,
    HiOutlineStatusOnline,
    HiOutlineDotsHorizontal,
    HiOutlineClipboardCopy,
} from "react-icons/hi"
import { protocolColors } from "@/components/node/protocol-badge"
import type { Inbound } from "@/lib/types"
import { toast } from "sonner"
import { INBOUND_GRID, EXPIRED_CELL } from "./inbound-grid"

export interface InboundDetail {
    label: string
    value: string
}

interface DesktopInboundRowProps {
    inbound: Inbound
    isExpanded: boolean
    isSelected: boolean
    accountCount: number
    onlineCount: number
    expiredCount: number
    trafficBytes: number
    /** Path / SNI / Host and friends. The first one rides in the row; the rest
     *  live in the panel's Details tab. */
    details: InboundDetail[]
    onToggleExpand: () => void
    onToggleSelect: () => void
    onToggleDisabled: () => void
    onEdit: () => void
    onMigrate: () => void
    onDelete: () => void
    expandedContent: React.ReactNode
}

export function DesktopInboundRow(props: DesktopInboundRowProps) {
    const {
        inbound, isExpanded, isSelected, accountCount, onlineCount, expiredCount,
        trafficBytes, details, onToggleExpand, onToggleSelect, onToggleDisabled,
        onEdit, onMigrate, onDelete, expandedContent,
    } = props

    const panelId = `inbound-panel-${inbound.id}`
    const primary = details[0]
    const disabled = inbound.is_disabled

    return (
        <div
            className={cn(
                "border-t border-border first:border-t-0 transition-colors",
                isSelected ? "bg-primary/5" : "hover:bg-muted/40",
            )}
        >
            {/* Header. The div carries the click target; the chevron carries the
                name and state for anyone not using a mouse. */}
            <div
                onClick={onToggleExpand}
                className={cn(INBOUND_GRID, "min-h-14 pl-2 pr-3 cursor-pointer")}
            >
                <button
                    type="button"
                    aria-label={isExpanded ? `Collapse ${inbound.tag}` : `Expand ${inbound.tag}`}
                    aria-expanded={isExpanded}
                    aria-controls={panelId}
                    onClick={(e) => { e.stopPropagation(); onToggleExpand() }}
                    className="h-7 w-7 rounded-md flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                    <HiChevronRight className={cn("w-4 h-4 transition-transform duration-200", isExpanded && "rotate-90")} />
                </button>

                <Checkbox
                    checked={isSelected}
                    onCheckedChange={onToggleSelect}
                    onClick={(e) => e.stopPropagation()}
                    aria-label={`Select inbound ${inbound.tag}`}
                />

                {/* Enabled or not. Idle is not a warning, so there is no amber here. */}
                <Tooltip>
                    <TooltipTrigger asChild>
                        <span
                            className={cn(
                                "w-2.5 h-2.5 rounded-full shrink-0",
                                disabled
                                    ? "border-[1.5px] border-muted-foreground/50"
                                    : "bg-emerald-500 ring-4 ring-emerald-500/15",
                            )}
                        />
                    </TooltipTrigger>
                    <TooltipContent>{disabled ? "Disabled" : "Enabled"}</TooltipContent>
                </Tooltip>

                {/* Name */}
                <div className="min-w-0">
                    <div className="flex items-baseline gap-1.5 min-w-0">
                        <span
                            className={cn("font-mono font-semibold text-sm truncate", disabled && "text-muted-foreground")}
                            title={inbound.tag}
                        >
                            {inbound.tag}
                        </span>
                        <span className="font-mono text-sm text-muted-foreground shrink-0">:{inbound.port}</span>
                    </div>
                    <div className="flex items-center gap-1.5 text-[11px] font-mono text-muted-foreground truncate">
                        {disabled && <span className="font-medium text-foreground/70 shrink-0">Disabled</span>}
                        {disabled && <span className="text-muted-foreground/50 shrink-0">·</span>}
                        <span className="shrink-0">{inbound.listen || "0.0.0.0"}</span>
                        {primary && (
                            <>
                                <span className="text-muted-foreground/50 shrink-0">·</span>
                                <span className="truncate" title={`${primary.label}: ${primary.value}`}>{primary.value}</span>
                            </>
                        )}
                    </div>
                </div>

                {/* Protocol. Colour marks the protocol; transport and security are plain text. */}
                <div className="flex items-center gap-2 min-w-0">
                    <Badge
                        variant="outline"
                        className={cn(
                            "font-mono text-[10px] px-1.5 py-0 h-5 shrink-0",
                            protocolColors[inbound.protocol.toLowerCase()] || "",
                            disabled && "opacity-60",
                        )}
                    >
                        {inbound.protocol.toUpperCase()}
                    </Badge>
                    <span className="font-mono text-[11px] text-muted-foreground truncate">
                        {(inbound.network || "tcp").toUpperCase()}
                        {inbound.security && inbound.security !== "none" && ` · ${inbound.security.toUpperCase()}`}
                    </span>
                </div>

                {/* Clients */}
                <div className="text-right">
                    <div className="font-mono text-sm tabular-nums">
                        <span className={cn(onlineCount > 0 ? "text-emerald-600 dark:text-emerald-400 font-semibold" : "text-muted-foreground")}>
                            {onlineCount}
                        </span>
                        <span className="text-muted-foreground/50"> / </span>
                        <span className={cn(accountCount === 0 && "text-muted-foreground")}>{accountCount}</span>
                    </div>
                    <div className="text-[11px] text-muted-foreground">online</div>
                </div>

                {/* Traffic */}
                <div className="text-right font-mono text-sm tabular-nums text-muted-foreground">
                    {trafficBytes > 0 ? formatBytes(trafficBytes) : <span className="text-muted-foreground/40">—</span>}
                </div>

                {/* Expired */}
                <div className={cn(EXPIRED_CELL, "text-right font-mono text-sm tabular-nums")}>
                    {expiredCount > 0
                        ? <span className="text-red-600 dark:text-red-400">{expiredCount}</span>
                        : <span className="text-muted-foreground/40">—</span>}
                </div>

                {/* Actions. Edit is the frequent one; Delete is last and alone in red. */}
                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={`Actions for ${inbound.tag}`}
                            onClick={(e) => e.stopPropagation()}
                            className="text-muted-foreground hover:text-foreground"
                        >
                            <HiOutlineDotsHorizontal className="w-4 h-4" />
                        </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
                        <DropdownMenuItem onClick={onEdit}>
                            <HiOutlineCog className="w-4 h-4 mr-2" />
                            Edit
                        </DropdownMenuItem>
                        <DropdownMenuItem
                            onClick={async () => {
                                await copyToClipboard(`${inbound.tag}:${inbound.port}`)
                                toast.success("Copied")
                            }}
                        >
                            <HiOutlineClipboardCopy className="w-4 h-4 mr-2" />
                            Copy tag:port
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onClick={onToggleDisabled}>
                            {disabled
                                ? <><HiOutlineStatusOnline className="w-4 h-4 mr-2" />Enable</>
                                : <><HiOutlineBan className="w-4 h-4 mr-2" />Disable</>}
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={onMigrate}>
                            <HiOutlineSwitchHorizontal className="w-4 h-4 mr-2" />
                            Migrate clients…
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onClick={onDelete} className="text-red-500 focus:text-red-500">
                            <HiOutlineTrash className="w-4 h-4 mr-2" />
                            Delete…
                        </DropdownMenuItem>
                    </DropdownMenuContent>
                </DropdownMenu>
            </div>

            {/* Panel — grid rows animate the height without measuring it. */}
            <div
                id={panelId}
                className={cn(
                    "grid transition-[grid-template-rows] duration-200 ease-out",
                    isExpanded ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
                )}
            >
                <div className="overflow-hidden">
                    <div className="border-t border-border bg-muted/30 px-4 pt-3 pb-4">
                        {expandedContent}
                    </div>
                </div>
            </div>
        </div>
    )
}
