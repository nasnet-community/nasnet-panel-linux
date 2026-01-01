import { useState, useMemo, useCallback } from "react"
import {
    useReactTable,
    getCoreRowModel,
    getSortedRowModel,
    getFilteredRowModel,
    getPaginationRowModel,
    createColumnHelper,
    flexRender,
    type SortingState,
    type ColumnFiltersState,
    type VisibilityState,
} from "@tanstack/react-table"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import {
    HiOutlineRefresh,
    HiOutlineServer,
    HiOutlineExclamationCircle,
    HiOutlineCheck,
    HiOutlineChevronLeft,
    HiOutlineChevronRight,
} from "react-icons/hi"
import { Search, Copy, Download, Columns3, ArrowUpDown, ArrowUp, ArrowDown, Pause, Play, X, Globe } from "lucide-react"
import { useAggregatedAccessLogs, useNodes } from "@/lib/queries"
import { PageHeader } from "@/components/shared/page-header"
import { cn } from "@/lib/utils"
import { toast } from "sonner"
import { formatDistanceToNowStrict } from "date-fns"
import { SubscriptionAutocomplete } from "@/components/ui/subscription-autocomplete"
import type { AggregatedAccessLogEntry, Node } from "@/lib/types"

// --- Column Helper ---
const columnHelper = createColumnHelper<AggregatedAccessLogEntry>()

function RelativeTime({ ts }: { ts: number }) {
    const d = new Date(ts * 1000)
    const abs = d.toLocaleString("en-US", {
        month: "short", day: "numeric",
        hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
    })
    let relative: string
    try {
        relative = formatDistanceToNowStrict(d, { addSuffix: true })
    } catch {
        relative = abs
    }
    return <span title={abs} className="cursor-default">{relative}</span>
}

const FLAG_FALLBACK: Record<string, string> = {}
function countryFlag(code: string) {
    if (!code) return ""
    const upper = code.toUpperCase()
    if (FLAG_FALLBACK[upper]) return FLAG_FALLBACK[upper]
    return String.fromCodePoint(...[...upper].map(c => 0x1F1E6 + c.charCodeAt(0) - 65))
}

