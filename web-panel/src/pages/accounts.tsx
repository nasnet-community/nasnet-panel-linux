import { useEffect, useState, useMemo, useRef } from "react"
import { getUsageBarColor } from "@/lib/constants/usage-thresholds"
import { useQuery } from "@tanstack/react-query"
import { listNodes, type Account } from "@/lib/admin-api"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { FilterPills, type FilterPill } from "@/components/ui/filter-pills"
import { Checkbox } from "@/components/ui/checkbox"
import {
    HiOutlineSearch,
    HiOutlineRefresh,
    HiOutlineServer,
    HiOutlineGlobeAlt,
    HiOutlineUser,
    HiOutlineClipboardCopy,
    HiOutlineUserGroup,
    HiOutlineX,
    HiOutlineAdjustments,
} from "react-icons/hi"
import { HiOutlineFunnel } from "react-icons/hi2"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Plus, Trash2, RefreshCw, Power, PowerOff, X as XIcon } from "lucide-react"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { formatBytes, getExpiryInfo, cn, copyToClipboard } from "@/lib/utils"
import { PaginationNav } from "@/components/ui/pagination-nav"
import { EmptyState } from "@/components/ui/empty-state"
import { Link } from "react-router"
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
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import { toast } from "sonner"
import { AutoRefreshControl } from "@/components/ui/auto-refresh-control"
import { motion, AnimatePresence } from "framer-motion"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useQueryClient } from "@tanstack/react-query"

// Store
import { useAccountsStore } from "@/store/accounts-store"

// Query hooks
import {
    useAccountList,
    useAccountCounts,
    useSyncAccountMutation,
    useCopyAccountLink,
    useDeleteAccountMutation,
    useDisableAccountMutation,
    useEnableAccountMutation,
    useBulkAccountAction,
    type BulkAccountAction,
} from "@/lib/queries"

// Components
import { AccountDetailsSheet } from "@/components/accounts/account-details-sheet"
import { DeleteAccountDialog } from "@/components/accounts/delete-account-dialog"
import { CreateAccountDialog } from "@/components/account/create-account-dialog"
import { SwipeableAccountRow } from "@/components/accounts/swipeable-account-row"

import { HiDotsHorizontal } from "react-icons/hi"

// Table skeleton
function TableSkeleton() {
    return (
        <>
            {[1, 2, 3, 4, 5].map((i) => (
                <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                    <TableCell><Skeleton className="h-2.5 w-2.5 rounded-full" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-6 w-16 rounded-full" /></TableCell>
                    <TableCell><Skeleton className="h-6 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-full" /></TableCell>
                    <TableCell><Skeleton className="h-8 w-16" /></TableCell>
                </TableRow>
            ))}
        </>
    )
}

