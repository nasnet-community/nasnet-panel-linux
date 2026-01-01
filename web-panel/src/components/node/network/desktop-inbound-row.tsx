import { cn, formatBytes, copyToClipboard } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Button } from "@/components/ui/button"
import {
    Tooltip, TooltipContent, TooltipTrigger,
} from "@/components/ui/tooltip"
import {
    HiChevronRight, HiOutlineCog, HiOutlineSwitchHorizontal,
    HiOutlineTrash, HiOutlineBan, HiOutlineStatusOnline,
    HiOutlineUsers, HiOutlineClock, HiOutlineArrowsExpand,
} from "react-icons/hi"
import { protocolColors } from "@/components/node/protocol-badge"
import type { Inbound } from "@/lib/types"
import { toast } from "sonner"

interface DesktopInboundRowProps {
    inbound: Inbound
    isExpanded: boolean
    isSelected: boolean
    accountCount: number
    onlineCount: number
    hasOnline: boolean
    counts: { online: number; disabled: number; expired: number; trafficBytes: number } | undefined
    details: { label: string; value: string }[]
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
        inbound, isExpanded, isSelected, accountCount, onlineCount, hasOnline,
        counts, details, onToggleExpand, onToggleSelect,
        onToggleDisabled, onEdit, onMigrate, onDelete, expandedContent,
    } = props

    return (
        <div
            className={cn(
                "group relative rounded-xl border bg-card/60 backdrop-blur-sm overflow-hidden transition-all duration-200",
                "hover:border-primary/30 hover:shadow-lg hover:shadow-primary/5",
                isSelected && "border-primary/50 bg-primary/5",
                inbound.is_disabled && "saturate-50",
                isExpanded && "shadow-md shadow-primary/10 border-primary/20",
            )}
        >
            {/* Left rail — animated chevron gutter */}
            <button
                type="button"
                aria-label={isExpanded ? "Collapse details" : "Expand details"}
                aria-expanded={isExpanded}
                onClick={onToggleExpand}
                className={cn(
                    "absolute inset-y-0 left-0 w-8 flex items-center justify-center",
                    "bg-gradient-to-r from-muted/30 to-transparent",
                    "text-muted-foreground/50 hover:text-foreground hover:bg-muted/40",
                    "focus-visible:outline-none focus-visible:bg-muted/60 focus-visible:text-foreground",
                    "transition-colors",
                )}
            >
                <HiChevronRight
                    className={cn(
                        "w-4 h-4 transition-transform duration-300 ease-out",
                        isExpanded && "rotate-90",
                    )}
                />
            </button>

            <div
                role="button"
                tabIndex={0}
                aria-expanded={isExpanded}
                aria-label={`Inbound ${inbound.tag} on port ${inbound.port}`}
                onClick={onToggleExpand}
                onKeyDown={(e) => {
                    if ((e.key === "Enter" || e.key === " ") && e.target === e.currentTarget) {
                        e.preventDefault(); onToggleExpand()
                    }
                }}
                className="pl-10 pr-3 py-3 grid grid-cols-[auto_minmax(0,1fr)_minmax(0,1.2fr)_auto_auto] gap-x-4 items-center cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-xl"
            >
                {/* Col 1: Checkbox + status dot */}
                <div className="flex items-center gap-2.5 shrink-0">
                    <Checkbox
                        checked={isSelected}
                        onCheckedChange={onToggleSelect}
                        onClick={(e) => e.stopPropagation()}
                        aria-label={`Select inbound ${inbound.tag}`}
                        className="data-[state=checked]:bg-primary data-[state=checked]:border-primary"
                    />
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <span className="relative flex h-2.5 w-2.5 shrink-0" aria-hidden>
                                {hasOnline && (
                                    <span className="motion-safe:animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />
                                )}
                                <span className={cn(
                                    "relative inline-flex rounded-full h-2.5 w-2.5",
                                    hasOnline ? "bg-green-500" : accountCount > 0 ? "bg-amber-500/60" : "bg-zinc-500/40",
                                )} />
                            </span>
                        </TooltipTrigger>
                        <TooltipContent>
                            {hasOnline ? `${onlineCount} online` : accountCount > 0 ? "No active users" : "No accounts"}
                        </TooltipContent>
                    </Tooltip>
                </div>

                {/* Col 2: Identity */}
                <div className="min-w-0">
                    <div className="flex items-baseline gap-2 min-w-0">
                        <span className="font-mono font-semibold text-sm truncate" title={inbound.tag}>{inbound.tag}</span>
                        <span className="font-mono text-primary font-bold text-sm shrink-0">:{inbound.port}</span>
                        {inbound.is_disabled && (
                            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 bg-red-500/10 text-red-400 border-red-500/20 shrink-0">
                                Disabled
                            </Badge>
                        )}
                    </div>
                    {inbound.remark && (
                        <p className="text-[11px] text-muted-foreground/70 truncate mt-0.5" title={inbound.remark}>
                            {inbound.remark}
                        </p>
                    )}
                </div>

                {/* Col 3: Badges + details */}
                <div className="min-w-0 flex flex-col gap-1.5">
                    <div className="flex flex-wrap items-center gap-1.5">
                        <Badge variant="outline" className={cn("font-mono text-[10px] px-1.5 py-0 h-5", protocolColors[inbound.protocol.toLowerCase()] || "")}>
                            {inbound.protocol.toUpperCase()}
                        </Badge>
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-zinc-500/10 border-zinc-500/20">
                            {(inbound.network || "tcp").toUpperCase()}
                        </Badge>
                        <Badge
                            variant="outline"
                            className={cn(
                                "text-[10px] px-1.5 py-0 h-5",
                                inbound.security === "reality" && "bg-gradient-to-r from-cyan-500/20 to-purple-500/20 text-cyan-400 border-cyan-500/30",
                                inbound.security === "tls" && "bg-emerald-500/15 text-emerald-400 border-emerald-500/30",
                                (!inbound.security || inbound.security === "none") && "bg-zinc-500/15 text-muted-foreground border-zinc-500/30",
                            )}
                        >
                            {(inbound.security || "none").toUpperCase()}
                        </Badge>
                        {inbound.sniffing_settings?.enabled && (
                            <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-5 border-blue-500/30 text-blue-400 bg-blue-500/10">
                                SNIFF
                            </Badge>
                        )}
                    </div>
                    {details.length > 0 && (
                        <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[10px] font-mono text-muted-foreground/80">
                            {details.map((d, idx) => (
                                <Tooltip key={idx}>
                                    <TooltipTrigger asChild>
                                        <button
                                            type="button"
                                            className="flex items-center gap-1 hover:text-foreground transition-colors truncate max-w-[240px] focus-visible:outline-none focus-visible:text-foreground"
                                            onClick={async (e) => {
                                                e.stopPropagation()
                                                await copyToClipboard(d.value)
                                                toast.success(`${d.label} copied`)
                                            }}
                                        >
                                            <span className="text-muted-foreground/50 uppercase tracking-wider">{d.label}</span>
                                            <span className="truncate">{d.value}</span>
                                        </button>
                                    </TooltipTrigger>
                                    <TooltipContent>Click to copy: {d.value}</TooltipContent>
                                </Tooltip>
                            ))}
                        </div>
                    )}
                </div>

                {/* Col 4: Stats — fixed-width chip lane */}
                <div className="flex items-center gap-1.5 shrink-0 min-w-[220px] justify-end">
                    {(counts?.trafficBytes || 0) > 0 && (
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <div className="flex items-center gap-1 px-1.5 h-6 rounded-md bg-violet-500/10 border border-violet-500/20 cursor-help">
                                    <HiOutlineArrowsExpand className="w-3 h-3 text-violet-400" />
                                    <span className="text-[10px] font-mono font-medium text-violet-400 tabular-nums">
                                        {formatBytes(counts!.trafficBytes)}
                                    </span>
                                </div>
                            </TooltipTrigger>
                            <TooltipContent>All-time Traffic — {accountCount} accounts</TooltipContent>
                        </Tooltip>
                    )}
                    {accountCount > 0 && (
                        <>
                            <StatPill count={accountCount} Icon={HiOutlineUsers} color="emerald" title="Total" />
                            {(counts?.disabled || 0) > 0 && <StatPill count={counts!.disabled} Icon={HiOutlineBan} color="zinc" title="Disabled" />}
                            {(counts?.expired || 0) > 0 && <StatPill count={counts!.expired} Icon={HiOutlineClock} color="red" title="Expired" />}
                            {onlineCount > 0 && <StatPill count={onlineCount} Icon={HiOutlineStatusOnline} color="blue" title="Online" />}
                        </>
                    )}
                </div>

                {/* Col 5: Actions */}
                <div className="flex items-center gap-0.5 opacity-60 group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
                    <ActionBtn label={inbound.is_disabled ? "Enable" : "Disable"} onClick={onToggleDisabled} color={inbound.is_disabled ? "emerald" : "amber"}>
                        {inbound.is_disabled ? <HiOutlineStatusOnline className="w-4 h-4" /> : <HiOutlineBan className="w-4 h-4" />}
                    </ActionBtn>
                    <ActionBtn label="Edit" onClick={onEdit}>
                        <HiOutlineCog className="w-4 h-4" />
                    </ActionBtn>
                    <ActionBtn label="Migrate" onClick={onMigrate} color="blue">
                        <HiOutlineSwitchHorizontal className="w-4 h-4" />
                    </ActionBtn>
                    <ActionBtn label="Delete" onClick={onDelete} color="red">
                        <HiOutlineTrash className="w-4 h-4" />
                    </ActionBtn>
                </div>
            </div>

            {/* Expanded content — CSS grid animation for height */}
            <div
                className={cn(
                    "grid transition-[grid-template-rows] duration-300 ease-out",
                    isExpanded ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
                )}
            >
                <div className="overflow-hidden">
                    <div className="border-t border-white/5 bg-muted/5 p-4 space-y-4">
                        {expandedContent}
                    </div>
                </div>
            </div>
        </div>
    )
}

