import React, { useState, useMemo } from "react"
import { useSearchParams } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { queryKeys } from "@/lib/queries/keys"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { Checkbox } from "@/components/ui/checkbox"
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import {
    HiOutlineGlobeAlt,
    HiOutlineSwitchHorizontal,
    HiOutlineMap,
    HiOutlineDownload,
    HiOutlineUpload,
    HiOutlinePlus,
    HiOutlinePencil,
    HiOutlineTrash,
    HiChevronRight,
    HiChevronDown,
    HiOutlineClipboard,
    HiOutlineUsers,
    HiOutlineX,
    HiOutlineBan,
    HiOutlineClock,
    HiOutlineStatusOnline,
    HiOutlineCog,
    HiOutlineArrowsExpand,
} from "react-icons/hi"
import { cn, formatBytes, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"
import { InboundSettingsDialog } from "@/components/inbound/inbound-settings-dialog"
import { MigrateInboundDialog } from "@/components/inbound/migrate-inbound-dialog"
import { OutboundSettingsDialog } from "@/components/outbound/outbound-settings-dialog"
import { RoutingRuleDialog } from "@/components/routing/routing-rule-dialog"
import { RoutingSettingsCard, PRESET_RULE_TAGS } from "@/components/routing/routing-settings-card"
import { DNSSettingsCard } from "@/components/dns/dns-settings-card"
import { BalancingRulesCard } from "@/components/routing/balancing-rules-card"
import { ReverseProxyTable } from "@/components/reverse-proxy/reverse-proxy-table"
import { ReverseProxyDialog } from "@/components/reverse-proxy/reverse-proxy-dialog"
import type { Inbound, Outbound, OutboundTestResult, RoutingRule, ReverseProxy } from "@/lib/types"
import { deleteNodeInbound, toggleInboundDisabled } from "@/lib/admin-api"
import {
    useNodes,
    useNodeInbounds,
    useNodeOutbounds,
    useNodeRouting,
    useReverseProxies,
    useBalancingRules,
    useAddInbound,
    useUpdateInbound,
    useDeleteInbound,
    useToggleInbound,
    useDiscoverInbounds,
    useSyncInbounds,
    useAddOutbound,
    useUpdateOutbound,
    useDeleteOutbound,
    useToggleOutbound,
    useTestOutbound,
    useAddRoutingRule,
    useUpdateRoutingRule,
    useDeleteRoutingRule,
    useToggleRoutingRule,
    useAddReverseProxy,
    useUpdateReverseProxy,
    useDeleteReverseProxy,
} from "@/lib/queries/use-nodes"
import { listNodeInbounds } from "@/lib/admin-api"
import { NodeXrayConfigEditor } from "./node-xray-config-editor"
import { NodeSettingsXray } from "./settings/node-settings-xray"
import { HiOutlineCode, HiOutlineAdjustments } from "react-icons/hi"
import { InboundAccountsRow } from "./inbound-accounts-row"
import { HostList } from "@/components/host/host-list"
import { useAccountsByNode } from "@/lib/queries/use-accounts"
import { type Account as NodeAccount } from "@/lib/api/accounts"
import { ProtocolBadge, protocolColors } from "./protocol-badge"
import { AnimatePresence, motion } from "framer-motion"
import { SwipeableInboundRow } from "./swipeable-inbound-row"
import { SwipeableOutboundRow } from "./swipeable-outbound-row"
import { RoutingRulesTable } from "@/components/routing/routing-rules-table"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { HiOutlineDotsVertical } from "react-icons/hi"
import { OutboundTestResultDialog } from "@/components/outbound/outbound-test-result-dialog"
import { BulkActionBar } from "./network/bulk-action-bar"
import { DesktopInboundRow } from "./network/desktop-inbound-row"
import { HiOutlineCheckCircle, HiOutlineXCircle } from "react-icons/hi"
import { Power, Loader2 } from "lucide-react"

interface NodeNetworkConfigProps {
    nodeId: number
    onRefresh?: () => void
    isOnline?: boolean
}

interface AccountChipProps {
    title: string
    count: number
    accounts: NodeAccount[]
    icon: React.ComponentType<{ className?: string }>
    colorClass: string // applied to icon + count text + bg/border tint shorthand
    bgClass: string // tailwind bg + border classes
}

function AccountChip({ title, count, accounts, icon: Icon, colorClass, bgClass }: AccountChipProps) {
    if (count === 0) return null
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <div className={cn("flex items-center gap-1.5 px-2 py-1 rounded-md transition-colors cursor-help", bgClass)}>
                    <Icon className={cn("w-3.5 h-3.5", colorClass)} />
                    <span className={cn("text-xs font-mono font-medium", colorClass)}>{count}</span>
                </div>
            </TooltipTrigger>
            <TooltipContent>
                <div className="space-y-1">
                    <p className="font-medium text-xs border-b border-border/50 pb-1 mb-1">{title} ({count})</p>
                    <div className="flex flex-col gap-0.5 text-xs text-muted-foreground">
                        {accounts.slice(0, 5).map(acc => (
                            <div key={acc.id} className="truncate max-w-[200px]">
                                {acc.email || `ID: ${acc.id}`}
                            </div>
                        ))}
                        {count > 5 && (
                            <div className="text-xs opacity-70 italic pt-1">
                                ... and {count - 5} more
                            </div>
                        )}
                    </div>
                </div>
            </TooltipContent>
        </Tooltip>
    )
}

