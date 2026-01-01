import { useState, useMemo, useCallback } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Search, ChevronDown, Server } from "lucide-react"
import { cn } from "@/lib/utils"
import type { Node, Inbound } from "@/lib/types"

// ==================== Types ====================

export interface InboundTreeSelectorProps {
    nodes: Node[]
    selectedInboundIds: Set<number>
    onToggleInbound: (inboundId: number) => void
    onToggleNode: (nodeId: number, inbounds: Inbound[], selectAll: boolean) => void
    disabledInboundIds?: Set<number>
    disabledLabel?: string
    inboundAnnotations?: Map<number, string>
    filterInboundIds?: Set<number>
}

// ==================== Constants ====================

const COUNTRY_FLAGS: Record<string, string> = {
    US: "🇺🇸", GB: "🇬🇧", DE: "🇩🇪", NL: "🇳🇱", FR: "🇫🇷",
    FI: "🇫🇮", SE: "🇸🇪", NO: "🇳🇴", CA: "🇨🇦", JP: "🇯🇵",
    SG: "🇸🇬", AU: "🇦🇺", IR: "🇮🇷", TR: "🇹🇷", RU: "🇷🇺",
    HK: "🇭🇰", KR: "🇰🇷", IN: "🇮🇳", BR: "🇧🇷", AE: "🇦🇪",
    ZA: "🇿🇦", IT: "🇮🇹", ES: "🇪🇸", PL: "🇵🇱", AT: "🇦🇹",
    CH: "🇨🇭", IE: "🇮🇪", DK: "🇩🇰", CZ: "🇨🇿", RO: "🇷🇴",
    UA: "🇺🇦", BG: "🇧🇬", HU: "🇭🇺", LT: "🇱🇹", LV: "🇱🇻",
    EE: "🇪🇪", PT: "🇵🇹", GR: "🇬🇷", RS: "🇷🇸", HR: "🇭🇷",
    SK: "🇸🇰", SI: "🇸🇮", IL: "🇮🇱", TW: "🇹🇼", VN: "🇻🇳",
    TH: "🇹🇭", MY: "🇲🇾", PH: "🇵🇭", ID: "🇮🇩", MX: "🇲🇽",
    AR: "🇦🇷", CL: "🇨🇱", CO: "🇨🇴",
}

const PROTOCOL_STYLES: Record<string, { bg: string; text: string; border: string }> = {
    vless:       { bg: "bg-indigo-500/10",  text: "text-indigo-400",  border: "border-indigo-500/20" },
    vmess:       { bg: "bg-violet-500/10",  text: "text-violet-400",  border: "border-violet-500/20" },
    trojan:      { bg: "bg-amber-500/10",   text: "text-amber-400",   border: "border-amber-500/20" },
    shadowsocks: { bg: "bg-cyan-500/10",    text: "text-cyan-400",    border: "border-cyan-500/20" },
    hysteria:    { bg: "bg-rose-500/10",    text: "text-rose-400",    border: "border-rose-500/20" },
    hysteria2:   { bg: "bg-rose-500/10",    text: "text-rose-400",    border: "border-rose-500/20" },
    tuic:        { bg: "bg-teal-500/10",    text: "text-teal-400",    border: "border-teal-500/20" },
    wireguard:   { bg: "bg-emerald-500/10", text: "text-emerald-400", border: "border-emerald-500/20" },
}

const DEFAULT_PROTOCOL_STYLE = { bg: "bg-primary/10", text: "text-primary", border: "border-primary/20" }

function getProtocolStyle(protocol: string) {
    return PROTOCOL_STYLES[protocol.toLowerCase()] || DEFAULT_PROTOCOL_STYLE
}

// ==================== InboundTreeSelector ====================

