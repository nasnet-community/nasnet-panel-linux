import { useEffect, useState, useMemo, useRef } from "react"
import { Link } from "react-router"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import { FilterPills, type FilterPill } from "@/components/ui/filter-pills"
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
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import {
    HiOutlineGlobeAlt,
    HiOutlineRefresh,
    HiOutlineDotsVertical,
    HiOutlineUser,
    HiOutlinePause,
    HiOutlinePlay,
    HiOutlineX,
    HiOutlineSearch,
    HiOutlineClipboardCopy,
    HiOutlineDatabase,
    HiOutlineClock,
    HiOutlineServer,
    HiArrowUp,
    HiArrowDown,
    HiOutlineSortAscending,
    HiOutlineSortDescending,
    HiOutlineAdjustments,
} from "react-icons/hi"
import { HiOutlineFunnel } from "react-icons/hi2"
import type { Subscription } from "@/lib/types"
import { BANDWIDTH_OPTIONS } from "@/lib/types"
import { cn, formatBytes as formatBytesUtil, formatCompact, getExpiryInfo, copyToClipboard } from "@/lib/utils"
import { PaginationNav } from "@/components/ui/pagination-nav"
import { EmptyState } from "@/components/ui/empty-state"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { SubscriptionStatusBadge } from "@/components/ui/status-badge"
import {
    useSubscriptions,
    usePauseSubscription,
    useResumeSubscription,
    useRevokeSubscription,
    useResetData,
    useDeleteSubscription,
    useBulkSubscriptionAction,
    useBulkSetBandwidthLimit,
    useSubscriptionCounts,
    useOnlineUsersWithIPs,
} from "@/lib/queries"
import { useSubscriptionsStore, type SubscriptionSortField } from "@/store/subscriptions-store"
import { useQueryClient } from "@tanstack/react-query"
import { AutoRefreshControl } from "@/components/ui/auto-refresh-control"
import { toast } from "sonner"
import { Plus, Trash2, Pause, Play, X as XIcon, Ban, Wifi, Network } from "lucide-react"
import { motion, AnimatePresence } from "framer-motion"
import { SubscriptionDetailsSheet } from "@/components/subscription/subscription-details-sheet"
import { CreateManualSubscriptionDialog } from "@/components/subscription/create-manual-subscription-dialog"
import { DataLimitDialog } from "@/components/subscription/data-limit-dialog"
import { SwipeableSubscriptionRow } from "@/components/subscription/swipeable-subscription-row"
import { BulkManageInboundsDialog } from "@/components/subscription/bulk-manage-inbounds-dialog"
import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { getUsageBarColor } from "@/lib/constants/usage-thresholds"

