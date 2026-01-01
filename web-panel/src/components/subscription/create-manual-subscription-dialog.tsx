import { useState, useMemo, useCallback } from "react"
import { useQuery } from "@tanstack/react-query"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import { Card } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Search,
    ChevronDown,
    ChevronRight,
    Server,
    ArrowLeft,
    ArrowRight,
    Check,
    Loader2,
    AlertCircle,
    Globe,
    Tag,
    HardDrive,
    Calendar,
    Smartphone,
    Settings,
    User as UserIcon,
    UserPlus,
    Wifi,
} from "lucide-react"
import { FaTelegram } from "react-icons/fa"
import { listNodes, createUser as createUserAPI } from "@/lib/admin-api"
import { useCreateManualSubscription } from "@/lib/queries/use-subscriptions"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import { UserSearchSelect } from "@/components/ui/user-search-select"
import { cn } from "@/lib/utils"
import { toast } from "sonner"
import type { Node, Inbound, User } from "@/lib/types"
import { BANDWIDTH_OPTIONS } from "@/lib/types"

// ==================== Types ====================

interface InboundSelection {
    inbound: Inbound
    nodeName: string
    nodeCountry: string
}

// ==================== Step Indicator ====================

function StepIndicator({ currentStep }: { currentStep: number }) {
    const steps = ["Servers", "User", "Configure", "Review"]

    return (
        <div className="flex items-center justify-center gap-1.5 sm:gap-2 py-2">
            {steps.map((label, index) => (
                <div key={label} className="flex items-center gap-1.5 sm:gap-2">
                    <div className="flex items-center gap-1 sm:gap-1.5">
                        <div
                            className={cn(
                                "w-2 h-2 rounded-full transition-all duration-300",
                                index === currentStep
                                    ? "w-6 bg-primary"
                                    : index < currentStep
                                        ? "bg-primary"
                                        : "bg-muted-foreground/30"
                            )}
                        />
                        <span
                            className={cn(
                                "text-xs transition-colors duration-200",
                                index === currentStep
                                    ? "text-foreground font-medium"
                                    : "text-muted-foreground hidden sm:inline"
                            )}
                        >
                            {label}
                        </span>
                    </div>
                    {index < steps.length - 1 && (
                        <div className="w-4 sm:w-8 h-px bg-border" />
                    )}
                </div>
            ))}
        </div>
    )
}

// ==================== Step 1: Select Servers ====================

const COUNTRY_FLAGS: Record<string, string> = {
    DE: "🇩🇪", US: "🇺🇸", GB: "🇬🇧", NL: "🇳🇱", FR: "🇫🇷", FI: "🇫🇮",
    SE: "🇸🇪", NO: "🇳🇴", CA: "🇨🇦", AU: "🇦🇺", JP: "🇯🇵", SG: "🇸🇬",
    HK: "🇭🇰", KR: "🇰🇷", IN: "🇮🇳", BR: "🇧🇷", TR: "🇹🇷", RU: "🇷🇺",
    AE: "🇦🇪", ZA: "🇿🇦", IR: "🇮🇷", IT: "🇮🇹", ES: "🇪🇸", PL: "🇵🇱",
    AT: "🇦🇹", CH: "🇨🇭", IE: "🇮🇪", DK: "🇩🇰", CZ: "🇨🇿", RO: "🇷🇴",
    UA: "🇺🇦", BG: "🇧🇬", HU: "🇭🇺", LT: "🇱🇹", LV: "🇱🇻", EE: "🇪🇪",
    PT: "🇵🇹", GR: "🇬🇷", RS: "🇷🇸", HR: "🇭🇷", SK: "🇸🇰", SI: "🇸🇮",
    IL: "🇮🇱", TW: "🇹🇼", VN: "🇻🇳", TH: "🇹🇭", MY: "🇲🇾", PH: "🇵🇭",
    ID: "🇮🇩", MX: "🇲🇽", AR: "🇦🇷", CL: "🇨🇱", CO: "🇨🇴",
}

const PROTOCOL_STYLES: Record<string, { bg: string; text: string; border: string }> = {
    vless: { bg: "bg-indigo-500/10", text: "text-indigo-400", border: "border-indigo-500/20" },
    vmess: { bg: "bg-violet-500/10", text: "text-violet-400", border: "border-violet-500/20" },
    trojan: { bg: "bg-amber-500/10", text: "text-amber-400", border: "border-amber-500/20" },
    shadowsocks: { bg: "bg-cyan-500/10", text: "text-cyan-400", border: "border-cyan-500/20" },
    hysteria: { bg: "bg-rose-500/10", text: "text-rose-400", border: "border-rose-500/20" },
    hysteria2: { bg: "bg-rose-500/10", text: "text-rose-400", border: "border-rose-500/20" },
    tuic: { bg: "bg-teal-500/10", text: "text-teal-400", border: "border-teal-500/20" },
    wireguard: { bg: "bg-emerald-500/10", text: "text-emerald-400", border: "border-emerald-500/20" },
}

const DEFAULT_PROTOCOL_STYLE = { bg: "bg-primary/10", text: "text-primary", border: "border-primary/20" }

function getProtocolStyle(protocol: string) {
    return PROTOCOL_STYLES[protocol.toLowerCase()] || DEFAULT_PROTOCOL_STYLE
}

