import { useEffect, useState, useCallback } from "react"
import { useNavigate, useSearchParams, useLocation } from "react-router"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/ui/badge"
import {
    HiOutlineServer,
    HiOutlineRefresh,
    HiOutlineChartBar,
    HiOutlineCog,
    HiOutlineTerminal,
    HiOutlineGlobeAlt,
    HiOutlineUsers,
    HiOutlineDotsVertical,
} from "react-icons/hi"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
    getNode,
    listNodes,
    restartXrayProcess,
} from "@/lib/admin-api"
import { Loader2, Satellite } from "lucide-react"
import { useNodeStats, useNodeStatsHistory, queryKeys } from "@/lib/queries"
import { useQueryClient } from "@tanstack/react-query"
import type { Node } from "@/lib/types"
import { cn } from "@/lib/utils"
import { toast } from "sonner"
import { Globe } from "lucide-react"

// Import modular components
import { NodeOverview } from "@/components/node/node-overview"
import { NodeNetworkConfig } from "@/components/node/node-network-config"
import { NodeSettings } from "@/components/node/node-settings"
import { NodeLogs } from "@/components/node/node-logs"
import { NodeAccessLogs } from "@/components/node/node-access-logs"
import { NodeTerminal } from "@/components/node/node-terminal"
import { NodeAccountsList } from "@/components/node/node-accounts-list"
import { GeofilesDialog } from "@/components/node/geofiles-dialog"
import { CopyableText } from "@/components/ui/copyable-text"
import { AccountDetailsSheet } from "@/components/accounts/account-details-sheet"
import { StarlinkDashboard } from "@/components/node/starlink/starlink-dashboard"

// Shared tab trigger class to avoid duplication
const tabTriggerClass = "w-auto md:w-full justify-start px-4 py-2.5 md:py-3 h-auto rounded-full md:rounded-lg bg-muted/50 md:bg-transparent data-[state=active]:bg-foreground data-[state=active]:text-background md:data-[state=active]:bg-primary/10 md:data-[state=active]:text-primary data-[state=active]:shadow-sm md:data-[state=active]:shadow-none border border-transparent md:data-[state=active]:border-primary/20 hover:bg-muted/80 md:hover:bg-muted/50 transition-all"

