import React, { useState, useEffect, useMemo, useCallback } from "react"
import {
    DndContext,
    DragOverlay,
    PointerSensor,
    KeyboardSensor,
    closestCenter,
    useSensor,
    useSensors,
    type DragStartEvent,
    type DragEndEvent,
} from "@dnd-kit/core"
import {
    SortableContext,
    verticalListSortingStrategy,
    useSortable,
    arrayMove,
} from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import { GripVertical } from "lucide-react"
import { HiOutlinePencil, HiOutlineTrash, HiOutlineMap, HiOutlinePlus } from "react-icons/hi"
import { cn } from "@/lib/utils"
import { toast } from "sonner"
import { reorderRoutingRules } from "@/lib/admin-api"
import { SwipeableRoutingRow } from "@/components/node/swipeable-routing-row"
import type { RoutingRule } from "@/lib/types"

type DisplayRule = RoutingRule & { _unsaved?: boolean }

interface RoutingRulesTableProps {
    nodeId: number
    rules: DisplayRule[]
    onEdit: (rule: RoutingRule) => void
    onDelete: (rule: DisplayRule) => void
    onToggle: (rule: RoutingRule) => void
    onReorderSaved: () => void
    onCreate: () => void
}

// --- Sortable Row ---

function SortableRoutingRow({
    rule,
    rowIndex,
    onEdit,
    onDelete,
    onToggle,
}: {
    rule: RoutingRule
    rowIndex: number
    onEdit: (rule: RoutingRule) => void
    onDelete: (rule: DisplayRule) => void
    onToggle: (rule: RoutingRule) => void
}) {
    const {
        attributes,
        listeners,
        setNodeRef,
        transform,
        transition,
        isDragging,
    } = useSortable({ id: rule.id })

    // Don't apply CSS transform to <tr> elements — browsers don't support it
    // properly with table layout. The DragOverlay handles the visual feedback.
    const style = {
        transform: CSS.Translate.toString(transform),
        transition,
        opacity: isDragging ? 0.4 : undefined,
    }

    return (
        <TableRow
            ref={setNodeRef}
            style={style}
            className={cn("group", isDragging && "bg-muted/50")}
        >
            <RuleRowCells
                rule={rule}
                rowIndex={rowIndex}
                isUnsaved={false}
                dragHandleProps={{ ...attributes, ...listeners }}
                onEdit={onEdit}
                onDelete={onDelete}
                onToggle={onToggle}
            />
        </TableRow>
    )
}

// --- Shared Row Cells ---

function formatDomainRules(rule: RoutingRule): string {
    if (!rule.domain_rules?.length) return ""
    const items = rule.domain_rules.map(d => {
        // Values with xray-recognized prefixes (geosite:, ext:) are shown as-is
        if (d.value.startsWith("geosite:") || d.value.startsWith("ext:")) return d.value
        if (d.type === "plain") return d.value
        return `${d.type}:${d.value}`
    })
    if (items.length <= 2) return items.join(", ")
    return `${items.slice(0, 2).join(", ")} +${items.length - 2}`
}

function formatIPRules(rule: RoutingRule): string {
    const parts: string[] = []
    if (rule.geoip_rules?.length) {
        // Values already include "geoip:" prefix from DB
        parts.push(...rule.geoip_rules)
    }
    if (rule.ipcidr_rules?.length) {
        parts.push(...rule.ipcidr_rules)
    }
    if (parts.length === 0) return ""
    if (parts.length <= 2) return parts.join(", ")
    return `${parts.slice(0, 2).join(", ")} +${parts.length - 2}`
}

