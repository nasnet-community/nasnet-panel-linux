import { useMemo } from "react"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import {
    HiOutlineGlobeAlt,
    HiOutlineClipboardCopy,
    HiOutlineDatabase,
    HiOutlinePause,
    HiOutlinePlay,
    HiOutlineX,
    HiOutlinePlus,
    HiOutlineRefresh,
    HiOutlineDotsVertical,
} from "react-icons/hi"
import type { Subscription } from "@/lib/types"
import { cn, formatBytes, formatCompact, getExpiryInfo } from "@/lib/utils"
import { SubscriptionStatusBadge } from "@/components/ui/status-badge"
import { EmptyState } from "@/components/ui/empty-state"

interface UserSubscriptionsTabProps {
    subscriptions: Subscription[]
    isSubMutationPending: boolean
    onToggle: (sub: Subscription, checked: boolean) => void
    onCopyLink: (sub: Subscription) => void
    onSetDataLimit: (sub: Subscription) => void
    onExtend: (sub: Subscription) => void
    onResetData: (sub: Subscription) => void
    onPause: (subId: number) => void
    onResume: (subId: number) => void
    onRevoke: (sub: Subscription) => void
    onOpenDetails: (sub: Subscription) => void
    onCreateSubscription?: () => void
}

function canToggle(sub: Subscription) {
    return sub.status === "active" || sub.status === "paused"
}

function DataUsageBar({ used, limit, maxUsage = 0, isTerminated = false }: { used: number; limit: number; maxUsage?: number; isTerminated?: boolean }) {
    const percentage = limit > 0 ? Math.min((used / limit) * 100, 100) : 0
    const isUnlimited = limit === 0
    const relativePercent = isUnlimited && maxUsage > 0 ? Math.max(2, (used / maxUsage) * 100) : 0

    if (isTerminated) {
        return (
            <div className="space-y-1 min-w-[120px] opacity-40">
                <div className="flex justify-between text-xs">
                    <span>{formatBytes(used)}</span>
                    <span className="text-muted-foreground">Terminated</span>
                </div>
            </div>
        )
    }

    return (
        <div className="space-y-1 min-w-[120px]">
            <div className="flex justify-between text-xs">
                <span>{formatBytes(used)}</span>
                <span className="text-muted-foreground">
                    {isUnlimited ? "Unlimited" : formatBytes(limit)}
                </span>
            </div>
            <div className="h-1.5 bg-muted rounded-full overflow-hidden">
                <div
                    className={cn(
                        "h-full rounded-full transition-all",
                        isUnlimited ? "bg-emerald-500" :
                            percentage > 90 ? "bg-red-500" :
                                percentage > 70 ? "bg-amber-500" : "bg-emerald-500"
                    )}
                    style={{ width: isUnlimited ? `${relativePercent}%` : `${percentage}%` }}
                />
            </div>
            {!isUnlimited && percentage > 0 && (
                <p className="text-[10px] text-muted-foreground">{percentage.toFixed(1)}% used</p>
            )}
        </div>
    )
}

