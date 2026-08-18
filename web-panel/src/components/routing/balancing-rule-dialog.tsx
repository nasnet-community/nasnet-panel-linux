import { useEffect, useMemo, useState } from "react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { toast } from "sonner"
import { HiOutlineExclamation, HiOutlineInformationCircle, HiOutlineSearch } from "react-icons/hi"
import { addBalancingRule, updateBalancingRule } from "@/lib/admin-api"
import type { BalancingRule, Outbound, RoutingRule } from "@/lib/types"

export const STRATEGIES = [
    { value: "leastping", label: "Least ping", description: "Fastest responder wins.", probes: true },
    { value: "random", label: "Random", description: "Even spread, no measurement.", probes: false },
    { value: "roundrobin", label: "Round robin", description: "Each connection takes the next one.", probes: false },
    { value: "leastload", label: "Least load", description: "Lowest measured load wins.", probes: true },
] as const

// Strategies that depend on observatory probe data. The BE auto-emits an
// observatory block when any of these are configured, but users should know
// it adds periodic outbound probes.
const STRATEGY_NEEDS_OBSERVATORY = new Set(["leastping", "leastload"])

const TAG_RE = /^[a-zA-Z0-9_:.-]+$/

type Strategy = BalancingRule["strategy"]

interface FormData {
    tag: string
    strategy: Strategy
    outbound_selectors: string[]
    fallback_tag: string
    enabled: boolean
}

const emptyForm: FormData = {
    tag: "",
    strategy: "random",
    outbound_selectors: [],
    fallback_tag: "",
    enabled: true,
}

/**
 * xray matches balancer selectors by PREFIX, not by exact tag: a selector of
 * "epodonios" also picks up an outbound tagged "epodoniosR". Returns the
 * outbound tags that join the pool without being ticked.
 */
export function expandSelectorPrefixes(selectors: string[], outbounds: Outbound[]): string[] {
    const chosen = new Set(selectors)
    const extra = new Set<string>()
    for (const ob of outbounds) {
        if (chosen.has(ob.tag)) continue
        if (selectors.some(sel => sel !== "" && ob.tag.startsWith(sel))) extra.add(ob.tag)
    }
    return Array.from(extra)
}

/** Routing rules that point at this balancer — deleting it while these exist breaks the xray config. */
export function rulesUsingBalancer(tag: string, rules: RoutingRule[]): RoutingRule[] {
    if (!tag) return []
    return rules.filter(r => r.balancing_tag === tag)
}

interface BalancingRuleDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    mode: "create" | "edit"
    rule: BalancingRule | null
    nodeId: number
    outbounds: Outbound[]
    existingBalancers: BalancingRule[]
    routingRules?: RoutingRule[]
    /** Fired after a successful save with the balancer's tag — parent refreshes and may select it. */
    onSaved: (tag: string) => void
    /** Nested inside the routing-rule dialog: only the fields needed to make a working balancer. */
    compact?: boolean
}