function StatPill({ count, Icon, color, title }: {
    count: number
    Icon: React.ComponentType<{ className?: string }>
    color: "emerald" | "zinc" | "red" | "blue"
    title: string
}) {
    const map = {
        emerald: "text-emerald-500 bg-emerald-500/10 border-emerald-500/20",
        zinc: "text-muted-foreground bg-zinc-500/10 border-zinc-500/20",
        red: "text-red-500 bg-red-500/10 border-red-500/20",
        blue: "text-blue-500 bg-blue-500/10 border-blue-500/20",
    } as const
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <div className={cn("flex items-center gap-1 px-1.5 h-6 rounded-md border cursor-help min-w-[36px] justify-center", map[color])}>
                    <Icon className="w-3 h-3" />
                    <span className="text-[10px] font-mono font-medium tabular-nums">{count}</span>
                </div>
            </TooltipTrigger>
            <TooltipContent>{title}: {count}</TooltipContent>
        </Tooltip>
    )
}

function ActionBtn({ children, label, onClick, color }: {
    children: React.ReactNode
    label: string
    onClick: () => void
    color?: "emerald" | "amber" | "blue" | "red"
}) {
    const colorMap = {
        emerald: "text-green-500 hover:text-green-600 hover:bg-green-500/10",
        amber: "text-amber-500 hover:text-amber-600 hover:bg-amber-500/10",
        blue: "text-blue-500 hover:text-blue-600 hover:bg-blue-500/10",
        red: "text-red-500 hover:text-red-600 hover:bg-red-500/10",
    } as const
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    variant="ghost"
                    size="icon"
                    className={cn("h-8 w-8", color && colorMap[color])}
                    onClick={(e) => { e.stopPropagation(); onClick() }}
                    aria-label={label}
                >
                    {children}
                </Button>
            </TooltipTrigger>
            <TooltipContent>{label}</TooltipContent>
        </Tooltip>
    )
}
