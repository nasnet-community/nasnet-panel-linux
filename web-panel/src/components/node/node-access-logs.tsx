import { useState, useMemo } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import { HiOutlineRefresh, HiOutlineExclamationCircle } from "react-icons/hi"
import { Search, Copy, Settings } from "lucide-react"
import { useAccessLogs } from "@/lib/queries"
import { cn } from "@/lib/utils"
import { toast } from "sonner"

interface NodeAccessLogsProps {
    nodeId: number
    isOnline: boolean
}

function formatTime(unixSeconds: number): string {
    const d = new Date(unixSeconds * 1000)
    return d.toLocaleTimeString("en-US", { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" })
}

function formatDateTime(unixSeconds: number): string {
    const d = new Date(unixSeconds * 1000)
    return d.toLocaleString("en-US", {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
    })
}

export function NodeAccessLogs({ nodeId, isOnline }: NodeAccessLogsProps) {
    const [emailFilter, setEmailFilter] = useState("")
    const [domainSearch, setDomainSearch] = useState("")
    const [limit, setLimit] = useState(100)

    const { data: entries = [], isLoading, isFetching, refetch } = useAccessLogs(
        nodeId,
        emailFilter || undefined,
        limit,
        isOnline,
    )

    const filteredEntries = useMemo(() => {
        if (!domainSearch) return entries
        const lower = domainSearch.toLowerCase()
        return entries.filter(e => e.domain.toLowerCase().includes(lower))
    }, [entries, domainSearch])

    const copyToClipboard = (text: string) => {
        navigator.clipboard.writeText(text)
        toast.success(`Copied: ${text}`)
    }

    if (!isOnline) {
        return (
            <Card className="bg-card/50 backdrop-blur-sm border-white/5">
                <CardContent className="py-12">
                    <div className="flex flex-col items-center justify-center text-muted-foreground">
                        <HiOutlineExclamationCircle className="w-12 h-12 mb-4 opacity-50" />
                        <h3 className="text-lg font-medium text-foreground">Node is Offline</h3>
                        <p>Start the node agent to view access logs.</p>
                    </div>
                </CardContent>
            </Card>
        )
    }

    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardHeader>
                <div className="flex items-center justify-between">
                    <div>
                        <CardTitle>Access Logs</CardTitle>
                        <CardDescription>Destination logs from xray access log (auto-refreshes every 10s)</CardDescription>
                    </div>
                    <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => refetch()}
                        disabled={isFetching}
                        aria-label="Refresh access logs"
                    >
                        <HiOutlineRefresh className={cn("w-4 h-4", isFetching && "animate-spin")} />
                    </Button>
                </div>
            </CardHeader>
            <CardContent className="space-y-4">
                {/* Filters */}
                <div className="flex flex-col sm:flex-row gap-3">
                    <div className="relative flex-1">
                        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                        <Input
                            placeholder="Filter by email..."
                            value={emailFilter}
                            onChange={(e) => setEmailFilter(e.target.value)}
                            className="pl-9"
                        />
                    </div>
                    <div className="relative flex-1">
                        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                        <Input
                            placeholder="Search domain..."
                            value={domainSearch}
                            onChange={(e) => setDomainSearch(e.target.value)}
                            className="pl-9"
                        />
                    </div>
                    <Select value={limit.toString()} onValueChange={(v) => setLimit(Number(v))}>
                        <SelectTrigger className="w-[120px]">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="50">50 rows</SelectItem>
                            <SelectItem value="100">100 rows</SelectItem>
                            <SelectItem value="200">200 rows</SelectItem>
                            <SelectItem value="500">500 rows</SelectItem>
                        </SelectContent>
                    </Select>
                </div>

                {/* Table */}
                <div className="rounded-md border">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead className="w-[140px]">Time</TableHead>
                                <TableHead className="w-[90px]">Status</TableHead>
                                <TableHead className="w-[60px]">Net</TableHead>
                                <TableHead>Domain</TableHead>
                                <TableHead className="w-[70px]">Port</TableHead>
                                <TableHead className="hidden lg:table-cell">Email</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {isLoading ? (
                                <TableRow>
                                    <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                                        Loading access logs...
                                    </TableCell>
                                </TableRow>
                            ) : filteredEntries.length === 0 ? (
                                <TableRow>
                                    <TableCell colSpan={6} className="h-24 text-center">
                                        <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                            {emailFilter || domainSearch ? (
                                                <>
                                                    <Search className="w-8 h-8 opacity-50" />
                                                    <span>No matching entries found</span>
                                                </>
                                            ) : (
                                                <>
                                                    <Settings className="w-8 h-8 opacity-50" />
                                                    <span>No access logs available</span>
                                                    <span className="text-xs">
                                                        Enable access log capture in Settings &rarr; Xray to start collecting logs
                                                    </span>
                                                </>
                                            )}
                                        </div>
                                    </TableCell>
                                </TableRow>
                            ) : (
                                filteredEntries.map((entry, i) => (
                                    <TableRow key={`${entry.timestamp}-${entry.email}-${i}`}>
                                        <TableCell className="font-mono text-xs text-muted-foreground">
                                            <span className="hidden sm:inline">{formatDateTime(entry.timestamp)}</span>
                                            <span className="sm:hidden">{formatTime(entry.timestamp)}</span>
                                        </TableCell>
                                        <TableCell>
                                            <Badge
                                                variant="outline"
                                                className={cn(
                                                    "text-[11px] font-medium",
                                                    entry.status === "accepted"
                                                        ? "text-emerald-600 border-emerald-600/30"
                                                        : "text-red-500 border-red-500/30"
                                                )}
                                            >
                                                {entry.status === "accepted" ? "ACC" : "REJ"}
                                            </Badge>
                                        </TableCell>
                                        <TableCell>
                                            <span className="text-xs text-muted-foreground uppercase">{entry.network}</span>
                                        </TableCell>
                                        <TableCell>
                                            <button
                                                type="button"
                                                className="flex items-center gap-1 group cursor-pointer bg-transparent border-none p-0 text-left"
                                                onClick={() => copyToClipboard(entry.domain)}
                                            >
                                                <span className="font-mono text-sm truncate max-w-[300px]">{entry.domain}</span>
                                                <Copy className="w-3 h-3 text-muted-foreground/0 group-hover:text-muted-foreground transition-colors shrink-0" />
                                            </button>
                                        </TableCell>
                                        <TableCell className="font-mono text-xs text-muted-foreground">
                                            {entry.port}
                                        </TableCell>
                                        <TableCell className="hidden lg:table-cell">
                                            <button
                                                type="button"
                                                className="flex items-center gap-1 group cursor-pointer bg-transparent border-none p-0 text-left"
                                                onClick={() => copyToClipboard(entry.email)}
                                            >
                                                <span className="font-mono text-xs text-muted-foreground truncate max-w-[180px]">{entry.email}</span>
                                                <Copy className="w-3 h-3 text-muted-foreground/0 group-hover:text-muted-foreground transition-colors shrink-0" />
                                            </button>
                                        </TableCell>
                                    </TableRow>
                                ))
                            )}
                        </TableBody>
                    </Table>
                </div>

                {/* Footer */}
                {filteredEntries.length > 0 && (
                    <div className="flex items-center justify-between text-xs text-muted-foreground px-1">
                        <span>
                            Showing {filteredEntries.length} {filteredEntries.length !== entries.length && `of ${entries.length} `}entries
                        </span>
                        {isFetching && <span className="animate-pulse">Refreshing...</span>}
                    </div>
                )}
            </CardContent>
        </Card>
    )
}
