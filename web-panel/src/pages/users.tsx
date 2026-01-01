import { useEffect, useState, useMemo, useRef } from "react"
import { useNavigate } from "react-router"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
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
    DropdownMenuLabel,
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
    HiOutlineSearch,
    HiOutlineDotsVertical,
    HiOutlineEye,
    HiOutlineCash,
    HiOutlineBan,
    HiOutlineCheckCircle,
    HiOutlineShieldCheck,
    HiOutlineRefresh,
    HiOutlineUsers,
    HiOutlineSortAscending,
    HiOutlineSortDescending,
    HiOutlineX,
    HiOutlineClock,
} from "react-icons/hi"
import { HiOutlineAdjustments } from "react-icons/hi"
import { motion, AnimatePresence } from "framer-motion"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import type { User, UserListItem } from "@/lib/types"
import type { SortField, FilterType } from "@/store/users-store"
import { cn } from "@/lib/utils"
import { FilterPills, type FilterPill } from "@/components/ui/filter-pills"
import { PaginationNav } from "@/components/ui/pagination-nav"
import { useConfirm } from "@/components/ui/confirm-dialog"
import {
    useUsers,
    useBanUser,
    useToggleAdmin,
    useBulkBan,
    useBulkUnban,
    useDashboardStats,
} from "@/lib/queries"
import { useUsersStore } from "@/store/users-store"
import { useQueryClient } from "@tanstack/react-query"

// ==================== Helpers ====================

function getDisplayName(user: User): string {
    if (user.username) return `@${user.username}`
    if (user.first_name || user.last_name) {
        return [user.first_name, user.last_name].filter(Boolean).join(" ")
    }
    return `ID: ${user.telegram_id}`
}

function formatRelativeTime(dateStr: string | null | undefined): string {
    if (!dateStr) return "Never"
    const now = Date.now()
    const then = new Date(dateStr).getTime()
    const diffMs = now - then
    if (diffMs < 0) return "Just now"
    const seconds = Math.floor(diffMs / 1000)
    if (seconds < 60) return "Just now"
    const minutes = Math.floor(seconds / 60)
    if (minutes < 60) return `${minutes}m ago`
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return `${hours}h ago`
    const days = Math.floor(hours / 24)
    if (days < 30) return `${days}d ago`
    const months = Math.floor(days / 30)
    if (months < 12) return `${months}mo ago`
    return `${Math.floor(months / 12)}y ago`
}

function getLastActiveColor(dateStr: string | null | undefined): string {
    if (!dateStr) return "text-muted-foreground/50"
    const diffMs = Date.now() - new Date(dateStr).getTime()
    const hours = diffMs / (1000 * 60 * 60)
    if (hours < 24) return "text-emerald-500"
    if (hours < 168) return "text-muted-foreground" // < 7 days
    return "text-muted-foreground/50"
}

function formatSubCount(active: number, total: number): string {
    if (active === 0 && total === 0) return "\u2014"
    return `${active}/${total}`
}

// ==================== Components ====================

function UserAvatar({ user, size = "md" }: { user: User; size?: "sm" | "md" | "lg" }) {
    const sizeClasses = {
        sm: "w-8 h-8 text-xs",
        md: "w-10 h-10 text-sm",
        lg: "w-12 h-12 text-base",
    }
    const initials = user.username
        ? user.username.charAt(0).toUpperCase()
        : user.first_name
            ? user.first_name.charAt(0).toUpperCase()
            : "U"
    return (
        <div className={cn(
            "rounded-full bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center font-medium text-primary shrink-0",
            sizeClasses[size]
        )}>
            {initials}
        </div>
    )
}

function TableSkeleton() {
    return (
        <>
            {[1, 2, 3, 4, 5].map((i) => (
                <TableRow key={i}>
                    <TableCell><Skeleton className="w-4 h-4" /></TableCell>
                    <TableCell>
                        <div className="flex items-center gap-3">
                            <Skeleton className="w-8 h-8 rounded-full" />
                            <Skeleton className="h-4 w-24" />
                        </div>
                    </TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-14" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-5 w-14 rounded-full" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-7 w-7" /></TableCell>
                </TableRow>
            ))}
        </>
    )
}