function StepSelectServers({
    selectedInboundIds,
    onToggleInbound,
    onToggleNode,
    nodes,
    isLoading,
    isError,
}: {
    selectedInboundIds: Set<number>
    onToggleInbound: (id: number) => void
    onToggleNode: (nodeId: number, inbounds: Inbound[], selectAll: boolean) => void
    nodes: Node[]
    isLoading: boolean
    isError: boolean
}) {
    const [search, setSearch] = useState("")
    // Auto-expand if there's only one node
    const [expandedNodes, setExpandedNodes] = useState<Set<number>>(() => {
        if (nodes.length === 1) return new Set([nodes[0].id])
        return new Set()
    })

    const toggleExpanded = useCallback((nodeId: number) => {
        setExpandedNodes((prev) => {
            const next = new Set(prev)
            if (next.has(nodeId)) {
                next.delete(nodeId)
            } else {
                next.add(nodeId)
            }
            return next
        })
    }, [])

    const filteredNodes = useMemo(() => {
        if (!search.trim()) return nodes
        const q = search.toLowerCase()
        return nodes
            .map((node) => {
                const nodeMatch = node.name.toLowerCase().includes(q) ||
                    node.country_code.toLowerCase().includes(q)
                const filteredInbounds = (node.inbounds || []).filter(
                    (inb) =>
                        inb.protocol.toLowerCase().includes(q) ||
                        inb.tag.toLowerCase().includes(q) ||
                        inb.remark.toLowerCase().includes(q)
                )
                if (nodeMatch) return node
                if (filteredInbounds.length > 0) {
                    return { ...node, inbounds: filteredInbounds }
                }
                return null
            })
            .filter(Boolean) as Node[]
    }, [nodes, search])

    const selectedCount = selectedInboundIds.size

    if (isLoading) {
        return (
            <div className="space-y-3">
                <Skeleton className="h-10 w-full rounded-lg" />
                {[1, 2, 3].map((i) => (
                    <Skeleton key={i} className="h-20 w-full rounded-xl" />
                ))}
            </div>
        )
    }

    if (isError) {
        return (
            <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
                <div className="w-12 h-12 rounded-full bg-red-500/10 flex items-center justify-center mb-4">
                    <AlertCircle className="w-6 h-6 text-red-500" />
                </div>
                <p className="text-sm font-medium text-foreground">Failed to load nodes</p>
                <p className="text-xs mt-1.5 text-muted-foreground">Check your connection and try again.</p>
            </div>
        )
    }

    return (
        <div className="flex flex-col gap-3 h-full">
            {/* Search + Count Header */}
            <div className="flex items-center gap-3 shrink-0">
                <div className="relative flex-1">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
                    <Input
                        placeholder="Search nodes or protocols..."
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        className="pl-9 h-10"
                    />
                </div>
                <Badge
                    variant={selectedCount > 0 ? "default" : "secondary"}
                    className={cn(
                        "shrink-0 tabular-nums transition-all duration-200",
                        selectedCount > 0 && "shadow-sm shadow-primary/20"
                    )}
                >
                    {selectedCount} selected
                </Badge>
            </div>

            {/* Node List */}
            <ScrollArea className="flex-1 min-h-0 pr-1">
                {filteredNodes.length === 0 ? (
                    <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
                        <div className="w-12 h-12 rounded-full bg-muted/50 flex items-center justify-center mb-3">
                            <Server className="w-5 h-5 opacity-50" />
                        </div>
                        <p className="text-sm font-medium text-foreground/70">
                            {search ? "No nodes match your search" : "No nodes available"}
                        </p>
                        {search && (
                            <p className="text-xs mt-1 text-muted-foreground">
                                Try a different search term
                            </p>
                        )}
                    </div>
                ) : (
                    <div className="space-y-2.5">
                        {filteredNodes.map((node) => {
                            const inbounds = node.inbounds || []
                            const isExpanded = expandedNodes.has(node.id)
                            const allSelected = inbounds.length > 0 && inbounds.every((i) => selectedInboundIds.has(i.id))
                            const someSelected = inbounds.some((i) => selectedInboundIds.has(i.id))
                            const selectedInNode = inbounds.filter((i) => selectedInboundIds.has(i.id)).length
                            const flag = COUNTRY_FLAGS[node.country_code.toUpperCase()] || "🌐"

                            return (
                                <div
                                    key={node.id}
                                    className={cn(
                                        "rounded-xl border transition-all duration-200 overflow-hidden",
                                        someSelected
                                            ? "border-primary/30 bg-primary/[0.02] shadow-sm shadow-primary/5"
                                            : "border-border bg-card hover:border-border/80"
                                    )}
                                >
                                    {/* Node Header */}
                                    <button
                                        type="button"
                                        className={cn(
                                            "w-full flex items-center gap-3 px-4 py-3.5 transition-colors text-left group",
                                            "hover:bg-muted/40"
                                        )}
                                        onClick={() => toggleExpanded(node.id)}
                                    >
                                        <div className={cn(
                                            "w-5 h-5 flex items-center justify-center transition-transform duration-200",
                                            isExpanded && "rotate-0",
                                            !isExpanded && "-rotate-90"
                                        )}>
                                            <ChevronDown className="w-4 h-4 text-muted-foreground group-hover:text-foreground transition-colors" />
                                        </div>

                                        <div className="flex items-center gap-2.5 flex-1 min-w-0">
                                            <span className="text-lg leading-none" role="img" aria-label={node.country_code}>
                                                {flag}
                                            </span>
                                            <span className="font-semibold text-sm truncate">
                                                {node.name}
                                            </span>
                                            <Badge variant="outline" className="text-[10px] px-1.5 py-0 uppercase shrink-0 font-mono tracking-wider hidden sm:inline-flex">
                                                {node.country_code}
                                            </Badge>
                                            <div className={cn(
                                                "flex items-center gap-1.5 shrink-0",
                                            )}>
                                                <div className={cn(
                                                    "w-1.5 h-1.5 rounded-full",
                                                    node.is_online
                                                        ? "bg-emerald-500 shadow-sm shadow-emerald-500/50"
                                                        : "bg-red-500 shadow-sm shadow-red-500/50"
                                                )} />
                                                <span className={cn(
                                                    "text-[11px] font-medium hidden sm:inline",
                                                    node.is_online ? "text-emerald-500" : "text-red-500"
                                                )}>
                                                    {node.is_online ? "Online" : "Offline"}
                                                </span>
                                            </div>
                                        </div>

                                        <div className="flex items-center gap-2 shrink-0">
                                            {someSelected && (
                                                <span className="text-xs font-medium text-primary tabular-nums">
                                                    {selectedInNode}/{inbounds.length}
                                                </span>
                                            )}
                                            {!someSelected && inbounds.length > 0 && (
                                                <span className="text-[11px] text-muted-foreground hidden sm:inline">
                                                    {inbounds.length} inbound{inbounds.length !== 1 ? "s" : ""}
                                                </span>
                                            )}
                                            {inbounds.length > 0 && (
                                                <Button
                                                    variant="ghost"
                                                    size="sm"
                                                    className={cn(
                                                        "h-7 px-2.5 text-xs font-medium rounded-lg transition-all",
                                                        allSelected
                                                            ? "text-red-400 hover:text-red-300 hover:bg-red-500/10"
                                                            : "text-primary hover:bg-primary/10"
                                                    )}
                                                    onClick={(e) => {
                                                        e.stopPropagation()
                                                        onToggleNode(node.id, inbounds, !allSelected)
                                                    }}
                                                >
                                                    <span className="hidden sm:inline">{allSelected ? "Deselect All" : "Select All"}</span>
                                                    <span className="sm:hidden">{allSelected ? "None" : "All"}</span>
                                                </Button>
                                            )}
                                        </div>
                                    </button>

                                    {/* Inbounds List */}
                                    {isExpanded && (
                                        <div className="border-t border-border/50">
                                            {inbounds.length === 0 ? (
                                                <div className="flex items-center justify-center gap-2 py-6 text-muted-foreground">
                                                    <Server className="w-4 h-4 opacity-40" />
                                                    <p className="text-xs">No inbounds configured</p>
                                                </div>
                                            ) : (
                                                <div className="p-2 space-y-1">
                                                    {inbounds.map((inbound) => {
                                                        const isSelected = selectedInboundIds.has(inbound.id)
                                                        const pStyle = getProtocolStyle(inbound.protocol)
                                                        return (
                                                            <label
                                                                key={inbound.id}
                                                                className={cn(
                                                                    "flex items-center gap-2 sm:gap-3 px-2 sm:px-3 py-2.5 rounded-lg cursor-pointer transition-all duration-150",
                                                                    "hover:bg-muted/50",
                                                                    isSelected
                                                                        ? "bg-primary/[0.06] ring-1 ring-primary/15"
                                                                        : "bg-transparent"
                                                                )}
                                                            >
                                                                <Checkbox
                                                                    checked={isSelected}
                                                                    onCheckedChange={() => onToggleInbound(inbound.id)}
                                                                    className="transition-all duration-150"
                                                                />
                                                                <div className="flex items-center gap-2 flex-1 min-w-0">
                                                                    {/* Protocol badge with unique color */}
                                                                    <span
                                                                        className={cn(
                                                                            "inline-flex items-center rounded-md px-2 py-0.5 text-[10px] font-bold font-mono uppercase tracking-wider border shrink-0",
                                                                            pStyle.bg,
                                                                            pStyle.text,
                                                                            pStyle.border
                                                                        )}
                                                                    >
                                                                        {inbound.protocol}
                                                                    </span>
                                                                    {/* Port pill */}
                                                                    <span className="inline-flex items-center rounded-md bg-muted/80 px-1.5 py-0.5 text-[11px] font-mono text-muted-foreground">
                                                                        :{inbound.port}
                                                                    </span>
                                                                    {/* Network pill */}
                                                                    <span className="inline-flex items-center rounded-md bg-muted/60 px-1.5 py-0.5 text-[11px] text-muted-foreground">
                                                                        {inbound.network}
                                                                    </span>
                                                                    {/* Security badge */}
                                                                    {inbound.security !== "none" && (
                                                                        <Badge
                                                                            variant={inbound.security === "tls" ? "success" : inbound.security === "reality" ? "warning" : "secondary"}
                                                                            className="text-[10px] px-1.5 py-0 shrink-0"
                                                                        >
                                                                            {inbound.security}
                                                                        </Badge>
                                                                    )}
                                                                </div>
                                                                {inbound.remark && (
                                                                    <span className="text-[10px] text-muted-foreground/70 truncate max-w-[120px] italic hidden sm:inline">
                                                                        {inbound.remark}
                                                                    </span>
                                                                )}
                                                            </label>
                                                        )
                                                    })}
                                                </div>
                                            )}
                                        </div>
                                    )}
                                </div>
                            )
                        })}
                    </div>
                )}
            </ScrollArea>
        </div>
    )
}