export default function NodeDetailPage() {
    const navigate = useNavigate()
    const [searchParams] = useSearchParams()
    const { pathname } = useLocation()
    const queryClient = useQueryClient()

    // one node (local machine)
    const [node, setNode] = useState<Node | null>(null)
    const nodeId = node?.id ?? 0

    // Tab state from URL
    const rawTab = searchParams.get("tab") || "overview"
    const activeTab = rawTab
    const [isLoading, setIsLoading] = useState(true)

    const [geofilesOpen, setGeofilesOpen] = useState(false)
    const [isRestarting, setIsRestarting] = useState(false)

    const handleRestartXray = async () => {
        if (!node) return
        try {
            setIsRestarting(true)
            const res = await restartXrayProcess(nodeId)
            if (res.success) {
                toast.success("Xray service restarted successfully")
                queryClient.invalidateQueries({ queryKey: queryKeys.nodeStats(nodeId) })
            } else {
                toast.error("Failed to restart Xray: " + res.error)
            }
        } catch {
            toast.error("Failed to restart Xray service")
        } finally {
            setIsRestarting(false)
        }
    }

    const handleTabChange = (value: string) => {
        const params = new URLSearchParams(searchParams)
        params.set("tab", value)
        navigate(`${pathname}?${params.toString()}`)
    }

    // Fetch stats using TanStack Query
    const { data: stats, isFetching: isStatsFetching, refetch: refetchStats } = useNodeStats(
        nodeId,
        node?.is_online ?? false
    )

    // Fetch history
    const { data: historyData } = useNodeStatsHistory(
        nodeId,
        60, // Limit
        node?.is_online ?? false
    )

    const loadData = useCallback(async () => {
        try {
            if (!node) setIsLoading(true)

            // Resolve the single local node. then load its full detail
            const listRes = await listNodes()
            const first = listRes.success && listRes.data && listRes.data.length > 0 ? listRes.data[0] : null
            if (!first) {
                toast.error("No server configured")
                return
            }

            const nodeRes = await getNode(first.id)
            if (nodeRes.success && nodeRes.data) {
                setNode(nodeRes.data)
            } else {
                toast.error("Failed to load server data")
            }
        } catch {
            toast.error("Failed to load server data")
        } finally {
            setIsLoading(false)
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    useEffect(() => {
        loadData()
    }, [loadData])

    const handleRefresh = useCallback(() => {
        loadData()
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeStats(nodeId) })
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeStatsHistory(nodeId) })
    }, [loadData, queryClient, nodeId])

    if (isLoading && !node) {
        return (
            <div className="space-y-6 animate-in fade-in duration-500">
                <div className="flex items-center gap-4">
                    <Skeleton className="h-10 w-10" />
                    <Skeleton className="h-8 w-48" />
                </div>
                <Skeleton className="h-[180px] w-full" />
                <Skeleton className="h-[400px] w-full" />
            </div>
        )
    }

    if (!node) {
        return (
            <div className="flex flex-col items-center justify-center py-12">
                <p className="text-muted-foreground mb-4">Server unavailable</p>
                <Button variant="outline" onClick={handleRefresh}>
                    <HiOutlineRefresh className="w-4 h-4 mr-2" />
                    Retry
                </Button>
            </div>
        )
    }

    return (
        <div className="min-h-screen animate-in fade-in duration-500 pb-20">
            {/* Top Header / Breadcrumbs */}
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-6 md:mb-8">
                <div className="flex items-center gap-3">
                    <div className={cn(
                        "w-9 h-9 rounded-lg flex items-center justify-center shrink-0",
                        node.is_online ? "bg-emerald-500/10 text-emerald-500" : "bg-red-500/10 text-red-500"
                    )}>
                        <HiOutlineServer className="w-5 h-5" />
                    </div>
                    <div className="flex items-center gap-2">
                        <h1 className="text-lg font-bold text-foreground">{node.name}</h1>
                        <Badge
                            variant={node.is_online ? "success" : "danger"}
                            className="px-1.5 py-0 text-[11px] uppercase font-bold tracking-wider"
                        >
                            {node.is_online ? "Online" : "Offline"}
                        </Badge>
                    </div>
                </div>

                <div className="flex items-center gap-2 self-end md:self-auto">
                    <Button
                        size="sm"
                        onClick={handleRestartXray}
                        disabled={isLoading || isRestarting}
                        variant="outline"
                        className="hidden md:inline-flex h-8 px-3"
                    >
                        {isRestarting ? (
                            <Loader2 className="w-3.5 h-3.5 mr-2 animate-spin" />
                        ) : (
                            <HiOutlineRefresh className="w-3.5 h-3.5 mr-2" />
                        )}
                        Restart Xray
                    </Button>
                    <GeofilesDialog
                        nodeId={nodeId}
                        isOnline={node.is_online}
                        onRefresh={handleRefresh}
                        trigger={
                            <Button variant="outline" size="sm" className="hidden md:inline-flex h-8 px-3" disabled={!node.is_online}>
                                <Globe className="w-3.5 h-3.5 mr-2" />
                                Geofiles
                            </Button>
                        }
                    />
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={handleRefresh}
                        disabled={isLoading || isStatsFetching}
                        className="h-8 px-2.5 md:px-3"
                    >
                        <HiOutlineRefresh className={cn("w-3.5 h-3.5 md:mr-2", (isLoading || isStatsFetching) && "animate-spin")} />
                        <span className="hidden md:inline">Refresh</span>
                    </Button>
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button variant="outline" size="sm" className="md:hidden h-8 px-2.5">
                                <HiOutlineDotsVertical className="w-4 h-4" />
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={handleRestartXray} disabled={isRestarting}>
                                <HiOutlineRefresh className={cn("w-4 h-4 mr-2", isRestarting && "animate-spin")} />
                                Restart Xray
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => setGeofilesOpen(true)} disabled={!node.is_online}>
                                <Globe className="w-4 h-4 mr-2" />
                                Update Geofiles
                            </DropdownMenuItem>
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>
            </div>

            <Tabs value={activeTab} onValueChange={handleTabChange} className="flex flex-col md:grid md:grid-cols-[240px_1fr] gap-6 md:gap-8">
                {/* Sidebar Navigation (Mobile: Horizontal Scroll, Desktop: Vertical Sidebar) */}
                <aside className="space-y-4">
                    {/* Node Info Card (Hidden on Mobile, Visible on Desktop) */}
                    <div className="hidden md:flex items-center gap-3 p-4 rounded-xl bg-card border shadow-sm mb-2">
                        <div className={cn(
                            "w-10 h-10 rounded-lg flex items-center justify-center shrink-0",
                            node.is_online
                                ? "bg-emerald-500/10 text-emerald-500"
                                : "bg-red-500/10 text-red-500"
                        )}>
                            <HiOutlineServer className="w-5 h-5" />
                        </div>
                        <div className="overflow-hidden">
                            <h2 className="font-bold truncate">{node.name}</h2>
                            <CopyableText
                                text={node.ip}
                                className="text-xs text-muted-foreground font-mono"
                            />
                        </div>
                    </div>

                    <div className="relative md:static">
                    <TabsList className="flex flex-row md:flex-col h-auto bg-transparent p-0 gap-2 md:gap-1 w-full justify-start overflow-x-auto no-scrollbar pb-2 md:pb-0 pr-6 md:pr-0">
                        <TabsTrigger value="overview" className={tabTriggerClass}>
                            <HiOutlineChartBar className="w-4 h-4 md:w-5 md:h-5 mr-2 md:mr-3" />
                            <div className="text-left flex flex-col">
                                <span className="block font-medium text-sm md:text-base">Overview</span>
                                <span className="hidden md:block text-xs opacity-70 font-normal mt-0.5">Stats & Health</span>
                            </div>
                        </TabsTrigger>
                        <TabsTrigger value="network" className={tabTriggerClass}>
                            <HiOutlineGlobeAlt className="w-4 h-4 md:w-5 md:h-5 mr-2 md:mr-3" />
                            <div className="text-left flex flex-col">
                                <span className="block font-medium text-sm md:text-base">Network</span>
                                <span className="hidden md:block text-xs opacity-70 font-normal mt-0.5">Inbounds & Outbounds</span>
                            </div>
                        </TabsTrigger>
                        <TabsTrigger value="users" className={tabTriggerClass}>
                            <HiOutlineUsers className="w-4 h-4 md:w-5 md:h-5 mr-2 md:mr-3" />
                            <div className="text-left flex flex-col">
                                <span className="block font-medium text-sm md:text-base">Accounts</span>
                                <span className="hidden md:block text-xs opacity-70 font-normal mt-0.5">User Management</span>
                            </div>
                        </TabsTrigger>
                        {node.starlink_settings?.enabled && !node.is_stealth && (
                            <TabsTrigger value="starlink" className={tabTriggerClass}>
                                <Satellite className="w-4 h-4 md:w-5 md:h-5 mr-2 md:mr-3" />
                                <div className="text-left flex flex-col">
                                    <span className="block font-medium text-sm md:text-base">Starlink</span>
                                    <span className="hidden md:block text-xs opacity-70 font-normal mt-0.5">Satellite Link</span>
                                </div>
                            </TabsTrigger>
                        )}
                        <TabsTrigger value="settings" className={tabTriggerClass}>
                            <HiOutlineCog className="w-4 h-4 md:w-5 md:h-5 mr-2 md:mr-3" />
                            <div className="text-left flex flex-col">
                                <span className="block font-medium text-sm md:text-base">Settings</span>
                                <span className="hidden md:block text-xs opacity-70 font-normal mt-0.5">Node Configuration</span>
                            </div>
                        </TabsTrigger>
                        {!node?.is_stealth && (
                            <TabsTrigger value="access-logs" className={tabTriggerClass}>
                                <HiOutlineGlobeAlt className="w-4 h-4 md:w-5 md:h-5 mr-2 md:mr-3" />
                                <div className="text-left flex flex-col">
                                    <span className="block font-medium text-sm md:text-base">Access Logs</span>
                                    <span className="hidden md:block text-xs opacity-70 font-normal mt-0.5">Destinations & Domains</span>
                                </div>
                            </TabsTrigger>
                        )}
                        <TabsTrigger value="logs" className={tabTriggerClass}>
                            <HiOutlineTerminal className="w-4 h-4 md:w-5 md:h-5 mr-2 md:mr-3" />
                            <div className="text-left flex flex-col">
                                <span className="block font-medium text-sm md:text-base">Logs</span>
                                <span className="hidden md:block text-xs opacity-70 font-normal mt-0.5">System & Activity</span>
                            </div>
                        </TabsTrigger>
                        {!node?.is_stealth && (
                            <TabsTrigger value="terminal" className={tabTriggerClass}>
                                <HiOutlineTerminal className="w-4 h-4 md:w-5 md:h-5 mr-2 md:mr-3" />
                                <div className="text-left flex flex-col">
                                    <span className="block font-medium text-sm md:text-base">Terminal</span>
                                    <span className="hidden md:block text-xs opacity-70 font-normal mt-0.5">Interactive Shell</span>
                                </div>
                            </TabsTrigger>
                        )}
                    </TabsList>
                    <div className="absolute right-0 top-0 h-full w-8 bg-gradient-to-l from-background to-transparent pointer-events-none md:hidden" />
                    </div>
                </aside>

                {/* Main Content Area — only the active tab's component is mounted */}
                <main className="min-w-0">
                    <TabsContent value="overview" className="mt-0 space-y-6 focus-visible:outline-none animate-in fade-in slide-in-from-bottom-2 duration-300">
                        {activeTab === "overview" && (
                            <NodeOverview
                                node={node}
                                stats={stats}
                                history={historyData}
                                isLoadingStats={isStatsFetching}
                                onRefreshStats={refetchStats}
                                onNavigateTab={handleTabChange}
                            />
                        )}
                    </TabsContent>

                    <TabsContent value="network" className="mt-0 space-y-6 focus-visible:outline-none animate-in fade-in slide-in-from-bottom-2 duration-300">
                        {activeTab === "network" && (
                            <NodeNetworkConfig
                                nodeId={nodeId}
                                onRefresh={loadData}
                                isOnline={node?.is_online || false}
                            />
                        )}
                    </TabsContent>

                    <TabsContent value="starlink" className="mt-0 space-y-6 focus-visible:outline-none animate-in fade-in slide-in-from-bottom-2 duration-300">
                        {activeTab === "starlink" && (
                            <StarlinkDashboard nodeId={nodeId} isOnline={node.is_online} />
                        )}
                    </TabsContent>

                    <TabsContent value="settings" className="mt-0 space-y-6 focus-visible:outline-none animate-in fade-in slide-in-from-bottom-2 duration-300">
                        {activeTab === "settings" && (
                            <NodeSettings
                                node={node}
                                onRefresh={loadData}
                            />
                        )}
                    </TabsContent>

                    {!node?.is_stealth && (
                        <TabsContent value="access-logs" className="mt-0 space-y-6 focus-visible:outline-none animate-in fade-in slide-in-from-bottom-2 duration-300">
                            {activeTab === "access-logs" && (
                                <NodeAccessLogs nodeId={nodeId} isOnline={node?.is_online || false} />
                            )}
                        </TabsContent>
                    )}

                    <TabsContent value="logs" className="mt-0 space-y-6 focus-visible:outline-none animate-in fade-in slide-in-from-bottom-2 duration-300">
                        {activeTab === "logs" && (
                            <NodeLogs nodeId={node.id} isOnline={node?.is_online || false} />
                        )}
                    </TabsContent>

                    {!node?.is_stealth && (
                        <TabsContent value="terminal" className="mt-0 space-y-6 focus-visible:outline-none animate-in fade-in slide-in-from-bottom-2 duration-300">
                            {activeTab === "terminal" && (
                                <NodeTerminal nodeId={node.id} isOnline={node?.is_online || false} />
                            )}
                        </TabsContent>
                    )}

                    <TabsContent value="users" className="mt-0 space-y-6 focus-visible:outline-none animate-in fade-in slide-in-from-bottom-2 duration-300">
                        {activeTab === "users" && (
                            <NodeAccountsList
                                nodeId={nodeId}
                                isOnline={node?.is_online || false}
                            />
                        )}
                    </TabsContent>
                </main>
            </Tabs>

            <GeofilesDialog
                nodeId={nodeId}
                isOnline={node.is_online}
                onRefresh={handleRefresh}
                open={geofilesOpen}
                onOpenChange={setGeofilesOpen}
            />

            <AccountDetailsSheet />
        </div>
    )
}
