import React, { useState, useMemo } from "react"
import { useSearchParams } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { queryKeys } from "@/lib/queries/keys"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
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
    HiOutlinePlus,
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
import type { Inbound, Outbound, OutboundTestEntry, RoutingRule, ReverseProxy } from "@/lib/types"
import { deleteNodeInbound } from "@/lib/api/nodes"
import { toggleInboundDisabled } from "@/lib/api/inbounds"
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
    useOutboundTestSettings,
    useAddRoutingRule,
    useUpdateRoutingRule,
    useDeleteRoutingRule,
    useToggleRoutingRule,
    useAddReverseProxy,
    useUpdateReverseProxy,
    useDeleteReverseProxy,
} from "@/lib/queries/use-nodes"
import { listNodeInbounds } from "@/lib/api/nodes"
import { NodeXrayConfigEditor } from "./node-xray-config-editor"
import { NodeSettingsXray } from "./settings/node-settings-xray"
import { HiOutlineCode, HiOutlineAdjustments } from "react-icons/hi"
import { useAccountsByNode } from "@/lib/queries/use-accounts"
import { type Account as NodeAccount } from "@/lib/api/accounts"
import { AnimatePresence, motion } from "framer-motion"
import { SwipeableInboundRow } from "./swipeable-inbound-row"
import { SwipeableOutboundRow } from "./swipeable-outbound-row"
import { RoutingRulesTable } from "@/components/routing/routing-rules-table"
import { OutboundTestResultDialog } from "@/components/outbound/outbound-test-result-dialog"
import { OutboundTestSettingsCard } from "@/components/outbound/outbound-test-settings-card"
import { BulkActionBar } from "./network/bulk-action-bar"
import { DesktopInboundRow } from "./network/desktop-inbound-row"
import { InboundListHeader, type InboundSortField, type SortDir } from "./network/inbound-list-header"
import { InboundSectionHeader, type InboundSummary } from "./network/inbound-section-header"
import { InboundDetailPanel } from "./network/inbound-detail-panel"
import { buildInboundDetails } from "./network/inbound-details"
import { deleteNodeOutbound, toggleOutboundDisabled } from "@/lib/api/outbounds"
import { DesktopOutboundRow } from "./network/desktop-outbound-row"
import { OutboundListHeader, type OutboundSortField } from "./network/outbound-list-header"
import { OutboundSectionHeader, type OutboundSummary } from "./network/outbound-section-header"
import { ManagedOutboundList } from "./network/managed-outbound-list"
import { OutboundDetailPanel } from "./network/outbound-detail-panel"
import {
    buildOutboundDetails,
    buildOutboundUsage,
    defaultOutboundTag,
    partitionOutbounds,
    canTestOutbound,
    describeOutboundRoute,
    outboundTraffic,
    testEntryOf,
    usageCount,
} from "./network/outbound-details"

// Mirrors Outbound.IsTestable() on the server. Blackhole discards traffic by
// design, dns and loopback are internal routing targets, and an http proxy
// outbound has no share-link the tester's core accepts.
const UNTESTABLE_OUTBOUND_PROTOCOLS = ["blackhole", "dns", "loopback", "http"]
const TESTABLE_OUTBOUND_PROTOCOLS = (protocol: string) => !UNTESTABLE_OUTBOUND_PROTOCOLS.includes(protocol)
const DEFAULT_TEST_CONCURRENCY = 4

const ROUTING_VIEWS = [
    { value: "rules", label: "Rules" },
    { value: "presets", label: "Presets" },
    { value: "balancers", label: "Balancers" },
] as const