function SortableHeader({
    children,
    field,
    currentSort,
    currentDir,
    onSort,
}: {
    children: React.ReactNode
    field: SortField
    currentSort: SortField
    currentDir: string
    onSort: (field: SortField) => void
}) {
    const isActive = currentSort === field
    return (
        <button
            onClick={() => onSort(field)}
            className="flex items-center gap-1 hover:text-foreground transition-colors"
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

function SelectionActionBar({
    selectedCount,
    canBan,
    canUnban,
    onBan,
    onUnban,
    onClear,
    isLoading,
}: {
    selectedCount: number
    canBan: boolean
    canUnban: boolean
    onBan: () => void
    onUnban: () => void
    onClear: () => void
    isLoading: boolean
}) {
    return (
        <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 animate-in slide-in-from-bottom-4 fade-in duration-300 max-w-[calc(100%-2rem)] w-auto">
            <div className="bg-card border border-border rounded-xl shadow-2xl px-3 sm:px-4 py-3 flex flex-wrap items-center justify-center gap-2 sm:gap-4">
                <Badge variant="default" className="text-sm px-3 py-1">
                    {selectedCount} selected
                </Badge>
                <div className="h-6 w-px bg-border hidden sm:block" />
                <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={onBan} disabled={isLoading || !canBan}
                        className="text-red-500 hover:text-red-600 hover:bg-red-500/10 disabled:opacity-40">
                        <HiOutlineBan className="w-4 h-4 mr-1.5" />Ban
                    </Button>
                    <Button variant="outline" size="sm" onClick={onUnban} disabled={isLoading || !canUnban}
                        className="text-emerald-500 hover:text-emerald-600 hover:bg-emerald-500/10 disabled:opacity-40">
                        <HiOutlineCheckCircle className="w-4 h-4 mr-1.5" />Unban
                    </Button>
                </div>
                <div className="h-6 w-px bg-border hidden sm:block" />
                <Button variant="ghost" size="sm" onClick={onClear} className="text-muted-foreground hover:text-foreground">
                    <HiOutlineX className="w-4 h-4 mr-1" />Clear
                </Button>
            </div>
        </div>
    )
}

// ==================== Sort options ====================

const SORT_OPTIONS: { value: SortField; label: string }[] = [
    { value: "created_at", label: "Joined Date" },
    { value: "active_subscriptions", label: "Active Subs" },
    { value: "total_subscriptions", label: "Total Subs" },
    { value: "last_active_at", label: "Last Active" },
    { value: "username", label: "Username" },
]

// ==================== Main Page ====================

export default function UsersPage() {
    const navigate = useNavigate()
    const queryClient = useQueryClient()

    const {
        search, page, filter, sortField, sortDir,
        setSearch, setPage, setFilter, toggleSort, setSortDir,
        selectedUsers, selectAll, toggleSelectUser, clearSelection,
        autoRefreshInterval, setAutoRefreshInterval,
        mobileSearchExpanded, setMobileSearchExpanded,
        mobileFilterSheetOpen, setMobileFilterSheetOpen,
    } = useUsersStore()

    const mobileSearchRef = useRef<HTMLInputElement>(null)

    const perPage = 10
    const [debouncedSearch, setDebouncedSearch] = useState(search)

    const { data, isLoading, isRefetching, refetch } = useUsers({
        page,
        perPage,
        search: debouncedSearch,
        filter,
        sort: sortField,
        order: sortDir,
    }, { refetchInterval: autoRefreshInterval })

    const users = data?.users || []
    const total = data?.total || 0
    const totalPages = Math.ceil(total / perPage)

    // Auto-correct page when it exceeds total pages (e.g. after sort/filter change)
    useEffect(() => {
        if (totalPages > 0 && page > totalPages) {
            setPage(totalPages)
        }
    }, [totalPages, page, setPage])

    const { data: dashboardStats } = useDashboardStats()
    const stats = {
        total: total,
        active: dashboardStats?.active_users ?? 0,
        banned: dashboardStats?.banned_users ?? 0,
        admins: dashboardStats?.admin_users ?? 0,
    }

    const banMutation = useBanUser()
    const adminMutation = useToggleAdmin()
    const bulkBanMutation = useBulkBan()
    const bulkUnbanMutation = useBulkUnban()

    const selectionInfo = useMemo(() => {
        const selectedUsersList = users.filter(u => selectedUsers.has(u.id))
        const hasActiveUsers = selectedUsersList.some(u => !u.is_banned)
        const hasBannedUsers = selectedUsersList.some(u => u.is_banned)
        return { canBan: hasActiveUsers, canUnban: hasBannedUsers }
    }, [users, selectedUsers])

    useEffect(() => {
        const timer = setTimeout(() => setDebouncedSearch(search), 300)
        return () => clearTimeout(timer)
    }, [search])

    const confirmDialog = useConfirm()

    const handleSelectAll = () => {
        if (selectedUsers.size === users.length) clearSelection()
        else selectAll(users.map(u => u.id))
    }

    const handleBulkBan = async () => {
        if (selectedUsers.size === 0) return
        const confirmed = await confirmDialog({
            title: "Ban Users",
            description: `Are you sure you want to ban ${selectedUsers.size} selected users?`,
            confirmLabel: "Ban",
            variant: "destructive",
        })
        if (!confirmed) return
        bulkBanMutation.mutate(Array.from(selectedUsers), { onSuccess: clearSelection })
    }

    const handleBulkUnban = async () => {
        if (selectedUsers.size === 0) return
        const confirmed = await confirmDialog({
            title: "Unban Users",
            description: `Are you sure you want to unban ${selectedUsers.size} selected users?`,
            confirmLabel: "Unban",
        })
        if (!confirmed) return
        bulkUnbanMutation.mutate(Array.from(selectedUsers), { onSuccess: clearSelection })
    }

    const handleBanUser = (user: User) => {
        banMutation.mutate({ userId: user.id, isBanned: user.is_banned })
    }

    const handleToggleAdmin = (user: User) => {
        adminMutation.mutate({ userId: user.id, isAdmin: user.is_admin })
    }

    const isActionLoading = banMutation.isPending || adminMutation.isPending ||
        bulkBanMutation.isPending || bulkUnbanMutation.isPending

    return (
        <div className="space-y-4 md:space-y-6 animate-in fade-in duration-500">
            <div className="hidden md:flex flex-col gap-3">
                {/* Row 1: Title + Pills */}
                <div className="flex items-center gap-4">
                    <h1 className="text-2xl font-bold tracking-tight shrink-0">Users</h1>
                    <FilterPills<FilterType>
                        pills={[
                            { label: "total", value: stats.total, filter: "all", activeColor: "bg-primary/10 text-primary border-primary/30" },
                            { label: "active", value: stats.active, filter: "active", activeColor: "bg-emerald-500/10 text-emerald-500 border-emerald-500/30" },
                            { label: "banned", value: stats.banned, filter: "banned", activeColor: "bg-red-500/10 text-red-500 border-red-500/30" },
                            { label: "admins", value: stats.admins, filter: "admin", activeColor: "bg-amber-500/10 text-amber-500 border-amber-500/30" },
                        ]}
                        activeFilter={filter}
                        defaultFilter="all"
                        onFilterChange={setFilter}
                    />
                </div>

                {/* Row 2: Search + Filters */}
                <div className="flex flex-col md:flex-row items-stretch md:items-center gap-3">
                    {/* Search */}
                    <div className="relative flex-1">
                        <HiOutlineSearch className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                        <Input
                            placeholder="Search users..."
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            className="pl-9 h-9 bg-card/50"
                        />
                    </div>

                    {/* Filters & Controls */}
                    <div className="flex flex-wrap items-center gap-2">
                        {/* Status filter */}
                        <Select value={filter} onValueChange={(v) => setFilter(v as FilterType)}>
                            <SelectTrigger className="w-[140px] h-9 text-xs">
                                <SelectValue placeholder="Filter" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="all">All Users</SelectItem>
                                <SelectItem value="active">Active</SelectItem>
                                <SelectItem value="banned">Banned</SelectItem>
                                <SelectItem value="admin">Admins</SelectItem>
                                <SelectItem value="has_subscription">Has Active Sub</SelectItem>
                                <SelectItem value="no_subscription">No Subs</SelectItem>
                            </SelectContent>
                        </Select>

                        {/* Sort dropdown + direction toggle */}
                        <div className="flex items-center gap-0">
                            <Select value={sortField} onValueChange={(v) => toggleSort(v as SortField)}>
                                <SelectTrigger className="w-[140px] h-9 text-xs md:rounded-r-md rounded-r-none border-r-0 md:border-r">
                                    <SelectValue placeholder="Sort" />
                                </SelectTrigger>
                                <SelectContent>
                                    {SORT_OPTIONS.map(opt => (
                                        <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>

                        {/* Auto refresh + Manual refresh */}
                        <div className="flex items-center gap-1 border rounded-md bg-background p-0.5 h-9">
                            <DropdownMenu>
                                <DropdownMenuTrigger asChild>
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        className={cn(
                                            "h-8 gap-2 px-2 text-xs font-normal border-r rounded-r-none",
                                            autoRefreshInterval > 0 && "text-primary bg-primary/10"
                                        )}
                                    >
                                        <HiOutlineClock className={cn("w-3.5 h-3.5", autoRefreshInterval > 0 && "animate-pulse")} />
                                        <span className="hidden sm:inline">
                                            {autoRefreshInterval === 0 ? "Auto" : `${autoRefreshInterval / 1000}s`}
                                        </span>
                                    </Button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="end">
                                    <DropdownMenuLabel>Auto Refresh</DropdownMenuLabel>
                                    <DropdownMenuSeparator />
                                    {[0, 5000, 10000, 30000, 60000].map(interval => (
                                        <DropdownMenuItem key={interval} onClick={() => setAutoRefreshInterval(interval)}>
                                            {autoRefreshInterval === interval && <HiOutlineCheckCircle className="w-3.5 h-3.5 mr-2 text-primary" />}
                                            <span className={cn(autoRefreshInterval !== interval && "ml-5.5")}>
                                                {interval === 0 ? "Off" : interval < 60000 ? `${interval / 1000} seconds` : "1 minute"}
                                            </span>
                                        </DropdownMenuItem>
                                    ))}
                                </DropdownMenuContent>
                            </DropdownMenu>

                            <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => refetch()}
                                disabled={isLoading || isRefetching}
                                className="h-8 w-8 p-0 rounded-l-none"
                                title="Refresh list"
                            >
                                <HiOutlineRefresh className={cn("w-3.5 h-3.5", (isLoading || isRefetching) && "animate-spin")} />
                            </Button>
                        </div>
                    </div>
                </div>
            </div>

            {/* Desktop Table — Subscription-First */}
            <Card className="hidden md:block overflow-hidden">
                <div className="p-0">
                    <Table>
                        <TableHeader>
                            <TableRow className="hover:bg-transparent">
                                <TableHead className="w-[40px] pl-4">
                                    <Checkbox
                                        checked={selectedUsers.size === users.length && users.length > 0}
                                        onCheckedChange={handleSelectAll}
                                        aria-label="Select all"
                                    />
                                </TableHead>
                                <TableHead>
                                    <SortableHeader field="username" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                        User
                                    </SortableHeader>
                                </TableHead>
                                <TableHead>
                                    <SortableHeader field="active_subscriptions" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                        Active Subs
                                    </SortableHeader>
                                </TableHead>
                                <TableHead>
                                    <SortableHeader field="last_active_at" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                        Last Active
                                    </SortableHeader>
                                </TableHead>
                                <TableHead>Status</TableHead>
                                <TableHead>
                                    <SortableHeader field="created_at" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                        Joined
                                    </SortableHeader>
                                </TableHead>
                                <TableHead className="w-[50px]"></TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {isLoading ? (
                                <TableSkeleton />
                            ) : users.length > 0 ? (
                                users.map((user) => (
                                    <TableRow
                                        key={user.id}
                                        className="group cursor-pointer hover:bg-muted/40 h-10 border-b-border/50"
                                        onClick={(e) => {
                                            const target = e.target as HTMLElement
                                            if (target.closest('[role="checkbox"]') || target.closest('button') || target.closest('[role="menuitem"]')) return
                                            navigate(`/users/${user.id}`)
                                        }}
                                    >
                                        <TableCell className="pl-4 py-2">
                                            <Checkbox
                                                checked={selectedUsers.has(user.id)}
                                                onCheckedChange={() => toggleSelectUser(user.id)}
                                                aria-label={`Select user ${user.id}`}
                                            />
                                        </TableCell>
                                        <TableCell className="py-2">
                                            <div className="flex items-center gap-2">
                                                <UserAvatar user={user} size="sm" />
                                                <p className="font-medium text-sm truncate max-w-[160px]">{getDisplayName(user)}</p>
                                            </div>
                                        </TableCell>
                                        <TableCell className="py-2">
                                            {user.active_subscriptions > 0 ? (
                                                <span className="inline-flex items-center gap-1 text-sm font-medium">
                                                    <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 shrink-0" />
                                                    {formatSubCount(user.active_subscriptions, user.total_subscriptions)}
                                                </span>
                                            ) : user.total_subscriptions > 0 ? (
                                                <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
                                                    <span className="w-1.5 h-1.5 rounded-full bg-muted-foreground/30 shrink-0" />
                                                    {formatSubCount(0, user.total_subscriptions)}
                                                </span>
                                            ) : (
                                                <span className="text-sm text-muted-foreground/50">{"\u2014"}</span>
                                            )}
                                        </TableCell>
                                        <TableCell className={cn("py-2 text-xs", getLastActiveColor(user.last_active_at))}>
                                            {formatRelativeTime(user.last_active_at)}
                                        </TableCell>
                                        <TableCell className="py-2">
                                            {user.is_banned ? (
                                                <Badge variant="danger" className="py-0 h-5 text-[10px]">Banned</Badge>
                                            ) : user.is_admin ? (
                                                <Badge variant="default" className="py-0 h-5 text-[10px]">Admin</Badge>
                                            ) : (
                                                <Badge variant="success" className="py-0 h-5 text-[10px] bg-emerald-500/10 text-emerald-500 border-emerald-500/20">Active</Badge>
                                            )}
                                        </TableCell>
                                        <TableCell className="text-muted-foreground text-xs py-2">
                                            {new Date(user.created_at).toLocaleDateString()}
                                        </TableCell>
                                        <TableCell className="py-2 pr-4">
                                            <DropdownMenu>
                                                <DropdownMenuTrigger asChild>
                                                    <Button variant="ghost" size="icon" className="h-7 w-7" disabled={isActionLoading}>
                                                        <HiOutlineDotsVertical className="w-3.5 h-3.5" />
                                                    </Button>
                                                </DropdownMenuTrigger>
                                                <DropdownMenuContent align="end">
                                                    <DropdownMenuItem asChild>
                                                        <a href={`/users/${user.id}`}>
                                                            <HiOutlineEye className="w-4 h-4 mr-2" />View Details
                                                        </a>
                                                    </DropdownMenuItem>
                                                    <DropdownMenuSeparator />
                                                    <DropdownMenuItem onClick={() => handleToggleAdmin(user)}>
                                                        <HiOutlineShieldCheck className="w-4 h-4 mr-2" />
                                                        {user.is_admin ? "Remove Admin" : "Make Admin"}
                                                    </DropdownMenuItem>
                                                    <DropdownMenuItem onClick={() => handleBanUser(user)} className={user.is_banned ? "text-emerald-500" : "text-red-500"}>
                                                        {user.is_banned ? "Unban User" : "Ban User"}
                                                    </DropdownMenuItem>
                                                </DropdownMenuContent>
                                            </DropdownMenu>
                                        </TableCell>
                                    </TableRow>
                                ))
                            ) : (
                                <TableRow>
                                    <TableCell colSpan={8} className="text-center py-12 text-muted-foreground text-sm">No users found</TableCell>
                                </TableRow>
                            )}
                        </TableBody>
                    </Table>
                </div>
            </Card>

            {/* Mobile Compact Header */}
            <div className="md:hidden flex items-center justify-between px-4 py-2.5">
                <div className="flex items-baseline gap-2">
                    <h1 className="text-xl font-bold tracking-tight">Users</h1>
                    <span className="text-sm text-muted-foreground">{stats.total}</span>
                </div>
                <div className="flex items-center gap-1">
                    <Button variant="ghost" size="icon" className="h-9 w-9"
                        onClick={() => setMobileSearchExpanded(true)}>
                        <HiOutlineSearch className="w-4.5 h-4.5" />
                    </Button>
                    <Button variant="ghost" size="icon" className="h-9 w-9 relative"
                        onClick={() => setMobileFilterSheetOpen(true)}>
                        <HiOutlineAdjustments className="w-4.5 h-4.5" />
                        {filter !== "all" && (
                            <span className="absolute top-1 right-1 w-2 h-2 rounded-full bg-primary" />
                        )}
                    </Button>
                </div>
            </div>

            {/* Mobile Expandable Search */}
            <div className="md:hidden">
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
                                    placeholder="Search users..."
                                    value={search}
                                    onChange={(e) => setSearch(e.target.value)}
                                    className="pl-9 h-9"
                                    autoFocus
                                />
                            </div>
                            <Button variant="ghost" size="icon" className="h-9 w-9 shrink-0"
                                onClick={() => { setSearch(""); setMobileSearchExpanded(false) }}>
                                <HiOutlineX className="w-4 h-4" />
                            </Button>
                        </motion.div>
                    )}
                </AnimatePresence>
            </div>

            {/* Mobile List — Subscription-First */}
            <div className="md:hidden space-y-2">
                {isLoading ? (
                    <div className="space-y-2">
                        {[1, 2, 3, 4, 5].map(i => <Skeleton key={i} className="h-16 w-full rounded-lg" />)}
                    </div>
                ) : users.length > 0 ? (
                    <div className="rounded-lg border bg-card/50 overflow-hidden divide-y">
                        {users.map((user) => (
                            <div
                                key={user.id}
                                className="p-3 flex items-center justify-between hover:bg-muted/50 transition-colors cursor-pointer"
                                onClick={(e) => {
                                    const target = e.target as HTMLElement
                                    if (target.closest('button') || target.closest('[role="menuitem"]')) return
                                    navigate(`/users/${user.id}`)
                                }}
                            >
                                <div className="flex items-center gap-3 overflow-hidden">
                                    <UserAvatar user={user} size="sm" />
                                    <div className="min-w-0">
                                        <div className="flex items-center gap-2">
                                            <p className="font-medium text-sm truncate">{getDisplayName(user)}</p>
                                            {user.is_admin && <Badge variant="secondary" className="px-1 py-0 text-[10px] h-4">Admin</Badge>}
                                            {user.is_banned && <Badge variant="danger" className="px-1 py-0 text-[10px] h-4">Banned</Badge>}
                                        </div>
                                        <p className="text-xs text-muted-foreground truncate">
                                            {user.active_subscriptions > 0
                                                ? `${user.active_subscriptions} active sub${user.active_subscriptions !== 1 ? "s" : ""}`
                                                : "No subs"
                                            }
                                            {" \u00B7 "}
                                            <span className={getLastActiveColor(user.last_active_at)}>
                                                {formatRelativeTime(user.last_active_at)}
                                            </span>
                                        </p>
                                    </div>
                                </div>
                                <DropdownMenu>
                                    <DropdownMenuTrigger asChild>
                                        <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0">
                                            <HiOutlineDotsVertical className="w-4 h-4 text-muted-foreground" />
                                        </Button>
                                    </DropdownMenuTrigger>
                                    <DropdownMenuContent align="end">
                                        <DropdownMenuItem onClick={() => navigate(`/users/${user.id}`)}>
                                            <HiOutlineEye className="w-4 h-4 mr-2" />Details
                                        </DropdownMenuItem>
                                        <DropdownMenuSeparator />
                                        <DropdownMenuItem onClick={() => handleToggleAdmin(user)}>
                                            <HiOutlineShieldCheck className="w-4 h-4 mr-2" />
                                            {user.is_admin ? "Demote" : "Make Admin"}
                                        </DropdownMenuItem>
                                        <DropdownMenuItem onClick={() => handleBanUser(user)} className={user.is_banned ? "text-emerald-500" : "text-red-500"}>
                                            {user.is_banned ? "Unban" : "Ban"}
                                        </DropdownMenuItem>
                                    </DropdownMenuContent>
                                </DropdownMenu>
                            </div>
                        ))}
                    </div>
                ) : (
                    <div className="text-center py-12 text-muted-foreground">No users found</div>
                )}
            </div>

            {/* Pagination */}
            {totalPages > 1 && (
                <PaginationNav
                    page={page}
                    hasNextPage={page < totalPages}
                    totalPages={totalPages}
                    onPageChange={setPage}
                    className="mt-4 pb-8"
                />
            )}

            {/* Floating Selection Action Bar */}
            {selectedUsers.size > 0 && (
                <SelectionActionBar
                    selectedCount={selectedUsers.size}
                    canBan={selectionInfo.canBan}
                    canUnban={selectionInfo.canUnban}
                    onBan={handleBulkBan}
                    onUnban={handleBulkUnban}
                    onClear={clearSelection}
                    isLoading={isActionLoading}
                />
            )}


            {/* Mobile Filter Sheet */}
            <Sheet open={mobileFilterSheetOpen} onOpenChange={setMobileFilterSheetOpen}>
                <SheetContent side="bottom" className="md:hidden rounded-t-2xl max-h-[85vh] overflow-y-auto">
                    <SheetHeader>
                        <SheetTitle>Filters</SheetTitle>
                    </SheetHeader>
                    <div className="space-y-4 py-4">
                        <div>
                            <label className="text-xs font-medium text-muted-foreground mb-2 block">Status</label>
                            <FilterPills<FilterType>
                                pills={[
                                    { label: "total", value: stats.total, filter: "all", activeColor: "bg-primary/10 text-primary border-primary/30" },
                                    { label: "active", value: stats.active, filter: "active", activeColor: "bg-emerald-500/10 text-emerald-500 border-emerald-500/30" },
                                    { label: "banned", value: stats.banned, filter: "banned", activeColor: "bg-red-500/10 text-red-500 border-red-500/30" },
                                    { label: "admins", value: stats.admins, filter: "admin", activeColor: "bg-amber-500/10 text-amber-500 border-amber-500/30" },
                                ]}
                                activeFilter={filter}
                                defaultFilter="all"
                                onFilterChange={setFilter}
                            />
                        </div>
                        <div>
                            <label className="text-xs font-medium text-muted-foreground mb-2 block">Sort</label>
                            <div className="flex gap-2">
                                <Select value={sortField} onValueChange={(v) => toggleSort(v as SortField)}>
                                    <SelectTrigger className="flex-1">
                                        <SelectValue placeholder="Sort by" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {SORT_OPTIONS.map(opt => (
                                            <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                <Button variant="outline" size="icon" className="h-10 w-10 shrink-0"
                                    onClick={() => setSortDir(sortDir === "asc" ? "desc" : "asc")}>
                                    {sortDir === "asc"
                                        ? <HiOutlineSortAscending className="w-4 h-4" />
                                        : <HiOutlineSortDescending className="w-4 h-4" />
                                    }
                                </Button>
                            </div>
                        </div>
                        <div>
                            <label className="text-xs font-medium text-muted-foreground mb-2 block">Filter</label>
                            <Select value={filter} onValueChange={(v) => setFilter(v as FilterType)}>
                                <SelectTrigger className="w-full">
                                    <SelectValue placeholder="Filter" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="all">All Users</SelectItem>
                                    <SelectItem value="active">Active</SelectItem>
                                    <SelectItem value="banned">Banned</SelectItem>
                                    <SelectItem value="admin">Admins</SelectItem>
                                    <SelectItem value="has_subscription">Has Active Sub</SelectItem>
                                    <SelectItem value="no_subscription">No Subs</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                </SheetContent>
            </Sheet>
        </div>
    )
}