// ==================== Step 2: Select User ====================

type UserMode = "none" | "existing" | "create"

interface NewUserForm {
    username: string
    firstName: string
    lastName: string
    telegramId: string
}

function StepSelectUser({
    userMode,
    onUserModeChange,
    selectedUser,
    onSelectedUserChange,
    newUserForm,
    onNewUserFormChange,
}: {
    userMode: UserMode
    onUserModeChange: (mode: UserMode) => void
    selectedUser: User | null
    onSelectedUserChange: (user: User | null) => void
    newUserForm: NewUserForm
    onNewUserFormChange: (form: NewUserForm) => void
}) {
    return (
        <ScrollArea className="h-full pr-1">
            <div className="space-y-4 pb-2">
                <p className="text-sm text-muted-foreground">
                    Optionally link this subscription to a user account.
                </p>

                {/* Mode Selection */}
                <div className="grid grid-cols-3 gap-2">
                    {([
                        { value: "none" as const, label: "No User", icon: UserIcon, desc: "Manual only" },
                        { value: "existing" as const, label: "Existing", icon: Search, desc: "Search users" },
                        { value: "create" as const, label: "New User", icon: UserPlus, desc: "Create account" },
                    ]).map((opt) => (
                        <button
                            key={opt.value}
                            type="button"
                            className={cn(
                                "flex flex-col items-center gap-1.5 p-3 rounded-lg border-2 transition-all",
                                userMode === opt.value
                                    ? "border-primary bg-primary/5"
                                    : "border-transparent bg-muted/50 hover:bg-muted"
                            )}
                            onClick={() => {
                                onUserModeChange(opt.value)
                                if (opt.value !== "existing") onSelectedUserChange(null)
                            }}
                        >
                            <opt.icon className={cn(
                                "w-5 h-5",
                                userMode === opt.value ? "text-primary" : "text-muted-foreground"
                            )} />
                            <span className={cn(
                                "text-sm font-medium",
                                userMode === opt.value ? "text-primary" : "text-foreground"
                            )}>
                                {opt.label}
                            </span>
                            <span className="text-[10px] text-muted-foreground">{opt.desc}</span>
                        </button>
                    ))}
                </div>

                {/* Existing User Search */}
                {userMode === "existing" && (
                    <div className="space-y-2">
                        <Label className="flex items-center gap-2">
                            <Search className="w-4 h-4 text-primary" />
                            Find User
                        </Label>
                        <UserSearchSelect
                            value={selectedUser}
                            onChange={onSelectedUserChange}
                            placeholder="Search by username, name, or Telegram ID..."
                        />
                    </div>
                )}

                {/* Create New User Form */}
                {userMode === "create" && (
                    <div className="space-y-4">
                        <div className="space-y-2">
                            <Label htmlFor="new-username" className="flex items-center gap-2">
                                <UserIcon className="w-4 h-4 text-primary" />
                                Username <span className="text-red-500">*</span>
                            </Label>
                            <Input
                                id="new-username"
                                placeholder="e.g. john_doe"
                                value={newUserForm.username}
                                onChange={(e) => onNewUserFormChange({ ...newUserForm, username: e.target.value })}
                            />
                        </div>
                        <div className="grid grid-cols-2 gap-3">
                            <div className="space-y-2">
                                <Label htmlFor="new-firstname">
                                    First Name
                                </Label>
                                <Input
                                    id="new-firstname"
                                    placeholder="John"
                                    value={newUserForm.firstName}
                                    onChange={(e) => onNewUserFormChange({ ...newUserForm, firstName: e.target.value })}
                                />
                            </div>
                            <div className="space-y-2">
                                <Label htmlFor="new-lastname">Last Name</Label>
                                <Input
                                    id="new-lastname"
                                    placeholder="Doe"
                                    value={newUserForm.lastName}
                                    onChange={(e) => onNewUserFormChange({ ...newUserForm, lastName: e.target.value })}
                                />
                            </div>
                        </div>
                        <div className="space-y-2">
                            <Label htmlFor="new-telegram" className="flex items-center gap-2">
                                <FaTelegram className="w-4 h-4 text-[#229ED9]" />
                                Telegram Chat ID
                            </Label>
                            <Input
                                id="new-telegram"
                                type="number"
                                placeholder="Optional"
                                value={newUserForm.telegramId}
                                onChange={(e) => onNewUserFormChange({ ...newUserForm, telegramId: e.target.value })}
                            />
                            <p className="text-xs text-muted-foreground">
                                Leave empty if the user doesn&apos;t have Telegram. They will be notified when linked.
                            </p>
                        </div>
                    </div>
                )}

                {/* No User Info */}
                {userMode === "none" && (
                    <Card className="p-4 bg-muted/30 border-dashed">
                        <p className="text-sm text-muted-foreground text-center">
                            The subscription will be created without a linked user.
                            You can assign a user later from the subscription details.
                        </p>
                    </Card>
                )}
            </div>
        </ScrollArea>
    )
}

