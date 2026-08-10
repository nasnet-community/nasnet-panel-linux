import { useState } from "react"
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { toast } from "sonner"
import { HiOutlinePlus, HiOutlinePencil, HiOutlineTrash } from "react-icons/hi"
import { addBalancingRule, updateBalancingRule, deleteBalancingRule } from "@/lib/admin-api"
import type { BalancingRule, Outbound } from "@/lib/types"

const STRATEGIES = [
    { value: "random", label: "Random" },
    { value: "leastping", label: "Least Ping" },
    { value: "roundrobin", label: "Round Robin" },
    { value: "leastload", label: "Least Load" },
] as const

// Strategies that depend on observatory probe data. The BE auto-emits an
// observatory block when any of these are configured, but users should know
// it adds periodic outbound probes.
const STRATEGY_NEEDS_OBSERVATORY = new Set(["leastping", "leastload"])

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

interface BalancingRulesCardProps {
    nodeId: number
    outbounds: Outbound[]
    rules: BalancingRule[]
    onRulesChanged: () => void
}

export function BalancingRulesCard({ nodeId, outbounds, rules, onRulesChanged }: BalancingRulesCardProps) {
    const [dialogOpen, setDialogOpen] = useState(false)
    const [dialogMode, setDialogMode] = useState<"create" | "edit">("create")
    const [editingId, setEditingId] = useState<number | null>(null)
    const [form, setForm] = useState<FormData>(emptyForm)
    const [saving, setSaving] = useState(false)

    const openCreate = () => {
        setForm(emptyForm)
        setDialogMode("create")
        setEditingId(null)
        setDialogOpen(true)
    }

    const openEdit = (rule: BalancingRule) => {
        setForm({
            tag: rule.tag,
            strategy: rule.strategy,
            outbound_selectors: [...rule.outbound_selectors],
            fallback_tag: rule.fallback_tag,
            enabled: rule.enabled,
        })
        setDialogMode("edit")
        setEditingId(rule.id)
        setDialogOpen(true)
    }

    const handleSave = async () => {
        const tag = form.tag.trim()
        if (!tag) {
            toast.error("Tag is required")
            return
        }
        if (!/^[a-zA-Z0-9_:.-]+$/.test(tag)) {
            toast.error("Tag can only contain letters, numbers, underscores, colons, dots, and hyphens")
            return
        }
        if (form.outbound_selectors.length === 0) {
            toast.error("Select at least one outbound")
            return
        }
        // Per-node uniqueness — BE enforces too, but block early.
        const conflict = rules.some(r =>
            r.tag === tag && (dialogMode === "create" || r.id !== editingId)
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
                tag: form.tag.trim(),
                strategy: form.strategy,
                outbound_selectors: form.outbound_selectors,
                fallback_tag: form.fallback_tag.trim(),
                enabled: form.enabled,
            }
            if (dialogMode === "create") {
                const res = await addBalancingRule(nodeId, payload)
                if (!res.success) throw new Error(res.error || "Failed to add balancing rule")
                toast.success("Balancing rule created")
            } else {
                const res = await updateBalancingRule(editingId!, payload)
                if (!res.success) throw new Error(res.error || "Failed to update balancing rule")
                toast.success("Balancing rule updated")
            }
            setDialogOpen(false)
            onRulesChanged()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Operation failed")
        } finally {
            setSaving(false)
        }
    }

    const handleDelete = async (rule: BalancingRule) => {
        if (!confirm(`Delete balancing rule "${rule.tag}"?`)) return
        try {
            const res = await deleteBalancingRule(rule.id)
            if (!res.success) throw new Error(res.error || "Failed to delete")
            toast.success("Balancing rule deleted")
            onRulesChanged()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Delete failed")
        }
    }

    const toggleSelector = (tag: string) => {
        setForm(prev => {
            const selected = new Set(prev.outbound_selectors)
            if (selected.has(tag)) {
                selected.delete(tag)
            } else {
                selected.add(tag)
            }
            return { ...prev, outbound_selectors: Array.from(selected) }
        })
    }

    const strategyLabel = (s: Strategy) =>
        STRATEGIES.find(st => st.value === s)?.label ?? s

    const strategyColor = (s: Strategy) => {
        switch (s) {
            case "random": return "bg-blue-500/10 text-blue-400 border-blue-500/30"
            case "leastping": return "bg-green-500/10 text-green-400 border-green-500/30"
            case "roundrobin": return "bg-amber-500/10 text-amber-400 border-amber-500/30"
            case "leastload": return "bg-purple-500/10 text-purple-400 border-purple-500/30"
        }
    }

    return (
        <>
            <Card className="mt-6">
                <CardHeader className="items-center pb-4">
                    <CardTitle className="text-lg font-semibold">Balancing Rules</CardTitle>
                    <CardAction className="self-center">
                        <Button size="sm" onClick={openCreate}>
                            <HiOutlinePlus className="w-4 h-4 mr-2" />
                            Add Balancer
                        </Button>
                    </CardAction>
                </CardHeader>
                <CardContent>
                    {rules.length === 0 ? (
                        <p className="text-sm text-muted-foreground text-center py-8">
                            No balancing rules configured
                        </p>
                    ) : (
                        <div className="space-y-3">
                            {rules.map(rule => (
                                <div
                                    key={rule.id}
                                    className="flex items-center gap-3 p-3 rounded-lg border bg-card/50 group"
                                >
                                    <div className="flex-1 min-w-0">
                                        <div className="flex items-center gap-2 flex-wrap">
                                            <span className="font-mono font-semibold text-sm">{rule.tag}</span>
                                            <Badge variant="outline" className={strategyColor(rule.strategy)}>
                                                {strategyLabel(rule.strategy)}
                                            </Badge>
                                            <Badge variant="secondary" className="text-xs">
                                                {rule.outbound_selectors.length} outbound{rule.outbound_selectors.length !== 1 ? "s" : ""}
                                            </Badge>
                                            {rule.fallback_tag && (
                                                <span className="text-xs text-muted-foreground">
                                                    fallback: <span className="font-mono">{rule.fallback_tag}</span>
                                                </span>
                                            )}
                                        </div>
                                    </div>
                                    <Badge variant={rule.enabled ? "success" : "secondary"} className="shrink-0">
                                        {rule.enabled ? "Active" : "Disabled"}
                                    </Badge>
                                    <div className="flex gap-1 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
                                        <Button variant="ghost" size="icon" onClick={() => openEdit(rule)}>
                                            <HiOutlinePencil className="w-4 h-4" />
                                        </Button>
                                        <Button
                                            variant="ghost"
                                            size="icon"
                                            onClick={() => handleDelete(rule)}
                                            className="text-red-500 hover:text-red-600 hover:bg-red-100/50"
                                        >
                                            <HiOutlineTrash className="w-4 h-4" />
                                        </Button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </CardContent>
            </Card>

            <ResponsiveDialog
                open={dialogOpen}
                onOpenChange={setDialogOpen}
                title={dialogMode === "create" ? "Add Balancing Rule" : "Edit Balancing Rule"}
                onSave={handleSave}
                saveLabel={saving ? "Saving..." : "Save"}
                saveDisabled={saving}
            >
                <div className="space-y-4">
                    <div className="space-y-2">
                        <Label htmlFor="bal-tag">Tag</Label>
                        <Input
                            id="bal-tag"
                            placeholder="e.g. my-balancer"
                            value={form.tag}
                            onChange={e => setForm(prev => ({ ...prev, tag: e.target.value }))}
                        />
                    </div>

                    <div className="space-y-2">
                        <Label>Strategy</Label>
                        <Select
                            value={form.strategy}
                            onValueChange={v => setForm(prev => ({ ...prev, strategy: v as Strategy }))}
                        >
                            <SelectTrigger>
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {STRATEGIES.map(s => (
                                    <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        {STRATEGY_NEEDS_OBSERVATORY.has(form.strategy) && (
                            <p className="text-[11px] text-muted-foreground leading-snug">
                                Uses xray observatory to probe each outbound (~10s interval). The probe URL comes from the routing settings &quot;Outbound Test URL&quot; field, or defaults to <span className="font-mono">https://www.google.com/generate_204</span>.
                            </p>
                        )}
                        {form.strategy === "random" && form.fallback_tag.trim() !== "" && (
                            <p className="text-[11px] text-muted-foreground leading-snug">
                                Random + fallback tag also requires the observatory; it is emitted automatically.
                            </p>
                        )}
                    </div>

                    <div className="space-y-2">
                        <Label>Outbound Selectors</Label>
                        <p className="text-xs text-muted-foreground">
                            Select which outbounds this balancer can route to
                        </p>
                        <div className="max-h-48 overflow-y-auto rounded-lg border p-2 space-y-1">
                            {outbounds.length === 0 ? (
                                <p className="text-xs text-muted-foreground text-center py-2">No outbounds available</p>
                            ) : (
                                outbounds.map(ob => (
                                    <label
                                        key={ob.id}
                                        className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-accent cursor-pointer"
                                    >
                                        <Checkbox
                                            checked={form.outbound_selectors.includes(ob.tag)}
                                            onCheckedChange={() => toggleSelector(ob.tag)}
                                        />
                                        <span className="font-mono text-sm">{ob.tag}</span>
                                        <Badge variant="outline" className="text-[10px] ml-auto">{ob.protocol}</Badge>
                                    </label>
                                ))
                            )}
                        </div>
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="bal-fallback">Fallback Outbound</Label>
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
                            Used when no outbound is alive (or when the strategy returns empty).
                        </p>
                    </div>

                    <div className="flex items-center justify-between">
                        <Label htmlFor="bal-enabled">Enabled</Label>
                        <Switch
                            id="bal-enabled"
                            checked={form.enabled}
                            onCheckedChange={v => setForm(prev => ({ ...prev, enabled: v }))}
                        />
                    </div>
                </div>
            </ResponsiveDialog>
        </>
    )
}