// Data usage progress bar with percentage
// When unlimited, uses relative scaling against maxUsage to visually differentiate usage levels
function DataUsageBar({ used, limit, maxUsage = 0, isTerminated = false }: { used: number; limit: number; maxUsage?: number; isTerminated?: boolean }) {
    const percentage = limit > 0 ? Math.min((used / limit) * 100, 100) : 0
    const isUnlimited = limit === 0
    const relativePercent = isUnlimited && maxUsage > 0 ? Math.max(2, (used / maxUsage) * 100) : 0

    if (isTerminated) {
        return (
            <div className="space-y-0.5 min-w-[120px] opacity-40">
                <div className="flex justify-between text-xs">
                    <span>{formatBytesUtil(used)}</span>
                    <span className="text-muted-foreground">Terminated</span>
                </div>
            </div>
        )
    }

    return (
        <div className="space-y-0.5 min-w-[120px]">
            <div className="flex justify-between text-xs">
                <span>{formatBytesUtil(used)}</span>
                <span className="text-muted-foreground">
                    {isUnlimited ? "Unlimited" : formatBytesUtil(limit)}
                </span>
            </div>
            <div className="h-1 bg-muted rounded-full overflow-hidden">
                <div
                    className={cn(
                        "h-full rounded-full transition-all",
                        isUnlimited ? "bg-emerald-500" : getUsageBarColor(percentage)
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

// Sortable column header (reused pattern from users page)
function SortableHeader({
    children,
    field,
    currentSort,
    currentDir,
    onSort,
}: {
    children: React.ReactNode
    field: SubscriptionSortField
    currentSort: SubscriptionSortField
    currentDir: string
    onSort: (field: SubscriptionSortField) => void
}) {
    const isActive = currentSort === field
    return (
        <button
            onClick={() => onSort(field)}
            className="inline-flex items-center gap-1 hover:text-foreground transition-colors"
        >
            {children}
            {isActive && (
                currentDir === "asc" ?
                    <HiOutlineSortAscending className="w-4 h-4" /> :
                    <HiOutlineSortDescending className="w-4 h-4" />
            )}
        </button>
    )
}

// Table skeleton
function TableSkeleton() {
    return (
        <>
            {[1, 2, 3, 4, 5].map((i) => (
                <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                    <TableCell><Skeleton className="h-3 w-3 rounded-full" /></TableCell>
                    <TableCell><Skeleton className="h-5 w-9" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-10" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell className="text-center"><Skeleton className="h-4 w-8 mx-auto" /></TableCell>
                    <TableCell><Skeleton className="h-6 w-16 rounded-full" /></TableCell>
                    <TableCell className="text-center"><Skeleton className="h-4 w-6 mx-auto" /></TableCell>
                    <TableCell><Skeleton className="h-6 w-28" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-8 w-16" /></TableCell>
                </TableRow>
            ))}
        </>
    )
}

export default function SubscriptionsPage() {
    const queryClient = useQueryClient()
    const confirmDialog = useConfirm()
    const perPage = 20

    // Zustand store for UI state
    const {
        status, page, search, selectedSubscriptions,
        isMultiSelectMode, mobileSearchExpanded,
        sourceFilter, exhaustedFilter, filtersExpanded,
        sortField, sortDir, toggleSort,
        setStatus, setPage, setSearch, openDetailsSheet, openCreateManualDialog,
        setSourceFilter, setExhaustedFilter,
        clearAdvancedFilters, toggleFiltersExpanded,
        toggleSelectSubscription, selectAll, clearSelection,
        enterMultiSelectMode, exitMultiSelectMode, setMobileSearchExpanded,
        mobileFilterSheetOpen, setMobileFilterSheetOpen,
    } = useSubscriptionsStore()

    // Debounced search
    const [debouncedSearch, setDebouncedSearch] = useState(search)
    useEffect(() => {
        const timer = setTimeout(() => setDebouncedSearch(search), 300)
        return () => clearTimeout(timer)
    }, [search])

    // Data limit dialog state
    const [dataLimitSub, setDataLimitSub] = useState<Subscription | null>(null)

    // Bulk manage inbounds dialog state
    const [bulkInboundsOpen, setBulkInboundsOpen] = useState(false)

    // Mobile swipe row management — only one row open at a time
    const [openSwipeRowId, setOpenSwipeRowId] = useState<number | null>(null)
    const mobileSearchRef = useRef<HTMLInputElement>(null)

    // Online users with IPs for live status indicator + IP count column
    const { data: onlineUsersWithIPs = {} } = useOnlineUsersWithIPs()
    const onlineEmails = useMemo(() => new Set(Object.keys(onlineUsersWithIPs)), [onlineUsersWithIPs])

    // Compute active advanced filter count
    const activeFilterCount = [sourceFilter, exhaustedFilter].filter(v => v !== "all").length

    // TanStack Query hooks
    const { data: subscriptions = [], isLoading, isRefetching, dataUpdatedAt } = useSubscriptions({
        status,
        page,
        perPage,
        search: debouncedSearch,
        source: sourceFilter,
        exhausted: exhaustedFilter,
        sort: sortField,
        order: sortDir,
    })

    // Subscription counts
    const { data: counts } = useSubscriptionCounts()

    // Auto-correct page when beyond results (e.g. after filter/delete changes)
    useEffect(() => {
        if (!isLoading && subscriptions.length === 0 && page > 1) {
            setPage(1)
        }
    }, [isLoading, subscriptions.length, page, setPage])

    // Mutation hooks
    const pauseMutation = usePauseSubscription()
    const resumeMutation = useResumeSubscription()
    const revokeMutation = useRevokeSubscription()
    const resetDataMutation = useResetData()
    const deleteMutation = useDeleteSubscription()
    const bulkActionMutation = useBulkSubscriptionAction()
    const bulkBandwidthMutation = useBulkSetBandwidthLimit()

    // Max data usage among unlimited subscriptions (for relative bar scaling)
    const maxUnlimitedUsage = useMemo(() => {
        return subscriptions.reduce((max, s) => {
            const limit = s.custom_data_limit ?? s.data_limit
            if (limit === 0) return Math.max(max, s.data_used)
            return max
        }, 0)
    }, [subscriptions])

    // Selection helpers
    const selectionInfo = useMemo(() => {
        const selectedSubs = subscriptions.filter(s => selectedSubscriptions.has(s.id))
        return {
            canPause: selectedSubs.some(s => s.status === "active"),
            canResume: selectedSubs.some(s => s.status === "paused"),
            canRevoke: selectedSubs.some(s => s.status !== "cancelled"),
            canDelete: selectedSubs.length > 0,
        }
    }, [subscriptions, selectedSubscriptions])

    const allOnPageSelected = subscriptions.length > 0 && subscriptions.every(s => selectedSubscriptions.has(s.id))
    const someOnPageSelected = subscriptions.some(s => selectedSubscriptions.has(s.id))

    const handleSelectAllToggle = () => {
        if (allOnPageSelected) {
            clearSelection()
        } else {
            selectAll(subscriptions.map(s => s.id))
        }
    }

    const handlePauseResume = (sub: Subscription) => {
        if (sub.status === "paused") {
            resumeMutation.mutate(sub.id)
        } else {
            pauseMutation.mutate(sub.id)
        }
    }

    const handleToggle = (sub: Subscription, checked: boolean) => {
        if (checked && sub.status === "paused") {
            resumeMutation.mutate(sub.id)
        } else if (!checked && sub.status === "active") {
            pauseMutation.mutate(sub.id)
        }
    }

    const handleRevoke = async (sub: Subscription) => {
        const confirmed = await confirmDialog({
            title: "Revoke Subscription",
            description: "Are you sure you want to revoke this subscription? The user will immediately lose access.",
            confirmLabel: "Revoke",
            variant: "destructive",
        })
        if (!confirmed) return
        revokeMutation.mutate(sub.id)
    }

    const handleDelete = async (sub: Subscription) => {
        const confirmed = await confirmDialog({
            title: "Delete Subscription Permanently",
            description: "This will permanently remove the subscription and all associated accounts from the database. This action cannot be undone.",
            confirmLabel: "Delete Permanently",
            variant: "destructive",
        })
        if (!confirmed) return
        deleteMutation.mutate(sub.id)
    }

    const handleResetData = async (sub: Subscription) => {
        const confirmed = await confirmDialog({
            title: "Reset Data Usage",
            description: "Are you sure you want to reset the data usage to 0? This cannot be undone.",
            confirmLabel: "Reset",
            variant: "destructive",
        })
        if (!confirmed) return
        resetDataMutation.mutate(sub.id)
    }

    const handleCopyLink = async (sub: Subscription) => {
        const link = sub.subscription_url || sub.sub_link
        if (link) {
            await copyToClipboard(link)
            toast.success("Subscription link copied")
        } else {
            toast.error("No subscription link available")
        }
    }

    // Bulk action handlers
    const handleBulkAction = async (action: string, label: string) => {
        const ids = Array.from(selectedSubscriptions)
        const confirmed = await confirmDialog({
            title: `Bulk ${label}`,
            description: `Are you sure you want to ${label.toLowerCase()} ${ids.length} subscription${ids.length > 1 ? "s" : ""}?${action === "delete" ? " This action cannot be undone." : ""}`,
            confirmLabel: label,
            variant: action === "delete" || action === "revoke" ? "destructive" : "default",
        })
        if (!confirmed) return
        bulkActionMutation.mutate({ action, ids }, {
            onSuccess: () => clearSelection(),
        })
    }

    const handleBulkBandwidth = async (limitMbps: number | null) => {
        const ids = Array.from(selectedSubscriptions)
        const label = limitMbps === null ? "reset to default" : limitMbps === 0 ? "set to unlimited" : `set to ${limitMbps} Mbps`
        const confirmed = await confirmDialog({
            title: "Bulk Speed Change",
            description: `Are you sure you want to ${label} for ${ids.length} subscription${ids.length > 1 ? "s" : ""}?`,
            confirmLabel: "Apply",
            variant: "default",
        })
        if (!confirmed) return
        bulkBandwidthMutation.mutate({ ids, bandwidthLimit: limitMbps }, {
            onSuccess: () => clearSelection(),
        })
    }

    const handleRefresh = () => {
        queryClient.invalidateQueries({ queryKey: ['subscriptions'] })
    }

    const isAnyMutationPending = pauseMutation.isPending || resumeMutation.isPending || revokeMutation.isPending || resetDataMutation.isPending || deleteMutation.isPending || bulkActionMutation.isPending || bulkBandwidthMutation.isPending

    const canToggle = (sub: Subscription) => sub.status === "active" || sub.status === "paused"

    return (
        <TooltipProvider delayDuration={300}>
            <div className="space-y-4 md:space-y-6 animate-in fade-in duration-500">
                {/* Header (desktop only) — consolidated 2-row layout */}
                <div className="hidden md:flex flex-col gap-3">
                    {/* Row 1: Title + Pills | Auto-refresh + New Subscription */}
                    <div className="flex items-center gap-4">
                        <h1 className="text-2xl font-bold tracking-tight shrink-0">Subscriptions</h1>
                        <FilterPills
                            pills={[
                                { label: "total", value: counts?.all ?? 0, filter: "all", activeColor: "bg-primary/10 text-primary border-primary/30" },
                                { label: "active", value: counts?.active ?? 0, filter: "active", activeColor: "bg-emerald-500/10 text-emerald-500 border-emerald-500/30" },
                                { label: "paused", value: counts?.paused ?? 0, filter: "paused", activeColor: "bg-amber-500/10 text-amber-500 border-amber-500/30" },
                                { label: "expired", value: counts?.expired ?? 0, filter: "expired", activeColor: "bg-red-500/10 text-red-500 border-red-500/30" },
                                { label: "cancelled", value: counts?.cancelled ?? 0, filter: "cancelled", activeColor: "bg-muted-foreground/10 text-muted-foreground border-muted-foreground/30" },
                            ]}
                            activeFilter={status}
                            defaultFilter="all"
                            onFilterChange={(v) => { setStatus(v); setPage(1) }}
                        />
                        <div className="flex-1" />
                        <AutoRefreshControl
                            onRefresh={handleRefresh}
                            isRefreshing={isRefetching}
                            dataUpdatedAt={dataUpdatedAt}
                        />
                        <Button onClick={() => openCreateManualDialog()} size="sm" className="h-9">
                            <Plus className="w-4 h-4 mr-1.5" />
                            New Subscription
                        </Button>
                    </div>

                    {/* Row 2: Search + Filters */}
                    <div className="flex items-center gap-3">
                        <div className="relative flex-1 max-w-md">
                            <HiOutlineSearch className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                            <Input
                                placeholder="Search by user or label..."
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                className="pl-9 h-9 text-sm"
                            />
                            {search && (
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="absolute right-1 top-1/2 -translate-y-1/2 h-6 w-6"
                                    onClick={() => setSearch("")}
                                >
                                    <HiOutlineX className="w-3.5 h-3.5" />
                                </Button>
                            )}
                        </div>
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={toggleFiltersExpanded}
                            className="gap-2 h-9"
                        >
                            <HiOutlineFunnel className="w-4 h-4" />
                            Filters
                            {activeFilterCount > 0 && (
                                <Badge variant="secondary" className="h-5 w-5 p-0 flex items-center justify-center text-[10px]">
                                    {activeFilterCount}
                                </Badge>
                            )}
                        </Button>
                    </div>
                </div>

                {/* Collapsible Advanced Filters */}
                <AnimatePresence>
                    {filtersExpanded && (
                        <motion.div
                            initial={{ height: 0, opacity: 0 }}
                            animate={{ height: "auto", opacity: 1 }}
                            exit={{ height: 0, opacity: 0 }}
                            transition={{ duration: 0.2 }}
                            className="overflow-hidden"
                        >
                            <div className="flex flex-col sm:flex-row sm:flex-wrap items-stretch sm:items-center gap-2 pt-3">
                                <Select value={sourceFilter} onValueChange={setSourceFilter}>
                                    <SelectTrigger className="w-full sm:w-[150px]">
                                        <SelectValue placeholder="Source" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Sources</SelectItem>
                                        <SelectItem value="manual">Manual</SelectItem>
                                    </SelectContent>
                                </Select>
                                <Select value={exhaustedFilter} onValueChange={setExhaustedFilter}>
                                    <SelectTrigger className="w-full sm:w-[140px]">
                                        <SelectValue placeholder="Usage" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Usage</SelectItem>
                                        <SelectItem value="false">Available</SelectItem>
                                        <SelectItem value="true">Exhausted</SelectItem>
                                    </SelectContent>
                                </Select>
                                {activeFilterCount > 0 && (
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={clearAdvancedFilters}
                                        className="text-muted-foreground hover:text-foreground"
                                    >
                                        <HiOutlineX className="h-4 w-4 mr-1" />
                                        Clear Filters
                                    </Button>
                                )}
                            </div>
                        </motion.div>
                    )}
                </AnimatePresence>

                {/* Subscriptions Table (Desktop) */}
                <Card className="hidden md:block">
                    <CardContent className="pt-3">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead className="w-[40px]">
                                        <Checkbox
                                            checked={allOnPageSelected}
                                            ref={(el) => {
                                                if (el) {
                                                    (el as unknown as HTMLInputElement).indeterminate = someOnPageSelected && !allOnPageSelected
                                                }
                                            }}
                                            onCheckedChange={handleSelectAllToggle}
                                            aria-label="Select all"
                                        />
                                    </TableHead>
                                    <TableHead className="w-[32px] px-1">
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <span>
                                                    <SortableHeader field="last_active_at" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                                        <span className="inline-block w-2.5 h-2.5 rounded-full bg-muted-foreground/40" />
                                                    </SortableHeader>
                                                </span>
                                            </TooltipTrigger>
                                            <TooltipContent>Online status (sort by last active)</TooltipContent>
                                        </Tooltip>
                                    </TableHead>
                                    <TableHead className="w-[52px]">On</TableHead>
                                    <TableHead className="w-[52px]">
                                        <SortableHeader field="id" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                            ID
                                        </SortableHeader>
                                    </TableHead>
                                    <TableHead>User</TableHead>
                                    <TableHead>Label</TableHead>
                                    <TableHead className="w-[80px]">Status</TableHead>
                                    <TableHead className="w-[60px] text-center">IPs</TableHead>
                                    <TableHead>
                                        <SortableHeader field="data_used" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                            Data Usage
                                        </SortableHeader>
                                    </TableHead>
                                    <TableHead className="text-center">
                                        <SortableHeader field="lifetime_data_used" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                            Total Traffic
                                        </SortableHeader>
                                    </TableHead>
                                    <TableHead className="text-center">
                                        <SortableHeader field="end_date" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                            Expires
                                        </SortableHeader>
                                    </TableHead>
                                    <TableHead className="w-[100px] text-right">Actions</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {isLoading ? (
                                    <TableSkeleton />
                                ) : subscriptions.length > 0 ? (
                                    subscriptions.map((sub) => (
                                        <TableRow
                                            key={sub.id}
                                            className={cn(
                                                "group cursor-pointer hover:bg-muted/50",
                                                selectedSubscriptions.has(sub.id) && "bg-muted/30"
                                            )}
                                            onClick={(e) => {
                                                const target = e.target as HTMLElement
                                                if (target.closest('a') || target.closest('button') || target.closest('[role="menuitem"]') || target.closest('[role="switch"]') || target.closest('[role="checkbox"]')) {
                                                    return
                                                }
                                                openDetailsSheet(sub)
                                            }}
                                        >
                                            {/* Checkbox */}
                                            <TableCell>
                                                <Checkbox
                                                    checked={selectedSubscriptions.has(sub.id)}
                                                    onCheckedChange={() => toggleSelectSubscription(sub.id)}
                                                    aria-label={`Select subscription ${sub.id}`}
                                                />
                                            </TableCell>

                                            {/* Online indicator */}
                                            <TableCell className="px-1">
                                                <Tooltip>
                                                    <TooltipTrigger asChild>
                                                        <span
                                                            className={cn(
                                                                "inline-block w-2.5 h-2.5 rounded-full",
                                                                sub.config_email && onlineEmails.has(sub.config_email)
                                                                    ? "bg-emerald-500 animate-pulse"
                                                                    : "bg-muted-foreground/30"
                                                            )}
                                                        />
                                                    </TooltipTrigger>
                                                    <TooltipContent>
                                                        {sub.config_email && onlineEmails.has(sub.config_email) ? "Online" : "Offline"}
                                                    </TooltipContent>
                                                </Tooltip>
                                            </TableCell>

                                            {/* Toggle switch */}
                                            <TableCell>
                                                <Tooltip>
                                                    <TooltipTrigger asChild>
                                                        <div>
                                                            <Switch
                                                                checked={sub.status === "active"}
                                                                onCheckedChange={(checked) => handleToggle(sub, checked)}
                                                                disabled={!canToggle(sub) || isAnyMutationPending}
                                                                className="data-[state=checked]:bg-emerald-500"
                                                            />
                                                        </div>
                                                    </TooltipTrigger>
                                                    <TooltipContent side="right">
                                                        {canToggle(sub)
                                                            ? sub.status === "active" ? "Pause subscription" : "Resume subscription"
                                                            : `Cannot toggle (${sub.status})`
                                                        }
                                                    </TooltipContent>
                                                </Tooltip>
                                            </TableCell>

                                            <TableCell className="font-mono text-muted-foreground">#{sub.id}</TableCell>
                                            <TableCell>
                                                {sub.user_id === null || sub.user_id === 0 ? (
                                                    <Badge variant="outline">Manual</Badge>
                                                ) : (
                                                    <Link
                                                        to={`/users/${sub.user_id}`}
                                                        className="inline-flex items-center gap-1.5 hover:text-primary hover:underline transition-colors font-medium"
                                                    >
                                                        <HiOutlineUser className="w-3.5 h-3.5 text-muted-foreground" />
                                                        {sub.user?.username || `User #${sub.user_id}`}
                                                    </Link>
                                                )}
                                            </TableCell>
                                            <TableCell>
                                                <span className="text-sm">{sub.label || "—"}</span>
                                            </TableCell>
                                            <TableCell>
                                                <SubscriptionStatusBadge status={sub.status} />
                                            </TableCell>
                                            <TableCell className="text-center">
                                                {(() => {
                                                    const ips = sub.config_email ? (onlineUsersWithIPs[sub.config_email] || []) : []
                                                    if (ips.length === 0) {
                                                        return <span className="text-muted-foreground/50">&mdash;</span>
                                                    }
                                                    return (
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <span className="inline-flex items-center gap-1 text-sm font-medium cursor-default">
                                                                    <HiOutlineGlobeAlt className="w-3.5 h-3.5 text-muted-foreground" />
                                                                    {ips.length}
                                                                </span>
                                                            </TooltipTrigger>
                                                            <TooltipContent side="bottom" className="max-w-[240px]">
                                                                <div className="space-y-0.5">
                                                                    <p className="text-xs font-medium mb-1">Connected IPs</p>
                                                                    {ips.map((ip) => (
                                                                        <div key={ip} className="text-xs font-mono">{ip}</div>
                                                                    ))}
                                                                </div>
                                                            </TooltipContent>
                                                        </Tooltip>
                                                    )
                                                })()}
                                            </TableCell>
                                            <TableCell>
                                                <DataUsageBar
                                                    used={sub.data_used}
                                                    limit={sub.custom_data_limit ?? sub.data_limit}
                                                    maxUsage={maxUnlimitedUsage}
                                                    isTerminated={sub.status === "cancelled" || sub.status === "expired"}
                                                />
                                            </TableCell>
                                            <TableCell className="text-center">
                                                <Tooltip>
                                                    <TooltipTrigger asChild>
                                                        <span className="text-sm font-mono text-muted-foreground cursor-default">
                                                            {formatCompact((sub.lifetime_data_upload || 0) + (sub.lifetime_data_download || 0))}
                                                        </span>
                                                    </TooltipTrigger>
                                                    <TooltipContent side="bottom">
                                                        <div className="flex flex-col gap-1 text-xs font-mono">
                                                            <span className="inline-flex items-center gap-1">
                                                                <HiArrowUp className="w-3 h-3 text-blue-500" />
                                                                {formatCompact(sub.lifetime_data_upload || 0)}
                                                            </span>
                                                            <span className="inline-flex items-center gap-1">
                                                                <HiArrowDown className="w-3 h-3 text-emerald-500" />
                                                                {formatCompact(sub.lifetime_data_download || 0)}
                                                            </span>
                                                        </div>
                                                    </TooltipContent>
                                                </Tooltip>
                                            </TableCell>
                                            <TableCell className="text-muted-foreground text-sm text-center">
                                                {(() => {
                                                    const expiryDate = sub.is_end_date_custom ? sub.custom_end_date : sub.end_date
                                                    const expiryInfo = getExpiryInfo(expiryDate)
                                                    return (
                                                        <div className="flex justify-center" title={expiryDate ? new Date(expiryDate).toLocaleString() : "Unlimited duration"}>
                                                            <Badge variant={expiryInfo.variant}>
                                                                {expiryInfo.text}
                                                            </Badge>
                                                        </div>
                                                    )
                                                })()}
                                            </TableCell>

                                            {/* Action buttons */}
                                            <TableCell>
                                                <div className="flex items-center justify-end gap-1">
                                                    {/* Copy link button */}
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <Button
                                                                variant="ghost"
                                                                size="icon"
                                                                className="h-8 w-8"
                                                                onClick={() => handleCopyLink(sub)}
                                                            >
                                                                <HiOutlineClipboardCopy className="w-4 h-4" />
                                                            </Button>
                                                        </TooltipTrigger>
                                                        <TooltipContent>Copy subscription link</TooltipContent>
                                                    </Tooltip>

                                                    {/* Data limit button */}
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <Button
                                                                variant="ghost"
                                                                size="icon"
                                                                className="h-8 w-8"
                                                                onClick={() => setDataLimitSub(sub)}
                                                            >
                                                                <HiOutlineDatabase className="w-4 h-4" />
                                                            </Button>
                                                        </TooltipTrigger>
                                                        <TooltipContent>Change data limit</TooltipContent>
                                                    </Tooltip>

                                                    {/* Three dots menu */}
                                                    <DropdownMenu>
                                                        <DropdownMenuTrigger asChild>
                                                            <Button
                                                                variant="ghost"
                                                                size="icon"
                                                                className="h-8 w-8"
                                                                disabled={isAnyMutationPending}
                                                            >
                                                                <HiOutlineDotsVertical className="w-4 h-4" />
                                                            </Button>
                                                        </DropdownMenuTrigger>
                                                        <DropdownMenuContent align="end" side="left">
                                                            {sub.user_id !== null && sub.user_id !== 0 && (
                                                                <DropdownMenuItem asChild>
                                                                    <Link to={`/users/${sub.user_id}`}>
                                                                        <HiOutlineUser className="w-4 h-4 mr-2" />
                                                                        View User
                                                                    </Link>
                                                                </DropdownMenuItem>
                                                            )}
                                                            <DropdownMenuItem onClick={() => openDetailsSheet(sub)}>
                                                                <HiOutlineGlobeAlt className="w-4 h-4 mr-2" />
                                                                Manage Subscription
                                                            </DropdownMenuItem>
                                                            <DropdownMenuItem onClick={() => handleCopyLink(sub)}>
                                                                <HiOutlineClipboardCopy className="w-4 h-4 mr-2" />
                                                                Copy Link
                                                            </DropdownMenuItem>
                                                            <DropdownMenuItem onClick={() => setDataLimitSub(sub)}>
                                                                <HiOutlineDatabase className="w-4 h-4 mr-2" />
                                                                Data Limit
                                                            </DropdownMenuItem>
                                                            <DropdownMenuItem onClick={() => handleResetData(sub)}>
                                                                <HiOutlineRefresh className="w-4 h-4 mr-2" />
                                                                Reset Usage
                                                            </DropdownMenuItem>
                                                            <DropdownMenuSeparator />
                                                            {sub.status === "active" && (
                                                                <DropdownMenuItem onClick={() => handlePauseResume(sub)}>
                                                                    <HiOutlinePause className="w-4 h-4 mr-2" />
                                                                    Pause
                                                                </DropdownMenuItem>
                                                            )}
                                                            {sub.status === "paused" && (
                                                                <DropdownMenuItem onClick={() => handlePauseResume(sub)}>
                                                                    <HiOutlinePlay className="w-4 h-4 mr-2" />
                                                                    Resume
                                                                </DropdownMenuItem>
                                                            )}
                                                            <DropdownMenuSeparator />
                                                            <DropdownMenuItem
                                                                onClick={() => handleRevoke(sub)}
                                                                className="text-red-500"
                                                            >
                                                                <HiOutlineX className="w-4 h-4 mr-2" />
                                                                Revoke
                                                            </DropdownMenuItem>
                                                            <DropdownMenuItem
                                                                onClick={() => handleDelete(sub)}
                                                                className="text-red-600"
                                                            >
                                                                <Trash2 className="w-4 h-4 mr-2" />
                                                                Delete Permanently
                                                            </DropdownMenuItem>
                                                        </DropdownMenuContent>
                                                    </DropdownMenu>
                                                </div>
                                            </TableCell>
                                        </TableRow>
                                    ))
                                ) : (
                                    <TableRow>
                                        <TableCell colSpan={13}>
                                            <EmptyState
                                                icon={HiOutlineGlobeAlt}
                                                title="No subscriptions found"
                                                description={search || activeFilterCount > 0 ? "Try adjusting your search or filters" : undefined}
                                            />
                                        </TableCell>
                                    </TableRow>
                                )}
                            </TableBody>
                        </Table>
                    </CardContent>
                </Card>

                {/* Mobile Compact Header */}
                <div className="md:hidden flex items-center justify-between px-4 py-2.5">
                    <div className="flex items-baseline gap-2">
                        <h1 className="text-xl font-bold tracking-tight">Subscriptions</h1>
                        <span className="text-sm text-muted-foreground">{counts?.all ?? 0}</span>
                    </div>
                    <div className="flex items-center gap-1">
                        <Button
                            variant="ghost"
                            size="icon"
                            className="h-9 w-9"
                            onClick={() => setMobileSearchExpanded(true)}
                        >
                            <HiOutlineSearch className="w-4.5 h-4.5" />
                        </Button>
                        <Button
                            variant="ghost"
                            size="icon"
                            className="h-9 w-9 relative"
                            onClick={() => setMobileFilterSheetOpen(true)}
                        >
                            <HiOutlineAdjustments className="w-4.5 h-4.5" />
                            {(status !== "all" || activeFilterCount > 0) && (
                                <span className="absolute top-1 right-1 w-2 h-2 rounded-full bg-primary" />
                            )}
                        </Button>
                    </div>
                </div>

                {/* Mobile List View */}
                <div className="md:hidden space-y-0 -mt-4 md:mt-0">
                    {/* Mobile select-mode header / search */}
                    {isMultiSelectMode ? (
                        <div className="flex items-center justify-between px-4 py-2 bg-primary/5 border-b">
                            <div className="flex items-center gap-3">
                                <Checkbox
                                    checked={allOnPageSelected}
                                    ref={(el) => {
                                        if (el) {
                                            (el as unknown as HTMLInputElement).indeterminate = someOnPageSelected && !allOnPageSelected
                                        }
                                    }}
                                    onCheckedChange={handleSelectAllToggle}
                                />
                                <span className="text-sm font-medium">
                                    {selectedSubscriptions.size > 0
                                        ? `${selectedSubscriptions.size} selected`
                                        : "Select all"}
                                </span>
                            </div>
                            <Button variant="ghost" size="sm" onClick={exitMultiSelectMode}>
                                <HiOutlineX className="w-4 h-4 mr-1" />
                                Cancel
                            </Button>
                        </div>
                    ) : (
                        <AnimatePresence>
                            {mobileSearchExpanded && (
                                <motion.div
                                    key="search-input"
                                    className="flex items-center gap-2 px-4 py-2 border-b"
                                    initial={{ height: 0, opacity: 0 }}
                                    animate={{ height: "auto", opacity: 1 }}
                                    exit={{ height: 0, opacity: 0 }}
                                    transition={{ duration: 0.2 }}
                                >
                                    <div className="relative flex-1">
                                        <HiOutlineSearch className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                                        <Input
                                            ref={mobileSearchRef}
                                            placeholder="Search..."
                                            value={search}
                                            onChange={(e) => setSearch(e.target.value)}
                                            className="pl-9 h-9"
                                            autoFocus
                                        />
                                    </div>
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="h-9 w-9 shrink-0"
                                        onClick={() => {
                                            setSearch("")
                                            setMobileSearchExpanded(false)
                                        }}
                                    >
                                        <HiOutlineX className="w-4 h-4" />
                                    </Button>
                                </motion.div>
                            )}
                        </AnimatePresence>
                    )}

                    {/* Subscription rows */}
                    {isLoading ? (
                        <div className="divide-y">
                            {[1, 2, 3, 4, 5, 6].map(i => (
                                <div key={i} className="px-4 py-3 space-y-2">
                                    <div className="flex items-center gap-3">
                                        <Skeleton className="h-5 w-10 rounded-full" />
                                        <Skeleton className="h-4 w-32" />
                                        <Skeleton className="h-5 w-14 rounded-full ml-auto" />
                                    </div>
                                    <div className="flex items-center gap-2 ml-[52px]">
                                        <Skeleton className="h-1.5 flex-1 rounded-full" />
                                        <Skeleton className="h-3 w-16" />
                                    </div>
                                </div>
                            ))}
                        </div>
                    ) : subscriptions.length > 0 ? (
                        <div className="divide-y">
                            {subscriptions.map((sub) => (
                                <SwipeableSubscriptionRow
                                    key={sub.id}
                                    subscription={sub}
                                    maxUsage={maxUnlimitedUsage}
                                    connectedIPs={sub.config_email ? (onlineUsersWithIPs[sub.config_email] || []) : []}
                                    isSelected={selectedSubscriptions.has(sub.id)}
                                    isMultiSelectMode={isMultiSelectMode}
                                    isAnyMutationPending={isAnyMutationPending}
                                    shouldClose={openSwipeRowId !== null && openSwipeRowId !== sub.id}
                                    onOpen={(id) => setOpenSwipeRowId(id)}
                                    onTap={openDetailsSheet}
                                    onToggle={handleToggle}
                                    onToggleSelect={toggleSelectSubscription}
                                    onLongPress={(id) => {
                                        enterMultiSelectMode()
                                        toggleSelectSubscription(id)
                                    }}
                                    onCopyLink={handleCopyLink}
                                    onPauseResume={handlePauseResume}
                                    onDelete={handleDelete}
                                />
                            ))}
                        </div>
                    ) : (
                        <EmptyState
                            icon={HiOutlineGlobeAlt}
                            title="No subscriptions found"
                            description={search || activeFilterCount > 0 ? "Try adjusting your search or filters" : undefined}
                        />
                    )}
                </div>

                {/* Unified Pagination */}
                {subscriptions.length > 0 && (
                    <PaginationNav
                        page={page}
                        hasNextPage={subscriptions.length >= perPage}
                        onPageChange={setPage}
                        showingCount={subscriptions.length}
                    />
                )}

                {/* Floating Selection Action Bar */}
                {selectedSubscriptions.size > 0 && (
                    <div className="fixed bottom-[100px] md:bottom-6 left-1/2 -translate-x-1/2 z-50 animate-in slide-in-from-bottom-4 fade-in duration-300">
                        {/* Desktop variant */}
                        <div className="hidden md:flex items-center gap-2 bg-background/95 backdrop-blur-sm border rounded-xl shadow-2xl px-4 py-3">
                            <Badge variant="secondary" className="font-mono text-sm px-3">
                                {selectedSubscriptions.size} selected
                            </Badge>
                            <div className="h-6 w-px bg-border" />
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleBulkAction("pause", "Pause")}
                                disabled={!selectionInfo.canPause || bulkActionMutation.isPending}
                            >
                                <Pause className="w-3.5 h-3.5 mr-1.5" />
                                Pause
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleBulkAction("resume", "Resume")}
                                disabled={!selectionInfo.canResume || bulkActionMutation.isPending}
                            >
                                <Play className="w-3.5 h-3.5 mr-1.5" />
                                Resume
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                className="text-amber-600 hover:text-amber-700 border-amber-500/30 hover:border-amber-500/50"
                                onClick={() => handleBulkAction("revoke", "Revoke")}
                                disabled={!selectionInfo.canRevoke || bulkActionMutation.isPending}
                            >
                                <Ban className="w-3.5 h-3.5 mr-1.5" />
                                Revoke
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                className="text-red-600 hover:text-red-700 border-red-500/30 hover:border-red-500/50"
                                onClick={() => handleBulkAction("delete", "Delete")}
                                disabled={!selectionInfo.canDelete || bulkActionMutation.isPending}
                            >
                                <Trash2 className="w-3.5 h-3.5 mr-1.5" />
                                Delete
                            </Button>
                            <div className="h-6 w-px bg-border" />
                            {/*  Speed limit feature - disabled
                            <DropdownMenu>
                                <DropdownMenuTrigger asChild>
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        disabled={bulkBandwidthMutation.isPending}
                                    >
                                        <Wifi className="w-3.5 h-3.5 mr-1.5" />
                                        Speed
                                    </Button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="center" side="top" className="min-w-[140px]">
                                    {BANDWIDTH_OPTIONS.map((opt) => (
                                        <DropdownMenuItem
                                            key={opt.value}
                                            onClick={() => handleBulkBandwidth(opt.value === 0 ? 0 : opt.value)}
                                        >
                                            {opt.label}
                                        </DropdownMenuItem>
                                    ))}
                                    <DropdownMenuSeparator />
                                    <DropdownMenuItem
                                        onClick={() => handleBulkBandwidth(null)}
                                        className="text-muted-foreground"
                                    >
                                        Reset to default
                                    </DropdownMenuItem>
                                </DropdownMenuContent>
                            </DropdownMenu>
                            */}
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => setBulkInboundsOpen(true)}
                                disabled={bulkActionMutation.isPending}
                            >
                                <Network className="w-3.5 h-3.5 mr-1.5" />
                                Inbounds
                            </Button>
                            <div className="h-6 w-px bg-border" />
                            <Button
                                variant="ghost"
                                size="sm"
                                onClick={clearSelection}
                            >
                                <XIcon className="w-3.5 h-3.5 mr-1.5" />
                                Clear
                            </Button>
                        </div>

                        {/* Mobile variant — icon-only compact buttons */}
                        <div className="flex md:hidden items-center gap-1.5 bg-background/95 backdrop-blur-sm border rounded-xl shadow-2xl px-3 py-2.5">
                            <Badge variant="secondary" className="font-mono text-xs px-2">
                                {selectedSubscriptions.size}
                            </Badge>
                            <div className="h-5 w-px bg-border" />
                            <Button
                                variant="outline"
                                size="icon"
                                className="h-8 w-8"
                                onClick={() => handleBulkAction("pause", "Pause")}
                                disabled={!selectionInfo.canPause || bulkActionMutation.isPending}
                            >
                                <Pause className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                                variant="outline"
                                size="icon"
                                className="h-8 w-8"
                                onClick={() => handleBulkAction("resume", "Resume")}
                                disabled={!selectionInfo.canResume || bulkActionMutation.isPending}
                            >
                                <Play className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                                variant="outline"
                                size="icon"
                                className="h-8 w-8 text-amber-600 border-amber-500/30"
                                onClick={() => handleBulkAction("revoke", "Revoke")}
                                disabled={!selectionInfo.canRevoke || bulkActionMutation.isPending}
                            >
                                <Ban className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                                variant="outline"
                                size="icon"
                                className="h-8 w-8 text-red-600 border-red-500/30"
                                onClick={() => handleBulkAction("delete", "Delete")}
                                disabled={!selectionInfo.canDelete || bulkActionMutation.isPending}
                            >
                                <Trash2 className="w-3.5 h-3.5" />
                            </Button>
                            <div className="h-5 w-px bg-border" />
                            {/*  Speed limit feature (bulk edit) - disabled
                            <DropdownMenu>
                                <DropdownMenuTrigger asChild>
                                    <Button
                                        variant="outline"
                                        size="icon"
                                        className="h-8 w-8"
                                        disabled={bulkBandwidthMutation.isPending}
                                    >
                                        <Wifi className="w-3.5 h-3.5" />
                                    </Button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="center" side="top" className="min-w-[140px]">
                                    {BANDWIDTH_OPTIONS.map((opt) => (
                                        <DropdownMenuItem
                                            key={opt.value}
                                            onClick={() => handleBulkBandwidth(opt.value === 0 ? 0 : opt.value)}
                                        >
                                            {opt.label}
                                        </DropdownMenuItem>
                                    ))}
                                    <DropdownMenuSeparator />
                                    <DropdownMenuItem
                                        onClick={() => handleBulkBandwidth(null)}
                                        className="text-muted-foreground"
                                    >
                                        Reset to default
                                    </DropdownMenuItem>
                                </DropdownMenuContent>
                            </DropdownMenu>
                            */}
                            <Button
                                variant="outline"
                                size="icon"
                                className="h-8 w-8"
                                onClick={() => setBulkInboundsOpen(true)}
                                disabled={bulkActionMutation.isPending}
                            >
                                <Network className="w-3.5 h-3.5" />
                            </Button>
                            <div className="h-5 w-px bg-border" />
                            <Button
                                variant="ghost"
                                size="icon"
                                className="h-8 w-8"
                                onClick={clearSelection}
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </Button>
                        </div>
                    </div>
                )}

                {/* Mobile FAB — New Subscription */}
                <AnimatePresence>
                    {selectedSubscriptions.size === 0 && (
                        <motion.button
                            className="fixed bottom-[100px] right-6 z-40 md:hidden w-14 h-14 rounded-full bg-primary text-primary-foreground shadow-lg flex items-center justify-center active:scale-95"
                            onClick={() => openCreateManualDialog()}
                            initial={{ scale: 0, opacity: 0 }}
                            animate={{ scale: 1, opacity: 1 }}
                            exit={{ scale: 0, opacity: 0 }}
                            transition={{ type: "spring", stiffness: 300, damping: 25 }}
                        >
                            <Plus className="w-6 h-6" />
                        </motion.button>
                    )}
                </AnimatePresence>

                <SubscriptionDetailsSheet />
                <CreateManualSubscriptionDialog />

                {/* Data Limit Dialog */}
                <DataLimitDialog
                    open={dataLimitSub !== null}
                    onOpenChange={(open) => { if (!open) setDataLimitSub(null) }}
                    subscription={dataLimitSub}
                />

                {/* Bulk Manage Inbounds Dialog */}
                <BulkManageInboundsDialog
                    open={bulkInboundsOpen}
                    onOpenChange={setBulkInboundsOpen}
                    selectedSubscriptionIds={Array.from(selectedSubscriptions)}
                    onSuccess={clearSelection}
                />


                {/* Mobile Filter Sheet */}
                <Sheet open={mobileFilterSheetOpen} onOpenChange={setMobileFilterSheetOpen}>
                    <SheetContent side="bottom" className="md:hidden rounded-t-2xl max-h-[85vh] overflow-y-auto">
                        <SheetHeader>
                            <SheetTitle>Filters</SheetTitle>
                        </SheetHeader>
                        <div className="space-y-4 py-4">
                            <div>
                                <label className="text-xs font-medium text-muted-foreground mb-2 block">Status</label>
                                <FilterPills
                                    pills={[
                                        { label: "total", value: counts?.all ?? 0, filter: "all" as const, activeColor: "bg-primary/10 text-primary border-primary/30" },
                                        { label: "active", value: counts?.active ?? 0, filter: "active" as const, activeColor: "bg-emerald-500/10 text-emerald-500 border-emerald-500/30" },
                                        { label: "paused", value: counts?.paused ?? 0, filter: "paused" as const, activeColor: "bg-amber-500/10 text-amber-500 border-amber-500/30" },
                                        { label: "expired", value: counts?.expired ?? 0, filter: "expired" as const, activeColor: "bg-red-500/10 text-red-500 border-red-500/30" },
                                        { label: "cancelled", value: counts?.cancelled ?? 0, filter: "cancelled" as const, activeColor: "bg-muted-foreground/10 text-muted-foreground border-muted-foreground/30" },
                                    ]}
                                    activeFilter={status}
                                    defaultFilter="all"
                                    onFilterChange={(v) => { setStatus(v); setPage(1) }}
                                />
                            </div>
                            <div>
                                <label className="text-xs font-medium text-muted-foreground mb-2 block">Auto Refresh</label>
                                <AutoRefreshControl
                                    onRefresh={handleRefresh}
                                    isRefreshing={isRefetching}
                                    dataUpdatedAt={dataUpdatedAt}
                                />
                            </div>
                            <div className="space-y-2">
                                <label className="text-xs font-medium text-muted-foreground block">Advanced</label>
                                <Select value={sourceFilter} onValueChange={setSourceFilter}>
                                    <SelectTrigger className="w-full">
                                        <SelectValue placeholder="Source" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Sources</SelectItem>
                                        <SelectItem value="manual">Manual</SelectItem>
                                    </SelectContent>
                                </Select>
                                <Select value={exhaustedFilter} onValueChange={setExhaustedFilter}>
                                    <SelectTrigger className="w-full">
                                        <SelectValue placeholder="Usage" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Usage</SelectItem>
                                        <SelectItem value="false">Available</SelectItem>
                                        <SelectItem value="true">Exhausted</SelectItem>
                                    </SelectContent>
                                </Select>
                                {activeFilterCount > 0 && (
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={clearAdvancedFilters}
                                        className="text-muted-foreground hover:text-foreground w-full"
                                    >
                                        <HiOutlineX className="h-4 w-4 mr-1" />
                                        Clear Filters
                                    </Button>
                                )}
                            </div>
                        </div>
                    </SheetContent>
                </Sheet>
            </div>
        </TooltipProvider>
    )
}