// ==================== Step 3: Configure ====================

function StepConfigure({
    label,
    onLabelChange,
    hasDataLimit,
    onHasDataLimitChange,
    dataLimitGb,
    onDataLimitGbChange,
    bandwidthLimit,
    onBandwidthLimitChange,
    hasExpiry,
    onHasExpiryChange,
    endDate,
    onEndDateChange,
    maxDevices,
    onMaxDevicesChange,
}: {
    label: string
    onLabelChange: (v: string) => void
    hasDataLimit: boolean
    onHasDataLimitChange: (v: boolean) => void
    dataLimitGb: string
    onDataLimitGbChange: (v: string) => void
    bandwidthLimit: string
    onBandwidthLimitChange: (v: string) => void
    hasExpiry: boolean
    onHasExpiryChange: (v: boolean) => void
    endDate: string
    onEndDateChange: (v: string) => void
    maxDevices: string
    onMaxDevicesChange: (v: string) => void
}) {
    const addDaysToNow = useCallback(
        (days: number) => {
            const d = new Date()
            d.setDate(d.getDate() + days)
            onEndDateChange(d.toISOString().split("T")[0])
        },
        [onEndDateChange]
    )

    const addMonthsToNow = useCallback(
        (months: number) => {
            const d = new Date()
            d.setMonth(d.getMonth() + months)
            onEndDateChange(d.toISOString().split("T")[0])
        },
        [onEndDateChange]
    )

    return (
        <ScrollArea className="h-full pr-1">
            <div className="space-y-6 pb-2">
                {/* Label */}
                <div className="space-y-2">
                    <Label htmlFor="sub-label" className="flex items-center gap-2">
                        <Tag className="w-4 h-4 text-primary" />
                        Label
                    </Label>
                    <Input
                        id="sub-label"
                        placeholder="e.g. John's VPN, Office Connection..."
                        value={label}
                        onChange={(e) => onLabelChange(e.target.value)}
                    />
                    <p className="text-xs text-muted-foreground">
                        A descriptive name for this subscription.
                    </p>
                </div>

                <Separator />

                {/* Data Limit */}
                <div className="space-y-3">
                    <div className="flex items-center justify-between">
                        <Label className="flex items-center gap-2">
                            <HardDrive className="w-4 h-4 text-primary" />
                            Data Limit
                        </Label>
                        <div className="flex items-center gap-2">
                            <span className="text-xs text-muted-foreground">
                                {hasDataLimit ? "Limited" : "Unlimited"}
                            </span>
                            <Switch
                                checked={hasDataLimit}
                                onCheckedChange={onHasDataLimitChange}
                            />
                        </div>
                    </div>
                    {hasDataLimit && (
                        <div className="space-y-3 pl-6 border-l-2 border-primary/20">
                            <div className="flex items-center gap-2">
                                <Input
                                    type="number"
                                    min="1"
                                    step="1"
                                    placeholder="Data limit"
                                    value={dataLimitGb}
                                    onChange={(e) => onDataLimitGbChange(e.target.value)}
                                    className="flex-1"
                                />
                                <span className="text-sm text-muted-foreground font-medium">
                                    GB
                                </span>
                            </div>
                            <div className="flex flex-wrap gap-1.5">
                                {[10, 25, 50, 100, 200, 500].map((gb) => (
                                    <Button
                                        key={gb}
                                        type="button"
                                        variant={dataLimitGb === String(gb) ? "default" : "outline"}
                                        size="sm"
                                        className="h-7 px-2.5 text-xs"
                                        onClick={() => onDataLimitGbChange(String(gb))}
                                    >
                                        {gb} GB
                                    </Button>
                                ))}
                            </div>
                        </div>
                    )}
                </div>

                {/* Speed limit feature - disabled
                <Separator />

                <div className="space-y-2">
                    <Label className="flex items-center gap-2">
                        <Wifi className="w-4 h-4 text-primary" />
                        Speed Limit
                    </Label>
                    <select
                        value={bandwidthLimit}
                        onChange={(e) => onBandwidthLimitChange(e.target.value)}
                        className="w-full h-9 rounded-md border border-input bg-background px-3 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
                    >
                        {BANDWIDTH_OPTIONS.map((opt) => (
                            <option key={opt.value} value={String(opt.value)}>
                                {opt.label}
                            </option>
                        ))}
                    </select>
                    <p className="text-xs text-muted-foreground">
                        Maximum bandwidth per connection. Requires TC bandwidth shaping on nodes.
                    </p>
                </div>
                */}

                <Separator />

                {/* Expiration */}
                <div className="space-y-3">
                    <div className="flex items-center justify-between">
                        <Label className="flex items-center gap-2">
                            <Calendar className="w-4 h-4 text-primary" />
                            Expiration
                        </Label>
                        <div className="flex items-center gap-2">
                            <span className="text-xs text-muted-foreground">
                                {hasExpiry ? "Set Date" : "No Expiry"}
                            </span>
                            <Switch
                                checked={hasExpiry}
                                onCheckedChange={onHasExpiryChange}
                            />
                        </div>
                    </div>
                    {hasExpiry && (
                        <div className="space-y-3 pl-6 border-l-2 border-primary/20">
                            <Input
                                type="date"
                                value={endDate}
                                onChange={(e) => onEndDateChange(e.target.value)}
                                min={new Date().toISOString().split("T")[0]}
                            />
                            <div className="flex flex-wrap gap-1.5">
                                {[
                                    { label: "+30d", fn: () => addDaysToNow(30) },
                                    { label: "+60d", fn: () => addDaysToNow(60) },
                                    { label: "+90d", fn: () => addDaysToNow(90) },
                                    { label: "+6mo", fn: () => addMonthsToNow(6) },
                                    { label: "+1yr", fn: () => addMonthsToNow(12) },
                                ].map((opt) => (
                                    <Button
                                        key={opt.label}
                                        type="button"
                                        variant="outline"
                                        size="sm"
                                        className="h-7 px-2.5 text-xs"
                                        onClick={opt.fn}
                                    >
                                        {opt.label}
                                    </Button>
                                ))}
                            </div>
                        </div>
                    )}
                </div>

                <Separator />

                {/* Max Devices */}
                <div className="space-y-2">
                    <Label htmlFor="max-devices" className="flex items-center gap-2">
                        <Smartphone className="w-4 h-4 text-primary" />
                        Max Devices
                    </Label>
                    <Input
                        id="max-devices"
                        type="number"
                        min="0"
                        value={maxDevices}
                        onChange={(e) => onMaxDevicesChange(e.target.value)}
                    />
                    <p className="text-xs text-muted-foreground">
                        Max concurrent WireGuard devices. 0 = inherit the plan's limit
                        (or 1 if no plan is attached).
                    </p>
                </div>
            </div>
        </ScrollArea>
    )
}