function RuleRowCells({
    rule,
    rowIndex,
    isUnsaved,
    dragHandleProps,
    onEdit,
    onDelete,
    onToggle,
}: {
    rule: RoutingRule
    rowIndex: number
    isUnsaved: boolean
    dragHandleProps?: Record<string, unknown>
    onEdit: (rule: RoutingRule) => void
    onDelete: (rule: DisplayRule) => void
    onToggle?: (rule: RoutingRule) => void
}) {
    const domainText = formatDomainRules(rule)
    const ipText = formatIPRules(rule)
    const portText = rule.port_rules?.length ? rule.port_rules.join(", ") : ""
    const networkParts: string[] = []
    if (rule.network_rules?.length) networkParts.push(...rule.network_rules)
    if (rule.protocol_rules?.length) networkParts.push(...rule.protocol_rules)
    const networkText = networkParts.join(", ")
    const inboundText = rule.inbound_tags?.length ? rule.inbound_tags.join(", ") : ""
    const outboundText = rule.outbound_tag || rule.balancing_tag || "—"

    return (
        <>
            {/* # */}
            <TableCell className="w-10 text-center text-muted-foreground text-xs font-mono tabular-nums">
                {rowIndex + 1}
            </TableCell>

            {/* Drag handle */}
            <TableCell className="w-8 px-0">
                {!isUnsaved && dragHandleProps ? (
                    <button
                        className="cursor-grab active:cursor-grabbing p-1 text-muted-foreground hover:text-foreground touch-none"
                        {...dragHandleProps}
                    >
                        <GripVertical className="w-4 h-4" />
                    </button>
                ) : (
                    <span className="w-4 h-4 block" />
                )}
            </TableCell>

            {/* Tag */}
            <TableCell className="max-w-[140px]">
                <div className="truncate">
                    <span className="font-mono font-semibold text-sm">{rule.rule_tag}</span>
                    {rule.remark && (
                        <p className="text-xs text-muted-foreground truncate">
                            {rule.remark.startsWith("[reverse]") ? (
                                <Badge variant="outline" className="text-[10px] px-1 py-0 mr-1 text-purple-400 border-purple-500/30">Reverse</Badge>
                            ) : null}
                            {rule.remark.startsWith("[reverse]") ? rule.remark.replace(/^\[reverse\]\s*/, "") : rule.remark}
                        </p>
                    )}
                </div>
            </TableCell>

            {/* Network */}
            <TableCell className="max-w-[100px]">
                {networkText ? (
                    <span className="text-xs font-mono truncate block">{networkText}</span>
                ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                )}
            </TableCell>

            {/* Domain */}
            <TableCell className="max-w-[180px]">
                {domainText ? (
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <span className="text-xs font-mono truncate block cursor-help">{domainText}</span>
                        </TooltipTrigger>
                        <TooltipContent side="bottom" className="max-w-sm">
                            <div className="space-y-0.5 font-mono text-xs">
                                {rule.domain_rules.map((d, i) => (
                                    <div key={i}>{
                                        d.value.startsWith("geosite:") || d.value.startsWith("ext:") ? d.value
                                        : d.type === "plain" ? d.value
                                        : `${d.type}:${d.value}`
                                    }</div>
                                ))}
                            </div>
                        </TooltipContent>
                    </Tooltip>
                ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                )}
            </TableCell>

            {/* IP */}
            <TableCell className="max-w-[140px]">
                {ipText ? (
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <span className="text-xs font-mono truncate block cursor-help">{ipText}</span>
                        </TooltipTrigger>
                        <TooltipContent side="bottom" className="max-w-sm">
                            <div className="space-y-0.5 font-mono text-xs">
                                {rule.geoip_rules?.map((g, i) => <div key={`g${i}`}>{g}</div>)}
                                {rule.ipcidr_rules?.map((c, i) => <div key={`c${i}`}>{c}</div>)}
                            </div>
                        </TooltipContent>
                    </Tooltip>
                ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                )}
            </TableCell>

            {/* Port */}
            <TableCell className="max-w-[80px]">
                {portText ? (
                    <span className="text-xs font-mono truncate block">{portText}</span>
                ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                )}
            </TableCell>

            {/* Inbound */}
            <TableCell className="max-w-[100px]">
                {inboundText ? (
                    <span className="text-xs font-mono truncate block">{inboundText}</span>
                ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                )}
            </TableCell>

            {/* Outbound */}
            <TableCell className="max-w-[120px]">
                <span className="text-xs font-mono font-medium truncate block">{outboundText}</span>
            </TableCell>

            {/* Status */}
            <TableCell>
                {isUnsaved ? (
                    <Badge variant="outline" className="text-blue-400 border-blue-500/30">Pending</Badge>
                ) : (
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <button onClick={() => onToggle?.(rule)} className="cursor-pointer">
                                <Badge variant={rule.enabled ? "success" : "secondary"}>
                                    {rule.enabled ? "Active" : "Disabled"}
                                </Badge>
                            </button>
                        </TooltipTrigger>
                        <TooltipContent>Click to {rule.enabled ? "disable" : "enable"}</TooltipContent>
                    </Tooltip>
                )}
            </TableCell>

            {/* Actions */}
            <TableCell>
                {!isUnsaved && (
                    <div className="flex gap-1 justify-end">
                        {rule.remark?.startsWith("[reverse]") ? (
                            <Tooltip>
                                <TooltipTrigger asChild>
                                    <span className="text-xs text-muted-foreground opacity-0 group-hover:opacity-100 px-2">Managed</span>
                                </TooltipTrigger>
                                <TooltipContent>Managed by reverse proxy — edit or delete from the Reverse tab</TooltipContent>
                            </Tooltip>
                        ) : (
                            <>
                                <Button variant="ghost" size="icon" onClick={() => onEdit(rule)} className="opacity-0 group-hover:opacity-100">
                                    <HiOutlinePencil className="w-4 h-4" />
                                </Button>
                                <Button variant="ghost" size="icon" onClick={() => onDelete(rule)} className="opacity-0 group-hover:opacity-100 text-red-500 hover:text-red-600 hover:bg-red-100/50">
                                    <HiOutlineTrash className="w-4 h-4" />
                                </Button>
                            </>
                        )}
                    </div>
                )}
            </TableCell>
        </>
    )
}