// --- Page ---
export default function AccessLogsPage() {
    // --- State ---
    const [selectedNodeIds, setSelectedNodeIds] = useState<number[]>([])
    const [emailFilter, setEmailFilter] = useState("")
    const [debouncedEmail, setDebouncedEmail] = useState("")
    const [limit, setLimit] = useState(500)
    const [refreshInterval, setRefreshInterval] = useState(10_000)
    const [sorting, setSorting] = useState<SortingState>([{ id: "timestamp", desc: true }])
    const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
    const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({
        source_ip: false,
        inbound_tag: false,
        outbound_tag: false,
    })
    const [globalFilter, setGlobalFilter] = useState("")
    const [nodePopoverOpen, setNodePopoverOpen] = useState(false)

    // Debounce email
    const emailTimeout = useMemo(() => {
        let t: ReturnType<typeof setTimeout>
        return (v: string) => {
            clearTimeout(t)
            t = setTimeout(() => setDebouncedEmail(v), 400)
        }
    }, [])
    const handleEmailChange = useCallback((v: string) => {
        setEmailFilter(v)
        emailTimeout(v)
    }, [emailTimeout])

    // --- Data ---
    const { data: nodes = [] } = useNodes()
    const accessLogNodes = useMemo(() => nodes.filter((n: Node) => n.enable_access_log && n.is_online), [nodes])

    const { data: entries = [], isLoading, isFetching, refetch } = useAggregatedAccessLogs({
        nodeIds: selectedNodeIds,
        email: debouncedEmail || undefined,
        limit,
        refreshInterval,
    })

    // --- Summary stats ---
    const stats = useMemo(() => {
        const accepted = entries.filter(e => e.status === "accepted").length
        const rejected = entries.length - accepted
        const uniqueEmails = new Set(entries.map(e => e.email)).size
        const uniqueDomains = new Set(entries.map(e => e.domain)).size
        return { total: entries.length, accepted, rejected, uniqueEmails, uniqueDomains }
    }, [entries])

    // Unique emails from loaded entries for autocomplete
    const localEmails = useMemo(() => [...new Set(entries.map(e => e.email).filter(Boolean))], [entries])

    // --- Columns ---
    const columns = useMemo(() => [
        columnHelper.accessor("timestamp", {
            header: ({ column }) => (
                <SortButton column={column}>Time</SortButton>
            ),
            cell: info => (
                <span className="font-mono text-xs text-muted-foreground whitespace-nowrap">
                    <RelativeTime ts={info.getValue()} />
                </span>
            ),
            size: 110,
        }),
        columnHelper.accessor("node_name", {
            id: "node",
            header: ({ column }) => (
                <SortButton column={column}>Node</SortButton>
            ),
            cell: info => {
                const row = info.row.original
                return (
                    <span className="whitespace-nowrap text-sm" title={row.node_name}>
                        {countryFlag(row.node_country)}{" "}
                        <span className="text-muted-foreground">{row.node_name}</span>
                    </span>
                )
            },
            size: 120,
        }),
        columnHelper.accessor("status", {
            header: ({ column }) => (
                <SortButton column={column}>Status</SortButton>
            ),
            cell: info => (
                <Badge
                    variant="outline"
                    className={cn(
                        "text-[11px] font-medium",
                        info.getValue() === "accepted"
                            ? "text-emerald-600 border-emerald-600/30"
                            : "text-red-500 border-red-500/30"
                    )}
                >
                    {info.getValue() === "accepted" ? "ACC" : "REJ"}
                </Badge>
            ),
            filterFn: "equals",
            size: 80,
        }),
        columnHelper.accessor("network", {
            header: "Net",
            cell: info => (
                <span className="text-xs text-muted-foreground uppercase">{info.getValue()}</span>
            ),
            filterFn: "equals",
            size: 60,
        }),
        columnHelper.accessor("email", {
            header: ({ column }) => (
                <SortButton column={column}>Subscription</SortButton>
            ),
            cell: info => (
                <ClickToCopy value={info.getValue()}>
                    <span className="font-mono text-xs truncate max-w-[180px]">{info.getValue()}</span>
                </ClickToCopy>
            ),
            size: 160,
        }),
        columnHelper.accessor("source_ip", {
            header: ({ column }) => (
                <SortButton column={column}>Source IP</SortButton>
            ),
            cell: info => (
                <ClickToCopy value={info.getValue()}>
                    <span className="font-mono text-xs text-muted-foreground">{info.getValue()}</span>
                </ClickToCopy>
            ),
            size: 120,
        }),
        columnHelper.accessor("domain", {
            header: ({ column }) => (
                <SortButton column={column}>Domain</SortButton>
            ),
            cell: info => (
                <ClickToCopy value={info.getValue()}>
                    <span className="font-mono text-xs truncate max-w-[180px]">{info.getValue()}</span>
                </ClickToCopy>
            ),
            size: 200,
        }),
        columnHelper.accessor("port", {
            header: "Port",
            cell: info => (
                <span className="font-mono text-xs text-muted-foreground">{info.getValue()}</span>
            ),
            size: 60,
        }),
        columnHelper.accessor(row => `${row.inbound_tag} → ${row.outbound_tag}`, {
            id: "route",
            header: "Route",
            cell: info => {
                const row = info.row.original
                return (
                    <span className="font-mono text-xs text-muted-foreground whitespace-nowrap truncate max-w-[200px]" title={`${row.inbound_tag} → ${row.outbound_tag}`}>
                        {row.inbound_tag ? `${row.inbound_tag} → ${row.outbound_tag}` : row.outbound_tag}
                    </span>
                )
            },
            size: 160,
        }),
        columnHelper.accessor("inbound_tag", {
            header: "Inbound",
            cell: info => <span className="font-mono text-xs text-muted-foreground">{info.getValue()}</span>,
            size: 100,
        }),
        columnHelper.accessor("outbound_tag", {
            header: "Outbound",
            cell: info => <span className="font-mono text-xs text-muted-foreground">{info.getValue()}</span>,
            size: 100,
        }),
    ], [])

    // --- Table ---
    const table = useReactTable({
        data: entries,
        columns,
        state: { sorting, columnFilters, columnVisibility, globalFilter },
        onSortingChange: setSorting,
        onColumnFiltersChange: setColumnFilters,
        onColumnVisibilityChange: setColumnVisibility,
        onGlobalFilterChange: setGlobalFilter,
        getCoreRowModel: getCoreRowModel(),
        getSortedRowModel: getSortedRowModel(),
        getFilteredRowModel: getFilteredRowModel(),
        getPaginationRowModel: getPaginationRowModel(),
        initialState: {
            pagination: { pageSize: 100 },
        },
    })

    // --- Faceted filter values ---
    const statusValues = useMemo(() => {
        const s = new Set(entries.map(e => e.status))
        return [...s]
    }, [entries])
    const networkValues = useMemo(() => {
        const s = new Set(entries.map(e => e.network))
        return [...s]
    }, [entries])

    const activeStatusFilter = (columnFilters.find(f => f.id === "status")?.value as string) || ""
    const activeNetworkFilter = (columnFilters.find(f => f.id === "network")?.value as string) || ""

    const activeFilterCount = [
        selectedNodeIds.length > 0,
        emailFilter,
        activeStatusFilter,
        activeNetworkFilter,
        globalFilter,
    ].filter(Boolean).length

    const clearFilters = () => {
        setSelectedNodeIds([])
        setEmailFilter("")
        setDebouncedEmail("")
        setColumnFilters([])
        setGlobalFilter("")
    }

    // --- Node multi-select helpers ---
    const toggleNode = (id: number) => {
        setSelectedNodeIds(prev =>
            prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id]
        )
    }

    // --- CSV Export ---
    const exportCSV = () => {
        const rows = table.getFilteredRowModel().rows
        const headers = ["time", "node", "status", "network", "email", "source_ip", "domain", "port", "inbound", "outbound"]
        const csvRows = rows.map(r => {
            const d = r.original
            return [
                new Date(d.timestamp * 1000).toISOString(),
                d.node_name,
                d.status,
                d.network,
                d.email,
                d.source_ip,
                d.domain,
                d.port,
                d.inbound_tag,
                d.outbound_tag,
            ].join(",")
        })
        const csv = [headers.join(","), ...csvRows].join("\n")
        const blob = new Blob([csv], { type: "text/csv" })
        const url = URL.createObjectURL(blob)
        const a = document.createElement("a")
        a.href = url
        a.download = `access-logs-${new Date().toISOString().slice(0, 10)}.csv`
        a.click()
        URL.revokeObjectURL(url)
        toast.success(`Exported ${rows.length} entries`)
    }

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            <PageHeader
                title="Access Logs"
                description="Real-time traffic monitoring across all nodes"
            />

            <div className="space-y-2">
                    {/* Toolbar */}
                    <div className="flex flex-wrap items-center gap-2">
                        {/* Node multi-select */}
                        <Popover open={nodePopoverOpen} onOpenChange={setNodePopoverOpen}>
                            <PopoverTrigger asChild>
                                <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs">
                                    <HiOutlineServer className="w-3.5 h-3.5" />
                                    {selectedNodeIds.length === 0
                                        ? "All Nodes"
                                        : `${selectedNodeIds.length} Node${selectedNodeIds.length > 1 ? "s" : ""}`}
                                </Button>
                            </PopoverTrigger>
                            <PopoverContent className="w-56 p-2" align="start">
                                <div className="space-y-1 max-h-60 overflow-y-auto">
                                    {accessLogNodes.length === 0 ? (
                                        <p className="text-xs text-muted-foreground p-2">No nodes with access logs enabled</p>
                                    ) : (
                                        accessLogNodes.map((n: Node) => (
                                            <button
                                                key={n.id}
                                                onClick={() => toggleNode(n.id)}
                                                className={cn(
                                                    "w-full flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors",
                                                    selectedNodeIds.includes(n.id)
                                                        ? "bg-primary/10 text-primary"
                                                        : "hover:bg-muted"
                                                )}
                                            >
                                                <div className={cn(
                                                    "w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0",
                                                    selectedNodeIds.includes(n.id)
                                                        ? "bg-primary border-primary"
                                                        : "border-muted-foreground/30"
                                                )}>
                                                    {selectedNodeIds.includes(n.id) && (
                                                        <HiOutlineCheck className="w-2.5 h-2.5 text-primary-foreground" />
                                                    )}
                                                </div>
                                                <span className="text-xs">{countryFlag(n.country_code)} {n.name}</span>
                                            </button>
                                        ))
                                    )}
                                </div>
                                {selectedNodeIds.length > 0 && (
                                    <Button variant="ghost" size="sm" className="w-full mt-1 h-7 text-xs" onClick={() => setSelectedNodeIds([])}>
                                        Clear selection
                                    </Button>
                                )}
                            </PopoverContent>
                        </Popover>

                        {/* Subscription / email filter */}
                        <SubscriptionAutocomplete
                            value={emailFilter}
                            onChange={handleEmailChange}
                            onSelect={(email) => { setEmailFilter(email); setDebouncedEmail(email) }}
                            localEmails={localEmails}
                            placeholder="Subscription..."
                            className="h-8 w-[200px] text-xs"
                        />

                        {/* Status */}
                        <Select
                            value={activeStatusFilter || "__all__"}
                            onValueChange={v => {
                                if (v === "__all__") {
                                    setColumnFilters(prev => prev.filter(f => f.id !== "status"))
                                } else {
                                    setColumnFilters(prev => [
                                        ...prev.filter(f => f.id !== "status"),
                                        { id: "status", value: v },
                                    ])
                                }
                            }}
                        >
                            <SelectTrigger className="h-8 w-[110px] text-xs">
                                <SelectValue placeholder="Status" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="__all__">All Status</SelectItem>
                                {statusValues.map(s => (
                                    <SelectItem key={s} value={s}>
                                        {s === "accepted" ? "Accepted" : "Rejected"}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>

                        {/* Network */}
                        <Select
                            value={activeNetworkFilter || "__all__"}
                            onValueChange={v => {
                                if (v === "__all__") {
                                    setColumnFilters(prev => prev.filter(f => f.id !== "network"))
                                } else {
                                    setColumnFilters(prev => [
                                        ...prev.filter(f => f.id !== "network"),
                                        { id: "network", value: v },
                                    ])
                                }
                            }}
                        >
                            <SelectTrigger className="h-8 w-[100px] text-xs">
                                <SelectValue placeholder="Network" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="__all__">All Net</SelectItem>
                                {networkValues.map(n => (
                                    <SelectItem key={n} value={n}>{n.toUpperCase()}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>

                        {/* Global search */}
                        <div className="relative flex-1 min-w-[140px]">
                            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
                            <Input
                                placeholder="Search domain, IP, port..."
                                value={globalFilter}
                                onChange={e => setGlobalFilter(e.target.value)}
                                className="h-8 pl-8 text-xs"
                            />
                        </div>

                        {/* Clear filters */}
                        {activeFilterCount > 0 && (
                            <Button variant="ghost" size="sm" className="h-8 gap-1 text-xs text-muted-foreground" onClick={clearFilters}>
                                <X className="w-3.5 h-3.5" />
                                Clear ({activeFilterCount})
                            </Button>
                        )}

                        {/* Separator */}
                        <div className="h-5 w-px bg-border hidden sm:block" />

                        {/* Limit */}
                        <Select value={limit.toString()} onValueChange={v => setLimit(Number(v))}>
                            <SelectTrigger className="h-8 w-[80px] text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="200">200</SelectItem>
                                <SelectItem value="500">500</SelectItem>
                                <SelectItem value="1000">1000</SelectItem>
                            </SelectContent>
                        </Select>

                        {/* Columns */}
                        <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                                <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs">
                                    <Columns3 className="w-3.5 h-3.5" />
                                    Columns
                                </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" className="w-48">
                                {table.getAllLeafColumns()
                                    .filter(c => c.getCanHide())
                                    .map(column => (
                                        <DropdownMenuCheckboxItem
                                            key={column.id}
                                            checked={column.getIsVisible()}
                                            onCheckedChange={v => column.toggleVisibility(!!v)}
                                            className="capitalize"
                                        >
                                            {column.id === "node" ? "Node" : column.id.replace(/_/g, " ")}
                                        </DropdownMenuCheckboxItem>
                                    ))}
                            </DropdownMenuContent>
                        </DropdownMenu>

                        {/* Export */}
                        <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs" onClick={exportCSV} disabled={entries.length === 0}>
                            <Download className="w-3.5 h-3.5" />
                            Export
                        </Button>

                        {/* Refresh controls */}
                        <Select value={refreshInterval.toString()} onValueChange={v => setRefreshInterval(Number(v))}>
                            <SelectTrigger className="w-[110px] h-8 text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="0">
                                    <span className="flex items-center gap-1.5"><Pause className="w-3 h-3" /> Paused</span>
                                </SelectItem>
                                <SelectItem value="5000">
                                    <span className="flex items-center gap-1.5"><Play className="w-3 h-3" /> 5s</span>
                                </SelectItem>
                                <SelectItem value="10000">
                                    <span className="flex items-center gap-1.5"><Play className="w-3 h-3" /> 10s</span>
                                </SelectItem>
                                <SelectItem value="30000">
                                    <span className="flex items-center gap-1.5"><Play className="w-3 h-3" /> 30s</span>
                                </SelectItem>
                            </SelectContent>
                        </Select>
                        <Button variant="outline" size="sm" className="h-8" onClick={() => refetch()} disabled={isFetching}>
                            <HiOutlineRefresh className={cn("w-3.5 h-3.5", isFetching && "animate-spin")} />
                        </Button>
                    </div>

                    {/* Compact stats bar */}
                    {entries.length > 0 && (
                        <div className="flex items-center gap-3 text-xs px-0.5">
                            <StatBadge label="Total" value={stats.total} />
                            <span className="text-border">|</span>
                            <StatBadge label="Accepted" value={stats.accepted} className="text-emerald-600" />
                            <StatBadge label="Rejected" value={stats.rejected} className="text-red-500" />
                            <span className="text-border">|</span>
                            <StatBadge label="Users" value={stats.uniqueEmails} />
                            <StatBadge label="Domains" value={stats.uniqueDomains} />
                            {isFetching && <span className="text-muted-foreground animate-pulse ml-1">Refreshing...</span>}
                        </div>
                    )}

                    {/* Data Table */}
                    <div className="rounded-lg border">
                        <Table>
                            <TableHeader>
                                {table.getHeaderGroups().map(headerGroup => (
                                    <TableRow key={headerGroup.id}>
                                        {headerGroup.headers.map(header => (
                                            <TableHead
                                                key={header.id}
                                                className="h-9 text-xs"
                                                style={{ width: header.getSize() !== 150 ? header.getSize() : undefined }}
                                            >
                                                {header.isPlaceholder
                                                    ? null
                                                    : flexRender(header.column.columnDef.header, header.getContext())}
                                            </TableHead>
                                        ))}
                                    </TableRow>
                                ))}
                            </TableHeader>
                            <TableBody>
                                {isLoading ? (
                                    <TableRow>
                                        <TableCell colSpan={columns.length} className="h-32 text-center text-muted-foreground">
                                            <div className="flex items-center justify-center gap-2">
                                                <HiOutlineRefresh className="w-5 h-5 animate-spin" />
                                                Loading access logs...
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                ) : table.getRowModel().rows.length === 0 ? (
                                    <TableRow>
                                        <TableCell colSpan={columns.length} className="h-32 text-center">
                                            <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                                <HiOutlineExclamationCircle className="w-8 h-8 opacity-50" />
                                                {entries.length === 0
                                                    ? (
                                                        <>
                                                            <span>No access logs available</span>
                                                            <span className="text-xs">Enable access log capture in node Settings &rarr; Xray</span>
                                                        </>
                                                    ) : (
                                                        <>
                                                            <span>No matching entries</span>
                                                            <Button variant="ghost" size="sm" onClick={clearFilters}>Clear filters</Button>
                                                        </>
                                                    )}
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                ) : (
                                    table.getRowModel().rows.map(row => (
                                        <TableRow key={row.id} className="h-8">
                                            {row.getVisibleCells().map(cell => (
                                                <TableCell key={cell.id} className="py-1.5">
                                                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                                                </TableCell>
                                            ))}
                                        </TableRow>
                                    ))
                                )}
                            </TableBody>
                        </Table>

                        {/* Pagination */}
                        {table.getPageCount() > 1 && (
                            <div className="flex items-center justify-between px-4 py-2 border-t">
                                <div className="text-xs text-muted-foreground">
                                    Page {table.getState().pagination.pageIndex + 1} of {table.getPageCount()}
                                    {" "}({table.getFilteredRowModel().rows.length} entries)
                                </div>
                                <div className="flex items-center gap-1">
                                    <Select
                                        value={table.getState().pagination.pageSize.toString()}
                                        onValueChange={v => table.setPageSize(Number(v))}
                                    >
                                        <SelectTrigger className="h-7 w-[75px] text-xs">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {[50, 100, 200].map(size => (
                                                <SelectItem key={size} value={size.toString()}>{size}/page</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                    <Button
                                        variant="outline"
                                        size="icon"
                                        className="h-7 w-7"
                                        onClick={() => table.previousPage()}
                                        disabled={!table.getCanPreviousPage()}
                                    >
                                        <HiOutlineChevronLeft className="w-3.5 h-3.5" />
                                    </Button>
                                    <Button
                                        variant="outline"
                                        size="icon"
                                        className="h-7 w-7"
                                        onClick={() => table.nextPage()}
                                        disabled={!table.getCanNextPage()}
                                    >
                                        <HiOutlineChevronRight className="w-3.5 h-3.5" />
                                    </Button>
                                </div>
                            </div>
                        )}
                    </div>
            </div>
        </div>
    )
}

// --- Utility Components ---

function SortButton({ column, children }: { column: { getIsSorted: () => false | "asc" | "desc"; toggleSorting: (desc?: boolean) => void }; children: React.ReactNode }) {
    const sorted = column.getIsSorted()
    return (
        <button
            type="button"
            className="flex items-center gap-1 hover:text-foreground transition-colors -ml-1 px-1"
            onClick={() => column.toggleSorting(sorted === "asc")}
        >
            {children}
            {sorted === "asc" ? (
                <ArrowUp className="w-3.5 h-3.5" />
            ) : sorted === "desc" ? (
                <ArrowDown className="w-3.5 h-3.5" />
            ) : (
                <ArrowUpDown className="w-3.5 h-3.5 opacity-30" />
            )}
        </button>
    )
}

function ClickToCopy({ value, children }: { value: string; children: React.ReactNode }) {
    return (
        <button
            type="button"
            className="flex items-center gap-1 group cursor-pointer bg-transparent border-none p-0 text-left"
            onClick={() => {
                navigator.clipboard.writeText(value)
                toast.success(`Copied: ${value}`)
            }}
        >
            {children}
            <Copy className="w-3 h-3 text-muted-foreground/0 group-hover:text-muted-foreground transition-colors shrink-0" />
        </button>
    )
}

function StatBadge({ label, value, className }: { label: string; value: number; className?: string }) {
    return (
        <div className="flex items-center gap-1.5 text-muted-foreground">
            <span className="text-xs">{label}</span>
            <span className={cn("font-semibold tabular-nums text-foreground", className)}>{value.toLocaleString()}</span>
        </div>
    )
}
