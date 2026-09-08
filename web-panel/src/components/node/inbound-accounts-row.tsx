import { useState, useMemo } from "react"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogFooter,
} from "@/components/ui/dialog"
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
import {
    HiOutlineClipboard,
    HiOutlineTrash,
    HiOutlineQrcode,
    HiDotsVertical,
    HiOutlineSortAscending,
    HiOutlineSortDescending,
    HiOutlineSearch,
    HiOutlinePlus,
    HiOutlineExternalLink,
    HiOutlineChevronLeft,
    HiOutlineChevronRight,
    HiOutlineX,
} from "react-icons/hi"
import { cn, formatBytes, formatDataLimit, getExpiryInfo, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"
import { deleteAccount } from "@/lib/admin-api"
import { type Account, disableAccount, enableAccount, getAccountLink } from "@/lib/api/accounts"
import { useQueryClient } from "@tanstack/react-query"
import { QRCodeSVG } from "qrcode.react"
import { Link } from "react-router"
import { AccountMigrationDialog } from "./account-migration-dialog"
import { CreateAccountDialog } from "./create-account-dialog"
import { Input } from "@/components/ui/input"

import { HiArrowRightOnRectangle } from "react-icons/hi2"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import { useAccountsStore } from "@/store/accounts-store"
import { SubscriptionDetailsSheet } from "@/components/subscription/subscription-details-sheet"

interface InboundAccountsRowProps {
    accounts: Account[]
    nodeId: number
    isOnline: boolean
    onAccountChange?: () => void
    /** Set when the list is scoped to one inbound: enables "+ Client" prefill. */
    inboundId?: number
}

type StatusFilter = "all" | "online" | "active" | "disabled" | "expired"

const STATUS_FILTERS: { value: StatusFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "online", label: "Online" },
    { value: "active", label: "Active" },
    { value: "disabled", label: "Disabled" },
    { value: "expired", label: "Expired" },
]

// Small enough that an expanded inbound never buries the ones below it.
const PAGE_SIZE = 8

type InboundSortField = "client" | "traffic" | "duration" | "status"
type SortDir = "asc" | "desc"