// ==================== Step 3: Review ====================

function StepReview({
    label,
    selectedInbounds,
    hasDataLimit,
    dataLimitGb,
    bandwidthLimit,
    hasExpiry,
    endDate,
    maxDevices,
    userMode,
    selectedUser,
    newUserForm,
}: {
    label: string
    selectedInbounds: InboundSelection[]
    hasDataLimit: boolean
    dataLimitGb: string
    bandwidthLimit: string
    hasExpiry: boolean
    endDate: string
    maxDevices: string
    userMode: UserMode
    selectedUser: User | null
    newUserForm: NewUserForm
}) {
    const deviceCount = parseInt(maxDevices) || 0

    return (
        <ScrollArea className="h-full pr-1">
            <div className="space-y-4 pb-2">
                {/* Summary Card */}
                <Card className="p-4 bg-card/50 border-primary/20 space-y-3">
                    <h4 className="text-sm font-medium flex items-center gap-2">
                        <Settings className="w-4 h-4 text-primary" />
                        Subscription Summary
                    </h4>
                    <div className="grid grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                Label
                            </p>
                            <p className="text-sm font-medium">{label || "—"}</p>
                        </div>
                        <div className="space-y-1">
                            <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                Servers
                            </p>
                            <p className="text-sm font-medium">
                                {selectedInbounds.length} inbound{selectedInbounds.length !== 1 ? "s" : ""}
                            </p>
                        </div>
                        <div className="space-y-1">
                            <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                Data Limit
                            </p>
                            <p className="text-sm font-medium">
                                {hasDataLimit && dataLimitGb ? `${dataLimitGb} GB` : "Unlimited"}
                            </p>
                        </div>
                        <div className="space-y-1">
                            <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                Speed Limit
                            </p>
                            <p className="text-sm font-medium">
                                {parseInt(bandwidthLimit) > 0 ? `${bandwidthLimit} Mbps` : "Unlimited"}
                            </p>
                        </div>
                        <div className="space-y-1">
                            <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                Expiration
                            </p>
                            <p className="text-sm font-medium">
                                {hasExpiry && endDate
                                    ? new Date(endDate).toLocaleDateString(undefined, {
                                        month: "short",
                                        day: "numeric",
                                        year: "numeric",
                                    })
                                    : "No Expiry"}
                            </p>
                        </div>
                        <div className="space-y-1">
                            <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                Max Devices
                            </p>
                            <p className="text-sm font-medium">
                                {deviceCount === 0 ? "Unlimited" : deviceCount}
                            </p>
                        </div>
                        <div className="space-y-1">
                            <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                User
                            </p>
                            <p className="text-sm font-medium">
                                {userMode === "none" && "No User"}
                                {userMode === "existing" && selectedUser && (
                                    <span className="flex items-center gap-1.5">
                                        <UserIcon className="w-3.5 h-3.5" />
                                        {selectedUser.username || `User #${selectedUser.id}`}
                                    </span>
                                )}
                                {userMode === "create" && (
                                    <span className="flex items-center gap-1.5">
                                        <UserPlus className="w-3.5 h-3.5" />
                                        {newUserForm.username} (new)
                                    </span>
                                )}
                            </p>
                        </div>
                    </div>
                </Card>

                {/* Selected Inbounds */}
                <div className="space-y-2">
                    <h4 className="text-sm font-medium flex items-center gap-2">
                        <Globe className="w-4 h-4 text-primary" />
                        Selected Inbounds
                    </h4>
                    <Card className="overflow-hidden divide-y divide-border/50">
                        {selectedInbounds.map(({ inbound, nodeName, nodeCountry }) => (
                            <div
                                key={inbound.id}
                                className="flex items-center gap-3 px-3 py-2 text-sm"
                            >
                                <Badge
                                    variant="outline"
                                    className="text-[10px] px-1.5 uppercase shrink-0"
                                >
                                    {nodeCountry}
                                </Badge>
                                <span className="text-muted-foreground truncate flex-1">
                                    {nodeName}
                                </span>
                                <Badge
                                    variant="default"
                                    className="text-[10px] px-1.5 font-mono uppercase shrink-0"
                                >
                                    {inbound.protocol}
                                </Badge>
                                <span className="text-xs font-mono text-muted-foreground shrink-0">
                                    :{inbound.port}
                                </span>
                            </div>
                        ))}
                    </Card>
                </div>
            </div>
        </ScrollArea>
    )
}