export function BalancingRuleDialog({
    open,
    onOpenChange,
    mode,
    rule,
    nodeId,
    outbounds,
    existingBalancers,
    routingRules = [],
    onSaved,
    compact = false,
}: BalancingRuleDialogProps) {
    const [form, setForm] = useState<FormData>(emptyForm)
    const [saving, setSaving] = useState(false)
    const [filter, setFilter] = useState("")

    useEffect(() => {
        if (!open) return
        setFilter("")
        if (mode === "edit" && rule) {
            setForm({
                tag: rule.tag,
                strategy: rule.strategy,
                outbound_selectors: [...rule.outbound_selectors],
                fallback_tag: rule.fallback_tag,
                enabled: rule.enabled,
            })
        } else {
            setForm(emptyForm)
        }
    }, [open, mode, rule])

    const toggleSelector = (tag: string) => {
        setForm(prev => {
            const selected = new Set(prev.outbound_selectors)
            if (selected.has(tag)) selected.delete(tag)
            else selected.add(tag)
            return { ...prev, outbound_selectors: Array.from(selected) }
        })
    }

    const visibleOutbounds = useMemo(() => {
        const q = filter.trim().toLowerCase()
        if (!q) return outbounds
        return outbounds.filter(o => o.tag.toLowerCase().includes(q) || o.protocol.toLowerCase().includes(q))
    }, [outbounds, filter])

    const prefixExtras = useMemo(
        () => expandSelectorPrefixes(form.outbound_selectors, outbounds),
        [form.outbound_selectors, outbounds],
    )
    const effectiveCount = form.outbound_selectors.length + prefixExtras.length

    const usedBy = useMemo(
        () => (mode === "edit" && rule ? rulesUsingBalancer(rule.tag, routingRules) : []),
        [mode, rule, routingRules],
    )

    const invalidReason = (() => {
        const tag = form.tag.trim()
        if (!tag) return "Give the balancer a tag"
        if (!TAG_RE.test(tag)) return "Tag: letters, numbers, - _ : . only"
        if (form.outbound_selectors.length === 0) return "Select at least one outbound"
        return null
    })()

    const handleSave = async () => {
        const tag = form.tag.trim()
        if (invalidReason) {
            toast.error(invalidReason)
            return
        }
        // Per-node uniqueness — BE enforces too, but block early.
        const conflict = existingBalancers.some(r =>
            r.tag === tag && (mode === "create" || r.id !== rule?.id)
        )
        if (conflict) {
            toast.error(`A balancer with tag "${tag}" already exists on this node`)
            return
        }
        const fallback = form.fallback_tag.trim()
        if (fallback && !outbounds.some(o => o.tag === fallback)) {
            toast.error(`Fallback tag "${fallback}" must match an existing outbound`)
            return
        }
        setSaving(true)
        try {
            const payload = {
                tag,
                strategy: form.strategy,
                outbound_selectors: form.outbound_selectors,
                fallback_tag: fallback,
                enabled: form.enabled,
            }
            if (mode === "create") {
                const res = await addBalancingRule(nodeId, payload)
                if (!res.success) throw new Error(res.error || "Failed to add balancing rule")
                toast.success("Balancer created")
            } else {
                const res = await updateBalancingRule(rule!.id, payload)
                if (!res.success) throw new Error(res.error || "Failed to update balancing rule")
                toast.success("Balancer updated")
            }
            onOpenChange(false)
            onSaved(tag)
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Operation failed")
        } finally {
            setSaving(false)
        }
    }

    const strategyProbes = STRATEGY_NEEDS_OBSERVATORY.has(form.strategy)
    const randomWithFallback = form.strategy === "random" && form.fallback_tag.trim() !== ""

    return (
        <ResponsiveDialog
            open={open}
            onOpenChange={onOpenChange}
            title={mode === "create" ? (compact ? "New balancer" : "Add balancer") : "Edit balancer"}
            description={
                compact
                    ? "It will be selected for this rule when you save."
                    : "Spread matched traffic across several outbounds."
            }
            onSave={handleSave}
            saveLabel={saving ? "Saving..." : mode === "create" ? (compact ? "Create and use" : "Add balancer") : "Save changes"}
            saveDisabled={saving || Boolean(invalidReason)}
            className="max-w-xl"
            footerNote={
                invalidReason ? (
                    <span className="text-amber-400">{invalidReason}</span>
                ) : compact ? (
                    "Returns you to the rule"
                ) : (
                    <>
                        {effectiveCount} outbound{effectiveCount !== 1 ? "s" : ""}
                        {prefixExtras.length > 0 && " after prefix expansion"} ·{" "}
                        {STRATEGIES.find(s => s.value === form.strategy)?.label.toLowerCase()}
                        {form.fallback_tag && <> · fallback <span className="font-mono">{form.fallback_tag}</span></>}
                    </>
                )
            }
        >
            {/* ═══ Identity ═══ */}
            <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-4 sm:items-start">
                <div className="space-y-2">
                    <Label htmlFor="bal-tag">Tag <span className="text-red-400">*</span></Label>
                    <Input
                        id="bal-tag"
                        className="font-mono"
                        placeholder="eu-pool"
                        value={form.tag}
                        onChange={e => setForm(prev => ({ ...prev, tag: e.target.value }))}
                    />
                    <p className="text-[11px] text-muted-foreground">
                        {usedBy.length > 0
                            ? `Rules point at this name. Renaming it detaches the ${usedBy.length} rule${usedBy.length !== 1 ? "s" : ""} below.`
                            : "Routing rules target a balancer by this name."}
                    </p>
                </div>
                {!compact && (
                    <div className="space-y-2">
                        <Label htmlFor="bal-enabled">Balancer is active</Label>
                        <div className="flex items-center gap-3 h-9">
                            <Switch
                                id="bal-enabled"
                                checked={form.enabled}
                                onCheckedChange={v => setForm(prev => ({ ...prev, enabled: v }))}
                            />
                            <span className="text-sm">{form.enabled ? "Enabled" : "Disabled"}</span>
                        </div>
                    </div>
                )}
            </div>

            {/* ═══ Strategy ═══ */}
            <div className="space-y-3">
                <SectionLabel>Strategy</SectionLabel>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    {STRATEGIES.map(s => {
                        const active = form.strategy === s.value
                        return (
                            <button
                                key={s.value}
                                type="button"
                                onClick={() => setForm(prev => ({ ...prev, strategy: s.value as Strategy }))}
                                className={`text-left rounded-lg border p-3 transition-colors ${active
                                    ? "border-primary/45 bg-white/[0.05]"
                                    : "border-white/10 bg-white/[0.012] hover:bg-white/[0.03]"
                                    }`}
                            >
                                <div className="flex items-center gap-2">
                                    <span className={`w-3.5 h-3.5 rounded-full border shrink-0 ${active ? "border-primary bg-primary" : "border-white/25"}`} />
                                    <span className="text-[13px] font-semibold">{s.label}</span>
                                    {s.probes && (
                                        <Badge variant="outline" className="ml-auto text-[9px] uppercase tracking-wide text-amber-300 border-amber-500/35">
                                            probes
                                        </Badge>
                                    )}
                                </div>
                                <p className="text-[11px] text-muted-foreground mt-1 ml-[22px] leading-snug">{s.description}</p>
                            </button>
                        )
                    })}
                </div>
                {(strategyProbes || randomWithFallback) && (
                    <Notice tone="info">
                        {strategyProbes ? (
                            <>
                                Probes every selected outbound about every 10s using the routing settings{" "}
                                <span className="font-mono">Outbound Test URL</span>, or{" "}
                                <span className="font-mono">https://www.google.com/generate_204</span> by default.
                            </>
                        ) : (
                            <>Random + a fallback tag also requires the observatory; it is emitted automatically.</>
                        )}
                    </Notice>
                )}
            </div>

            {/* ═══ Outbounds ═══ */}
            <div className="space-y-3">
                <SectionLabel
                    right={`${form.outbound_selectors.length} of ${outbounds.length} selected`}
                >
                    Outbounds
                </SectionLabel>
                <div className="rounded-lg border border-white/10 overflow-hidden">
                    {outbounds.length > 6 && (
                        <div className="flex items-center gap-2 px-2 py-2 border-b border-white/[0.06] bg-white/[0.02]">
                            <HiOutlineSearch className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                            <input
                                value={filter}
                                onChange={e => setFilter(e.target.value)}
                                placeholder="Filter by tag or protocol"
                                className="flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground"
                            />
                            <Button variant="ghost" size="sm" className="h-7 text-xs"
                                onClick={() => setForm(prev => ({ ...prev, outbound_selectors: outbounds.map(o => o.tag) }))}>
                                All
                            </Button>
                            <Button variant="ghost" size="sm" className="h-7 text-xs"
                                onClick={() => setForm(prev => ({ ...prev, outbound_selectors: [] }))}>
                                None
                            </Button>
                        </div>
                    )}
                    <div className="max-h-64 overflow-y-auto">
                        {visibleOutbounds.length === 0 ? (
                            <p className="text-xs text-muted-foreground text-center py-4">
                                {outbounds.length === 0 ? "No outbounds on this node" : "Nothing matches that filter"}
                            </p>
                        ) : (
                            visibleOutbounds.map(ob => {
                                const checked = form.outbound_selectors.includes(ob.tag)
                                const viaPrefix = !checked && prefixExtras.includes(ob.tag)
                                return (
                                    <button
                                        key={ob.id}
                                        type="button"
                                        onClick={() => toggleSelector(ob.tag)}
                                        className="w-full flex items-center gap-3 px-3 py-2 border-b border-white/[0.06] last:border-b-0 hover:bg-white/[0.03] text-left"
                                    >
                                        <span className={`w-4 h-4 rounded border shrink-0 flex items-center justify-center text-[10px] font-bold ${checked ? "bg-primary border-primary text-primary-foreground" : "border-white/25"}`}>
                                            {checked ? "✓" : ""}
                                        </span>
                                        <span className="font-mono text-[13px]">{ob.tag}</span>
                                        <Badge variant="outline" className="text-[10px]">{ob.protocol}</Badge>
                                        {viaPrefix && (
                                            <span className="ml-auto text-[10px] text-amber-300">matched by prefix</span>
                                        )}
                                    </button>
                                )
                            })
                        )}
                    </div>
                </div>
                {prefixExtras.length > 0 && (
                    <Notice tone="warn">
                        xray matches selectors by <b>prefix</b>.{" "}
                        <span className="font-mono">{prefixExtras.join(", ")}</span>{" "}
                        {prefixExtras.length === 1 ? "is" : "are"} pulled in too, so this balancer really has{" "}
                        <b>{effectiveCount}</b> outbounds.
                    </Notice>
                )}
            </div>

            {/* ═══ Fallback ═══ */}
            {!compact && (
                <div className="space-y-2">
                    <SectionLabel>Fallback</SectionLabel>
                    <Select
                        value={form.fallback_tag === "" ? "__none" : form.fallback_tag}
                        onValueChange={v => setForm(prev => ({ ...prev, fallback_tag: v === "__none" ? "" : v }))}
                    >
                        <SelectTrigger id="bal-fallback">
                            <SelectValue placeholder="None" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="__none">None</SelectItem>
                            {outbounds.map(ob => (
                                <SelectItem key={ob.id} value={ob.tag}>
                                    <span className="font-mono">{ob.tag}</span>
                                    <span className="text-muted-foreground"> ({ob.protocol})</span>
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <p className="text-[11px] text-muted-foreground leading-snug">
                        Used when nothing in the pool answers, or the strategy comes back empty. Leave as None to drop the connection.
                    </p>
                </div>
            )}

            {/* ═══ Used by ═══ */}
            {!compact && mode === "edit" && (
                <div className="space-y-2">
                    <SectionLabel right={`${usedBy.length} routing rule${usedBy.length !== 1 ? "s" : ""}`}>
                        Used by
                    </SectionLabel>
                    {usedBy.length === 0 ? (
                        <p className="text-[11px] text-muted-foreground">
                            No routing rule targets this balancer yet — it has no effect until one does.
                        </p>
                    ) : (
                        <>
                            <div className="flex flex-wrap gap-1.5">
                                {usedBy.map(r => (
                                    <Badge key={r.id} variant="secondary" className="font-mono text-[11px]">{r.rule_tag}</Badge>
                                ))}
                            </div>
                            <Notice tone="danger">
                                Deleting this balancer while rules point at it makes xray reject the whole config on the
                                next push, and the node stops routing. Repoint those rules first.
                            </Notice>
                        </>
                    )}
                </div>
            )}
        </ResponsiveDialog>
    )
}

function SectionLabel({ children, right }: { children: React.ReactNode; right?: string }) {
    return (
        <div className="flex items-center gap-2">
            <span className="text-[10.5px] uppercase tracking-[0.12em] text-muted-foreground/70 font-medium">{children}</span>
            {right && <span className="text-[11px] text-muted-foreground">{right}</span>}
            <span className="flex-1 h-px bg-white/[0.06]" />
        </div>
    )
}

function Notice({ tone, children }: { tone: "info" | "warn" | "danger"; children: React.ReactNode }) {
    const styles = {
        info: "border-blue-500/28 bg-blue-500/[0.07] text-blue-200",
        warn: "border-amber-500/30 bg-amber-500/[0.07] text-amber-200",
        danger: "border-red-500/30 bg-red-500/[0.07] text-red-200",
    }[tone]
    const Icon = tone === "info" ? HiOutlineInformationCircle : HiOutlineExclamation
    return (
        <div className={`flex gap-2 items-start rounded-lg border px-3 py-2 text-[12px] leading-relaxed ${styles}`}>
            <Icon className="w-4 h-4 shrink-0 mt-0.5" />
            <div>{children}</div>
        </div>
    )
}
