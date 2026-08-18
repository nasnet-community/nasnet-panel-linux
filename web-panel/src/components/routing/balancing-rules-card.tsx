import { useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { toast } from "sonner"
import { HiOutlinePlus, HiOutlinePencil, HiOutlineTrash } from "react-icons/hi"
import { deleteBalancingRule } from "@/lib/admin-api"
import {
    BalancingRuleDialog,
    STRATEGIES,
    expandSelectorPrefixes,
    rulesUsingBalancer,
} from "@/components/routing/balancing-rule-dialog"
import type { BalancingRule, Outbound, RoutingRule } from "@/lib/types"

type Strategy = BalancingRule["strategy"]

interface BalancingRulesCardProps {
    nodeId: number
    outbounds: Outbound[]
    rules: BalancingRule[]
    routingRules?: RoutingRule[]
    onRulesChanged: () => void
}

export function BalancingRulesCard({ nodeId, outbounds, rules, routingRules = [], onRulesChanged }: BalancingRulesCardProps) {
    const [dialogOpen, setDialogOpen] = useState(false)
    const [dialogMode, setDialogMode] = useState<"create" | "edit">("create")
    const [editing, setEditing] = useState<BalancingRule | null>(null)

    const openCreate = () => {
        setEditing(null)
        setDialogMode("create")
        setDialogOpen(true)
    }

    const openEdit = (rule: BalancingRule) => {
        setEditing(rule)
        setDialogMode("edit")
        setDialogOpen(true)
    }

    const handleDelete = async (rule: BalancingRule) => {
        // A rule left pointing at a deleted balancer makes xray reject the whole
        // config on the next push — the BE does not check this, so block here.
        const used = rulesUsingBalancer(rule.tag, routingRules)
        if (used.length > 0) {
            toast.error(
                `${used.length} routing rule${used.length !== 1 ? "s" : ""} still target "${rule.tag}" (${used.map(r => r.rule_tag).join(", ")}). Repoint them first.`,
            )
            return
        }
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

    const strategyLabel = (s: Strategy) => STRATEGIES.find(st => st.value === s)?.label ?? s

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
            <Card>
                <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
                    <div>
                        <CardTitle className="text-lg font-semibold">Balancing Rules</CardTitle>
                        <p className="text-sm text-muted-foreground mt-0.5">
                            Spread matched traffic across several outbounds
                        </p>
                    </div>
                    <Button size="sm" onClick={openCreate}>
                        <HiOutlinePlus className="w-4 h-4 mr-2" />
                        Add Balancer
                    </Button>
                </CardHeader>
                <CardContent>
                    {rules.length === 0 ? (
                        <p className="text-sm text-muted-foreground text-center py-8">
                            No balancing rules configured
                        </p>
                    ) : (
                        <div className="space-y-3">
                            {rules.map(rule => {
                                const extras = expandSelectorPrefixes(rule.outbound_selectors, outbounds)
                                const used = rulesUsingBalancer(rule.tag, routingRules)
                                return (
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
                                                    {rule.outbound_selectors.length + extras.length} outbound{rule.outbound_selectors.length + extras.length !== 1 ? "s" : ""}
                                                </Badge>
                                                {extras.length > 0 && (
                                                    <span className="text-[11px] text-amber-300">
                                                        +{extras.length} by prefix
                                                    </span>
                                                )}
                                                {rule.fallback_tag && (
                                                    <span className="text-xs text-muted-foreground">
                                                        fallback: <span className="font-mono">{rule.fallback_tag}</span>
                                                    </span>
                                                )}
                                            </div>
                                            <p className="text-[11px] text-muted-foreground mt-1">
                                                {used.length === 0 ? (
                                                    "No routing rule targets it yet"
                                                ) : (
                                                    <>Used by <span className="font-mono">{used.map(r => r.rule_tag).join(", ")}</span></>
                                                )}
                                            </p>
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
                                )
                            })}
                        </div>
                    )}
                </CardContent>
            </Card>

            <BalancingRuleDialog
                open={dialogOpen}
                onOpenChange={setDialogOpen}
                mode={dialogMode}
                rule={editing}
                nodeId={nodeId}
                outbounds={outbounds}
                existingBalancers={rules}
                routingRules={routingRules}
                onSaved={onRulesChanged}
            />
        </>
    )
}