type RoutingView = (typeof ROUTING_VIEWS)[number]["value"]

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
    const [selectedOutbounds, setSelectedOutbounds] = useState<Set<number>>(new Set())
    const [expandedOutbounds, setExpandedOutbounds] = useState<Set<number>>(new Set())
    const [mountedOutboundPanels, setMountedOutboundPanels] = useState<Set<number>>(new Set())
    const [outboundSort, setOutboundSort] = useState<{ field: OutboundSortField | null; dir: SortDir }>({
        field: null,
        dir: "desc",
    })

    // ESC key clears bulk selection on whichever list has one
    React.useEffect(() => {
        if (selectedInbounds.size === 0 && selectedOutbounds.size === 0) return
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                setSelectedInbounds(new Set())
                setSelectedOutbounds(new Set())
            }
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [selectedInbounds.size, selectedOutbounds.size])

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

    // Routing pane persisted in URL via ?view= — rules, the presets that write
    // rules, and the balancers those rules target are three separate jobs.
    const rawView = searchParams.get("view") || "rules"
    const routingView = (ROUTING_VIEWS.some(v => v.value === rawView) ? rawView : "rules") as RoutingView
    const setRoutingView = (value: RoutingView) => {
        const next = new URLSearchParams(searchParams)
        next.set("subtab", "routing")
        next.set("view", value)
        setSearchParams(next, { replace: true })
    }

    const [openSwipeRowId, setOpenSwipeRowId] = useState<number | null>(null)
    const [openSwipeOutboundId, setOpenSwipeOutboundId] = useState<number | null>(null)

    // Data via React Query — caches across tab switches and dedupes on refetch
    const { data: inbounds = [], isLoading: ibLoading } = useNodeInbounds(nodeId)
    const { data: allOutbounds = [], isLoading: obLoading } = useNodeOutbounds(nodeId)
    const { outbounds, managedOutbounds } = useMemo(() => partitionOutbounds(allOutbounds), [allOutbounds])
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
    const { data: testSettings } = useOutboundTestSettings(nodeId)
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

    // Inbound list sort. Null field keeps the server's order, which is the one
    // people already know; a header click opts into something else.
    const [inboundSort, setInboundSort] = useState<{ field: InboundSortField | null; dir: SortDir }>({
        field: null,
        dir: "desc",
    })

    const handleInboundSort = (field: InboundSortField) => {
        setInboundSort(prev => prev.field === field
            ? { field, dir: prev.dir === "asc" ? "desc" : "asc" }
            : { field, dir: field === "name" ? "asc" : "desc" })
    }

    const sortedInbounds = useMemo(() => {
        const field = inboundSort.field
        if (!field) return inbounds
        const key = (i: Inbound): string | number => {
            switch (field) {
                case "name": return i.tag.toLowerCase()
                case "clients": return accountsByInbound.get(i.id)?.length ?? 0
                case "traffic": return inboundCounts.get(i.id)?.trafficBytes ?? 0
                case "expired": return inboundCounts.get(i.id)?.expired ?? 0
            }
        }
        return [...inbounds].sort((a, b) => {
            const ka = key(a), kb = key(b)
            const cmp = typeof ka === "string" && typeof kb === "string"
                ? ka.localeCompare(kb)
                : (ka as number) - (kb as number)
            return inboundSort.dir === "asc" ? cmp : -cmp
        })
    }, [inbounds, inboundSort, accountsByInbound, inboundCounts])

    // The header line: what the old "Manage incoming connection endpoints" never said.
    const inboundSummary = useMemo<InboundSummary>(() => {
        let clients = 0, online = 0, expired = 0, trafficBytes = 0
        let lastChangedAt: string | null = null
        inbounds.forEach(i => {
            const c = inboundCounts.get(i.id)
            clients += accountsByInbound.get(i.id)?.length ?? 0
            online += c?.online ?? 0
            expired += c?.expired ?? 0
            trafficBytes += c?.trafficBytes ?? 0
            if (i.updated_at && (!lastChangedAt || i.updated_at > lastChangedAt)) lastChangedAt = i.updated_at
        })
        return {
            total: inbounds.length,
            disabled: inbounds.filter(i => i.is_disabled).length,
            clients, online, expired, trafficBytes, lastChangedAt,
        }
    }, [inbounds, inboundCounts, accountsByInbound])

    // Toggle expand/collapse for inbound row. Panels mount on first open and
    // stay mounted, so a collapsed list does not build six client tables and the
    // close animation still has something to animate.
    const [mountedPanels, setMountedPanels] = useState<Set<number>>(new Set())

    const toggleExpand = (inboundId: number) => {
        setExpandedRows(prev => {
            const next = new Set(prev)
            if (next.has(inboundId)) next.delete(inboundId)
            else next.add(inboundId)
            return next
        })
        setMountedPanels(prev => prev.has(inboundId) ? prev : new Set(prev).add(inboundId))
    }

    // --- Outbound list derivations ---

    // Who routes to each outbound, from the rules and balancers already loaded.
    const outboundUsage = useMemo(
        () => buildOutboundUsage(allOutbounds, routingRules, balancingRules),
        [allOutbounds, routingRules, balancingRules],
    )
    const defaultOutbound = useMemo(() => defaultOutboundTag(allOutbounds), [allOutbounds])
    const outboundRoutes = useMemo(
        () => new Map(outbounds.map(o => [o.id, describeOutboundRoute(o)] as const)),
        [outbounds],
    )
    const outboundDetails = useMemo(
        () => new Map(outbounds.map(o => [o.id, buildOutboundDetails(o)] as const)),
        [outbounds],
    )

    const handleOutboundSort = (field: OutboundSortField) => {
        setOutboundSort(prev => prev.field === field
            ? { field, dir: prev.dir === "asc" ? "desc" : "asc" }
            : { field, dir: field === "name" || field === "test" ? "asc" : "desc" })
    }

    const sortedOutbounds = useMemo(() => {
        const field = outboundSort.field
        if (!field) return outbounds
        const key = (o: Outbound): string | number => {
            switch (field) {
                case "name": return o.tag.toLowerCase()
                case "usage": return usageCount(outboundUsage.get(o.tag))
                case "traffic": return outboundTraffic(o)
                case "test": {
                    // Ascending = best first: passing results by latency, then failures, then untested.
                    const e = testEntryOf(o)
                    if (!e) return 2_000_000
                    return e.result.success ? e.result.latency_ms : 1_000_000
                }
            }
        }
        return [...outbounds].sort((a, b) => {
            const ka = key(a), kb = key(b)
            const cmp = typeof ka === "string" && typeof kb === "string"
                ? ka.localeCompare(kb)
                : (ka as number) - (kb as number)
            return outboundSort.dir === "asc" ? cmp : -cmp
        })
    }, [outbounds, outboundSort, outboundUsage])

    const outboundSummary = useMemo<OutboundSummary>(() => {
        let trafficBytes = 0
        let unused = 0
        let lastTestedAt: string | null = null
        outbounds.forEach(o => {
            trafficBytes += outboundTraffic(o)
            if (o.last_tested_at && (!lastTestedAt || o.last_tested_at > lastTestedAt)) lastTestedAt = o.last_tested_at
            if (!o.is_disabled && o.tag !== defaultOutbound && usageCount(outboundUsage.get(o.tag)) === 0) unused++
        })
        return {
            total: allOutbounds.length,
            disabled: outbounds.filter(o => o.is_disabled).length,
            unused,
            trafficBytes,
            lastTestedAt,
        }
    }, [outbounds, allOutbounds.length, outboundUsage, defaultOutbound])

    const toggleOutboundExpand = (outboundId: number) => {
        setExpandedOutbounds(prev => {
            const next = new Set(prev)
            if (next.has(outboundId)) next.delete(outboundId)
            else next.add(outboundId)
            return next
        })
        setMountedOutboundPanels(prev => prev.has(outboundId) ? prev : new Set(prev).add(outboundId))
    }

    const toggleOutboundSelection = (outboundId: number) => {
        setSelectedOutbounds(prev => {
            const next = new Set(prev)
            if (next.has(outboundId)) next.delete(outboundId)
            else next.add(outboundId)
            return next
        })
    }

    const toggleAllOutbounds = () => {
        setSelectedOutbounds(prev => prev.size === outbounds.length ? new Set() : new Set(outbounds.map(o => o.id)))
    }

    const clearOutboundSelection = () => setSelectedOutbounds(new Set())

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

    // Outbound testing. A set rather than a single id: Test All runs several
    // tests at once, and each row needs its own spinner.
    const [testingIds, setTestingIds] = useState<Set<number>>(new Set())
    const [testAllProgress, setTestAllProgress] = useState<{ done: number; total: number } | null>(null)
    const [testResultDialog, setTestResultDialog] = useState<{
        open: boolean
        outboundId: number | null
        outboundTag: string
    }>({ open: false, outboundId: null, outboundTag: "" })

    // The outbound whose result dialog is open, if any.
    const dialogOutbound = testResultDialog.outboundId !== null
        ? outbounds.find(o => o.id === testResultDialog.outboundId) ?? null
        : null

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

    // Rules the Presets pane owns — shown as its count in the pane switcher.
    const presetRuleCount = useMemo(
        () => routingRules.filter(r => PRESET_RULE_TAGS.has(r.rule_tag)).length,
        [routingRules],
    )

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
        // Let failures reject so the editor retains its values.
        if (inboundDialog.mode === "create") {
            await addInbound.mutateAsync({ nodeId, inbound: data })
        } else {
            await updateInbound.mutateAsync({ nodeId, inboundId: data.id!, inbound: data })
        }
        setInboundDialog({ open: false, mode: "create", inbound: null })
        onRefresh?.()
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
        // Reject on failure so the editor retains its values and Save & test
        // cannot test an old configuration after the save was refused.
        if (outboundDialog.mode === "create") {
            await addOutbound.mutateAsync(data)
        } else {
            if (outboundDialog.outbound?.managed || !data.id) throw new Error("Managed outbounds cannot be edited")
            await updateOutbound.mutateAsync({ outboundId: data.id, outbound: data })
        }
        setOutboundDialog({ open: false, mode: "create", outbound: null })
        onRefresh?.()
    }

    const handleDeleteOutbound = async (outbound: Outbound) => {
        if (outbound.managed || outbound.id <= 0) return
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
        if (outbound.managed || outbound.id <= 0) return
        try {
            await toggleOutbound.mutateAsync(outbound)
            onRefresh?.()
        } catch {
            // mutation surfaces toast
        }
    }

    const markTesting = (outboundId: number, testing: boolean) => {
        setTestingIds(prev => {
            const next = new Set(prev)
            if (testing) next.add(outboundId)
            else next.delete(outboundId)
            return next
        })
    }

    const handleTestOutbound = async (
        outbound: Outbound,
        speedtest = false,
        opts?: { silent?: boolean },
    ): Promise<OutboundTestEntry | null> => {
        if (!canTestOutbound(outbound)) return null
        markTesting(outbound.id, true)
        try {
            const entry = await testOutboundMut.mutateAsync({ outboundId: outbound.id, speedtest })
            // Write straight into the cached list instead of invalidating it:
            // the result is already authoritative, and a refetch per test would
            // hammer the endpoint during Test All.
            queryClient.setQueryData<Outbound[]>(queryKeys.nodeOutbounds(nodeId), prev =>
                prev?.map(o => o.id === outbound.id
                    ? { ...o, last_test_result: entry.result, last_tested_at: entry.tested_at }
                    : o))
            if (!opts?.silent) {
                const { result } = entry
                if (result.success) {
                    toast.success(`${outbound.tag}: ${result.message || `Connected (${result.latency_ms}ms)`}`)
                } else {
                    toast.error(`${outbound.tag}: ${result.error || "Test failed"}`)
                }
            }
            return entry
        } catch (e) {
            if (!opts?.silent) {
                toast.error(e instanceof Error ? e.message : "Failed to test outbound")
            }
            return null
        } finally {
            markTesting(outbound.id, false)
        }
    }

    // A plain worker pool over the per-outbound endpoint. Speedtest is
    // deliberately off here — running it across many upstreams burns real traffic.
    const runTestQueue = async (queue: Outbound[]) => {
        if (queue.length === 0) {
            toast.info("No testable outbounds")
            return
        }

        const concurrency = Math.max(1, testSettings?.concurrency || DEFAULT_TEST_CONCURRENCY)
        setTestAllProgress({ done: 0, total: queue.length })

        const pending = [...queue]
        let passed = 0
        const worker = async () => {
            for (let next = pending.shift(); next; next = pending.shift()) {
                const entry = await handleTestOutbound(next, false, { silent: true })
                if (entry?.result.success) passed++
                setTestAllProgress(prev => (prev ? { ...prev, done: prev.done + 1 } : prev))
            }
        }
        await Promise.all(Array.from({ length: Math.min(concurrency, queue.length) }, worker))

        setTestAllProgress(null)
        const failed = queue.length - passed
        if (failed === 0) {
            toast.success(`All ${queue.length} outbound${queue.length === 1 ? "" : "s"} passed`)
        } else if (passed === 0) {
            toast.error(`All ${queue.length} outbound${queue.length === 1 ? "" : "s"} failed`)
        } else {
            toast.warning(`${passed} passed, ${failed} failed`)
        }
    }

    const testableOutbounds = (source: Outbound[]) =>
        source.filter(o => !o.is_disabled && canTestOutbound(o))

    const handleTestAllOutbounds = () => runTestQueue(testableOutbounds(outbounds))

    const handleBulkOutboundTest = async () => {
        const queue = testableOutbounds(outbounds.filter(o => selectedOutbounds.has(o.id)))
        setSelectedOutbounds(new Set())
        await runTestQueue(queue)
    }

    const handleBulkOutboundDelete = async () => {
        const count = selectedOutbounds.size
        if (count === 0) return
        const selected = outbounds.filter(o => selectedOutbounds.has(o.id))
        const ids = selected.map(o => o.id)
        const referenced = selected.filter(o => usageCount(outboundUsage.get(o.tag)) > 0).length
        const confirmed = await confirm({
            title: `Delete ${count} outbound${count > 1 ? "s" : ""}`,
            description: `This will permanently delete ${count} outbound${count > 1 ? "s" : ""}.`
                + (referenced > 0 ? ` ${referenced} of them ${referenced === 1 ? "is" : "are"} targeted by routing rules.` : ""),
            confirmLabel: "Delete",
            variant: "destructive",
            ...(count > 5 ? { typeToConfirm: String(count) } : {}),
        })
        if (!confirmed) return

        setActionLoading("bulk-delete")
        const idToTag = new Map(outbounds.map(o => [o.id, o.tag] as const))
        const results = await Promise.allSettled(ids.map(id => deleteNodeOutbound(nodeId, id)))

        const failedTags: string[] = []
        let okCount = 0
        results.forEach((r, idx) => {
            if (r.status === "fulfilled" && r.value.success) okCount++
            else failedTags.push(idToTag.get(ids[idx]) || `#${ids[idx]}`)
        })

        if (failedTags.length === 0) {
            toast.success(`Deleted ${okCount} outbound(s)`)
        } else {
            toast.warning(`Deleted ${okCount}, failed ${failedTags.length}: ${failedTags.slice(0, 3).join(", ")}${failedTags.length > 3 ? "…" : ""}`)
        }

        setSelectedOutbounds(new Set())
        setActionLoading(null)
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeOutbounds(nodeId) })
        onRefresh?.()
    }

    const handleBulkOutboundToggle = async (disable: boolean) => {
        if (selectedOutbounds.size === 0) return

        const targets = outbounds.filter(o => selectedOutbounds.has(o.id) && o.is_disabled !== disable)
        if (targets.length === 0) {
            toast.info(`All selected outbounds are already ${disable ? "disabled" : "enabled"}`)
            return
        }

        setActionLoading(disable ? "bulk-disable" : "bulk-enable")
        const results = await Promise.allSettled(targets.map(o => toggleOutboundDisabled(o.id)))

        const failedTags: string[] = []
        let okCount = 0
        results.forEach((r, idx) => {
            if (r.status === "fulfilled" && r.value.success) okCount++
            else failedTags.push(targets[idx].tag)
        })

        const verb = disable ? "Disabled" : "Enabled"
        if (failedTags.length === 0) {
            toast.success(`${verb} ${okCount} outbound(s)`)
        } else {
            toast.warning(`${verb} ${okCount}, failed ${failedTags.length}: ${failedTags.slice(0, 3).join(", ")}${failedTags.length > 3 ? "…" : ""}`)
        }

        setSelectedOutbounds(new Set())
        setActionLoading(null)
        queryClient.invalidateQueries({ queryKey: queryKeys.nodeOutbounds(nodeId) })
        onRefresh?.()
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
                            <Badge variant="secondary" className="ml-2 px-1.5 py-0 text-xs bg-muted/50 hidden sm:inline-flex">{allOutbounds.length}</Badge>
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
                            <InboundSectionHeader
                                summary={inboundSummary}
                                actionLoading={actionLoading}
                                onDiscover={handleDiscover}
                                onSync={handleSync}
                                onAdd={() => setInboundDialog({ open: true, mode: "create", inbound: null })}
                            />

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

                                    {/* Mobile list — swipe for actions, tap to expand */}
                                    <div className="md:hidden divide-y rounded-xl border overflow-hidden">
                                        <div className="flex items-center justify-between px-3 py-2 bg-muted/50">
                                            <label className="flex items-center gap-2.5 text-sm text-muted-foreground cursor-pointer select-none">
                                                <Checkbox
                                                    checked={selectedInbounds.size === inbounds.length && inbounds.length > 0}
                                                    onCheckedChange={toggleAllInbounds}
                                                    aria-label="Select all inbounds"
                                                />
                                                {selectedInbounds.size > 0 ? `${selectedInbounds.size} selected` : "Select all"}
                                            </label>
                                        </div>
                                        {sortedInbounds.map((inbound) => {
                                            const inboundAccounts = accountsByInbound.get(inbound.id) || []
                                            const counts = inboundCounts.get(inbound.id)

                                            return (
                                                <SwipeableInboundRow
                                                    key={inbound.id}
                                                    inbound={inbound}
                                                    accountCount={inboundAccounts.length}
                                                    onlineCount={counts?.online || 0}
                                                    expiredCount={counts?.expired || 0}
                                                    totalTraffic={counts?.trafficBytes || 0}
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
                                                    expandedContent={mountedPanels.has(inbound.id) ? (
                                                        <InboundDetailPanel
                                                            inbound={inbound}
                                                            accounts={inboundAccounts}
                                                            details={buildInboundDetails(inbound)}
                                                            nodeId={nodeId}
                                                            isOnline={isOnline}
                                                            onAccountChange={refetchAccounts}
                                                        />
                                                    ) : null}
                                                />
                                            )
                                        })}
                                    </div>

                                    {/* Desktop list — one table, so columns line up down the page.
                                        Density follows the list's own width: the two side navs
                                        leave it far narrower than the viewport suggests. */}
                                    <div className="hidden md:block @container">
                                        <div className="rounded-xl border bg-card overflow-x-auto">
                                            <div className="min-w-[520px]">
                                                <InboundListHeader
                                                    allSelected={selectedInbounds.size === inbounds.length && inbounds.length > 0}
                                                    someSelected={selectedInbounds.size > 0 && selectedInbounds.size < inbounds.length}
                                                    onToggleAll={toggleAllInbounds}
                                                    sortField={inboundSort.field}
                                                    sortDir={inboundSort.dir}
                                                    onSort={handleInboundSort}
                                                />
                                                {sortedInbounds.map((inbound) => {
                                                    const inboundAccounts = accountsByInbound.get(inbound.id) || []
                                                    const counts = inboundCounts.get(inbound.id)
                                                    const details = buildInboundDetails(inbound)

                                                    return (
                                                        <DesktopInboundRow
                                                            key={inbound.id}
                                                            inbound={inbound}
                                                            isExpanded={expandedRows.has(inbound.id)}
                                                            isSelected={selectedInbounds.has(inbound.id)}
                                                            accountCount={inboundAccounts.length}
                                                            onlineCount={counts?.online || 0}
                                                            expiredCount={counts?.expired || 0}
                                                            trafficBytes={counts?.trafficBytes || 0}
                                                            details={details}
                                                            onToggleExpand={() => toggleExpand(inbound.id)}
                                                            onToggleSelect={() => toggleInboundSelection(inbound.id)}
                                                            onToggleDisabled={() => handleToggleInbound(inbound)}
                                                            onEdit={() => setInboundDialog({ open: true, mode: "edit", inbound })}
                                                            onMigrate={() => handleMigrateInbound(inbound)}
                                                            onDelete={() => handleDeleteInbound(inbound)}
                                                            expandedContent={mountedPanels.has(inbound.id) ? (
                                                                <InboundDetailPanel
                                                                    inbound={inbound}
                                                                    accounts={inboundAccounts}
                                                                    details={details}
                                                                    nodeId={nodeId}
                                                                    isOnline={isOnline}
                                                                    onAccountChange={refetchAccounts}
                                                                />
                                                            ) : null}
                                                        />
                                                    )
                                                })}
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            ) : (
                                <div className="flex flex-col items-center justify-center py-16 text-muted-foreground rounded-2xl border-2 border-dashed bg-muted/5">
                                    <HiOutlineGlobeAlt className="w-12 h-12 opacity-50 mb-4" />
                                    <h3 className="text-lg font-medium text-foreground mb-1">No Inbounds Configured</h3>
                                    <p className="text-sm mb-4">Create an inbound or import the ones Xray already runs</p>
                                    <div className="flex gap-2">
                                        <Button variant="outline" size="sm" onClick={handleDiscover}>
                                            <HiOutlineDownload className="w-4 h-4 mr-2" />
                                            Import from Xray
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
                    <TooltipProvider delayDuration={200}>
                        <div className="space-y-4">
                            <OutboundSectionHeader
                                summary={outboundSummary}
                                testAllProgress={testAllProgress}
                                canTestAll={testableOutbounds(outbounds).length > 0}
                                onTestAll={handleTestAllOutbounds}
                                onAdd={() => setOutboundDialog({ open: true, mode: "create", outbound: null })}
                            />

                            <ManagedOutboundList
                                outbounds={managedOutbounds}
                                usage={outboundUsage}
                                defaultTag={defaultOutbound}
                                onOpenRules={() => setRoutingView("rules")}
                            />

                            {outbounds.length > 0 ? (
                                <div className="space-y-3">
                                    <BulkActionBar
                                        label="Bulk outbound actions"
                                        count={selectedOutbounds.size}
                                        onCancel={clearOutboundSelection}
                                        onDisable={() => handleBulkOutboundToggle(true)}
                                        onEnable={() => handleBulkOutboundToggle(false)}
                                        onDelete={handleBulkOutboundDelete}
                                        onTest={handleBulkOutboundTest}
                                        actionLoading={actionLoading}
                                    />

                                    {/* Mobile list — swipe for actions, tap to expand */}
                                    <div className="md:hidden divide-y rounded-xl border overflow-hidden">
                                        <div className="flex items-center justify-between px-3 py-2 bg-muted/50">
                                            <label className="flex items-center gap-2.5 text-sm text-muted-foreground cursor-pointer select-none">
                                                <Checkbox
                                                    checked={selectedOutbounds.size === outbounds.length && outbounds.length > 0}
                                                    onCheckedChange={toggleAllOutbounds}
                                                    aria-label="Select all outbounds"
                                                />
                                                {selectedOutbounds.size > 0 ? `${selectedOutbounds.size} selected` : "Select all"}
                                            </label>
                                        </div>
                                        {sortedOutbounds.map((outbound) => {
                                            const usage = outboundUsage.get(outbound.tag) ?? { rules: [], balancers: [] }
                                            const route = outboundRoutes.get(outbound.id) ?? ""
                                            const details = outboundDetails.get(outbound.id) ?? []
                                            return (
                                                <SwipeableOutboundRow
                                                    key={outbound.id}
                                                    outbound={outbound}
                                                    route={route}
                                                    trafficBytes={outboundTraffic(outbound)}
                                                    isDefault={outbound.tag === defaultOutbound}
                                                    isSelected={selectedOutbounds.has(outbound.id)}
                                                    isMultiSelectMode={selectedOutbounds.size > 0}
                                                    shouldClose={openSwipeOutboundId !== null && openSwipeOutboundId !== outbound.id}
                                                    isExpanded={expandedOutbounds.has(outbound.id)}
                                                    testEntry={testEntryOf(outbound)}
                                                    isTesting={testingIds.has(outbound.id)}
                                                    canTest={TESTABLE_OUTBOUND_PROTOCOLS(outbound.protocol)}
                                                    testDisabled={!!testAllProgress}
                                                    onOpen={setOpenSwipeOutboundId}
                                                    onTap={() => toggleOutboundExpand(outbound.id)}
                                                    onToggleSelect={toggleOutboundSelection}
                                                    onLongPress={toggleOutboundSelection}
                                                    onEdit={(ob) => setOutboundDialog({ open: true, mode: "edit", outbound: ob })}
                                                    onTest={(ob) => handleTestOutbound(ob)}
                                                    onViewTestResult={(ob) => setTestResultDialog({ open: true, outboundId: ob.id, outboundTag: ob.tag })}
                                                    onToggleDisabled={handleToggleOutbound}
                                                    onDelete={handleDeleteOutbound}
                                                    expandedContent={mountedOutboundPanels.has(outbound.id) ? (
                                                        <OutboundDetailPanel
                                                            outbound={outbound}
                                                            route={route}
                                                            details={details}
                                                            usage={usage}
                                                            isDefault={outbound.tag === defaultOutbound}
                                                            onOpenRules={() => setRoutingView("rules")}
                                                        />
                                                    ) : null}
                                                />
                                            )
                                        })}
                                    </div>

                                    {/* Desktop list — same grid as the inbound list, density from the list's own width. */}
                                    <div className="hidden md:block @container">
                                        <div className="rounded-xl border bg-card overflow-x-auto">
                                            <div className="min-w-[560px]">
                                                <OutboundListHeader
                                                    allSelected={selectedOutbounds.size === outbounds.length && outbounds.length > 0}
                                                    someSelected={selectedOutbounds.size > 0 && selectedOutbounds.size < outbounds.length}
                                                    onToggleAll={toggleAllOutbounds}
                                                    sortField={outboundSort.field}
                                                    sortDir={outboundSort.dir}
                                                    onSort={handleOutboundSort}
                                                />
                                                {sortedOutbounds.map((outbound) => {
                                                    const usage = outboundUsage.get(outbound.tag) ?? { rules: [], balancers: [] }
                                                    const route = outboundRoutes.get(outbound.id) ?? ""
                                                    const details = outboundDetails.get(outbound.id) ?? []
                                                    const canTest = TESTABLE_OUTBOUND_PROTOCOLS(outbound.protocol)
                                                    return (
                                                        <DesktopOutboundRow
                                                            key={outbound.id}
                                                            outbound={outbound}
                                                            isExpanded={expandedOutbounds.has(outbound.id)}
                                                            isSelected={selectedOutbounds.has(outbound.id)}
                                                            isDefault={outbound.tag === defaultOutbound}
                                                            usageCount={usageCount(usage)}
                                                            trafficBytes={outboundTraffic(outbound)}
                                                            route={route}
                                                            details={details}
                                                            testEntry={testEntryOf(outbound)}
                                                            isTesting={testingIds.has(outbound.id)}
                                                            canTest={canTest}
                                                            speedtestSupported={outbound.protocol !== "freedom"}
                                                            testDisabled={!!testAllProgress}
                                                            onToggleExpand={() => toggleOutboundExpand(outbound.id)}
                                                            onToggleSelect={() => toggleOutboundSelection(outbound.id)}
                                                            onToggleDisabled={() => handleToggleOutbound(outbound)}
                                                            onEdit={() => setOutboundDialog({ open: true, mode: "edit", outbound })}
                                                            onTest={() => handleTestOutbound(outbound)}
                                                            onTestSpeed={() => handleTestOutbound(outbound, true)}
                                                            onViewTest={() => setTestResultDialog({ open: true, outboundId: outbound.id, outboundTag: outbound.tag })}
                                                            onDelete={() => handleDeleteOutbound(outbound)}
                                                            expandedContent={mountedOutboundPanels.has(outbound.id) ? (
                                                                <OutboundDetailPanel
                                                                    outbound={outbound}
                                                                    route={route}
                                                                    details={details}
                                                                    usage={usage}
                                                                    isDefault={outbound.tag === defaultOutbound}
                                                                    onOpenRules={() => setRoutingView("rules")}
                                                                />
                                                            ) : null}
                                                        />
                                                    )
                                                })}
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            ) : (
                                <div className="flex flex-col items-center justify-center py-16 text-muted-foreground rounded-2xl border-2 border-dashed bg-muted/5">
                                    <HiOutlineSwitchHorizontal className="w-12 h-12 opacity-50 mb-4" />
                                    <h3 className="text-lg font-medium text-foreground mb-1">No Custom Outbounds Configured</h3>
                                    <p className="text-sm mb-4">Add an upstream proxy or routing target</p>
                                    <Button size="sm" onClick={() => setOutboundDialog({ open: true, mode: "create", outbound: null })}>
                                        <HiOutlinePlus className="w-4 h-4 mr-2" />
                                        Add Outbound
                                    </Button>
                                </div>
                            )}
                        </div>
                    </TooltipProvider>
                </TabsContent>

                {/* Routing Content — one pane per job: the rules, the presets that write rules, the balancers rules target */}
                <TabsContent value="routing" className="animate-in fade-in-50 duration-300">
                    <div className="flex items-center gap-1 p-[3px] mb-5 rounded-lg border border-white/[0.06] bg-muted/40 w-fit max-w-full overflow-x-auto no-scrollbar">
                        {ROUTING_VIEWS.map(v => {
                            const active = routingView === v.value
                            const count =
                                v.value === "rules" ? displayRoutingRules.length
                                    : v.value === "balancers" ? balancingRules.length
                                        : presetRuleCount
                            return (
                                <button
                                    key={v.value}
                                    type="button"
                                    onClick={() => setRoutingView(v.value)}
                                    className={`flex items-center gap-2 px-3.5 py-1.5 rounded-md text-sm font-medium transition-colors whitespace-nowrap ${active
                                        ? "bg-white/10 text-foreground shadow-[inset_0_0_0_1px_rgba(255,255,255,0.08)]"
                                        : "text-muted-foreground hover:text-foreground"
                                        }`}
                                >
                                    {v.label}
                                    <span className="text-[11px] px-1.5 rounded bg-white/[0.08]">{count}</span>
                                </button>
                            )
                        })}
                    </div>

                    {routingView === "rules" && (
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
                    )}

                    {routingView === "presets" && (
                        <RoutingSettingsCard
                            nodeId={nodeId}
                            existingRules={routingRules}
                            outbounds={allOutbounds}
                            onSettingsSaved={() => queryClient.invalidateQueries({ queryKey: queryKeys.nodeRouting(nodeId) })}
                            onPresetRulesChanged={setPendingPresetRules}
                        />
                    )}

                    {routingView === "balancers" && (
                        <BalancingRulesCard
                            nodeId={nodeId}
                            outbounds={allOutbounds}
                            rules={balancingRules}
                            routingRules={routingRules}
                            onRulesChanged={() => {
                                queryClient.invalidateQueries({ queryKey: queryKeys.nodeBalancingRules(nodeId) })
                                onRefresh?.()
                            }}
                        />
                    )}
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
                    <div className="space-y-4">
                        <NodeSettingsXray nodeId={nodeId} isOnline={true} />
                        <OutboundTestSettingsCard nodeId={nodeId} />
                    </div>
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
                    {activeTab === "outbounds" && selectedOutbounds.size === 0 && (
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
                onTest={(ob) => handleTestOutbound(ob)}
                mode={outboundDialog.mode}
                allOutbounds={allOutbounds}
            />

            <RoutingRuleDialog
                open={routingDialog.open}
                onOpenChange={(open) => setRoutingDialog(prev => ({ ...prev, open }))}
                rule={routingDialog.rule}
                nodeId={nodeId}
                outbounds={allOutbounds}
                inbounds={inbounds}
                balancingRules={balancingRules}
                onSave={handleSaveRoutingRule}
                mode={routingDialog.mode}
            />

            <OutboundTestResultDialog
                open={testResultDialog.open}
                onOpenChange={(open) => setTestResultDialog(prev => ({ ...prev, open }))}
                entry={dialogOutbound ? testEntryOf(dialogOutbound) : null}
                outboundTag={testResultDialog.outboundTag}
                testing={dialogOutbound ? testingIds.has(dialogOutbound.id) : false}
                speedtestSupported={dialogOutbound ? dialogOutbound.protocol !== "freedom" : false}
                onRetest={(speedtest) => {
                    if (dialogOutbound) void handleTestOutbound(dialogOutbound, speedtest)
                }}
            />

            <ReverseProxyDialog
                open={reverseProxyDialog.open}
                onOpenChange={(open) => !open && setReverseProxyDialog({ open: false, mode: "create", reverseProxy: null })}
                mode={reverseProxyDialog.mode}
                reverseProxy={reverseProxyDialog.reverseProxy}
                inboundTags={inbounds.filter(i => !i.is_disabled).map(i => i.tag)}
                outboundTags={outbounds.filter(o => !o.managed && !o.is_disabled).map(o => o.tag)}
                vlessInboundTags={inbounds.filter(i => i.protocol === "vless" && !i.is_disabled).map(i => i.tag)}
                vlessOutboundTags={outbounds.filter(o => o.protocol === "vless" && !o.is_disabled).map(o => o.tag)}
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