export function InboundTreeSelector({
    nodes,
    selectedInboundIds,
    onToggleInbound,
    onToggleNode,
    disabledInboundIds = new Set(),
    disabledLabel = "Already assigned",
    inboundAnnotations,
    filterInboundIds,
}: InboundTreeSelectorProps) {
    const [search, setSearch] = useState("")
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
        // Apply filterInboundIds first (Remove tab mode)
        const visibleNodes = filterInboundIds
            ? nodes
                .map((node) => {
                    const visibleInbounds = (node.inbounds || []).filter((inb) =>
                        filterInboundIds.has(inb.id)
                    )
                    if (visibleInbounds.length === 0) return null
                    return { ...node, inbounds: visibleInbounds }
                })
                .filter(Boolean) as Node[]
            : nodes

        // Apply search filter
        if (!search.trim()) return visibleNodes
        const q = search.toLowerCase()
        return visibleNodes
            .map((node) => {
                const nodeMatch =
                    node.name.toLowerCase().includes(q) ||
                    node.country_code.toLowerCase().includes(q)
                const filteredInbounds = (node.inbounds || []).filter(
                    (inb) =>
                        inb.protocol.toLowerCase().includes(q) ||
                        inb.tag.toLowerCase().includes(q) ||
                        inb.remark.toLowerCase().includes(q) ||
                        String(inb.port).includes(q) ||
                        inb.network.toLowerCase().includes(q)
                )
                if (nodeMatch) return node
                if (filteredInbounds.length > 0) return { ...node, inbounds: filteredInbounds }
                return null
            })
            .filter(Boolean) as Node[]
    }, [nodes, search, filterInboundIds])

    const selectedCount = selectedInboundIds.size

    return (
        <div className="flex flex-col gap-3">
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
            <ScrollArea className="max-h-[400px] pr-1">
                {filteredNodes.length === 0 ? (
                    <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
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
                    <div className="space-y-2.5 pb-1">
                        {filteredNodes.map((node) => (
                            <NodeRow
                                key={node.id}
                                node={node}
                                isExpanded={expandedNodes.has(node.id)}
                                onToggleExpanded={toggleExpanded}
                                selectedInboundIds={selectedInboundIds}
                                onToggleInbound={onToggleInbound}
                                onToggleNode={onToggleNode}
                                disabledInboundIds={disabledInboundIds}
                                disabledLabel={disabledLabel}
                                inboundAnnotations={inboundAnnotations}
                            />
                        ))}
                    </div>
                )}
            </ScrollArea>
        </div>
    )
}

// ==================== NodeRow ====================

interface NodeRowProps {
    node: Node
    isExpanded: boolean
    onToggleExpanded: (nodeId: number) => void
    selectedInboundIds: Set<number>
    onToggleInbound: (inboundId: number) => void
    onToggleNode: (nodeId: number, inbounds: Inbound[], selectAll: boolean) => void
    disabledInboundIds: Set<number>
    disabledLabel: string
    inboundAnnotations?: Map<number, string>
}

function NodeRow({
    node,
    isExpanded,
    onToggleExpanded,
    selectedInboundIds,
    onToggleInbound,
    onToggleNode,
    disabledInboundIds,
    disabledLabel,
    inboundAnnotations,
}: NodeRowProps) {
    const inbounds = node.inbounds || []
    const flag = COUNTRY_FLAGS[node.country_code.toUpperCase()] || "🌐"

    // Only selectable inbounds (not disabled) count for select-all logic
    const selectableInbounds = inbounds.filter((inb) => !disabledInboundIds.has(inb.id))
    const allSelected =
        selectableInbounds.length > 0 &&
        selectableInbounds.every((inb) => selectedInboundIds.has(inb.id))
    const someSelected = inbounds.some((inb) => selectedInboundIds.has(inb.id))
    const selectedInNode = inbounds.filter((inb) => selectedInboundIds.has(inb.id)).length

    return (
        <div
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
                className="w-full flex items-center gap-3 px-4 py-3.5 transition-colors text-left group hover:bg-muted/40"
                onClick={() => onToggleExpanded(node.id)}
            >
                <div
                    className={cn(
                        "w-5 h-5 flex items-center justify-center transition-transform duration-200",
                        isExpanded ? "rotate-0" : "-rotate-90"
                    )}
                >
                    <ChevronDown className="w-4 h-4 text-muted-foreground group-hover:text-foreground transition-colors" />
                </div>

                <div className="flex items-center gap-2.5 flex-1 min-w-0">
                    <span className="text-lg leading-none" role="img" aria-label={node.country_code}>
                        {flag}
                    </span>
                    <span className="font-semibold text-sm truncate">{node.name}</span>
                    <Badge
                        variant="outline"
                        className="text-[10px] px-1.5 py-0 uppercase shrink-0 font-mono tracking-wider hidden sm:inline-flex"
                    >
                        {node.country_code}
                    </Badge>
                    <div className="flex items-center gap-1.5 shrink-0">
                        <div
                            className={cn(
                                "w-1.5 h-1.5 rounded-full",
                                node.is_online
                                    ? "bg-emerald-500 shadow-sm shadow-emerald-500/50"
                                    : "bg-red-500 shadow-sm shadow-red-500/50"
                            )}
                        />
                        <span
                            className={cn(
                                "text-[11px] font-medium hidden sm:inline",
                                node.is_online ? "text-emerald-500" : "text-red-500"
                            )}
                        >
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
                    {selectableInbounds.length > 0 && (
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
                                onToggleNode(node.id, selectableInbounds, !allSelected)
                            }}
                        >
                            <span className="hidden sm:inline">
                                {allSelected ? "Deselect All" : "Select All"}
                            </span>
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
                            {inbounds.map((inbound) => (
                                <InboundRow
                                    key={inbound.id}
                                    inbound={inbound}
                                    isSelected={selectedInboundIds.has(inbound.id)}
                                    isDisabled={disabledInboundIds.has(inbound.id)}
                                    disabledLabel={disabledLabel}
                                    annotation={inboundAnnotations?.get(inbound.id)}
                                    onToggle={onToggleInbound}
                                />
                            ))}
                        </div>
                    )}
                </div>
            )}
        </div>
    )
}