export function UserSubscriptionsTab({
    subscriptions,
    isSubMutationPending,
    onToggle,
    onCopyLink,
    onSetDataLimit,
    onExtend,
    onResetData,
    onPause,
    onResume,
    onRevoke,
    onOpenDetails,
    onCreateSubscription,
}: UserSubscriptionsTabProps) {
    const maxUnlimitedUsage = useMemo(() => {
        return subscriptions.reduce((max, s) => {
            const limit = s.custom_data_limit ?? s.data_limit
            if (limit === 0) return Math.max(max, s.data_used)
            return max
        }, 0)
    }, [subscriptions])

    return (
        <Card className="border-0 shadow-sm overflow-hidden">
            <CardContent className="p-0">
                {onCreateSubscription && (
                    <div className="flex items-center justify-between p-3 border-b">
                        <span className="text-xs text-muted-foreground">
                            {subscriptions.length} subscription{subscriptions.length !== 1 ? "s" : ""}
                        </span>
                        <Button variant="default" size="sm" onClick={onCreateSubscription}>
                            <HiOutlinePlus className="w-4 h-4 mr-1" /> Create Subscription
                        </Button>
                    </div>
                )}
                {subscriptions.length > 0 ? (
                    <>
                        {/* Desktop Table */}
                        <div className="hidden md:block overflow-x-auto">
                            <Table>
                                <TableHeader className="bg-muted/30">
                                    <TableRow>
                                        <TableHead className="w-[50px]">Active</TableHead>
                                        <TableHead className="w-[60px]">ID</TableHead>
                                        <TableHead>Plan</TableHead>
                                        <TableHead>Status</TableHead>
                                        <TableHead>Data Usage</TableHead>
                                        <TableHead className="text-center w-[90px]">Total Traffic</TableHead>
                                        <TableHead>Expires</TableHead>
                                        <TableHead className="w-[100px] text-right">Actions</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {subscriptions.map((sub) => {
                                        const isTerminated = sub.status === "cancelled" || sub.status === "expired"
                                        const expiryDate = sub.is_end_date_custom ? sub.custom_end_date : sub.end_date
                                        const expiryInfo = getExpiryInfo(expiryDate)

                                        return (
                                            <TableRow
                                                key={sub.id}
                                                className="group cursor-pointer hover:bg-muted/50"
                                                onClick={(e) => {
                                                    const target = e.target as HTMLElement
                                                    if (target.closest("a") || target.closest("button") || target.closest('[role="menuitem"]') || target.closest('[role="switch"]')) return
                                                    onOpenDetails(sub)
                                                }}
                                            >
                                                <TableCell>
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <div>
                                                                <Switch
                                                                    checked={sub.status === "active"}
                                                                    onCheckedChange={(checked) => onToggle(sub, checked)}
                                                                    disabled={!canToggle(sub) || isSubMutationPending}
                                                                    className="data-[state=checked]:bg-emerald-500"
                                                                />
                                                            </div>
                                                        </TooltipTrigger>
                                                        <TooltipContent side="right">
                                                            {canToggle(sub)
                                                                ? sub.status === "active" ? "Pause subscription" : "Resume subscription"
                                                                : `Cannot toggle (${sub.status})`}
                                                        </TooltipContent>
                                                    </Tooltip>
                                                </TableCell>
                                                <TableCell className="font-mono text-xs text-muted-foreground">#{sub.id}</TableCell>
                                                <TableCell>
                                                    <div className="font-medium">{sub.label || "\u2014"}</div>
                                                </TableCell>
                                                <TableCell>
                                                    <SubscriptionStatusBadge status={sub.status} />
                                                </TableCell>
                                                <TableCell>
                                                    <DataUsageBar
                                                        used={sub.data_used}
                                                        limit={sub.custom_data_limit ?? sub.data_limit}
                                                        maxUsage={maxUnlimitedUsage}
                                                        isTerminated={isTerminated}
                                                    />
                                                </TableCell>
                                                <TableCell className="text-center">
                                                    <div className="flex flex-col items-center gap-0.5 text-xs font-mono text-muted-foreground">
                                                        <span className="inline-flex items-center gap-0.5">
                                                            ↑{formatCompact(sub.lifetime_data_upload || 0)}
                                                        </span>
                                                        <span className="inline-flex items-center gap-0.5">
                                                            ↓{formatCompact(sub.lifetime_data_download || 0)}
                                                        </span>
                                                    </div>
                                                </TableCell>
                                                <TableCell className="text-muted-foreground text-sm">
                                                    <div title={expiryDate ? new Date(expiryDate).toLocaleString() : "Unlimited duration"}>
                                                        <Badge
                                                            variant={expiryInfo.variant}
                                                            className={`text-xs px-2 py-0.5 font-medium ${expiryInfo.variant === "secondary" ? "bg-blue-900 text-blue-100" : ""}`}
                                                        >
                                                            {expiryInfo.text}
                                                        </Badge>
                                                    </div>
                                                </TableCell>
                                                <TableCell>
                                                    <div className="flex items-center justify-end gap-1">
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <Button variant="ghost" size="icon" className="h-8 w-8 opacity-0 group-hover:opacity-100 transition-opacity" onClick={() => onCopyLink(sub)}>
                                                                    <HiOutlineClipboardCopy className="w-4 h-4" />
                                                                </Button>
                                                            </TooltipTrigger>
                                                            <TooltipContent>Copy subscription link</TooltipContent>
                                                        </Tooltip>
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <Button variant="ghost" size="icon" className="h-8 w-8 opacity-0 group-hover:opacity-100 transition-opacity" onClick={() => onSetDataLimit(sub)}>
                                                                    <HiOutlineDatabase className="w-4 h-4" />
                                                                </Button>
                                                            </TooltipTrigger>
                                                            <TooltipContent>Change data limit</TooltipContent>
                                                        </Tooltip>
                                                        <DropdownMenu>
                                                            <DropdownMenuTrigger asChild>
                                                                <Button variant="ghost" size="icon" className="h-8 w-8" disabled={isSubMutationPending}>
                                                                    <HiOutlineDotsVertical className="w-4 h-4" />
                                                                </Button>
                                                            </DropdownMenuTrigger>
                                                            <DropdownMenuContent align="end" side="left">
                                                                <DropdownMenuItem onClick={() => onExtend(sub)}>
                                                                    <HiOutlinePlus className="w-4 h-4 mr-2" /> Extend
                                                                </DropdownMenuItem>
                                                                <DropdownMenuItem onClick={() => onCopyLink(sub)}>
                                                                    <HiOutlineClipboardCopy className="w-4 h-4 mr-2" /> Copy Link
                                                                </DropdownMenuItem>
                                                                <DropdownMenuItem onClick={() => onSetDataLimit(sub)}>
                                                                    <HiOutlineDatabase className="w-4 h-4 mr-2" /> Data Limit
                                                                </DropdownMenuItem>
                                                                <DropdownMenuItem onClick={() => onResetData(sub)}>
                                                                    <HiOutlineRefresh className="w-4 h-4 mr-2" /> Reset Usage
                                                                </DropdownMenuItem>
                                                                <DropdownMenuSeparator />
                                                                {sub.status === "active" && (
                                                                    <DropdownMenuItem onClick={() => onPause(sub.id)}>
                                                                        <HiOutlinePause className="w-4 h-4 mr-2" /> Pause
                                                                    </DropdownMenuItem>
                                                                )}
                                                                {sub.status === "paused" && (
                                                                    <DropdownMenuItem onClick={() => onResume(sub.id)}>
                                                                        <HiOutlinePlay className="w-4 h-4 mr-2" /> Resume
                                                                    </DropdownMenuItem>
                                                                )}
                                                                <DropdownMenuSeparator />
                                                                <DropdownMenuItem onClick={() => onRevoke(sub)} className="text-red-500">
                                                                    <HiOutlineX className="w-4 h-4 mr-2" /> Revoke
                                                                </DropdownMenuItem>
                                                            </DropdownMenuContent>
                                                        </DropdownMenu>
                                                    </div>
                                                </TableCell>
                                            </TableRow>
                                        )
                                    })}
                                </TableBody>
                            </Table>
                        </div>

                        {/* Mobile Card View */}
                        <div className="md:hidden divide-y">
                            {subscriptions.map((sub) => {
                                const isTerminated = sub.status === "cancelled" || sub.status === "expired"
                                const expiryDate = sub.is_end_date_custom ? sub.custom_end_date : sub.end_date
                                const expiryInfo = getExpiryInfo(expiryDate)

                                return (
                                    <div
                                        key={sub.id}
                                        className="p-4 space-y-3 cursor-pointer active:bg-muted/30"
                                        onClick={(e) => {
                                            const target = e.target as HTMLElement
                                            if (target.closest("button") || target.closest('[role="menuitem"]') || target.closest('[role="switch"]')) return
                                            onOpenDetails(sub)
                                        }}
                                    >
                                        <div className="flex items-start justify-between">
                                            <div className="flex items-center gap-3">
                                                <Switch
                                                    checked={sub.status === "active"}
                                                    onCheckedChange={(checked) => onToggle(sub, checked)}
                                                    disabled={!canToggle(sub) || isSubMutationPending}
                                                    className="data-[state=checked]:bg-emerald-500 shrink-0"
                                                />
                                                <div>
                                                    <div className="font-medium">{sub.label || "\u2014"}</div>
                                                    <div className="text-xs text-muted-foreground">#{sub.id}</div>
                                                </div>
                                            </div>
                                            <SubscriptionStatusBadge status={sub.status} />
                                        </div>
                                        <DataUsageBar
                                            used={sub.data_used}
                                            limit={sub.custom_data_limit ?? sub.data_limit}
                                            maxUsage={maxUnlimitedUsage}
                                            isTerminated={isTerminated}
                                        />
                                        <div className="flex items-center text-xs font-mono text-muted-foreground pt-1 border-t border-border/50 mt-2">
                                            <span className="text-blue-500">{"↑"}{formatCompact(sub.lifetime_data_upload || 0)}</span>
                                            <span className="mx-1">{"·"}</span>
                                            <span className="text-emerald-500">{"↓"}{formatCompact(sub.lifetime_data_download || 0)}</span>
                                        </div>
                                        <div className="flex items-center justify-between">
                                            <div title={expiryDate ? new Date(expiryDate).toLocaleString() : "Unlimited duration"}>
                                                <Badge
                                                    variant={expiryInfo.variant}
                                                    className={`text-[10px] px-1.5 py-0 font-medium ${expiryInfo.variant === "secondary" ? "bg-blue-900 text-blue-100" : ""}`}
                                                >
                                                    {expiryInfo.text}
                                                </Badge>
                                            </div>
                                            <div className="flex items-center gap-1">
                                                <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => onCopyLink(sub)}>
                                                    <HiOutlineClipboardCopy className="w-4 h-4" />
                                                </Button>
                                                <DropdownMenu>
                                                    <DropdownMenuTrigger asChild>
                                                        <Button variant="ghost" size="icon" className="h-8 w-8">
                                                            <HiOutlineDotsVertical className="w-4 h-4" />
                                                        </Button>
                                                    </DropdownMenuTrigger>
                                                    <DropdownMenuContent align="end">
                                                        <DropdownMenuItem onClick={() => onExtend(sub)}>
                                                            <HiOutlinePlus className="w-4 h-4 mr-2" /> Extend
                                                        </DropdownMenuItem>
                                                        <DropdownMenuItem onClick={() => onSetDataLimit(sub)}>
                                                            <HiOutlineDatabase className="w-4 h-4 mr-2" /> Data Limit
                                                        </DropdownMenuItem>
                                                        <DropdownMenuItem onClick={() => onResetData(sub)}>
                                                            <HiOutlineRefresh className="w-4 h-4 mr-2" /> Reset Usage
                                                        </DropdownMenuItem>
                                                        <DropdownMenuSeparator />
                                                        {sub.status === "active" && (
                                                            <DropdownMenuItem onClick={() => onPause(sub.id)}>
                                                                <HiOutlinePause className="w-4 h-4 mr-2" /> Pause
                                                            </DropdownMenuItem>
                                                        )}
                                                        {sub.status === "paused" && (
                                                            <DropdownMenuItem onClick={() => onResume(sub.id)}>
                                                                <HiOutlinePlay className="w-4 h-4 mr-2" /> Resume
                                                            </DropdownMenuItem>
                                                        )}
                                                        <DropdownMenuSeparator />
                                                        <DropdownMenuItem onClick={() => onRevoke(sub)} className="text-red-500">
                                                            <HiOutlineX className="w-4 h-4 mr-2" /> Revoke
                                                        </DropdownMenuItem>
                                                    </DropdownMenuContent>
                                                </DropdownMenu>
                                            </div>
                                        </div>
                                    </div>
                                )
                            })}
                        </div>
                    </>
                ) : (
                    <EmptyState
                        icon={HiOutlineGlobeAlt}
                        title="No subscriptions"
                        description="This user hasn't subscribed to any plans yet."
                    />
                )}
            </CardContent>
        </Card>
    )
}