// --- Drag Overlay Row ---

function DragOverlayRow({ rule, rowIndex }: { rule: RoutingRule; rowIndex: number }) {
    return (
        <Table>
            <TableBody>
                <TableRow className="bg-card shadow-lg border">
                    <RuleRowCells
                        rule={rule}
                        rowIndex={rowIndex}
                        isUnsaved={false}
                        onEdit={() => {}}
                        onDelete={() => {}}
                    />
                </TableRow>
            </TableBody>
        </Table>
    )
}

// --- Main Component ---

export function RoutingRulesTable({
    nodeId,
    rules,
    onEdit,
    onDelete,
    onToggle,
    onReorderSaved,
    onCreate,
}: RoutingRulesTableProps) {
    // Separate saved (has id) vs unsaved rules
    const savedRules = useMemo(() => rules.filter(r => r.id > 0 && !r._unsaved), [rules])
    const unsavedRules = useMemo(() => rules.filter(r => r.id === 0 || r._unsaved), [rules])

    // Local order state for saved rules
    const [localOrder, setLocalOrder] = useState<number[]>([])
    const [originalOrder, setOriginalOrder] = useState<number[]>([])
    const [isSaving, setIsSaving] = useState(false)
    const [activeId, setActiveId] = useState<number | null>(null)

    // Swipeable state for mobile
    const [openSwipeId, setOpenSwipeId] = useState<number | null>(null)

    // Sync order from props
    useEffect(() => {
        const ids = savedRules.map(r => r.id)
        setLocalOrder(ids)
        setOriginalOrder(ids)
    }, [savedRules])

    const hasOrderChanged = useMemo(
        () => JSON.stringify(localOrder) !== JSON.stringify(originalOrder),
        [localOrder, originalOrder],
    )

    // Map localOrder to rule objects for display
    const ruleMap = useMemo(() => {
        const m = new Map<number, DisplayRule>()
        for (const r of savedRules) m.set(r.id, r)
        return m
    }, [savedRules])

    const orderedSavedRules = useMemo(
        () => localOrder.map(id => ruleMap.get(id)).filter(Boolean) as DisplayRule[],
        [localOrder, ruleMap],
    )

    // DnD sensors
    const sensors = useSensors(
        useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
        useSensor(KeyboardSensor),
    )

    const handleDragStart = useCallback((event: DragStartEvent) => {
        setActiveId(event.active.id as number)
    }, [])

    const handleDragEnd = useCallback((event: DragEndEvent) => {
        setActiveId(null)
        const { active, over } = event
        if (!over || active.id === over.id) return
        setLocalOrder(prev => {
            const oldIndex = prev.indexOf(active.id as number)
            const newIndex = prev.indexOf(over.id as number)
            if (oldIndex === -1 || newIndex === -1) return prev
            return arrayMove(prev, oldIndex, newIndex)
        })
    }, [])

    const handleLocalMove = useCallback((ruleId: number, direction: "up" | "down") => {
        setLocalOrder(prev => {
            const idx = prev.indexOf(ruleId)
            if (idx === -1) return prev
            const newIdx = direction === "up" ? idx - 1 : idx + 1
            if (newIdx < 0 || newIdx >= prev.length) return prev
            return arrayMove(prev, idx, newIdx)
        })
    }, [])

    const handleSaveOrder = useCallback(async () => {
        setIsSaving(true)
        try {
            const res = await reorderRoutingRules(nodeId, localOrder)
            if (!res.success) throw new Error(res.error || "Failed to save order")
            toast.success("Rule order saved")
            onReorderSaved()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to save order")
        } finally {
            setIsSaving(false)
        }
    }, [nodeId, localOrder, onReorderSaved])

    const handleDiscardOrder = useCallback(() => {
        setLocalOrder(originalOrder)
    }, [originalOrder])

    const activeRule = activeId ? ruleMap.get(activeId) : null
    const activeIndex = activeId ? localOrder.indexOf(activeId) : -1

    const allRules = rules
    const isEmpty = allRules.length === 0

    return (
        <div>
            {/* Header */}
            <div className="flex items-center justify-between mb-4">
                <div>
                    <h3 className="text-lg font-semibold">Routing Rules</h3>
                    <p className="text-sm text-muted-foreground">Control how traffic flows through the node</p>
                </div>
                <Button size="sm" onClick={onCreate}>
                    <HiOutlinePlus className="w-4 h-4 mr-2" />
                    Add Rule
                </Button>
            </div>

            {/* Save / Discard bar */}
            {hasOrderChanged && (
                <div className="flex items-center gap-3 px-4 py-2.5 mb-4 rounded-lg bg-amber-500/10 border border-amber-500/20 text-amber-200">
                    <span className="text-sm flex-1">Rule order has been changed</span>
                    <Button
                        variant="ghost"
                        size="sm"
                        onClick={handleDiscardOrder}
                        disabled={isSaving}
                    >
                        Discard
                    </Button>
                    <Button
                        size="sm"
                        onClick={handleSaveOrder}
                        disabled={isSaving}
                    >
                        {isSaving ? "Saving..." : "Save Order"}
                    </Button>
                </div>
            )}

            {/* Mobile view */}
            <div className="md:hidden">
                {!isEmpty ? (
                    <div className="divide-y rounded-xl border overflow-hidden mb-4">
                        {orderedSavedRules.map((rule, idx) => (
                            <SwipeableRoutingRow
                                key={rule.id}
                                rule={rule}
                                shouldClose={openSwipeId !== null && openSwipeId !== rule.id}
                                onOpen={setOpenSwipeId}
                                onEdit={(r) => onEdit(r)}
                                onDelete={onDelete}
                                onToggle={onToggle}
                                onMoveUp={() => handleLocalMove(rule.id, "up")}
                                onMoveDown={() => handleLocalMove(rule.id, "down")}
                                isFirst={idx === 0}
                                isLast={idx === orderedSavedRules.length - 1 && unsavedRules.length === 0}
                            />
                        ))}
                        {unsavedRules.map((rule) => (
                            <SwipeableRoutingRow
                                key={rule.rule_tag}
                                rule={rule}
                                shouldClose={false}
                                onOpen={() => {}}
                                onEdit={() => {}}
                                onDelete={onDelete}
                            />
                        ))}
                    </div>
                ) : (
                    <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                        <HiOutlineMap className="w-12 h-12 opacity-50 mb-4" />
                        <p>No routing rules configured</p>
                    </div>
                )}
            </div>

            {/* Desktop view */}
            <TooltipProvider delayDuration={200}>
                <div className="hidden md:block rounded-2xl border bg-card/50 backdrop-blur-sm border-white/5 overflow-hidden">
                    {!isEmpty ? (
                        <div className="overflow-x-auto">
                            <DndContext
                                sensors={sensors}
                                collisionDetection={closestCenter}
                                onDragStart={handleDragStart}
                                onDragEnd={handleDragEnd}
                            >
                                <Table>
                                    <TableHeader>
                                        <TableRow className="bg-muted/50">
                                            <TableHead className="w-10 text-center">#</TableHead>
                                            <TableHead className="w-8 px-0"></TableHead>
                                            <TableHead className="max-w-[140px]">Tag</TableHead>
                                            <TableHead className="max-w-[100px]">Network</TableHead>
                                            <TableHead className="max-w-[180px]">Domain</TableHead>
                                            <TableHead className="max-w-[140px]">IP</TableHead>
                                            <TableHead className="max-w-[80px]">Port</TableHead>
                                            <TableHead className="max-w-[100px]">Inbound</TableHead>
                                            <TableHead className="max-w-[120px]">Outbound</TableHead>
                                            <TableHead>Status</TableHead>
                                            <TableHead className="w-[100px]"></TableHead>
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        <SortableContext
                                            items={localOrder}
                                            strategy={verticalListSortingStrategy}
                                        >
                                            {orderedSavedRules.map((rule, idx) => (
                                                <SortableRoutingRow
                                                    key={rule.id}
                                                    rule={rule}
                                                    rowIndex={idx}
                                                    onEdit={onEdit}
                                                    onDelete={onDelete}
                                                    onToggle={onToggle}
                                                />
                                            ))}
                                        </SortableContext>

                                        {/* Unsaved rules at the bottom */}
                                        {unsavedRules.map((rule, idx) => (
                                            <TableRow
                                                key={rule.rule_tag}
                                                className="bg-blue-500/[0.03]"
                                            >
                                                <RuleRowCells
                                                    rule={rule as RoutingRule}
                                                    rowIndex={orderedSavedRules.length + idx}
                                                    isUnsaved={true}
                                                    onEdit={onEdit}
                                                    onDelete={onDelete}
                                                />
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>

                                <DragOverlay>
                                    {activeRule ? (
                                        <DragOverlayRow rule={activeRule} rowIndex={activeIndex} />
                                    ) : null}
                                </DragOverlay>
                            </DndContext>
                        </div>
                    ) : (
                        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                            <HiOutlineMap className="w-12 h-12 opacity-50 mb-4" />
                            <p>No routing rules configured</p>
                        </div>
                    )}
                </div>
            </TooltipProvider>
        </div>
    )
}