export function NodeNetworkConfig({
    nodeId,
    onRefresh,
    isOnline = false
}: NodeNetworkConfigProps) {
    const [actionLoading, setActionLoading] = useState<string | null>(null)
    const [expandedRows, setExpandedRows] = useState<Set<number>>(new Set())

    // Bulk selection state
    const [selectedInbounds, setSelectedInbounds] = useState<Set<number>>(new Set())

    // ESC key clears bulk selection
    React.useEffect(() => {
        if (selectedInbounds.size === 0) return
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") setSelectedInbounds(new Set())
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [selectedInbounds.size])

    // Sub-tab persisted in URL via ?subtab=
    const VALID_SUBTABS = ["inbounds", "outbounds", "routing", "reverse", "dns", "settings", "config"] as const
    const [searchParams, setSearchParams] = useSearchParams()
    const rawSubtab = searchParams.get("subtab") || "inbounds"
    const activeTab = (VALID_SUBTABS as readonly string[]).includes(rawSubtab) ? rawSubtab : "inbounds"
    const setActiveTab = (value: string) => {
        const next = new URLSearchParams(searchParams)
        next.set("subtab", value)
        setSearchParams(next, { replace: true })
    }

    const [openSwipeRowId, setOpenSwipeRowId] = useState<number | null>(null)
    const [openSwipeOutboundId, setOpenSwipeOutboundId] = useState<number | null>(null)

    // Data via React Query — caches across tab switches and dedupes on refetch
    const { data: inbounds = [], isLoading: ibLoading } = useNodeInbounds(nodeId)
    const { data: outbounds = [], isLoading: obLoading } = useNodeOutbounds(nodeId)
    const { data: routingRules = [], isLoading: rrLoading } = useNodeRouting(nodeId)
    const { data: balancingRules = [] } = useBalancingRules(nodeId)
    const { data: reverseProxies = [] } = useReverseProxies(nodeId)
    const isLoading = ibLoading || obLoading || rrLoading

    // Mutations — each invalidates relevant cache on success
    const addInbound = useAddInbound()
    const updateInbound = useUpdateInbound()
    const deleteInbound = useDeleteInbound()
    const toggleInbound = useToggleInbound(nodeId)
    const discoverInbounds = useDiscoverInbounds()
    const syncInbounds = useSyncInbounds()
    const addOutbound = useAddOutbound(nodeId)
    const updateOutbound = useUpdateOutbound(nodeId)
    const deleteOutbound = useDeleteOutbound(nodeId)
    const toggleOutbound = useToggleOutbound(nodeId)
    const testOutboundMut = useTestOutbound()
    const addRoutingRule = useAddRoutingRule(nodeId)
    const updateRoutingRule = useUpdateRoutingRule(nodeId)
    const deleteRoutingRule = useDeleteRoutingRule(nodeId)
    const toggleRoutingRuleMut = useToggleRoutingRule(nodeId)
    const addReverseProxyMut = useAddReverseProxy(nodeId)
    const updateReverseProxyMut = useUpdateReverseProxy(nodeId)
    const deleteReverseProxyMut = useDeleteReverseProxy(nodeId)

    const queryClient = useQueryClient()
    const confirm = useConfirm()

    // Migrate dialog state — data fetched lazily on open
    const [migrateDialog, setMigrateDialog] = useState<{ open: boolean; inbound: Inbound | null }>({ open: false, inbound: null })
    const { data: allNodesData } = useNodes()
    const [allInboundsMap, setAllInboundsMap] = useState<Map<number, Inbound[]>>(new Map())

    React.useEffect(() => {
        if (!migrateDialog.open || !allNodesData) return
        let cancelled = false
        const fetchAll = async () => {
            const results = await Promise.all(
                allNodesData.map(async (n) => {
                    try {
                        const res = await listNodeInbounds(n.id)
                        return [n.id, (res.success && res.data) ? res.data : []] as const
                    } catch {
                        return [n.id, [] as Inbound[]] as const
                    }
                })
            )
            if (!cancelled) setAllInboundsMap(new Map(results))
        }
        fetchAll()
        return () => { cancelled = true }
    }, [migrateDialog.open, allNodesData])

    // Fetch accounts for this node — only poll when the node is online
    const { data: accounts, refetch: refetchAccounts } = useAccountsByNode(nodeId, {
        refetchInterval: isOnline ? 5000 : false
    })

    // Group accounts by inbound_id
    const accountsByInbound = useMemo(() => {
        const map = new Map<number, NodeAccount[]>()
        accounts?.forEach(acc => {
            const existing = map.get(acc.inbound_id) || []
            existing.push(acc)
            map.set(acc.inbound_id, existing)
        })
        return map
    }, [accounts])

    // Pre-compute derived per-inbound counts in one pass.
    // Threshold = 15s = 3× the 5s polling interval, tolerant of jitter.
    const ONLINE_THRESHOLD_MS = 15_000
    const inboundCounts = useMemo(() => {
        type Counts = { online: number; disabled: number; expired: number; trafficBytes: number }
        const map = new Map<number, Counts>()
        const now = Date.now()
        accounts?.forEach(acc => {
            const c = map.get(acc.inbound_id) || { online: 0, disabled: 0, expired: 0, trafficBytes: 0 }
            if (acc.last_activity_at && now - new Date(acc.last_activity_at).getTime() < ONLINE_THRESHOLD_MS) c.online++
            if (acc.status === "disabled") c.disabled++
            if (acc.status === "expired" || (acc.data_limit > 0 && acc.data_used >= acc.data_limit)) c.expired++
            c.trafficBytes += acc.data_used || 0
            map.set(acc.inbound_id, c)
        })
        return map
    }, [accounts])

    const getOnlineCount = (id: number) => inboundCounts.get(id)?.online || 0
    const hasOnlineAccounts = (id: number) => getOnlineCount(id) > 0

    // Toggle expand/collapse for inbound row
    const toggleExpand = (inboundId: number) => {
        setExpandedRows(prev => {
            const next = new Set(prev)
            if (next.has(inboundId)) next.delete(inboundId)
            else next.add(inboundId)
            return next
        })
    }

    // Dialog States
    const [inboundDialog, setInboundDialog] = useState<{
        open: boolean
        mode: "create" | "edit"
        inbound: Inbound | null
    }>({ open: false, mode: "create", inbound: null })

    const [outboundDialog, setOutboundDialog] = useState<{
        open: boolean
        mode: "create" | "edit"
        outbound: Outbound | null
    }>({ open: false, mode: "create", outbound: null })

    const [routingDialog, setRoutingDialog] = useState<{
        open: boolean
        mode: "create" | "edit"
        rule: RoutingRule | null
    }>({ open: false, mode: "create", rule: null })

    const [reverseProxyDialog, setReverseProxyDialog] = useState<{
        open: boolean
        mode: "create" | "edit"
        reverseProxy: ReverseProxy | null
    }>({ open: false, mode: "create", reverseProxy: null })

    // Outbound testing
    const [testingOutboundId, setTestingOutboundId] = useState<number | null>(null)
    const [testResults, setTestResults] = useState<Map<number, OutboundTestResult>>(new Map())
    const [testResultDialog, setTestResultDialog] = useState<{
        open: boolean
        outboundId: number | null
        outboundTag: string
    }>({ open: false, outboundId: null, outboundTag: "" })

    // Pending rules generated by the settings card (not yet saved to DB)
    const [pendingPresetRules, setPendingPresetRules] = useState<Partial<RoutingRule>[]>([])

    // Compute display rules: DB rules (minus preset-tagged ones when dirty) + pending rules
    const displayRoutingRules = useMemo(() => {
        if (pendingPresetRules.length === 0) {
            // Settings card is clean — show DB rules as-is
            return routingRules
        }
        // Settings card is dirty — replace preset-tagged DB rules with pending generated ones
        const nonPresetDbRules = routingRules.filter(r => !PRESET_RULE_TAGS.has(r.rule_tag))
        const pendingAsDisplay = pendingPresetRules.map(r => ({
            ...r,
            id: 0, // No DB id yet
            _unsaved: true,
        })) as (RoutingRule & { _unsaved?: boolean })[]
        return [...nonPresetDbRules, ...pendingAsDisplay]
    }, [routingRules, pendingPresetRules])

    // --- Actions ---

    const handleDiscover = async () => {
        setActionLoading("discover")
        try {
            await discoverInbounds.mutateAsync(nodeId)
            onRefresh?.()
        } finally {
            setActionLoading(null)
        }
    }

    const handleSync = async () => {
        setActionLoading("sync")
        try {
            await syncInbounds.mutateAsync(nodeId)
            onRefresh?.()
        } finally {
            setActionLoading(null)
        }
    }

    // Inbound CRUD
    const handleSaveInbound = async (data: Partial<Inbound>) => {
        try {
            if (inboundDialog.mode === "create") {
                await addInbound.mutateAsync({ nodeId, inbound: data })
            } else {
                await updateInbound.mutateAsync({ nodeId, inboundId: data.id!, inbound: data })
            }
            setInboundDialog({ open: false, mode: "create", inbound: null })
            onRefresh?.()
        } catch {
            // toast already surfaced by mutation onError
        }
    }

    const handleDeleteInbound = async (inbound: Inbound) => {
        const ok = await confirm({
            title: "Delete inbound",
            description: <>Delete inbound <span className="font-mono font-semibold">{inbound.tag}</span>? This cannot be undone.</>,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await deleteInbound.mutateAsync({ nodeId, inboundId: inbound.id })
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const handleMigrateInbound = (inbound: Inbound) => {
        setMigrateDialog({ open: true, inbound })
    }

    const handleToggleInbound = async (inbound: Inbound) => {
        try {
            await toggleInbound.mutateAsync(inbound)
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    // Bulk selection helpers
    const toggleInboundSelection = (id: number) => {
        setSelectedInbounds(prev => {
            const next = new Set(prev)
            if (next.has(id)) next.delete(id)
            else next.add(id)
            return next
        })
    }

    const toggleAllInbounds = () => {
        if (selectedInbounds.size === inbounds.length) {
            setSelectedInbounds(new Set())
        } else {
            setSelectedInbounds(new Set(inbounds.map(i => i.id)))
        }
    }

    const clearSelection = () => setSelectedInbounds(new Set())

    const handleBulkDelete = async () => {
        if (selectedInbounds.size === 0) return
        const count = selectedInbounds.size
        const confirmed = await confirm({
            title: `Delete ${count} inbound${count > 1 ? "s" : ""}`,
            description: `This will permanently delete ${count} inbound${count > 1 ? "s" : ""} and cannot be undone.`,
            confirmLabel: "Delete",
            variant: "destructive",
            ...(count > 5 ? { typeToConfirm: String(count) } : {}),
        })
        if (!confirmed) return

        setActionLoading("bulk-delete")
        const ids = Array.from(selectedInbounds)
        const idToTag = new Map(inbounds.map(i => [i.id, i.tag] as const))
        const results = await Promise.allSettled(ids.map(id => deleteNodeInbound(nodeId, id)))

        const failedTags: string[] = []
        let okCount = 0
        results.forEach((r, idx) => {
            if (r.status === "fulfilled" && r.value.success) okCount++
            else failedTags.push(idToTag.get(ids[idx]) || `#${ids[idx]}`)
        })

        if (failedTags.length === 0) {
            toast.success(`Deleted ${okCount} inbound(s)`)
        } else {
            toast.warning(`Deleted ${okCount}, failed ${failedTags.length}: ${failedTags.slice(0, 3).join(", ")}${failedTags.length > 3 ? "…" : ""}`)
        }

        setSelectedInbounds(new Set())
        setActionLoading(null)
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeInbounds(nodeId) })
        onRefresh?.()
    }

    const handleBulkToggle = async (disable: boolean) => {
        if (selectedInbounds.size === 0) return

        const targets = inbounds.filter(i => selectedInbounds.has(i.id) && i.is_disabled !== disable)
        if (targets.length === 0) {
            toast.info(`All selected inbounds are already ${disable ? "disabled" : "enabled"}`)
            return
        }

        setActionLoading(disable ? "bulk-disable" : "bulk-enable")
        const results = await Promise.allSettled(targets.map(inb => toggleInboundDisabled(inb.id)))

        const failedTags: string[] = []
        let okCount = 0
        results.forEach((r, idx) => {
            if (r.status === "fulfilled" && r.value.success) okCount++
            else failedTags.push(targets[idx].tag)
        })

        const verb = disable ? "Disabled" : "Enabled"
        if (failedTags.length === 0) {
            toast.success(`${verb} ${okCount} inbound(s)`)
        } else {
            toast.warning(`${verb} ${okCount}, failed ${failedTags.length}: ${failedTags.slice(0, 3).join(", ")}${failedTags.length > 3 ? "…" : ""}`)
        }

        setSelectedInbounds(new Set())
        setActionLoading(null)
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeInbounds(nodeId) })
        onRefresh?.()
    }

    // Outbound CRUD
    const handleSaveOutbound = async (data: Partial<Outbound>) => {
        try {
            if (outboundDialog.mode === "create") {
                await addOutbound.mutateAsync(data)
            } else {
                await updateOutbound.mutateAsync({ outboundId: data.id!, outbound: data })
            }
            setOutboundDialog({ open: false, mode: "create", outbound: null })
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const handleDeleteOutbound = async (outbound: Outbound) => {
        const ok = await confirm({
            title: "Delete outbound",
            description: <>Delete outbound <span className="font-mono font-semibold">{outbound.tag}</span>? This cannot be undone.</>,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await deleteOutbound.mutateAsync(outbound.id)
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const handleToggleOutbound = async (outbound: Outbound) => {
        try {
            await toggleOutbound.mutateAsync(outbound)
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const handleTestOutbound = async (outbound: Outbound) => {
        setTestingOutboundId(outbound.id)
        try {
            const result = await testOutboundMut.mutateAsync(outbound.id)
            setTestResults(prev => new Map(prev).set(outbound.id, result))
            if (result.success) {
                toast.success(`${outbound.tag}: ${result.message || `Connected (${result.latency_ms}ms)`}`)
            } else {
                toast.error(`${outbound.tag}: ${result.error || "Test failed"}`)
            }
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to test outbound")
        } finally {
            setTestingOutboundId(null)
        }
    }

    // Routing CRUD
    const handleDeleteRule = async (rule: RoutingRule & { _unsaved?: boolean }) => {
        if (rule._unsaved) return // Can't delete unsaved rules from DB
        const ok = await confirm({
            title: "Delete routing rule",
            description: <>Delete routing rule <span className="font-mono font-semibold">{rule.rule_tag}</span>?</>,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await deleteRoutingRule.mutateAsync(rule.id)
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const handleToggleRule = async (rule: RoutingRule) => {
        try {
            await toggleRoutingRuleMut.mutateAsync(rule)
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const handleSaveRoutingRule = async (data: Partial<RoutingRule>) => {
        try {
            if (routingDialog.mode === "create") {
                await addRoutingRule.mutateAsync(data)
            } else {
                await updateRoutingRule.mutateAsync({ ruleId: data.id!, rule: data })
            }
            setRoutingDialog({ open: false, mode: "create", rule: null })
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const handleSaveReverseProxy = async (data: Partial<ReverseProxy>) => {
        try {
            if (reverseProxyDialog.mode === "edit" && reverseProxyDialog.reverseProxy) {
                await updateReverseProxyMut.mutateAsync({ id: reverseProxyDialog.reverseProxy.id, data })
            } else {
                await addReverseProxyMut.mutateAsync(data)
            }
            setReverseProxyDialog({ open: false, mode: "create", reverseProxy: null })
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const handleDeleteReverseProxy = async (rp: ReverseProxy) => {
        try {
            await deleteReverseProxyMut.mutateAsync(rp.id)
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    if (isLoading) {
        return (
            <div className="space-y-6">
                <div className="flex gap-1 overflow-x-auto pb-2">
                    {[1, 2, 3, 4].map(i => <Skeleton key={i} className="h-12 w-28 rounded-lg" />)}
                </div>
                <Card className="border-0 shadow-none bg-transparent">
                    <div className="flex items-center justify-between mb-4">
                        <div className="space-y-2">
                            <Skeleton className="h-6 w-48" />
                            <Skeleton className="h-4 w-72" />
                        </div>
                        <Skeleton className="h-9 w-32" />
                    </div>
                    <div className="space-y-2">
                        {[1, 2, 3, 4, 5].map(i => <Skeleton key={i} className="h-16 w-full rounded-xl" />)}
                    </div>
                </Card>
            </div>
        )
    }

    return (
        <div className="space-y-4 md:space-y-6">
            <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-4 md:space-y-6">
                <div className="relative">
                    <TabsList className="w-full justify-start h-auto p-0 bg-transparent border-b rounded-none gap-3 md:gap-6 mb-3 md:mb-6 overflow-x-auto no-scrollbar">
                        <TabsTrigger
                            value="inbounds"
                            className="rounded-none border-b-2 border-transparent px-1.5 py-2.5 md:px-2 md:py-3 data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none transition-colors hover:text-foreground/80"
                        >
                            <HiOutlineGlobeAlt className="w-4 h-4 mr-1.5 md:mr-2" />
                            Inbounds
                            <Badge variant="secondary" className="ml-2 px-1.5 py-0 text-xs bg-muted/50 hidden sm:inline-flex">{inbounds.length}</Badge>
                        </TabsTrigger>
                        <TabsTrigger
                            value="outbounds"
                            className="rounded-none border-b-2 border-transparent px-1.5 py-2.5 md:px-2 md:py-3 data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none transition-colors hover:text-foreground/80"
                        >
                            <HiOutlineSwitchHorizontal className="w-4 h-4 mr-1.5 md:mr-2" />
                            Outbounds
                            <Badge variant="secondary" className="ml-2 px-1.5 py-0 text-xs bg-muted/50 hidden sm:inline-flex">{outbounds.length}</Badge>
                        </TabsTrigger>
                        <TabsTrigger
                            value="routing"
                            className="rounded-none border-b-2 border-transparent px-1.5 py-2.5 md:px-2 md:py-3 data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none transition-colors hover:text-foreground/80"
                        >
                            <HiOutlineMap className="w-4 h-4 mr-1.5 md:mr-2" />
                            Routing
                            <Badge variant="secondary" className="ml-2 px-1.5 py-0 text-xs bg-muted/50 hidden sm:inline-flex">{routingRules.length}</Badge>
                        </TabsTrigger>
                        <TabsTrigger
                            value="reverse"
                            className="rounded-none border-b-2 border-transparent px-1.5 py-2.5 md:px-2 md:py-3 data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none transition-colors hover:text-foreground/80"
                        >
                            <HiOutlineArrowsExpand className="w-4 h-4 mr-1.5 md:mr-2" />
                            Reverse
                            <Badge variant="secondary" className="ml-2 px-1.5 py-0 text-xs bg-muted/50 hidden sm:inline-flex">{reverseProxies.length}</Badge>
                        </TabsTrigger>
                        <TabsTrigger
                            value="dns"
                            className="rounded-none border-b-2 border-transparent px-1.5 py-2.5 md:px-2 md:py-3 data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none transition-colors hover:text-foreground/80"
                        >
                            <HiOutlineGlobeAlt className="w-4 h-4 mr-1.5 md:mr-2" />
                            DNS
                        </TabsTrigger>
                        <TabsTrigger
                            value="settings"
                            className="rounded-none border-b-2 border-transparent px-1.5 py-2.5 md:px-2 md:py-3 data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none transition-colors hover:text-foreground/80"
                        >
                            <HiOutlineAdjustments className="w-4 h-4 mr-1.5 md:mr-2" />
                            Settings
                        </TabsTrigger>
                        <TabsTrigger
                            value="config"
                            className="rounded-none border-b-2 border-transparent px-1.5 py-2.5 md:px-2 md:py-3 data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none transition-colors hover:text-foreground/80"
                        >
                            <HiOutlineCode className="w-4 h-4 mr-1.5 md:mr-2" />
                            Xray Config
                        </TabsTrigger>
                    </TabsList>
                    <div className="absolute right-0 top-0 h-full w-8 bg-gradient-to-l from-background to-transparent pointer-events-none md:hidden" />
                </div>

                {/* Inbounds Content */}
                <TabsContent value="inbounds" className="animate-in fade-in-50 duration-300">
                    <TooltipProvider delayDuration={200}>
                        <div className="space-y-4">
                            {/* Mobile compact header */}
                            <div className="flex md:hidden items-center justify-between">
                                <div className="flex items-center gap-2">
                                    {inbounds.length > 0 && (
                                        <>
                                            <Checkbox
                                                id="select-all-inbounds-mobile"
                                                checked={selectedInbounds.size === inbounds.length && inbounds.length > 0}
                                                onCheckedChange={toggleAllInbounds}
                                                className="data-[state=checked]:bg-primary data-[state=checked]:border-primary"
                                            />
                                            <label htmlFor="select-all-inbounds-mobile" className="text-sm font-medium text-muted-foreground cursor-pointer select-none whitespace-nowrap">
                                                {selectedInbounds.size > 0 ? `${selectedInbounds.size} selected` : "Select All"}
                                            </label>
                                        </>
                                    )}
                                </div>
                                <DropdownMenu>
                                    <DropdownMenuTrigger asChild>
                                        <Button variant="ghost" size="icon" className="h-8 w-8">
                                            <HiOutlineDotsVertical className="w-4 h-4" />
                                        </Button>
                                    </DropdownMenuTrigger>
                                    <DropdownMenuContent align="end">
                                        <DropdownMenuItem onClick={handleDiscover} disabled={actionLoading === "discover"}>
                                            <HiOutlineDownload className="w-4 h-4 mr-2" />
                                            Discover
                                        </DropdownMenuItem>
                                        <DropdownMenuItem onClick={handleSync} disabled={actionLoading === "sync"}>
                                            <HiOutlineUpload className="w-4 h-4 mr-2" />
                                            Sync
                                        </DropdownMenuItem>
                                    </DropdownMenuContent>
                                </DropdownMenu>
                            </div>

                            {/* Desktop header with actions */}
                            <div className="hidden md:flex flex-col sm:flex-row sm:items-center justify-between gap-4 sm:gap-0">
                                <div>
                                    <h3 className="text-lg font-semibold">Inbound Connections</h3>
                                    <p className="text-sm text-muted-foreground">
                                        Manage incoming connection endpoints
                                        {inbounds.length > 0 && <span className="text-muted-foreground/60"> • {inbounds.length} total</span>}
                                    </p>
                                </div>
                                <div className="flex items-center gap-2">
                                    {inbounds.length > 0 && (
                                        <div className="flex items-center gap-2 mr-2 px-2">
                                            <Checkbox
                                                id="select-all-inbounds"
                                                checked={selectedInbounds.size === inbounds.length && inbounds.length > 0}
                                                onCheckedChange={toggleAllInbounds}
                                                className="data-[state=checked]:bg-primary data-[state=checked]:border-primary"
                                            />
                                            <label htmlFor="select-all-inbounds" className="text-sm font-medium text-muted-foreground cursor-pointer select-none whitespace-nowrap">
                                                Select All
                                            </label>
                                        </div>
                                    )}
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        onClick={handleDiscover}
                                        disabled={actionLoading === "discover"}
                                    >
                                        <HiOutlineDownload className={cn("w-4 h-4 mr-2", actionLoading === "discover" && "animate-spin")} />
                                        Discover
                                    </Button>
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        onClick={handleSync}
                                        disabled={actionLoading === "sync"}
                                    >
                                        <HiOutlineUpload className={cn("w-4 h-4 mr-2", actionLoading === "sync" && "animate-spin")} />
                                        Sync
                                    </Button>
                                    <Button
                                        size="sm"
                                        onClick={() => setInboundDialog({ open: true, mode: "create", inbound: null })}
                                    >
                                        <HiOutlinePlus className="w-4 h-4 mr-2" />
                                        Add Inbound
                                    </Button>
                                </div>
                            </div>

                            {/* Inbounds List */}
                            {inbounds.length > 0 ? (
                                <div className="space-y-3">


                                    <BulkActionBar
                                        count={selectedInbounds.size}
                                        onCancel={clearSelection}
                                        onDisable={() => handleBulkToggle(true)}
                                        onEnable={() => handleBulkToggle(false)}
                                        onDelete={handleBulkDelete}
                                        actionLoading={actionLoading}
                                    />

                                    {/* Mobile Inbound List */}
                                    <div className="md:hidden divide-y rounded-xl border overflow-hidden">
                                        {inbounds.map((inbound) => {
                                            const inboundAccounts = accountsByInbound.get(inbound.id) || []
                                            const counts = inboundCounts.get(inbound.id)
                                            const accountCount = inboundAccounts.length
                                            const onlineCount = counts?.online || 0
                                            const totalTraffic = counts?.trafficBytes || 0

                                            return (
                                                <SwipeableInboundRow
                                                    key={inbound.id}
                                                    inbound={inbound}
                                                    accountCount={accountCount}
                                                    onlineCount={onlineCount}
                                                    totalTraffic={totalTraffic}
                                                    isSelected={selectedInbounds.has(inbound.id)}
                                                    isMultiSelectMode={selectedInbounds.size > 0}
                                                    shouldClose={openSwipeRowId !== null && openSwipeRowId !== inbound.id}
                                                    isExpanded={expandedRows.has(inbound.id)}
                                                    onOpen={setOpenSwipeRowId}
                                                    onTap={() => toggleExpand(inbound.id)}
                                                    onToggleSelect={toggleInboundSelection}
                                                    onLongPress={(id) => { toggleInboundSelection(id) }}
                                                    onEdit={(inb) => setInboundDialog({ open: true, mode: "edit", inbound: inb })}
                                                    onCopy={async (inb) => { await copyToClipboard(`${inb.tag}:${inb.port}`); toast.success("Copied") }}
                                                    onToggleDisabled={handleToggleInbound}
                                                    onDelete={handleDeleteInbound}
                                                    onMigrate={handleMigrateInbound}
                                                    expandedContent={
                                                        <div className="space-y-4">
                                                            <HostList
                                                                inboundId={inbound.id}
                                                                initialHosts={inbound.hosts}
                                                            />
                                                            <InboundAccountsRow
                                                                accounts={inboundAccounts}
                                                                nodeId={nodeId}
                                                                isOnline={isOnline}
                                                                onAccountChange={refetchAccounts}
                                                            />
                                                        </div>
                                                    }
                                                />
                                            )
                                        })}
                                    </div>

                                    {/* Desktop Inbound Cards */}
                                    <div className="hidden md:block space-y-3">
                                        {inbounds.map((inbound) => {
                                            // Build details
                                            const details: { label: string; value: string }[] = []
                                            if (inbound.security === "tls" && inbound.tls_settings?.serverName) {
                                                details.push({ label: "SNI", value: inbound.tls_settings.serverName })
                                            }
                                            if (inbound.security === "reality" && inbound.reality_settings?.serverNames?.[0]) {
                                                details.push({ label: "SNI", value: inbound.reality_settings.serverNames[0] })
                                            }
                                            if (inbound.transport_settings?.host) {
                                                details.push({ label: "Host", value: inbound.transport_settings.host })
                                            }
                                            if (inbound.transport_settings?.path) {
                                                details.push({ label: "Path", value: inbound.transport_settings.path })
                                            }
                                            if (inbound.transport_settings?.serviceName) {
                                                details.push({ label: "Service", value: inbound.transport_settings.serviceName })
                                            }

                                            const inboundAccounts = accountsByInbound.get(inbound.id) || []
                                            const counts = inboundCounts.get(inbound.id)
                                            const isExpanded = expandedRows.has(inbound.id)
                                            const isSelected = selectedInbounds.has(inbound.id)
                                            const accountCount = inboundAccounts.length
                                            const onlineCount = counts?.online || 0
                                            const hasOnline = onlineCount > 0

                                            return (
                                                <DesktopInboundRow
                                                    key={inbound.id}
                                                    inbound={inbound}
                                                    isExpanded={isExpanded}
                                                    isSelected={isSelected}
                                                    accountCount={accountCount}
                                                    onlineCount={onlineCount}
                                                    hasOnline={hasOnline}
                                                    counts={counts}
                                                    details={details}
                                                    onToggleExpand={() => toggleExpand(inbound.id)}
                                                    onToggleSelect={() => toggleInboundSelection(inbound.id)}
                                                    onToggleDisabled={() => handleToggleInbound(inbound)}
                                                    onEdit={() => setInboundDialog({ open: true, mode: "edit", inbound })}
                                                    onMigrate={() => handleMigrateInbound(inbound)}
                                                    onDelete={() => handleDeleteInbound(inbound)}
                                                    expandedContent={
                                                        <>
                                                            <HostList inboundId={inbound.id} initialHosts={inbound.hosts} />
                                                            <InboundAccountsRow
                                                                accounts={inboundAccounts}
                                                                nodeId={nodeId}
                                                                isOnline={isOnline}
                                                                onAccountChange={refetchAccounts}
                                                            />
                                                        </>
                                                    }
                                                />
                                            )
                                        })}
                                    </div>
                                </div>
                            ) : (
                                <div className="flex flex-col items-center justify-center py-16 text-muted-foreground rounded-2xl border-2 border-dashed bg-muted/5">
                                    <HiOutlineGlobeAlt className="w-12 h-12 opacity-50 mb-4" />
                                    <h3 className="text-lg font-medium text-foreground mb-1">No Inbounds Configured</h3>
                                    <p className="text-sm mb-4">Create an inbound or discover from Xray</p>
                                    <div className="flex gap-2">
                                        <Button variant="outline" size="sm" onClick={handleDiscover}>
                                            <HiOutlineDownload className="w-4 h-4 mr-2" />
                                            Discover
                                        </Button>
                                        <Button size="sm" onClick={() => setInboundDialog({ open: true, mode: "create", inbound: null })}>
                                            <HiOutlinePlus className="w-4 h-4 mr-2" />
                                            Add Inbound
                                        </Button>
                                    </div>
                                </div>
                            )}
                        </div>
                    </TooltipProvider>
                </TabsContent>

                {/* Outbounds Content */}
                <TabsContent value="outbounds" className="animate-in fade-in-50 duration-300">
                    <Card className="border-0 shadow-none bg-transparent">
                        {/* Mobile header - just title */}
                        <div className="flex md:hidden items-center justify-between mb-4">
                            <h3 className="text-lg font-semibold">Outbounds</h3>
                        </div>
                        {/* Desktop header */}
                        <div className="hidden md:flex items-center justify-between mb-4">
                            <div>
                                <h3 className="text-lg font-semibold">Outbound Defaults</h3>
                                <p className="text-sm text-muted-foreground">Manage upstream proxies and routing targets</p>
                            </div>
                            <Button size="sm" onClick={() => setOutboundDialog({ open: true, mode: "create", outbound: null })}>
                                <HiOutlinePlus className="w-4 h-4 mr-2" />
                                Add Outbound
                            </Button>
                        </div>

                        {/* Mobile outbound list */}
                        {outbounds.length > 0 && (
                            <div className="md:hidden divide-y rounded-xl border overflow-hidden mb-4">
                                {outbounds.map((outbound) => (
                                    <SwipeableOutboundRow
                                        key={outbound.id}
                                        outbound={outbound}
                                        shouldClose={openSwipeOutboundId !== null && openSwipeOutboundId !== outbound.id}
                                        onOpen={setOpenSwipeOutboundId}
                                        onEdit={(ob) => setOutboundDialog({ open: true, mode: "edit", outbound: ob })}
                                        onDelete={handleDeleteOutbound}
                                        onToggleDisabled={handleToggleOutbound}
                                        onTest={handleTestOutbound}
                                        isTesting={testingOutboundId === outbound.id}
                                        testResult={testResults.get(outbound.id) ?? null}
                                        onViewTestResult={(ob) => setTestResultDialog({ open: true, outboundId: ob.id, outboundTag: ob.tag })}
                                    />
                                ))}
                            </div>
                        )}
                        {outbounds.length === 0 && (
                            <div className="md:hidden flex flex-col items-center justify-center py-12 text-muted-foreground">
                                <HiOutlineSwitchHorizontal className="w-12 h-12 opacity-50 mb-4" />
                                <p>No outbounds configured</p>
                            </div>
                        )}

                        {/* Desktop outbound table */}
                        <TooltipProvider>
                        <div className="hidden md:block rounded-2xl border bg-card/50 backdrop-blur-sm border-white/5 overflow-hidden">
                            {outbounds.length > 0 ? (
                                <Table className="[&_th]:border-r [&_th]:border-border/40 [&_th:last-child]:border-r-0 [&_td]:border-r [&_td]:border-border/40 [&_td:last-child]:border-r-0">
                                    <TableHeader>
                                        <TableRow className="bg-muted/50">
                                            <TableHead className="w-[200px]">Outbound</TableHead>
                                            <TableHead>Configuration</TableHead>
                                            <TableHead>Details</TableHead>
                                            <TableHead className="w-[140px] text-center">Traffic</TableHead>
                                            <TableHead className="w-[160px] text-center">Test Result</TableHead>
                                            <TableHead className="w-[60px] text-center">Test</TableHead>
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {outbounds.map((outbound) => {
                                            // Build destination display
                                            let destination = "—"
                                            if (outbound.address && outbound.port) {
                                                destination = `${outbound.address}:${outbound.port}`
                                            } else if (outbound.protocol === "freedom") {
                                                destination = "Direct"
                                            } else if (outbound.protocol === "blackhole") {
                                                destination = "Blocked"
                                            }

                                            // Build details string
                                            const details: string[] = []
                                            if (outbound.security === "tls" && outbound.tls_settings?.serverName) {
                                                details.push(`SNI: ${outbound.tls_settings.serverName}`)
                                            }
                                            if (outbound.security === "reality" && outbound.reality_settings?.serverNames?.[0]) {
                                                details.push(`SNI: ${outbound.reality_settings.serverNames[0]}`)
                                            }
                                            if (outbound.transport_settings?.host) {
                                                details.push(`Host: ${outbound.transport_settings.host}`)
                                            }
                                            if (outbound.transport_settings?.path) {
                                                details.push(`Path: ${outbound.transport_settings.path}`)
                                            }
                                            if (outbound.vless_settings?.flow) {
                                                details.push(`Flow: ${outbound.vless_settings.flow}`)
                                            }

                                            // Special styling for freedom/blackhole
                                            const isSpecial = outbound.protocol === "freedom" || outbound.protocol === "blackhole"
                                            const result = testResults.get(outbound.id)
                                            const hasTraffic = (outbound.uplink && outbound.uplink > 0) || (outbound.downlink && outbound.downlink > 0)

                                            return (
                                                <TableRow key={outbound.id} className={cn("group", outbound.is_disabled && "opacity-50")}>
                                                    {/* Outbound */}
                                                    <TableCell>
                                                        <div className="flex items-start gap-2">
                                                            <DropdownMenu>
                                                                <DropdownMenuTrigger asChild>
                                                                    <button aria-label="Outbound actions" className="p-1 rounded hover:bg-muted/80 text-muted-foreground hover:text-foreground transition-colors mt-0.5 shrink-0 opacity-60 group-hover:opacity-100 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                                                                        <HiOutlineDotsVertical className="w-4 h-4" />
                                                                    </button>
                                                                </DropdownMenuTrigger>
                                                                <DropdownMenuContent align="start">
                                                                    <DropdownMenuItem onClick={() => setOutboundDialog({ open: true, mode: "edit", outbound })}>
                                                                        <HiOutlinePencil className="w-4 h-4 mr-2" />
                                                                        Edit
                                                                    </DropdownMenuItem>
                                                                    <DropdownMenuItem onClick={() => handleToggleOutbound(outbound)}>
                                                                        {outbound.is_disabled ? <Power className="w-4 h-4 mr-2" /> : <HiOutlineBan className="w-4 h-4 mr-2" />}
                                                                        {outbound.is_disabled ? "Enable" : "Disable"}
                                                                    </DropdownMenuItem>
                                                                    <DropdownMenuItem onClick={() => handleDeleteOutbound(outbound)} className="text-red-500 focus:text-red-500">
                                                                        <HiOutlineTrash className="w-4 h-4 mr-2" />
                                                                        Delete
                                                                    </DropdownMenuItem>
                                                                </DropdownMenuContent>
                                                            </DropdownMenu>
                                                            <div className="space-y-1 min-w-0">
                                                                <div className="flex items-center gap-2">
                                                                    <span className="font-mono font-semibold">{outbound.tag}</span>
                                                                </div>
                                                                <p className={cn(
                                                                    "text-xs font-mono truncate max-w-[160px]",
                                                                    isSpecial ? "text-primary font-medium" : "text-muted-foreground"
                                                                )}>
                                                                    {destination}
                                                                </p>
                                                                {outbound.remark && (
                                                                    <p className="text-xs text-muted-foreground/70 truncate max-w-[160px]">{outbound.remark}</p>
                                                                )}
                                                            </div>
                                                        </div>
                                                    </TableCell>
                                                    {/* Configuration */}
                                                    <TableCell>
                                                        <div className="flex flex-wrap gap-1.5">
                                                            <ProtocolBadge protocol={outbound.protocol} />
                                                            {!isSpecial && (
                                                                <>
                                                                    <Badge variant="outline" className="text-xs">{outbound.network || "tcp"}</Badge>
                                                                    <Badge
                                                                        variant={outbound.security === "reality" ? "success" : outbound.security === "tls" ? "default" : "secondary"}
                                                                        className="text-xs"
                                                                    >
                                                                        {outbound.security || "none"}
                                                                    </Badge>
                                                                </>
                                                            )}
                                                        </div>
                                                    </TableCell>
                                                    {/* Details */}
                                                    <TableCell>
                                                        {details.length > 0 ? (
                                                            <p className="text-xs text-muted-foreground truncate max-w-[250px]">
                                                                {details.join(" • ")}
                                                            </p>
                                                        ) : (
                                                            <span className="text-xs text-muted-foreground/50">—</span>
                                                        )}
                                                    </TableCell>
                                                    {/* Traffic */}
                                                    <TableCell className="text-center">
                                                        {hasTraffic ? (
                                                            <Badge variant="outline" className="text-xs font-mono bg-emerald-500/5 border-emerald-500/20 text-emerald-400">
                                                                <HiOutlineUpload className="w-3 h-3 mr-1 inline" />
                                                                {formatBytes(outbound.uplink || 0)}
                                                                <span className="mx-1 text-muted-foreground/50">/</span>
                                                                {formatBytes(outbound.downlink || 0)}
                                                                <HiOutlineDownload className="w-3 h-3 ml-1 inline" />
                                                            </Badge>
                                                        ) : (
                                                            <span className="text-xs text-muted-foreground/50">—</span>
                                                        )}
                                                    </TableCell>
                                                    {/* Test Result */}
                                                    <TableCell className="text-center">
                                                        {result ? (
                                                            <button
                                                                onClick={() => setTestResultDialog({ open: true, outboundId: outbound.id, outboundTag: outbound.tag })}
                                                                className="inline-flex flex-col items-center gap-0.5 cursor-pointer group/result hover:opacity-80 transition-opacity"
                                                            >
                                                                {result.success ? (
                                                                    <HiOutlineCheckCircle className="w-5 h-5 text-emerald-500" />
                                                                ) : (
                                                                    <HiOutlineXCircle className="w-5 h-5 text-red-500" />
                                                                )}
                                                                {result.latency_ms > 0 && (
                                                                    <span className={cn(
                                                                        "text-[10px] font-mono font-medium",
                                                                        result.latency_ms < 300 ? "text-emerald-500" :
                                                                        result.latency_ms < 800 ? "text-amber-500" : "text-red-500"
                                                                    )}>
                                                                        {result.latency_ms}ms
                                                                    </span>
                                                                )}
                                                            </button>
                                                        ) : (
                                                            <span className="text-xs text-muted-foreground/50">—</span>
                                                        )}
                                                    </TableCell>
                                                    {/* Test Action */}
                                                    <TableCell className="text-center">
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <Button
                                                                    variant="ghost"
                                                                    size="icon"
                                                                    onClick={() => handleTestOutbound(outbound)}
                                                                    disabled={testingOutboundId === outbound.id}
                                                                    className="text-emerald-500 hover:text-emerald-600 hover:bg-emerald-500/10"
                                                                >
                                                                    <HiOutlineStatusOnline className={cn("w-5 h-5", testingOutboundId === outbound.id && "animate-pulse")} />
                                                                </Button>
                                                            </TooltipTrigger>
                                                            <TooltipContent>Test connectivity</TooltipContent>
                                                        </Tooltip>
                                                    </TableCell>
                                                </TableRow>
                                            )
                                        })}
                                    </TableBody>
                                </Table>
                            ) : (
                                <div className="flex flex-col items-center justify-center py-16 text-muted-foreground rounded-2xl border-2 border-dashed bg-muted/5">
                                    <HiOutlineSwitchHorizontal className="w-12 h-12 opacity-50 mb-4" />
                                    <h3 className="text-lg font-medium text-foreground mb-1">No Outbounds Configured</h3>
                                    <p className="text-sm mb-4">Add an upstream proxy or routing target</p>
                                    <Button size="sm" onClick={() => setOutboundDialog({ open: true, mode: "create", outbound: null })}>
                                        <HiOutlinePlus className="w-4 h-4 mr-2" />
                                        Add Outbound
                                    </Button>
                                </div>
                            )}
                        </div>
                        </TooltipProvider>
                    </Card>
                </TabsContent>

                {/* Routing Content */}
                <TabsContent value="routing" className="animate-in fade-in-50 duration-300">
                    <RoutingSettingsCard
                        nodeId={nodeId}
                        existingRules={routingRules}
                        outbounds={outbounds}
                        onSettingsSaved={() => queryClient.invalidateQueries({ queryKey: queryKeys.nodeRouting(nodeId) })}
                        onPresetRulesChanged={setPendingPresetRules}
                    />
                    <RoutingRulesTable
                        nodeId={nodeId}
                        rules={displayRoutingRules}
                        onEdit={(rule) => setRoutingDialog({ open: true, mode: "edit", rule })}
                        onDelete={handleDeleteRule}
                        onToggle={handleToggleRule}
                        onReorderSaved={() => {
                            queryClient.invalidateQueries({ queryKey: queryKeys.nodeRouting(nodeId) })
                            onRefresh?.()
                        }}
                        onCreate={() => setRoutingDialog({ open: true, mode: "create", rule: null })}
                    />
                    <BalancingRulesCard
                        nodeId={nodeId}
                        outbounds={outbounds}
                        rules={balancingRules}
                        onRulesChanged={() => {
                            queryClient.invalidateQueries({ queryKey: queryKeys.nodeBalancingRules(nodeId) })
                            onRefresh?.()
                        }}
                    />
                </TabsContent>

                {/* Reverse Proxy Content */}
                <TabsContent value="reverse" className="animate-in fade-in-50 duration-300">
                    <ReverseProxyTable
                        reverseProxies={reverseProxies}
                        onEdit={(rp) => setReverseProxyDialog({ open: true, mode: "edit", reverseProxy: rp })}
                        onDelete={handleDeleteReverseProxy}
                        onCreate={() => setReverseProxyDialog({ open: true, mode: "create", reverseProxy: null })}
                    />
                </TabsContent>

                {/* DNS Content */}
                <TabsContent value="dns" className="animate-in fade-in-50 duration-300">
                    <DNSSettingsCard nodeId={nodeId} />
                </TabsContent>

                {/* Settings Content - NEW */}
                <TabsContent value="settings" className="animate-in fade-in-50 duration-300">
                    <NodeSettingsXray nodeId={nodeId} isOnline={true} />
                </TabsContent>

                {/* Xray Config Content */}
                <TabsContent value="config" className="animate-in fade-in-50 duration-300">
                    <NodeXrayConfigEditor nodeId={nodeId} isOnline={true} />
                </TabsContent>
                {/* Mobile FAB - tab-aware */}
                <AnimatePresence>
                    {activeTab === "inbounds" && selectedInbounds.size === 0 && (
                        <motion.button
                            className="fixed bottom-[100px] right-6 z-40 md:hidden w-14 h-14 rounded-full bg-primary text-primary-foreground shadow-lg flex items-center justify-center active:scale-95"
                            onClick={() => setInboundDialog({ open: true, mode: "create", inbound: null })}
                            initial={{ scale: 0, opacity: 0 }}
                            animate={{ scale: 1, opacity: 1 }}
                            exit={{ scale: 0, opacity: 0 }}
                            transition={{ type: "spring", stiffness: 300, damping: 25 }}
                        >
                            <HiOutlinePlus className="w-6 h-6" />
                        </motion.button>
                    )}
                    {activeTab === "outbounds" && (
                        <motion.button
                            className="fixed bottom-[100px] right-6 z-40 md:hidden w-14 h-14 rounded-full bg-primary text-primary-foreground shadow-lg flex items-center justify-center active:scale-95"
                            onClick={() => setOutboundDialog({ open: true, mode: "create", outbound: null })}
                            initial={{ scale: 0, opacity: 0 }}
                            animate={{ scale: 1, opacity: 1 }}
                            exit={{ scale: 0, opacity: 0 }}
                            transition={{ type: "spring", stiffness: 300, damping: 25 }}
                        >
                            <HiOutlinePlus className="w-6 h-6" />
                        </motion.button>
                    )}
                    {activeTab === "routing" && (
                        <motion.button
                            className="fixed bottom-[100px] right-6 z-40 md:hidden w-14 h-14 rounded-full bg-primary text-primary-foreground shadow-lg flex items-center justify-center active:scale-95"
                            onClick={() => setRoutingDialog({ open: true, mode: "create", rule: null })}
                            initial={{ scale: 0, opacity: 0 }}
                            animate={{ scale: 1, opacity: 1 }}
                            exit={{ scale: 0, opacity: 0 }}
                            transition={{ type: "spring", stiffness: 300, damping: 25 }}
                        >
                            <HiOutlinePlus className="w-6 h-6" />
                        </motion.button>
                    )}
                </AnimatePresence>
            </Tabs>

            <InboundSettingsDialog
                open={inboundDialog.open}
                onOpenChange={(open) => setInboundDialog(prev => ({ ...prev, open }))}
                inbound={inboundDialog.inbound}
                nodeId={nodeId}
                onSave={handleSaveInbound}
                mode={inboundDialog.mode}
            />

            <OutboundSettingsDialog
                open={outboundDialog.open}
                onOpenChange={(open) => setOutboundDialog(prev => ({ ...prev, open }))}
                outbound={outboundDialog.outbound}
                nodeId={nodeId}
                onSave={handleSaveOutbound}
                mode={outboundDialog.mode}
                allOutbounds={outbounds}
            />

            <RoutingRuleDialog
                open={routingDialog.open}
                onOpenChange={(open) => setRoutingDialog(prev => ({ ...prev, open }))}
                rule={routingDialog.rule}
                nodeId={nodeId}
                outbounds={outbounds}
                inbounds={inbounds}
                balancingRules={balancingRules}
                onSave={handleSaveRoutingRule}
                mode={routingDialog.mode}
            />

            <OutboundTestResultDialog
                open={testResultDialog.open}
                onOpenChange={(open) => setTestResultDialog(prev => ({ ...prev, open }))}
                result={testResultDialog.outboundId !== null ? testResults.get(testResultDialog.outboundId) ?? null : null}
                outboundTag={testResultDialog.outboundTag}
            />

            <ReverseProxyDialog
                open={reverseProxyDialog.open}
                onOpenChange={(open) => !open && setReverseProxyDialog({ open: false, mode: "create", reverseProxy: null })}
                mode={reverseProxyDialog.mode}
                reverseProxy={reverseProxyDialog.reverseProxy}
                inboundTags={inbounds.map(i => i.tag)}
                outboundTags={outbounds.map(o => o.tag)}
                existingCount={reverseProxies.length}
                onSave={handleSaveReverseProxy}
            />

            {migrateDialog.inbound && (
                <MigrateInboundDialog
                    open={migrateDialog.open}
                    onOpenChange={(open) => {
                        setMigrateDialog({ ...migrateDialog, open })
                        if (!open) {
                            queryClient.invalidateQueries({ queryKey: [...queryKeys.nodes, "inbounds"], exact: false })
                        }
                    }}
                    sourceInbound={migrateDialog.inbound}
                    allNodes={allNodesData || []}
                    allInbounds={allInboundsMap}
                    accountCount={(accountsByInbound.get(migrateDialog.inbound.id) || []).length}
                />
            )}
        </div >
    )
}