function SortableHeader({
    children,
    field,
    currentSort,
    currentDir,
    onSort,
}: {
    children: React.ReactNode
    field: InboundSortField
    currentSort: InboundSortField
    currentDir: SortDir
    onSort: (field: InboundSortField) => void
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

function DataUsageBar({ used, limit, maxUsage = 0, isDisabled = false }: { used: number; limit: number; maxUsage?: number; isDisabled?: boolean }) {
    const percentage = limit > 0 ? Math.min((used / limit) * 100, 100) : 0
    const isUnlimited = limit === 0
    const relativePercent = isUnlimited && maxUsage > 0 ? Math.max(2, (used / maxUsage) * 100) : 0

    if (isDisabled) {
        return (
            <div className="space-y-1 min-w-[120px] opacity-40">
                <div className="flex justify-between text-xs">
                    <span>{formatBytes(used)}</span>
                    <span className="text-muted-foreground">
                        {isUnlimited ? "Unlimited" : formatBytes(limit)}
                    </span>
                </div>
                <div className="h-1.5 bg-muted rounded-full overflow-hidden">
                    <div
                        className="h-full rounded-full bg-muted-foreground/30"
                        style={{ width: isUnlimited ? `${relativePercent}%` : `${percentage}%` }}
                    />
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
                            percentage > 80 ? "bg-red-500" :
                                percentage >= 50 ? "bg-amber-500" : "bg-emerald-500"
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

function isAccountOnline(lastActivityAt?: string): boolean {
    if (!lastActivityAt) return false
    return (Date.now() - new Date(lastActivityAt).getTime()) < 10_000
}

export function InboundAccountsRow({ accounts, nodeId, isOnline, onAccountChange, inboundId }: InboundAccountsRowProps) {
    const queryClient = useQueryClient()
    const { openDetailsSheet: openSubscriptionSheet } = useSubscriptionsStore()
    const { openDetailsSheet: openAccountDetails } = useAccountsStore()
    const [loadingStates, setLoadingStates] = useState<Record<number, boolean>>({})
    const [qrDialog, setQrDialog] = useState<{ open: boolean; link: string; email: string }>({
        open: false,
        link: "",
        email: ""
    })
    const [migrateDialog, setMigrateDialog] = useState<{ open: boolean; account: Account | null }>({
        open: false,
        account: null
    })
    const [deleteDialog, setDeleteDialog] = useState<{ open: boolean; account: Account | null }>({
        open: false,
        account: null
    })
    const [sortField, setSortField] = useState<InboundSortField>("traffic")
    const [sortDir, setSortDir] = useState<SortDir>("desc")

    // Every list-shaping change sends you back to page 1; safePage below covers
    // the rest (a row deleted off the end of the last page).
    const toggleSort = (field: InboundSortField) => {
        if (sortField === field) {
            setSortDir(d => d === "asc" ? "desc" : "asc")
        } else {
            setSortField(field)
            setSortDir("desc")
        }
        setPage(1)
    }

    // Max data usage among unlimited accounts (for relative bar scaling)
    const maxUnlimitedUsage = useMemo(() => {
        return accounts.reduce((max, a) => {
            const isShared = !!a.subscription
            const limit = isShared
                ? (a.subscription?.custom_data_limit ?? a.subscription?.data_limit ?? a.data_limit)
                : a.data_limit
            const used = isShared
                ? (a.subscription?.data_used ?? a.data_used)
                : a.data_used
            if (limit === 0) return Math.max(max, used)
            return max
        }, 0)
    }, [accounts])

    const sortedAccounts = useMemo(() => {
        const getAccountData = (a: Account) => {
            const isShared = !!a.subscription
            const limit = isShared
                ? (a.subscription?.custom_data_limit ?? a.subscription?.data_limit ?? a.data_limit)
                : a.data_limit
            const used = isShared
                ? (a.subscription?.data_used ?? a.data_used)
                : a.data_used
            const expiry = a.subscription?.custom_end_date ?? a.subscription?.end_date ?? a.expires_at
            const user = a.subscription?.user
            const name = user?.username
                ? `@${user.username}`
                : user?.first_name || (a.source === "manual" ? a.email : "")
            return { limit, used, expiry, name }
        }

        const statusOrder: Record<string, number> = { active: 0, disabled: 1, expired: 2 }

        return [...accounts].sort((a, b) => {
            const da = getAccountData(a)
            const db = getAccountData(b)
            let cmp = 0

            switch (sortField) {
                case "client":
                    cmp = da.name.localeCompare(db.name)
                    break
                case "traffic":
                    cmp = da.used - db.used
                    break
                case "duration": {
                    const ta = da.expiry ? new Date(da.expiry).getTime() : Infinity
                    const tb = db.expiry ? new Date(db.expiry).getTime() : Infinity
                    cmp = ta - tb
                    break
                }
                case "status":
                    cmp = (statusOrder[a.status] ?? 3) - (statusOrder[b.status] ?? 3)
                    break
            }

            return sortDir === "asc" ? cmp : -cmp
        })
    }, [accounts, sortField, sortDir])

    const [query, setQuery] = useState("")
    const [statusFilter, setStatusFilter] = useState<StatusFilter>("all")
    const [createOpen, setCreateOpen] = useState(false)
    const [page, setPage] = useState(1)

    const visibleAccounts = useMemo(() => {
        const q = query.trim().toLowerCase()
        return sortedAccounts.filter(a => {
            const user = a.subscription?.user
            if (q) {
                const haystack = [
                    a.email,
                    user?.username,
                    user?.first_name,
                    a.subscription_id ? `sub #${a.subscription_id}` : "",
                ].filter(Boolean).join(" ").toLowerCase()
                if (!haystack.includes(q)) return false
            }
            if (statusFilter === "all") return true
            const limit = a.subscription
                ? (a.subscription.custom_data_limit ?? a.subscription.data_limit ?? a.data_limit)
                : a.data_limit
            const used = a.subscription ? (a.subscription.data_used ?? a.data_used) : a.data_used
            const exhausted = limit > 0 && used >= limit
            switch (statusFilter) {
                case "online": return isAccountOnline(a.last_activity_at)
                case "active": return a.status === "active" && !exhausted
                case "disabled": return a.status === "disabled"
                case "expired": return a.status === "expired" || exhausted
            }
        })
    }, [sortedAccounts, query, statusFilter])

    const pageCount = Math.max(1, Math.ceil(visibleAccounts.length / PAGE_SIZE))
    const safePage = Math.min(page, pageCount)
    const pageAccounts = visibleAccounts.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE)


    const setLoading = (id: number, loading: boolean) => {
        setLoadingStates(prev => ({ ...prev, [id]: loading }))
    }

    const handleCopyLink = async (accountId: number) => {
        try {
            const res = await getAccountLink(accountId)
            if (res.success && res.data?.link) {
                await copyToClipboard(res.data.link)
                toast.success("Link copied to clipboard")
            } else {
                toast.error("Failed to get link")
            }
        } catch {
            toast.error("Failed to copy link")
        }
    }

    const handleShowQR = async (accountId: number, email: string) => {
        try {
            const res = await getAccountLink(accountId)
            if (res.success && res.data?.link) {
                setQrDialog({ open: true, link: res.data.link, email })
            } else {
                toast.error("Failed to get link")
            }
        } catch {
            toast.error("Failed to generate QR code")
        }
    }

    const handleToggle = async (accountId: number, currentlyActive: boolean) => {
        setLoading(accountId, true)
        try {
            if (currentlyActive) {
                await disableAccount(accountId)
                toast.success("Account disabled")
            } else {
                await enableAccount(accountId)
                toast.success("Account enabled")
            }
            queryClient.invalidateQueries({ queryKey: ["accounts", "node", nodeId] })
            onAccountChange?.()
        } catch {
            toast.error(currentlyActive ? "Failed to disable" : "Failed to enable")
        } finally {
            setLoading(accountId, false)
        }
    }

    const handleDelete = async () => {
        if (!deleteDialog.account) return
        const accountId = deleteDialog.account.id
        setLoading(accountId, true)
        setDeleteDialog({ open: false, account: null })
        try {
            await deleteAccount(accountId)
            toast.success("Account deleted")
            queryClient.invalidateQueries({ queryKey: ["accounts", "node", nodeId] })
            onAccountChange?.()
        } catch {
            toast.error("Failed to delete account")
        } finally {
            setLoading(accountId, false)
        }
    }

    if (accounts.length === 0) {
        return (
            <div className="flex flex-col items-center gap-3 py-8 px-4 text-center bg-muted/30 rounded-lg border border-dashed">
                <p className="text-sm text-muted-foreground">No clients on this inbound</p>
                {inboundId !== undefined && (
                    <>
                        <Button variant="outline" size="sm" onClick={() => setCreateOpen(true)}>
                            <HiOutlinePlus className="w-4 h-4 mr-2" />
                            Add Client
                        </Button>
                        <CreateAccountDialog
                            nodeId={nodeId}
                            defaultInboundId={inboundId}
                            open={createOpen}
                            onOpenChange={setCreateOpen}
                            onSuccess={onAccountChange}
                        />
                    </>
                )}
            </div>
        )
    }

    const sortLabels: Record<InboundSortField, string> = {
        client: "Client",
        traffic: "Traffic",
        duration: "Expiry",
        status: "Status",
    }

    return (
        <TooltipProvider delayDuration={300}>
            {/* Toolbar — 93 clients is a list you search, not one you scroll. */}
            <div className="flex flex-wrap items-center gap-2 mb-3">
                <div className="relative flex-1 min-w-[180px] max-w-[280px]">
                    <HiOutlineSearch className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
                    <Input
                        value={query}
                        onChange={(e) => { setQuery(e.target.value); setPage(1) }}
                        placeholder="Search clients"
                        aria-label="Search clients"
                        className="h-8 pl-8 pr-8 text-sm"
                    />
                    {query && (
                        <button
                            type="button"
                            aria-label="Clear search"
                            onClick={() => { setQuery(""); setPage(1) }}
                            className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                        >
                            <HiOutlineX className="w-3.5 h-3.5" />
                        </button>
                    )}
                </div>

                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <Button variant="outline" size="sm" className="h-8 gap-1.5">
                            Status
                            <span className="text-muted-foreground">
                                {STATUS_FILTERS.find(f => f.value === statusFilter)?.label}
                            </span>
                        </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start">
                        {STATUS_FILTERS.map(f => (
                            <DropdownMenuItem key={f.value} onClick={() => { setStatusFilter(f.value); setPage(1) }}>
                                {f.label}
                            </DropdownMenuItem>
                        ))}
                    </DropdownMenuContent>
                </DropdownMenu>

                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <Button variant="outline" size="sm" className="h-8 gap-1.5 md:hidden">
                            Sort
                            <span className="text-muted-foreground">{sortLabels[sortField]}</span>
                        </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start">
                        {(Object.keys(sortLabels) as InboundSortField[]).map(f => (
                            <DropdownMenuItem key={f} onClick={() => toggleSort(f)}>
                                {sortLabels[f]}
                            </DropdownMenuItem>
                        ))}
                    </DropdownMenuContent>
                </DropdownMenu>

                <div className="flex-1" />

                {inboundId !== undefined && (
                    <Button variant="outline" size="sm" className="h-8" onClick={() => setCreateOpen(true)}>
                        <HiOutlinePlus className="w-4 h-4 mr-1.5" />
                        Client
                    </Button>
                )}
                <Button variant="ghost" size="sm" className="h-8 text-muted-foreground hover:text-foreground" asChild>
                    <Link to={`/nodes/${nodeId}?tab=users`}>
                        Open in Accounts
                        <HiOutlineExternalLink className="w-3.5 h-3.5 ml-1.5" />
                    </Link>
                </Button>
            </div>

            {visibleAccounts.length === 0 && (
                <div className="py-8 text-center text-sm text-muted-foreground bg-muted/30 rounded-lg border border-dashed">
                    No clients match this filter
                </div>
            )}

            {/* Mobile card view */}
            <div className={cn("md:hidden divide-y divide-border/50 rounded-lg border bg-card/50 overflow-hidden", visibleAccounts.length === 0 && "hidden")}>
                {pageAccounts.map((account) => {
                    const isShared = !!account.subscription
                    const dataLimit = isShared
                        ? (account.subscription?.custom_data_limit ?? account.subscription?.data_limit ?? account.data_limit)
                        : account.data_limit
                    const dataUsed = isShared
                        ? (account.subscription?.data_used ?? account.data_used)
                        : account.data_used
                    const isUnlimitedMobile = dataLimit === 0
                    const usagePercent = dataLimit > 0
                        ? Math.min(100, (dataUsed / dataLimit) * 100)
                        : 0
                    const relativePercentMobile = isUnlimitedMobile && maxUnlimitedUsage > 0 ? Math.max(2, (dataUsed / maxUnlimitedUsage) * 100) : 0
                    const isOnlineNow = isAccountOnline(account.last_activity_at)
                    const expiryDate = account.subscription?.custom_end_date ?? account.subscription?.end_date ?? account.expires_at
                    const expiryInfo = getExpiryInfo(expiryDate)
                    const user = account.subscription?.user
                    const userName = user?.username
                        ? `@${user.username}`
                        : user?.first_name || null
                    const isLoading = loadingStates[account.id] || false
                    const isActive = account.status === "active"

                    return (
                        <div
                            key={account.id}
                            className="px-3 py-2.5 flex flex-col gap-1.5 cursor-pointer"
                            onClick={() => openAccountDetails(account as any)}
                        >
                            {/* Line 1: Switch + online dot + name + 3-dot menu */}
                            <div className="flex items-center gap-2">
                                <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
                                    <Switch
                                        checked={isActive}
                                        onCheckedChange={() => handleToggle(account.id, isActive)}
                                        disabled={isLoading || account.status === "expired"}
                                        className="data-[state=checked]:bg-emerald-500 scale-[0.8]"
                                    />
                                </div>
                                {isOnlineNow && (
                                    <span className="relative flex h-2 w-2 shrink-0">
                                        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />
                                        <span className="relative inline-flex rounded-full h-2 w-2 bg-green-500" />
                                    </span>
                                )}
                                <span className="flex-1 min-w-0 text-sm font-medium truncate">
                                    {userName || (account.source === "manual" ? account.email : "Unknown")}
                                </span>
                                <div onClick={(e) => e.stopPropagation()}>
                                    <DropdownMenu>
                                        <DropdownMenuTrigger asChild>
                                            <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0">
                                                <HiDotsVertical className="w-4 h-4" />
                                            </Button>
                                        </DropdownMenuTrigger>
                                        <DropdownMenuContent align="end">
                                            <DropdownMenuItem
                                                onClick={() => handleCopyLink(account.id)}
                                                disabled={!isOnline}
                                            >
                                                <HiOutlineClipboard className="w-4 h-4 mr-2" />
                                                Copy Link
                                            </DropdownMenuItem>
                                            <DropdownMenuItem
                                                onClick={() => handleShowQR(account.id, account.email)}
                                                disabled={!isOnline}
                                            >
                                                <HiOutlineQrcode className="w-4 h-4 mr-2" />
                                                QR Code
                                            </DropdownMenuItem>
                                            <DropdownMenuSeparator />
                                            <DropdownMenuItem
                                                onClick={() => setMigrateDialog({ open: true, account })}
                                                disabled={!isOnline}
                                            >
                                                <HiArrowRightOnRectangle className="w-4 h-4 mr-2" />
                                                Migrate
                                            </DropdownMenuItem>
                                            <DropdownMenuSeparator />
                                            <DropdownMenuItem
                                                onClick={() => setDeleteDialog({ open: true, account })}
                                                className="text-red-500 focus:text-red-500"
                                            >
                                                <HiOutlineTrash className="w-4 h-4 mr-2" />
                                                Delete
                                            </DropdownMenuItem>
                                        </DropdownMenuContent>
                                    </DropdownMenu>
                                </div>
                            </div>

                            {/* Line 2: Traffic bar + duration */}
                            <div className="flex items-center gap-2 ml-[36px]">
                                <div className="flex-1 min-w-0">
                                    <div className="flex justify-between text-[10px] mb-0.5">
                                        <span className="font-mono">{formatBytes(dataUsed)}</span>
                                        <span className="text-muted-foreground font-mono">
                                            {isUnlimitedMobile ? "Unlimited" : formatDataLimit(dataLimit)}
                                        </span>
                                    </div>
                                    <div className="h-1.5 bg-muted rounded-full overflow-hidden">
                                        <div
                                            className={cn(
                                                "h-full rounded-full transition-all",
                                                isUnlimitedMobile ? "bg-emerald-500" :
                                                    usagePercent >= 90 ? "bg-red-500" :
                                                        usagePercent >= 70 ? "bg-amber-500" : "bg-emerald-500"
                                            )}
                                            style={{ width: isUnlimitedMobile ? `${relativePercentMobile}%` : `${usagePercent}%` }}
                                        />
                                    </div>
                                    {!isUnlimitedMobile && usagePercent > 0 && (
                                        <p className="text-[9px] text-muted-foreground mt-0.5">{usagePercent.toFixed(1)}% used</p>
                                    )}
                                </div>
                                <Badge
                                    variant={expiryInfo.variant}
                                    className={cn(
                                        "text-[10px] px-1.5 py-0 font-medium shrink-0",
                                        expiryInfo.variant === "secondary" && "bg-blue-900 text-blue-100"
                                    )}
                                >
                                    {expiryInfo.text}
                                </Badge>
                            </div>
                        </div>
                    )
                })}
            </div>

            {/* Desktop table view */}
            <div className={cn("hidden md:block rounded-lg border bg-card/50 overflow-hidden", visibleAccounts.length === 0 && "md:hidden")}>
                <Table>
                    <TableHeader>
                        <TableRow className="bg-muted/50 hover:bg-muted/50">
                            <TableHead className="w-[50px] text-xs font-medium">
                                <SortableHeader field="status" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                    Enabled
                                </SortableHeader>
                            </TableHead>
                            <TableHead className="w-[280px] text-xs font-medium">
                                <SortableHeader field="client" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                    Client
                                </SortableHeader>
                            </TableHead>
                            <TableHead className="text-xs font-medium">
                                <SortableHeader field="traffic" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                    Traffic
                                </SortableHeader>
                            </TableHead>
                            <TableHead className="w-[80px] text-xs font-medium text-center">
                                <SortableHeader field="duration" currentSort={sortField} currentDir={sortDir} onSort={toggleSort}>
                                    Duration
                                </SortableHeader>
                            </TableHead>
                            <TableHead className="w-[100px] text-xs font-medium text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {pageAccounts.map((account) => {
                            // Data usage
                            const isShared = !!account.subscription
                            const dataLimit = isShared
                                ? (account.subscription?.custom_data_limit ?? account.subscription?.data_limit ?? account.data_limit)
                                : account.data_limit
                            const dataUsed = isShared
                                ? (account.subscription?.data_used ?? account.data_used)
                                : account.data_used

                            // Online status
                            const isOnlineNow = isAccountOnline(account.last_activity_at)

                            // Expiry - prefer subscription custom_end_date, then end_date, finally account expires_at
                            const expiryDate = account.subscription?.custom_end_date ?? account.subscription?.end_date ?? account.expires_at
                            const expiryInfo = getExpiryInfo(expiryDate)

                            // User info
                            const user = account.subscription?.user
                            const userName = user?.username
                                ? `@${user.username}`
                                : user?.first_name || null

                            // Loading state
                            const isLoading = loadingStates[account.id] || false
                            const isActive = account.status === "active"
                            const isExhausted = dataLimit > 0 && dataUsed >= dataLimit
                            const isDisabled = account.status === "disabled"

                            return (
                                <TableRow
                                    key={account.id}
                                    className="group hover:bg-muted/30 transition-colors cursor-pointer"
                                    onClick={() => openAccountDetails(account as any)}
                                >
                                    {/* Enabled Toggle */}
                                    <TableCell className="py-2" onClick={(e) => e.stopPropagation()}>
                                        <Switch
                                            checked={isActive}
                                            onCheckedChange={() => handleToggle(account.id, isActive)}
                                            disabled={isLoading || account.status === "expired"}
                                            className="data-[state=checked]:bg-emerald-500"
                                        />
                                    </TableCell>

                                    {/* Client - User-first design with online indicator */}
                                    <TableCell className="py-2">
                                        <div className="flex items-start gap-2">
                                            {/* Online indicator */}
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <span className="relative flex h-2.5 w-2.5 mt-1.5 shrink-0">
                                                        {isOnlineNow && (
                                                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />
                                                        )}
                                                        <span className={cn(
                                                            "relative inline-flex rounded-full h-2.5 w-2.5",
                                                            isOnlineNow ? "bg-green-500" : "bg-slate-500/40"
                                                        )} />
                                                    </span>
                                                </TooltipTrigger>
                                                <TooltipContent>
                                                    {isOnlineNow ? "Online" : account.last_activity_at
                                                        ? `Last seen: ${new Date(account.last_activity_at).toLocaleString()}`
                                                        : "Never connected"
                                                    }
                                                </TooltipContent>
                                            </Tooltip>

                                            <div className="flex flex-col min-w-0">
                                                {/* Primary: User name or email */}
                                                <div className="flex items-center gap-1.5">
                                                    {userName ? (
                                                        <Link
                                                            to={`/users/${user?.id}`}
                                                            onClick={(e) => e.stopPropagation()}
                                                            className="font-medium text-sm hover:text-primary hover:underline transition-colors"
                                                        >
                                                            {userName}
                                                        </Link>
                                                    ) : (
                                                        <span className="font-medium text-sm">
                                                            {account.source === "manual" ? account.email : "Unknown"}
                                                        </span>
                                                    )}
                                                    {isOnlineNow && (
                                                        <span className="text-[9px] font-semibold uppercase tracking-wider text-green-500">
                                                            online
                                                        </span>
                                                    )}
                                                    {isExhausted && (
                                                        <Badge variant="danger" className="text-[9px] px-1.5 py-0 font-medium">
                                                            Exhausted
                                                        </Badge>
                                                    )}
                                                    {isDisabled && !isExhausted && (
                                                        <Badge variant="warning" className="text-[9px] px-1.5 py-0 font-medium">
                                                            Disabled
                                                        </Badge>
                                                    )}
                                                </div>

                                                {/* Secondary: Subscription link + truncated email */}
                                                <div className="flex items-center gap-1 text-xs text-muted-foreground">
                                                    {account.subscription_id && account.subscription ? (
                                                        <>
                                                            <button
                                                                onClick={(e) => {
                                                                    e.stopPropagation()
                                                                    openSubscriptionSheet(account.subscription!)
                                                                }}
                                                                className="hover:text-primary hover:underline transition-colors cursor-pointer"
                                                            >
                                                                Sub #{account.subscription_id}
                                                            </button>
                                                            <span className="text-muted-foreground/50">·</span>
                                                        </>
                                                    ) : (
                                                        <>
                                                            <span className="text-muted-foreground/70">Manual</span>
                                                            <span className="text-muted-foreground/50">·</span>
                                                        </>
                                                    )}
                                                    <span className="font-mono truncate max-w-[140px]" title={account.email}>
                                                        {account.email.length > 18
                                                            ? `${account.email.slice(0, 8)}...${account.email.slice(-6)}`
                                                            : account.email
                                                        }
                                                    </span>
                                                </div>
                                            </div>
                                        </div>
                                    </TableCell>

                                    {/* Traffic with DataUsageBar */}
                                    <TableCell className="py-2">
                                        <DataUsageBar
                                            used={dataUsed}
                                            limit={dataLimit}
                                            maxUsage={maxUnlimitedUsage}
                                            isDisabled={isDisabled || account.status === "expired"}
                                        />
                                    </TableCell>

                                    {/* Duration - Muted colors for normal */}
                                    <TableCell className="py-2 text-center">
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <Badge
                                                    variant={expiryInfo.variant}
                                                    className={cn(
                                                        "text-xs px-2 py-0.5 font-medium cursor-help",
                                                        expiryInfo.variant === "secondary" && "bg-blue-900 text-blue-100"
                                                    )}
                                                >
                                                    {expiryInfo.text}
                                                </Badge>
                                            </TooltipTrigger>
                                            <TooltipContent>
                                                {expiryDate ? new Date(expiryDate).toLocaleString() : "Unlimited duration"}
                                            </TooltipContent>
                                        </Tooltip>
                                    </TableCell>

                                    {/* Actions - Copy, QR, More (⋮) */}
                                    <TableCell className="py-2" onClick={(e) => e.stopPropagation()}>
                                        <div className="flex items-center justify-end gap-0.5">
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-7 w-7"
                                                        onClick={(e) => {
                                                            e.stopPropagation()
                                                            handleCopyLink(account.id)
                                                        }}
                                                        disabled={!isOnline}
                                                    >
                                                        <HiOutlineClipboard className="w-4 h-4" />
                                                    </Button>
                                                </TooltipTrigger>
                                                <TooltipContent>Copy Link</TooltipContent>
                                            </Tooltip>

                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-7 w-7"
                                                        onClick={(e) => {
                                                            e.stopPropagation()
                                                            handleShowQR(account.id, account.email)
                                                        }}
                                                        disabled={!isOnline}
                                                    >
                                                        <HiOutlineQrcode className="w-4 h-4" />
                                                    </Button>
                                                </TooltipTrigger>
                                                <TooltipContent>QR Code</TooltipContent>
                                            </Tooltip>

                                            {/* More menu with Delete */}
                                            <DropdownMenu>
                                                <DropdownMenuTrigger asChild>
                                                    <Button variant="ghost" size="icon" className="h-7 w-7">
                                                        <HiDotsVertical className="w-4 h-4" />
                                                    </Button>
                                                </DropdownMenuTrigger>
                                                <DropdownMenuContent align="end">
                                                    <DropdownMenuItem
                                                        onClick={(e) => {
                                                            e.stopPropagation()
                                                            handleCopyLink(account.id)
                                                        }}
                                                        disabled={!isOnline}
                                                    >
                                                        <HiOutlineClipboard className="w-4 h-4 mr-2" />
                                                        Copy Link
                                                    </DropdownMenuItem>
                                                    <DropdownMenuItem
                                                        onClick={(e) => {
                                                            e.stopPropagation()
                                                            handleShowQR(account.id, account.email)
                                                        }}
                                                        disabled={!isOnline}
                                                    >
                                                        <HiOutlineQrcode className="w-4 h-4 mr-2" />
                                                        Show QR Code
                                                    </DropdownMenuItem>
                                                    <DropdownMenuSeparator />

                                                    <DropdownMenuItem
                                                        onClick={(e) => {
                                                            e.stopPropagation()
                                                            setMigrateDialog({ open: true, account })
                                                        }}
                                                        disabled={!isOnline}
                                                    >
                                                        <HiArrowRightOnRectangle className="w-4 h-4 mr-2" />
                                                        Migrate Account
                                                    </DropdownMenuItem>
                                                    <DropdownMenuSeparator />
                                                    <DropdownMenuItem
                                                        onClick={(e) => {
                                                            e.stopPropagation()
                                                            setDeleteDialog({ open: true, account })
                                                        }}
                                                        className="text-red-500 focus:text-red-500"
                                                    >
                                                        <HiOutlineTrash className="w-4 h-4 mr-2" />
                                                        Delete Account
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



            {visibleAccounts.length > 0 && (
                <div className="flex items-center justify-between gap-3 pt-3 text-xs text-muted-foreground">
                    <span>
                        Showing{" "}
                        <span className="text-foreground tabular-nums">
                            {(safePage - 1) * PAGE_SIZE + 1}–{Math.min(safePage * PAGE_SIZE, visibleAccounts.length)}
                        </span>{" "}
                        of <span className="text-foreground tabular-nums">{visibleAccounts.length}</span>
                    </span>
                    {pageCount > 1 && (
                        <div className="flex items-center gap-1">
                            <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label="Previous page"
                                disabled={safePage <= 1}
                                onClick={() => setPage(p => Math.max(1, p - 1))}
                            >
                                <HiOutlineChevronLeft className="w-4 h-4" />
                            </Button>
                            <span className="px-2 tabular-nums">Page {safePage} of {pageCount}</span>
                            <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label="Next page"
                                disabled={safePage >= pageCount}
                                onClick={() => setPage(p => Math.min(pageCount, p + 1))}
                            >
                                <HiOutlineChevronRight className="w-4 h-4" />
                            </Button>
                        </div>
                    )}
                </div>
            )}

            {inboundId !== undefined && (
                <CreateAccountDialog
                    nodeId={nodeId}
                    defaultInboundId={inboundId}
                    open={createOpen}
                    onOpenChange={setCreateOpen}
                    onSuccess={onAccountChange}
                />
            )}

            {/* Migrate Dialog */}
            <AccountMigrationDialog
                open={migrateDialog.open}
                onOpenChange={(open) => setMigrateDialog({ ...migrateDialog, open })}
                account={migrateDialog.account}
                sourceNodeId={nodeId}
                onSuccess={() => {
                    onAccountChange?.()
                }}
            />

            {/* QR Code Dialog */}
            <Dialog open={qrDialog.open} onOpenChange={(open) => setQrDialog({ ...qrDialog, open })}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle className="text-center">QR Code</DialogTitle>
                        <DialogDescription className="text-center">{qrDialog.email}</DialogDescription>
                    </DialogHeader>
                    <div className="flex flex-col items-center gap-4 p-4">
                        <div className="bg-white p-4 rounded-lg">
                            <QRCodeSVG
                                value={qrDialog.link}
                                size={200}
                                level="M"
                                includeMargin={false}
                            />
                        </div>
                        <Button
                            variant="outline"
                            className="w-full"
                            onClick={async () => {
                                await copyToClipboard(qrDialog.link)
                                toast.success("Link copied")
                            }}
                        >
                            <HiOutlineClipboard className="w-4 h-4 mr-2" />
                            Copy Link
                        </Button>
                    </div>
                </DialogContent>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <Dialog open={deleteDialog.open} onOpenChange={(open) => setDeleteDialog({ ...deleteDialog, open })}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle>Delete Account</DialogTitle>
                        <DialogDescription>
                            Are you sure you want to delete <span className="font-mono font-medium">{deleteDialog.account?.email}</span>?
                            This action cannot be undone.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter className="gap-2 sm:gap-0">
                        <Button variant="outline" onClick={() => setDeleteDialog({ open: false, account: null })}>
                            Cancel
                        </Button>
                        <Button variant="destructive" onClick={handleDelete}>
                            Delete
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Subscription Details Sheet */}
            <SubscriptionDetailsSheet />
        </TooltipProvider>
    )
}