// ==================== InboundRow ====================

interface InboundRowProps {
    inbound: Inbound
    isSelected: boolean
    isDisabled: boolean
    disabledLabel: string
    annotation?: string
    onToggle: (inboundId: number) => void
}

function InboundRow({
    inbound,
    isSelected,
    isDisabled,
    disabledLabel,
    annotation,
    onToggle,
}: InboundRowProps) {
    const pStyle = getProtocolStyle(inbound.protocol)

    return (
        <label
            className={cn(
                "flex items-center gap-2 sm:gap-3 px-2 sm:px-3 py-2.5 rounded-lg transition-all duration-150",
                isDisabled
                    ? "opacity-50 cursor-not-allowed"
                    : "cursor-pointer hover:bg-muted/50",
                isSelected && !isDisabled
                    ? "bg-primary/[0.06] ring-1 ring-primary/15"
                    : "bg-transparent"
            )}
        >
            <Checkbox
                checked={isSelected}
                disabled={isDisabled}
                onCheckedChange={() => !isDisabled && onToggle(inbound.id)}
                className="transition-all duration-150"
            />
            <div className="flex items-center gap-2 flex-1 min-w-0">
                {/* Protocol badge */}
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
                <span className="inline-flex items-center rounded-md bg-muted/80 px-1.5 py-0.5 text-[11px] font-mono text-muted-foreground shrink-0">
                    :{inbound.port}
                </span>
                {/* Network pill */}
                <span className="inline-flex items-center rounded-md bg-muted/60 px-1.5 py-0.5 text-[11px] text-muted-foreground shrink-0">
                    {inbound.network}
                </span>
                {/* Security badge */}
                {inbound.security !== "none" && (
                    <Badge
                        variant={
                            inbound.security === "tls"
                                ? "success"
                                : inbound.security === "reality"
                                  ? "warning"
                                  : "secondary"
                        }
                        className="text-[10px] px-1.5 py-0 shrink-0"
                    >
                        {inbound.security}
                    </Badge>
                )}
                {/* Remark */}
                {inbound.remark && (
                    <span className="text-[10px] text-muted-foreground/70 truncate italic hidden sm:inline">
                        {inbound.remark}
                    </span>
                )}
            </div>

            {/* Disabled label or annotation */}
            {isDisabled ? (
                <span className="text-[10px] text-muted-foreground italic shrink-0">
                    {disabledLabel}
                </span>
            ) : annotation ? (
                <span className="text-[10px] text-muted-foreground tabular-nums shrink-0">
                    {annotation}
                </span>
            ) : null}
        </label>
    )
}