// ==================== Main Dialog ====================

export function CreateManualSubscriptionDialog() {
    const { createManualDialog, closeCreateManualDialog } = useSubscriptionsStore()
    const { open } = createManualDialog

    const createMutation = useCreateManualSubscription()

    // Wizard state
    const [step, setStep] = useState(0)

    const [selectedInboundIds, setSelectedInboundIds] = useState<Set<number>>(new Set())

    const [userMode, setUserMode] = useState<UserMode>("none")
    const [selectedUser, setSelectedUser] = useState<User | null>(null)
    const [newUserForm, setNewUserForm] = useState<NewUserForm>({
        username: "",
        firstName: "",
        lastName: "",
        telegramId: "",
    })
    const [creatingUser, setCreatingUser] = useState(false)

    const [label, setLabel] = useState("")
    const [hasDataLimit, setHasDataLimit] = useState(false)
    const [dataLimitGb, setDataLimitGb] = useState("")
    const [bandwidthLimit, setBandwidthLimit] = useState("0")
    const [hasExpiry, setHasExpiry] = useState(false)
    const [endDate, setEndDate] = useState("")
    const [maxDevices, setMaxDevices] = useState("0")

    // Fetch nodes
    const {
        data: nodesData,
        isLoading: nodesLoading,
        isError: nodesError,
    } = useQuery({
        queryKey: ["nodes"],
        queryFn: async () => {
            const res = await listNodes()
            if (!res.success) throw new Error(res.error || "Failed to fetch nodes")
            return res.data || []
        },
        enabled: open,
    })

    const nodes = nodesData || []

    // Build selected inbound details for review step
    const selectedInbounds = useMemo<InboundSelection[]>(() => {
        const result: InboundSelection[] = []
        for (const node of nodes) {
            for (const inbound of node.inbounds || []) {
                if (selectedInboundIds.has(inbound.id)) {
                    result.push({
                        inbound,
                        nodeName: node.name,
                        nodeCountry: node.country_code,
                    })
                }
            }
        }
        return result
    }, [nodes, selectedInboundIds])

    // Handlers
    const handleToggleInbound = useCallback((id: number) => {
        setSelectedInboundIds((prev) => {
            const next = new Set(prev)
            if (next.has(id)) {
                next.delete(id)
            } else {
                next.add(id)
            }
            return next
        })
    }, [])

    const handleToggleNode = useCallback(
        (_nodeId: number, inbounds: Inbound[], selectAll: boolean) => {
            setSelectedInboundIds((prev) => {
                const next = new Set(prev)
                for (const inbound of inbounds) {
                    if (selectAll) {
                        next.add(inbound.id)
                    } else {
                        next.delete(inbound.id)
                    }
                }
                return next
            })
        },
        []
    )

    const handleClose = useCallback(() => {
        closeCreateManualDialog()
        // Reset state after the dialog animation completes
        setTimeout(() => {
            setStep(0)
            setSelectedInboundIds(new Set())
            setUserMode("none")
            setSelectedUser(null)
            setNewUserForm({ username: "", firstName: "", lastName: "", telegramId: "" })
            setCreatingUser(false)
            setLabel("")
            setHasDataLimit(false)
            setDataLimitGb("")
            setHasExpiry(false)
            setEndDate("")
            setMaxDevices("0")
        }, 200)
    }, [closeCreateManualDialog])

    const handleCreate = useCallback(async () => {
        let userId: number | undefined

        // If creating a new user, do that first
        if (userMode === "create") {
            setCreatingUser(true)
            try {
                const res = await createUserAPI({
                    username: newUserForm.username.trim(),
                    first_name: newUserForm.firstName.trim(),
                    last_name: newUserForm.lastName.trim() || undefined,
                    telegram_id: newUserForm.telegramId ? parseInt(newUserForm.telegramId) : undefined,
                })
                if (!res.success || !res.data) {
                    throw new Error(res.error || "Failed to create user")
                }
                userId = res.data.id
            } catch (err) {
                toast.error(err instanceof Error ? err.message : "Failed to create user")
                setCreatingUser(false)
                return
            }
            setCreatingUser(false)
        } else if (userMode === "existing" && selectedUser) {
            userId = selectedUser.id
        }

        createMutation.mutate(
            {
                label: label.trim(),
                inbound_ids: Array.from(selectedInboundIds),
                data_limit_gb: hasDataLimit && dataLimitGb ? parseFloat(dataLimitGb) : null,
                bandwidth_limit: parseInt(bandwidthLimit) || 0,
                max_devices: parseInt(maxDevices) || 0,
                end_date: hasExpiry && endDate ? new Date(endDate + "T23:59:59").toISOString() : null,
                user_id: userId ?? null,
            },
            {
                onSuccess: () => {
                    handleClose()
                },
            }
        )
    }, [
        createMutation,
        label,
        selectedInboundIds,
        hasDataLimit,
        dataLimitGb,
        hasExpiry,
        endDate,
        maxDevices,
        userMode,
        selectedUser,
        newUserForm,
        handleClose,
    ])

    // Validation
    const canProceedStep0 = selectedInboundIds.size > 0
    const canProceedStep1 =
        userMode === "none" ||
        (userMode === "existing" && selectedUser !== null) ||
        (userMode === "create" && newUserForm.username.trim().length > 0)
    const canProceedStep2 = true
    const isLastStep = step === 3

    return (
        <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
            <DialogContent className="left-0 top-0 translate-x-0 translate-y-0 md:left-[50%] md:top-[50%] md:translate-x-[-50%] md:translate-y-[-50%] w-full md:w-[calc(100%-2rem)] max-w-none md:max-w-2xl h-[100dvh] md:h-[600px] md:max-h-[95vh] rounded-none md:rounded-2xl border-0 md:border flex flex-col gap-0 p-0 overflow-hidden">
                {/* Header */}
                <div className="px-4 md:px-6 pt-5 md:pt-6 pb-2 md:pb-3 space-y-2">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <Server className="w-5 h-5 text-primary" />
                            Create Manual Subscription
                        </DialogTitle>
                        <DialogDescription>
                            Create a VPN subscription, optionally linked to a user account.
                        </DialogDescription>
                    </DialogHeader>
                    <StepIndicator currentStep={step} />
                </div>

                <Separator />

                {/* Content */}
                <div className="px-4 md:px-6 py-4 flex-1 min-h-0 overflow-hidden">
                    {step === 0 && (
                        <StepSelectServers
                            selectedInboundIds={selectedInboundIds}
                            onToggleInbound={handleToggleInbound}
                            onToggleNode={handleToggleNode}
                            nodes={nodes}
                            isLoading={nodesLoading}
                            isError={nodesError}
                        />
                    )}
                    {step === 1 && (
                        <StepSelectUser
                            userMode={userMode}
                            onUserModeChange={setUserMode}
                            selectedUser={selectedUser}
                            onSelectedUserChange={setSelectedUser}
                            newUserForm={newUserForm}
                            onNewUserFormChange={setNewUserForm}
                        />
                    )}
                    {step === 2 && (
                        <StepConfigure
                            label={label}
                            onLabelChange={setLabel}
                            hasDataLimit={hasDataLimit}
                            onHasDataLimitChange={setHasDataLimit}
                            dataLimitGb={dataLimitGb}
                            onDataLimitGbChange={setDataLimitGb}
                            bandwidthLimit={bandwidthLimit}
                            onBandwidthLimitChange={setBandwidthLimit}
                            hasExpiry={hasExpiry}
                            onHasExpiryChange={setHasExpiry}
                            endDate={endDate}
                            onEndDateChange={setEndDate}
                            maxDevices={maxDevices}
                            onMaxDevicesChange={setMaxDevices}
                        />
                    )}
                    {step === 3 && (
                        <StepReview
                            label={label}
                            selectedInbounds={selectedInbounds}
                            hasDataLimit={hasDataLimit}
                            dataLimitGb={dataLimitGb}
                            bandwidthLimit={bandwidthLimit}
                            hasExpiry={hasExpiry}
                            endDate={endDate}
                            maxDevices={maxDevices}
                            userMode={userMode}
                            selectedUser={selectedUser}
                            newUserForm={newUserForm}
                        />
                    )}
                </div>

                <Separator />

                {/* Footer Navigation */}
                <div className="px-4 md:px-6 py-3 md:py-4 flex items-center justify-between">
                    <Button
                        variant="outline"
                        onClick={() => setStep((s) => s - 1)}
                        disabled={step === 0}
                        className={cn(step === 0 && "invisible")}
                    >
                        <ArrowLeft className="w-4 h-4 mr-2" />
                        Back
                    </Button>

                    {isLastStep ? (
                        <Button
                            onClick={handleCreate}
                            disabled={createMutation.isPending || creatingUser}
                        >
                            {createMutation.isPending || creatingUser ? (
                                <>
                                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                                    {creatingUser ? "Creating User..." : "Creating..."}
                                </>
                            ) : (
                                <>
                                    <Check className="w-4 h-4 mr-2" />
                                    Create Subscription
                                </>
                            )}
                        </Button>
                    ) : (
                        <Button
                            onClick={() => setStep((s) => s + 1)}
                            disabled={
                                (step === 0 && !canProceedStep0) ||
                                (step === 1 && !canProceedStep1) ||
                                (step === 2 && !canProceedStep2)
                            }
                        >
                            Next
                            <ArrowRight className="w-4 h-4 ml-2" />
                        </Button>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    )
}
