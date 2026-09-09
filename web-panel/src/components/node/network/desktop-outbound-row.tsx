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
    HiOutlineTrash,
    HiOutlineBan,
    HiOutlineStatusOnline,
    HiOutlineDotsHorizontal,
    HiOutlineClipboardCopy,
    HiOutlineUpload,
    HiOutlineDownload,
} from "react-icons/hi"
import { Power } from "lucide-react"
import { toast } from "sonner"
import { protocolColors } from "@/components/node/protocol-badge"
import type { Outbound, OutboundTestEntry } from "@/lib/types"
import { OUTBOUND_GRID } from "./outbound-grid"
import { hasStreamTransport, type OutboundDetail } from "./outbound-details"
import { OutboundTestCell } from "./outbound-test-cell"

interface DesktopOutboundRowProps {
    outbound: Outbound
    isExpanded: boolean
    isSelected: boolean
    /** First enabled outbound in config order — where unmatched traffic goes. */
    isDefault: boolean
    /** Routing rules + balancers that target this tag. */
    usageCount: number
    trafficBytes: number
    /** Second line of the name cell, from describeOutboundRoute. */
    route: string
    /** SNI / Host / Path and friends. The first rides in the row after the route. */
    details: OutboundDetail[]
    testEntry: OutboundTestEntry | null
    isTesting: boolean
    canTest: boolean
    speedtestSupported: boolean
    /** True while Test All runs. */
    testDisabled: boolean
    onToggleExpand: () => void
    onToggleSelect: () => void
    onToggleDisabled: () => void
    onEdit: () => void
    onTest: () => void
    onTestSpeed: () => void
    onViewTest: () => void
    onDelete: () => void
    expandedContent: React.ReactNode
}

export function DesktopOutboundRow(props: DesktopOutboundRowProps) {
    const {
        outbound, isExpanded, isSelected, isDefault, usageCount, trafficBytes, route, details,
        testEntry, isTesting, canTest, speedtestSupported, testDisabled,
        onToggleExpand, onToggleSelect, onToggleDisabled, onEdit, onTest, onTestSpeed, onViewTest, onDelete,
        expandedContent,
    } = props

    const panelId = `outbound-panel-${outbound.id}`
    const primary = details[0]
    const disabled = outbound.is_disabled
    const streamed = hasStreamTransport(outbound.protocol)

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
                className={cn(OUTBOUND_GRID, "min-h-14 pl-2 pr-3 cursor-pointer")}
            >
                <button
                    type="button"
                    aria-label={isExpanded ? `Collapse ${outbound.tag}` : `Expand ${outbound.tag}`}
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
                    aria-label={`Select outbound ${outbound.tag}`}
                />

                {/* Enabled or not. Same dot as the inbound list. */}
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

                {/* Name + where it sends traffic */}
                <div className="min-w-0">
                    <div className="flex items-center gap-1.5 min-w-0">
                        <span
                            className={cn("font-mono font-semibold text-sm truncate", disabled && "text-muted-foreground")}
                            title={outbound.tag}
                        >
                            {outbound.tag}
                        </span>
                        {isDefault && (
                            <Tooltip>
                                <TooltipTrigger asChild>
                                    <Badge
                                        variant="outline"
                                        className="h-4 px-1 text-[9px] uppercase tracking-wide font-medium text-primary border-primary/30 bg-primary/5 shrink-0"
                                    >
                                        Default
                                    </Badge>
                                </TooltipTrigger>
                                <TooltipContent>Xray's default outbound: traffic no rule matches goes here</TooltipContent>
                            </Tooltip>
                        )}
                    </div>
                    <div className="flex items-center gap-1.5 text-[11px] font-mono text-muted-foreground truncate">
                        {disabled && <span className="font-medium text-foreground/70 shrink-0">Disabled</span>}
                        {disabled && <span className="text-muted-foreground/50 shrink-0">·</span>}
                        <span className="truncate" title={route}>{route}</span>
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
                            protocolColors[outbound.protocol.toLowerCase()] || "",
                            disabled && "opacity-60",
                        )}
                    >
                        {outbound.protocol.toUpperCase()}
                    </Badge>
                    {streamed && (
                        <span className="font-mono text-[11px] text-muted-foreground truncate">
                            {(outbound.network || "tcp").toUpperCase()}
                            {outbound.security && outbound.security !== "none" && ` · ${outbound.security.toUpperCase()}`}
                        </span>
                    )}
                </div>

                {/* Used by */}
                <div className="text-right">
                    {usageCount > 0 ? (
                        <>
                            <div className="font-mono text-sm tabular-nums">{usageCount}</div>
                            <div className="text-[11px] text-muted-foreground">{usageCount === 1 ? "rule" : "rules"}</div>
                        </>
                    ) : (
                        <span className="font-mono text-sm text-muted-foreground/40">—</span>
                    )}
                </div>

                {/* Traffic — one total; the split waits in the tooltip. */}
                <div className="text-right font-mono text-sm tabular-nums text-muted-foreground">
                    {trafficBytes > 0 ? (
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <span className="cursor-help">{formatBytes(trafficBytes)}</span>
                            </TooltipTrigger>
                            <TooltipContent className="font-mono text-xs">
                                <span className="inline-flex items-center gap-1"><HiOutlineUpload className="w-3 h-3" />{formatBytes(outbound.uplink || 0)}</span>
                                <span className="mx-2 opacity-50">/</span>
                                <span className="inline-flex items-center gap-1"><HiOutlineDownload className="w-3 h-3" />{formatBytes(outbound.downlink || 0)}</span>
                            </TooltipContent>
                        </Tooltip>
                    ) : (
                        <span className="text-muted-foreground/40">—</span>
                    )}
                </div>

                {/* Test */}
                <OutboundTestCell
                    entry={testEntry}
                    isTesting={isTesting}
                    canTest={canTest}
                    disabled={testDisabled}
                    protocol={outbound.protocol}
                    onTest={onTest}
                    onView={onViewTest}
                />

                {/* Actions. Edit first; Delete last and alone in red. */}
                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={`Actions for ${outbound.tag}`}
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
                        <DropdownMenuItem onClick={onTest} disabled={!canTest || isTesting || testDisabled}>
                            <HiOutlineStatusOnline className="w-4 h-4 mr-2" />
                            Test
                        </DropdownMenuItem>
                        {canTest && speedtestSupported && (
                            <DropdownMenuItem onClick={onTestSpeed} disabled={isTesting || testDisabled}>
                                <HiOutlineStatusOnline className="w-4 h-4 mr-2" />
                                Test with speedtest
                            </DropdownMenuItem>
                        )}
                        <DropdownMenuItem
                            onClick={async () => {
                                await copyToClipboard(outbound.tag)
                                toast.success("Tag copied")
                            }}
                        >
                            <HiOutlineClipboardCopy className="w-4 h-4 mr-2" />
                            Copy tag
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onClick={onToggleDisabled}>
                            {disabled
                                ? <><Power className="w-4 h-4 mr-2" />Enable</>
                                : <><HiOutlineBan className="w-4 h-4 mr-2" />Disable</>}
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
