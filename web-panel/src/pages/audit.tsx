import { useState, useMemo } from "react"
import { PageHeader } from "@/components/shared/page-header"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { formatRelativeTime } from "@/lib/utils"
import { HiOutlineClipboardList, HiOutlineX, HiOutlineUsers, HiOutlineServer, HiOutlineCog } from "react-icons/hi"
import { Activity } from "lucide-react"
import { useAuditLogs } from "@/lib/queries"
import { PaginationNav } from "@/components/ui/pagination-nav"
import { EmptyState } from "@/components/ui/empty-state"
import type { AuditListParams } from "@/lib/api/audit"

const ENTITY_TYPES = [
    { value: "user", label: "User" },
    { value: "plan", label: "Plan" },
    { value: "subscription", label: "Subscription" },
    { value: "payment", label: "Payment" },
    { value: "node", label: "Node" },
    { value: "settings", label: "Settings" },
]

const PER_PAGE = 20

function TableSkeleton() {
    return (
        <>
            {[1, 2, 3, 4, 5].map((i) => (
                <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-28" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                </TableRow>
            ))}
        </>
    )
}

export default function AuditPage() {
    const [page, setPage] = useState(1)
    const [entityType, setEntityType] = useState<string>("")
    const [action, setAction] = useState("")

    const params: AuditListParams = {
        offset: (page - 1) * PER_PAGE,
        limit: PER_PAGE,
    }
    if (entityType) params.entity_type = entityType
    if (action) params.action = action

    const { data, isLoading } = useAuditLogs(params)

    const logs = data?.data || []
    const total = data?.meta?.total || 0
    const totalPages = Math.ceil(total / PER_PAGE)
    const hasNextPage = page < totalPages

    const hasFilters = entityType || action

    const clearFilters = () => {
        setEntityType("")
        setAction("")
        setPage(1)
    }

    // Stats summary from current data
    const entityBreakdown = useMemo(() => {
        if (!logs.length) return {}
        return logs.reduce((acc, log) => {
            acc[log.entity_type] = (acc[log.entity_type] || 0) + 1
            return acc
        }, {} as Record<string, number>)
    }, [logs])

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            {/* Header */}
            <PageHeader
                title="Audit Log"
                description="Track administrative actions and system changes"
            />

            {/* Summary Stats */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 md:gap-4 lg:gap-5">
                <div className="rounded-lg border border-border/50 bg-card/50 p-3 md:p-4 lg:p-5">
                    <div className="flex items-center gap-2 text-muted-foreground mb-1 md:mb-2 text-[10px] sm:text-[11px] md:text-xs">
                        <Activity className="w-3.5 h-3.5 md:w-4 md:h-4" />
                        <span className="uppercase tracking-wider font-medium">Total Events</span>
                    </div>
                    <p className="text-lg md:text-xl lg:text-2xl font-bold">{total}</p>
                </div>
                {Object.entries(entityBreakdown).slice(0, 3).map(([type, count]) => (
                    <div key={type} className="rounded-lg border border-border/50 bg-card/50 p-3 md:p-4 lg:p-5">
                        <div className="flex items-center gap-2 text-muted-foreground mb-1 md:mb-2 text-[10px] sm:text-[11px] md:text-xs">
                            <span className="uppercase tracking-wider font-medium">{type}</span>
                        </div>
                        <p className="text-lg md:text-xl lg:text-2xl font-bold">{count}</p>
                    </div>
                ))}
            </div>

            {/* Filters */}
            <div className="flex flex-wrap items-end gap-3">
                <div className="space-y-1">
                    <Label className="text-xs text-muted-foreground">Entity Type</Label>
                    <Select
                        value={entityType}
                        onValueChange={(v) => { setEntityType(v === "all" ? "" : v); setPage(1) }}
                    >
                        <SelectTrigger className="h-9 w-[160px] text-xs">
                            <SelectValue placeholder="All entities" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="all">All entities</SelectItem>
                            {ENTITY_TYPES.map((et) => (
                                <SelectItem key={et.value} value={et.value}>
                                    {et.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>
                <div className="space-y-1">
                    <Label className="text-xs text-muted-foreground">Action</Label>
                    <Input
                        type="text"
                        placeholder="Filter by action..."
                        value={action}
                        onChange={(e) => { setAction(e.target.value); setPage(1) }}
                        className="h-9 w-[180px] text-xs"
                    />
                </div>
                {hasFilters && (
                    <Button
                        variant="ghost"
                        size="sm"
                        onClick={clearFilters}
                        className="h-9 text-xs text-muted-foreground"
                    >
                        <HiOutlineX className="w-3.5 h-3.5 mr-1" />
                        Clear
                    </Button>
                )}
            </div>

            {/* Desktop Table */}
            <Card className="hidden md:block">
                <CardHeader>
                    <CardTitle>Activity History</CardTitle>
                    <CardDescription>
                        {total > 0 ? `${total} total entries` : "All recorded administrative actions"}
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Time</TableHead>
                                <TableHead>Actor</TableHead>
                                <TableHead>Action</TableHead>
                                <TableHead>Entity</TableHead>
                                <TableHead>IP</TableHead>
                                <TableHead>Source</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {isLoading ? (
                                <TableSkeleton />
                            ) : logs.length > 0 ? (
                                logs.map((log) => (
                                    <TableRow key={log.id}>
                                        <TableCell className="text-sm text-muted-foreground whitespace-nowrap" title={new Date(log.created_at).toLocaleString()}>
                                            {formatRelativeTime(log.created_at)}
                                        </TableCell>
                                        <TableCell className="font-medium">
                                            {log.actor_name || `User #${log.actor_id}`}
                                        </TableCell>
                                        <TableCell>
                                            <code className="text-xs bg-muted px-1.5 py-0.5 rounded">
                                                {log.action}
                                            </code>
                                        </TableCell>
                                        <TableCell className="text-sm">
                                            <span className="capitalize">{log.entity_type}</span>
                                            {log.entity_id > 0 && (
                                                <span className="text-muted-foreground ml-1">#{log.entity_id}</span>
                                            )}
                                        </TableCell>
                                        <TableCell className="text-sm text-muted-foreground font-mono">
                                            {log.ip_address || "-"}
                                        </TableCell>
                                        <TableCell className="text-sm text-muted-foreground capitalize">
                                            {log.source || "-"}
                                        </TableCell>
                                    </TableRow>
                                ))
                            ) : (
                                <TableRow>
                                    <TableCell colSpan={6}>
                                        <EmptyState
                                            icon={HiOutlineClipboardList}
                                            title="No audit logs found"
                                            description={hasFilters ? "Try adjusting your filters" : "Actions will appear here once recorded"}
                                        />
                                    </TableCell>
                                </TableRow>
                            )}
                        </TableBody>
                    </Table>

                    {logs.length > 0 && (
                        <PaginationNav
                            page={page}
                            hasNextPage={hasNextPage}
                            totalPages={totalPages > 0 ? totalPages : undefined}
                            onPageChange={setPage}
                            showingCount={logs.length}
                            className="mt-4 border-t pt-4"
                        />
                    )}
                </CardContent>
            </Card>

            {/* Mobile Card View */}
            <div className="md:hidden space-y-3">
                {isLoading ? (
                    <div className="space-y-3">
                        {[1, 2, 3].map((i) => <Skeleton key={i} className="h-28 w-full rounded-xl" />)}
                    </div>
                ) : logs.length > 0 ? (
                    logs.map((log) => (
                        <Card key={log.id}>
                            <CardContent className="p-4 space-y-2">
                                <div className="flex items-start justify-between">
                                    <div>
                                        <p className="font-medium text-sm">
                                            {log.actor_name || `User #${log.actor_id}`}
                                        </p>
                                        <p className="text-xs text-muted-foreground" title={new Date(log.created_at).toLocaleString()}>
                                            {formatRelativeTime(log.created_at)}
                                        </p>
                                    </div>
                                    <code className="text-xs bg-muted px-1.5 py-0.5 rounded">
                                        {log.action}
                                    </code>
                                </div>
                                <div className="grid grid-cols-2 gap-2 text-xs pt-1">
                                    <div>
                                        <span className="text-muted-foreground">Entity: </span>
                                        <span className="capitalize">{log.entity_type}</span>
                                        {log.entity_id > 0 && <span className="text-muted-foreground"> #{log.entity_id}</span>}
                                    </div>
                                    <div>
                                        <span className="text-muted-foreground">IP: </span>
                                        <span className="font-mono">{log.ip_address || "-"}</span>
                                    </div>
                                    <div>
                                        <span className="text-muted-foreground">Source: </span>
                                        <span className="capitalize">{log.source || "-"}</span>
                                    </div>
                                </div>
                            </CardContent>
                        </Card>
                    ))
                ) : (
                    <EmptyState
                        icon={HiOutlineClipboardList}
                        title="No audit logs found"
                        description={hasFilters ? "Try adjusting your filters" : "Actions will appear here once recorded"}
                    />
                )}

                {logs.length > 0 && (
                    <PaginationNav
                        page={page}
                        hasNextPage={hasNextPage}
                        totalPages={totalPages > 0 ? totalPages : undefined}
                        onPageChange={setPage}
                        showingCount={logs.length}
                    />
                )}
            </div>
        </div>
    )
}