export default function AccountsPage() {
    const queryClient = useQueryClient()
    const confirmDialog = useConfirm()
    const perPage = 20

    // Zustand store
    const {
        status, page, search,
        exhaustedFilter, nodeFilter, inboundFilter, sourceFilter,
        filtersExpanded,
        selectedAccounts, isMultiSelectMode,
        mobileSearchExpanded, mobileFilterSheetOpen,
        setStatus, setPage, setSearch,
        setExhaustedFilter, setNodeFilter, setInboundFilter, setSourceFilter,
        toggleFiltersExpanded, clearAdvancedFilters,
        toggleSelectAccount, selectAll, clearSelection,
        enterMultiSelectMode, exitMultiSelectMode,
        setMobileSearchExpanded, setMobileFilterSheetOpen,
        openDetailsSheet, openDeleteDialog, openCreateDialog,
    } = useAccountsStore()

    // Debounced search
    const [debouncedSearch, setDebouncedSearch] = useState(search)
    useEffect(() => {
        const timer = setTimeout(() => setDebouncedSearch(search), 300)
        return () => clearTimeout(timer)
    }, [search])

    // Mobile swipe row management
    const [openSwipeRowId, setOpenSwipeRowId] = useState<number | null>(null)
    const mobileSearchRef = useRef<HTMLInputElement>(null)

    // Fetch nodes for filter dropdown
    const { data: nodesData } = useQuery({
        queryKey: ["nodes"],
        queryFn: async () => {
            const res = await listNodes()
            if (!res.success) throw new Error(res.error || "Failed to fetch nodes")
            return res.data || []
        },
    })
    const nodes = nodesData || []
    const allInbounds = nodes.flatMap(n => n.inbounds || [])
    const filteredInbounds = nodeFilter === "all"
        ? allInbounds
        : allInbounds.filter(inb => inb.node_id === parseInt(nodeFilter))

    // TanStack Query hooks
    const { data, isLoading, isRefetching, dataUpdatedAt } = useAccountList({
        status,
        page,
        perPage,
        search: debouncedSearch,
        exhausted: exhaustedFilter,
        nodeId: nodeFilter !== "all" ? parseInt(nodeFilter) : undefined,
        inboundId: inboundFilter !== "all" ? parseInt(inboundFilter) : undefined,
        source: sourceFilter,
    })

    const accounts = data?.accounts || []
    const total = data?.total || 0
    const totalPages = total > 0 ? Math.ceil(total / perPage) : 0

    // Auto-correct page when it exceeds total pages (e.g. after filter/delete changes)
    useEffect(() => {
        if (totalPages > 0 && page > totalPages) {
            setPage(totalPages)
        } else if (!isLoading && accounts.length === 0 && page > 1) {
            setPage(1)
        }
    }, [totalPages, page, setPage, isLoading, accounts.length])

    // Counts
    const { data: counts } = useAccountCounts()

    // Mutations
    const syncMutation = useSyncAccountMutation()
    const copyLinkMutation = useCopyAccountLink()
    const deleteMutation = useDeleteAccountMutation()
    const disableMutation = useDisableAccountMutation()
    const enableMutation = useEnableAccountMutation()
    const bulkActionMutation = useBulkAccountAction()

    const isAnyMutationPending = syncMutation.isPending || copyLinkMutation.isPending ||
        deleteMutation.isPending || disableMutation.isPending || enableMutation.isPending || bulkActionMutation.isPending

    // Active filter count (for badge on the Filters button)
    const activeFilterCount = [
        nodeFilter !== "all",
        inboundFilter !== "all",
        sourceFilter !== "all",
        exhaustedFilter !== "all",
    ].filter(Boolean).length

    // Selection helpers
    const selectionInfo = useMemo(() => {
        const selectedAccs = accounts.filter(a => selectedAccounts.has(a.id))
        return {
            canSync: selectedAccs.length > 0,
            canDisable: selectedAccs.some(a => a.status === "active"),
            canEnable: selectedAccs.some(a => a.status === "disabled"),
            canDelete: selectedAccs.length > 0,
        }
    }, [accounts, selectedAccounts])

    const allOnPageSelected = accounts.length > 0 && accounts.every(a => selectedAccounts.has(a.id))
    const someOnPageSelected = accounts.some(a => selectedAccounts.has(a.id))

    const handleSelectAllToggle = () => {
        if (allOnPageSelected) {
            clearSelection()
        } else {
            selectAll(accounts.map(a => a.id))
        }
    }

    const handleCopyLink = (acc: Account) => {
        copyLinkMutation.mutate(acc.id)
    }

    const handleSync = (acc: Account) => {
        syncMutation.mutate(acc.id)
    }

    const handleDelete = async (acc: Account) => {
        openDeleteDialog(acc)
    }

    // Bulk action handlers
    const handleBulkAction = async (action: BulkAccountAction, label: string) => {
        const ids = Array.from(selectedAccounts)
        const confirmed = await confirmDialog({
            title: `Bulk ${label}`,
            description: `Are you sure you want to ${label.toLowerCase()} ${ids.length} account${ids.length > 1 ? "s" : ""}?${action === "delete" ? " This action cannot be undone." : ""}`,
            confirmLabel: label,
            variant: action === "delete" ? "destructive" : "default",
        })
        if (!confirmed) return
        bulkActionMutation.mutate(
            { action, ids },
            { onSuccess: () => clearSelection() },
        )
    }

    const handleRefresh = () => {
        queryClient.invalidateQueries({ queryKey: ['accounts'] })
    }

    return (
        <TooltipProvider delayDuration={300}>
            <div className="space-y-4 md:space-y-6 animate-in fade-in duration-500">
                {/* 1. Header (desktop only) */}
                <div className="hidden md:flex flex-col gap-3">
                    {/* Row 1: Title + Pills | Actions */}
                    <div className="flex items-center gap-4">
                        <h1 className="text-2xl font-bold tracking-tight shrink-0">Accounts</h1>
                        <FilterPills
                            pills={[
                                { label: "total", value: counts?.all ?? 0, filter: "all", activeColor: "bg-primary/10 text-primary border-primary/30" },
                                { label: "active", value: counts?.active ?? 0, filter: "active", activeColor: "bg-emerald-500/10 text-emerald-500 border-emerald-500/30" },
                                { label: "disabled", value: counts?.disabled ?? 0, filter: "disabled", activeColor: "bg-red-500/10 text-red-500 border-red-500/30" },
                                { label: "expired", value: counts?.expired ?? 0, filter: "expired", activeColor: "bg-amber-500/10 text-amber-500 border-amber-500/30" },
                            ]}
                            activeFilter={status}
                            defaultFilter="all"
                            onFilterChange={setStatus}
                        />
                        <div className="flex-1" />
                        <AutoRefreshControl
                            onRefresh={handleRefresh}
                            isRefreshing={isRefetching || isLoading}
                            dataUpdatedAt={dataUpdatedAt}
                        />
                        <Button onClick={openCreateDialog} size="sm" className="h-9 hidden md:inline-flex">
                            <Plus className="w-4 h-4 mr-1.5" />
                            Add Account
                        </Button>
                    </div>

                    {/* Row 2: Search + Filters */}
                    <div className="flex items-center gap-3">
                        <div className="relative flex-1 max-w-md">
                            <HiOutlineSearch className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                            <Input
                                placeholder="Search emails or UUIDs..."
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                className="pl-9 h-9 text-sm"
                            />
                            {search && (
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7"
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
                            className="h-9 gap-2"
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
                                <Select value={nodeFilter} onValueChange={setNodeFilter}>
                                    <SelectTrigger className="w-full sm:w-[150px]">
                                        <SelectValue placeholder="Node" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Nodes</SelectItem>
                                        {nodes.map(node => (
                                            <SelectItem key={node.id} value={node.id.toString()}>{node.name}</SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                <Select value={inboundFilter} onValueChange={setInboundFilter}>
                                    <SelectTrigger className="w-full sm:w-[150px]">
                                        <SelectValue placeholder="Inbound" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Inbounds</SelectItem>
                                        {filteredInbounds.map(inb => (
                                            <SelectItem key={inb.id} value={inb.id.toString()}>
                                                {inb.tag || inb.remark || `Port ${inb.port}`}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                <Select value={sourceFilter} onValueChange={setSourceFilter}>
                                    <SelectTrigger className="w-full sm:w-[150px]">
                                        <SelectValue placeholder="Source" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Sources</SelectItem>
                                        <SelectItem value="subscription">Subscription</SelectItem>
                                        <SelectItem value="manual">Manual</SelectItem>
                                        <SelectItem value="import">Import</SelectItem>
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

                {/* 4. Desktop Table */}
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
                                    <TableHead className="w-[30px]"></TableHead>
                                    <TableHead>Account</TableHead>
                                    <TableHead>Node / Inbound</TableHead>
                                    <TableHead>User / Sub</TableHead>
                                    <TableHead>Status</TableHead>
                                    <TableHead className="w-[80px] text-center">Duration</TableHead>
                                    <TableHead className="w-[200px]">Data Usage</TableHead>
                                    <TableHead className="w-[100px] text-right">Actions</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {isLoading ? (
                                    <TableSkeleton />
                                ) : accounts.length > 0 ? (
                                    accounts.map((acc: Account) => {
                                        const isShared = !!acc.subscription
                                        const totalLimit = isShared
                                            ? (acc.subscription?.custom_data_limit ?? acc.subscription?.data_limit ?? 0)
                                            : acc.data_limit
                                        const totalUsed = isShared
                                            ? (acc.subscription?.data_used ?? 0)
                                            : acc.data_used
                                        const percent = totalLimit > 0 ? (totalUsed / totalLimit) * 100 : 0
                                        const progressColor = getUsageBarColor(percent)

                                        const isOnline = acc.last_activity_at &&
                                            (new Date().getTime() - new Date(acc.last_activity_at).getTime()) < 10 * 1000

                                        return (
                                            <TableRow
                                                key={acc.id}
                                                className={cn(
                                                    "group cursor-pointer hover:bg-muted/50",
                                                    selectedAccounts.has(acc.id) && "bg-muted/30"
                                                )}
                                                onClick={(e) => {
                                                    const target = e.target as HTMLElement
                                                    if (target.closest('a') || target.closest('button') || target.closest('[role="menuitem"]') || target.closest('[role="checkbox"]')) return
                                                    openDetailsSheet(acc)
                                                }}
                                            >
                                                {/* Checkbox */}
                                                <TableCell>
                                                    <Checkbox
                                                        checked={selectedAccounts.has(acc.id)}
                                                        onCheckedChange={() => toggleSelectAccount(acc.id)}
                                                        aria-label={`Select account ${acc.id}`}
                                                    />
                                                </TableCell>

                                                {/* Online dot */}
                                                <TableCell>
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <span className="relative flex h-2.5 w-2.5">
                                                                {isOnline && <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />}
                                                                <span className={cn(
                                                                    "relative inline-flex rounded-full h-2.5 w-2.5",
                                                                    isOnline ? "bg-green-500" : "bg-slate-400/50"
                                                                )} />
                                                            </span>
                                                        </TooltipTrigger>
                                                        <TooltipContent side="right">
                                                            {isOnline ? "Online" : acc.last_activity_at
                                                                ? `Last seen: ${new Date(acc.last_activity_at).toLocaleString()}`
                                                                : "No recent activity"
                                                            }
                                                        </TooltipContent>
                                                    </Tooltip>
                                                </TableCell>

                                                {/* Account info */}
                                                <TableCell className="font-medium">
                                                    <div className="flex flex-col">
                                                        <span>{acc.email}</span>
                                                        <button
                                                            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors group/uuid"
                                                            onClick={async (e) => {
                                                                e.stopPropagation()
                                                                await copyToClipboard(acc.uuid)
                                                                toast.success("UUID copied")
                                                            }}
                                                            title={acc.uuid}
                                                        >
                                                            <span className="font-mono">{acc.uuid.slice(0, 8)}...</span>
                                                            <HiOutlineClipboardCopy className="h-3 w-3 opacity-0 group-hover/uuid:opacity-100 transition-opacity" />
                                                        </button>
                                                    </div>
                                                </TableCell>

                                                {/* Node / Inbound */}
                                                <TableCell onClick={(e) => e.stopPropagation()}>
                                                    <div className="flex flex-col gap-1">
                                                        <div className="flex items-center gap-1 text-sm">
                                                            <HiOutlineServer className="w-3 h-3 text-muted-foreground" />
                                                            {acc.inbound?.node ? (
                                                                <Link to={`/nodes/${acc.inbound.node.id}`} className="hover:underline hover:text-primary transition-colors">
                                                                    {acc.inbound.node.name}
                                                                </Link>
                                                            ) : (
                                                                <span className="text-muted-foreground">—</span>
                                                            )}
                                                        </div>
                                                        <div className="flex items-center gap-1 text-xs text-muted-foreground">
                                                            <HiOutlineGlobeAlt className="w-3 h-3" />
                                                            <span title={acc.inbound?.remark}>{acc.inbound?.tag || "—"} ({acc.inbound?.port})</span>
                                                        </div>
                                                    </div>
                                                </TableCell>

                                                {/* User / Sub */}
                                                <TableCell onClick={(e) => e.stopPropagation()}>
                                                    {acc.subscription ? (
                                                        <div className="flex flex-col">
                                                            <div className="flex items-center gap-1">
                                                                <HiOutlineUser className="w-3 h-3 text-muted-foreground" />
                                                                {acc.subscription.user ? (
                                                                    <Link to={`/users/${acc.subscription.user.id}`} className="hover:underline hover:text-primary transition-colors text-sm font-medium">
                                                                        {acc.subscription.user.username}
                                                                    </Link>
                                                                ) : (
                                                                    <span className="text-sm font-medium">Unknown User</span>
                                                                )}
                                                            </div>
                                                            <span className="text-xs text-muted-foreground">Sub #{acc.subscription.id}</span>
                                                        </div>
                                                    ) : (
                                                        <span className="text-sm text-muted-foreground">-</span>
                                                    )}
                                                </TableCell>

                                                {/* Status */}
                                                <TableCell>
                                                    <Badge
                                                        variant={acc.status === "active" ? "success" : acc.status === "disabled" ? "danger" : "secondary"}
                                                        className="capitalize w-fit"
                                                    >
                                                        {acc.status}
                                                    </Badge>
                                                </TableCell>

                                                {/* Duration */}
                                                <TableCell className="text-center">
                                                    {(() => {
                                                        const expiryDate = acc.subscription?.custom_end_date ?? acc.subscription?.end_date ?? acc.expires_at
                                                        const expiryInfo = getExpiryInfo(expiryDate)
                                                        return (
                                                            <div title={expiryDate ? new Date(expiryDate).toLocaleString() : "Unlimited duration"}>
                                                                <Badge
                                                                    variant={expiryInfo.variant}
                                                                    className={`text-xs px-2 py-0.5 font-medium cursor-help ${expiryInfo.variant === "secondary" ? "bg-blue-900 text-blue-100" : ""}`}
                                                                >
                                                                    {expiryInfo.text}
                                                                </Badge>
                                                            </div>
                                                        )
                                                    })()}
                                                </TableCell>

                                                {/* Data Usage */}
                                                <TableCell>
                                                    <div className={`space-y-1 ${acc.status === "disabled" ? "opacity-50" : ""}`}>
                                                        <div className="flex justify-between text-xs">
                                                            <span>{formatBytes(acc.data_used)}</span>
                                                            <span className="text-muted-foreground">
                                                                {isShared ? (
                                                                    <span title={`Shared Pool (Sub #${acc.subscription?.id})`}>
                                                                        Pool: {formatBytes(totalUsed)} / {totalLimit > 0 ? formatBytes(totalLimit) : "Unlimited"}
                                                                    </span>
                                                                ) : (
                                                                    <span>{totalLimit > 0 ? formatBytes(totalLimit) : "Unlimited"}</span>
                                                                )}
                                                            </span>
                                                        </div>
                                                        {acc.status !== "disabled" && (
                                                            <Progress value={Math.min(percent, 100)} className="h-2" indicatorClassName={progressColor} />
                                                        )}
                                                    </div>
                                                </TableCell>

                                                {/* Actions */}
                                                <TableCell>
                                                    <div className="flex items-center justify-end gap-1">
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <Button
                                                                    variant="ghost"
                                                                    size="icon"
                                                                    className="h-8 w-8"
                                                                    onClick={(e) => { e.stopPropagation(); handleCopyLink(acc) }}
                                                                >
                                                                    <HiOutlineClipboardCopy className="w-4 h-4" />
                                                                </Button>
                                                            </TooltipTrigger>
                                                            <TooltipContent>Copy subscription link</TooltipContent>
                                                        </Tooltip>

                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <Button
                                                                    variant="ghost"
                                                                    size="icon"
                                                                    className="h-8 w-8"
                                                                    onClick={(e) => { e.stopPropagation(); handleSync(acc) }}
                                                                    disabled={syncMutation.isPending}
                                                                >
                                                                    <HiOutlineRefresh className="w-4 h-4" />
                                                                </Button>
                                                            </TooltipTrigger>
                                                            <TooltipContent>Sync stats</TooltipContent>
                                                        </Tooltip>

                                                        <DropdownMenu>
                                                            <DropdownMenuTrigger asChild>
                                                                <Button
                                                                    variant="ghost"
                                                                    size="icon"
                                                                    className="h-8 w-8"
                                                                    onClick={(e) => e.stopPropagation()}
                                                                    disabled={isAnyMutationPending}
                                                                >
                                                                    <HiDotsHorizontal className="w-4 h-4" />
                                                                </Button>
                                                            </DropdownMenuTrigger>
                                                            <DropdownMenuContent align="end" side="left">
                                                                <DropdownMenuItem onClick={() => openDetailsSheet(acc)}>
                                                                    View Details
                                                                </DropdownMenuItem>
                                                                {acc.status === "active" && (
                                                                    <DropdownMenuItem onClick={() => disableMutation.mutate(acc.id)}>
                                                                        <PowerOff className="w-4 h-4 mr-2" />
                                                                        Disable
                                                                    </DropdownMenuItem>
                                                                )}
                                                                {acc.status === "disabled" && (
                                                                    <DropdownMenuItem onClick={() => enableMutation.mutate(acc.id)}>
                                                                        <Power className="w-4 h-4 mr-2" />
                                                                        Enable
                                                                    </DropdownMenuItem>
                                                                )}
                                                                <DropdownMenuSeparator />
                                                                <DropdownMenuItem
                                                                    className="text-destructive focus:text-destructive"
                                                                    onClick={() => handleDelete(acc)}
                                                                >
                                                                    <Trash2 className="w-4 h-4 mr-2" />
                                                                    Delete Account
                                                                </DropdownMenuItem>
                                                            </DropdownMenuContent>
                                                        </DropdownMenu>
                                                    </div>
                                                </TableCell>
                                            </TableRow>
                                        )
                                    })
                                ) : (
                                    <TableRow>
                                        <TableCell colSpan={9}>
                                            <EmptyState
                                                icon={HiOutlineUserGroup}
                                                title="No accounts found"
                                                description={activeFilterCount > 0 || search ? "Try adjusting your filters" : undefined}
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
                        <h1 className="text-xl font-bold tracking-tight">Accounts</h1>
                        <span className="text-sm text-muted-foreground">{counts?.all ?? 0}</span>
                    </div>
                    <div className="flex items-center gap-1">
                        <Button variant="ghost" size="icon" className="h-9 w-9"
                            onClick={() => setMobileSearchExpanded(true)}>
                            <HiOutlineSearch className="w-4.5 h-4.5" />
                        </Button>
                        <Button variant="ghost" size="icon" className="h-9 w-9 relative"
                            onClick={() => setMobileFilterSheetOpen(true)}>
                            <HiOutlineAdjustments className="w-4.5 h-4.5" />
                            {(status !== "all" || activeFilterCount > 0) && (
                                <span className="absolute top-1 right-1 w-2 h-2 rounded-full bg-primary" />
                            )}
                        </Button>
                    </div>
                </div>

                {/* 5. Mobile List */}
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
                                    {selectedAccounts.size > 0
                                        ? `${selectedAccounts.size} selected`
                                        : "Select all"}
                                </span>
                            </div>
                            <Button variant="ghost" size="sm" onClick={exitMultiSelectMode}>
                                <HiOutlineX className="w-4 h-4 mr-1" />
                                Cancel
                            </Button>
                        </div>
                    ) : (
                        <div className="flex items-center gap-2 px-4 py-2 border-b">
                            <AnimatePresence mode="wait">
                                {mobileSearchExpanded ? (
                                    <motion.div
                                        key="search-input"
                                        className="flex-1 flex items-center gap-2"
                                        initial={{ width: 0, opacity: 0 }}
                                        animate={{ width: "100%", opacity: 1 }}
                                        exit={{ width: 0, opacity: 0 }}
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
                                ) : (
                                    <motion.div
                                        key="search-icon"
                                        className="flex-1 flex justify-end"
                                        initial={{ opacity: 0 }}
                                        animate={{ opacity: 1 }}
                                        exit={{ opacity: 0 }}
                                    >
                                        <Button
                                            variant="ghost"
                                            size="icon"
                                            className="h-9 w-9"
                                            onClick={() => setMobileSearchExpanded(true)}
                                        >
                                            <HiOutlineSearch className="w-4 h-4" />
                                        </Button>
                                    </motion.div>
                                )}
                            </AnimatePresence>
                        </div>
                    )}

                    {/* Account rows */}
                    {isLoading ? (
                        <div className="divide-y">
                            {[1, 2, 3, 4, 5, 6].map(i => (
                                <div key={i} className="px-4 py-3 space-y-2">
                                    <div className="flex items-center gap-3">
                                        <Skeleton className="h-2.5 w-2.5 rounded-full" />
                                        <Skeleton className="h-4 w-32" />
                                        <Skeleton className="h-5 w-14 rounded-full ml-auto" />
                                    </div>
                                    <div className="flex items-center gap-2 ml-[36px]">
                                        <Skeleton className="h-1.5 flex-1 rounded-full" />
                                        <Skeleton className="h-3 w-16" />
                                    </div>
                                </div>
                            ))}
                        </div>
                    ) : accounts.length > 0 ? (
                        <div className="divide-y">
                            {accounts.map((acc) => (
                                <SwipeableAccountRow
                                    key={acc.id}
                                    account={acc}
                                    isSelected={selectedAccounts.has(acc.id)}
                                    isMultiSelectMode={isMultiSelectMode}
                                    isAnyMutationPending={isAnyMutationPending}
                                    shouldClose={openSwipeRowId !== null && openSwipeRowId !== acc.id}
                                    onOpen={(id) => setOpenSwipeRowId(id)}
                                    onTap={openDetailsSheet}
                                    onToggleSelect={toggleSelectAccount}
                                    onLongPress={(id) => {
                                        enterMultiSelectMode()
                                        toggleSelectAccount(id)
                                    }}
                                    onCopyLink={handleCopyLink}
                                    onSync={handleSync}
                                    onDelete={handleDelete}
                                />
                            ))}
                        </div>
                    ) : (
                        <EmptyState
                            icon={HiOutlineUserGroup}
                            title="No accounts found"
                            description={activeFilterCount > 0 || search ? "Try adjusting your filters" : undefined}
                        />
                    )}
                </div>

                {/* 6. Pagination */}
                {accounts.length > 0 && (
                    <PaginationNav
                        page={page}
                        hasNextPage={totalPages > 0 ? page < totalPages : accounts.length >= perPage}
                        totalPages={totalPages > 0 ? totalPages : undefined}
                        onPageChange={setPage}
                        showingCount={accounts.length}
                    />
                )}

                {/* 7. Floating bulk action bar */}
                {selectedAccounts.size > 0 && (
                    <div className="fixed bottom-[100px] md:bottom-6 left-1/2 -translate-x-1/2 z-50 animate-in slide-in-from-bottom-4 fade-in duration-300">
                        {/* Desktop variant */}
                        <div className="hidden md:flex items-center gap-2 bg-background/95 backdrop-blur-sm border rounded-xl shadow-2xl px-4 py-3">
                            <Badge variant="secondary" className="font-mono text-sm px-3">
                                {selectedAccounts.size} selected
                            </Badge>
                            <div className="h-6 w-px bg-border" />
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleBulkAction("sync", "Sync")}
                                disabled={!selectionInfo.canSync || bulkActionMutation.isPending}
                            >
                                <RefreshCw className="w-3.5 h-3.5 mr-1.5" />
                                Sync All
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleBulkAction("disable", "Disable")}
                                disabled={!selectionInfo.canDisable || bulkActionMutation.isPending}
                            >
                                <PowerOff className="w-3.5 h-3.5 mr-1.5" />
                                Disable
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleBulkAction("enable", "Enable")}
                                disabled={!selectionInfo.canEnable || bulkActionMutation.isPending}
                            >
                                <Power className="w-3.5 h-3.5 mr-1.5" />
                                Enable
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
                            <Button
                                variant="ghost"
                                size="sm"
                                onClick={clearSelection}
                            >
                                <XIcon className="w-3.5 h-3.5 mr-1.5" />
                                Clear
                            </Button>
                        </div>

                        {/* Mobile variant — icon-only compact */}
                        <div className="flex md:hidden items-center gap-1.5 bg-background/95 backdrop-blur-sm border rounded-xl shadow-2xl px-3 py-2.5">
                            <Badge variant="secondary" className="font-mono text-xs px-2">
                                {selectedAccounts.size}
                            </Badge>
                            <div className="h-5 w-px bg-border" />
                            <Button
                                variant="outline"
                                size="icon"
                                className="h-8 w-8"
                                onClick={() => handleBulkAction("sync", "Sync")}
                                disabled={!selectionInfo.canSync || bulkActionMutation.isPending}
                            >
                                <RefreshCw className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                                variant="outline"
                                size="icon"
                                className="h-8 w-8"
                                onClick={() => handleBulkAction("disable", "Disable")}
                                disabled={!selectionInfo.canDisable || bulkActionMutation.isPending}
                            >
                                <PowerOff className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                                variant="outline"
                                size="icon"
                                className="h-8 w-8"
                                onClick={() => handleBulkAction("enable", "Enable")}
                                disabled={!selectionInfo.canEnable || bulkActionMutation.isPending}
                            >
                                <Power className="w-3.5 h-3.5" />
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

                {/* 8. Mobile FAB */}
                <AnimatePresence>
                    {selectedAccounts.size === 0 && (
                        <motion.button
                            className="fixed bottom-[100px] right-6 z-40 md:hidden w-14 h-14 rounded-full bg-primary text-primary-foreground shadow-lg flex items-center justify-center active:scale-95"
                            onClick={openCreateDialog}
                            initial={{ scale: 0, opacity: 0 }}
                            animate={{ scale: 1, opacity: 1 }}
                            exit={{ scale: 0, opacity: 0 }}
                            transition={{ type: "spring", stiffness: 300, damping: 25 }}
                        >
                            <Plus className="w-6 h-6" />
                        </motion.button>
                    )}
                </AnimatePresence>

                {/* 9. Dialogs / Sheets */}
                <AccountDetailsSheet />
                <DeleteAccountDialog />
                <CreateAccountDialog />

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
                                        { label: "disabled", value: counts?.disabled ?? 0, filter: "disabled" as const, activeColor: "bg-red-500/10 text-red-500 border-red-500/30" },
                                        { label: "expired", value: counts?.expired ?? 0, filter: "expired" as const, activeColor: "bg-amber-500/10 text-amber-500 border-amber-500/30" },
                                    ]}
                                    activeFilter={status}
                                    defaultFilter="all"
                                    onFilterChange={setStatus}
                                />
                            </div>
                            <div>
                                <label className="text-xs font-medium text-muted-foreground mb-2 block">Auto Refresh</label>
                                <AutoRefreshControl
                                    onRefresh={handleRefresh}
                                    isRefreshing={isRefetching || isLoading}
                                    dataUpdatedAt={dataUpdatedAt}
                                />
                            </div>
                            <div className="space-y-2">
                                <label className="text-xs font-medium text-muted-foreground block">Advanced</label>
                                <Select value={nodeFilter} onValueChange={setNodeFilter}>
                                    <SelectTrigger className="w-full">
                                        <SelectValue placeholder="Node" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Nodes</SelectItem>
                                        {nodes.map(node => (
                                            <SelectItem key={node.id} value={node.id.toString()}>{node.name}</SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                <Select value={inboundFilter} onValueChange={setInboundFilter}>
                                    <SelectTrigger className="w-full">
                                        <SelectValue placeholder="Inbound" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Inbounds</SelectItem>
                                        {filteredInbounds.map(inb => (
                                            <SelectItem key={inb.id} value={inb.id.toString()}>
                                                {inb.tag || inb.remark || `Port ${inb.port}`}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                <Select value={sourceFilter} onValueChange={setSourceFilter}>
                                    <SelectTrigger className="w-full">
                                        <SelectValue placeholder="Source" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">All Sources</SelectItem>
                                        <SelectItem value="subscription">Subscription</SelectItem>
                                        <SelectItem value="manual">Manual</SelectItem>
                                        <SelectItem value="import">Import</SelectItem>
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
