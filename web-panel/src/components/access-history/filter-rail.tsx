import { useEffect, useMemo, useState } from "react"
import type { DateRange } from "react-day-picker"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { cn } from "@/lib/utils"
import { DateRangePicker } from "@/components/subscription/access-history/date-range-picker"
import { SubscriptionPicker, type SubscriptionPickerValue } from "./subscription-picker"
import { useNodes } from "@/lib/queries/use-nodes"

export type RangePreset = "24h" | "7d" | "14d" | "30d" | "custom"

export interface FilterRailValue {
    preset: RangePreset
    customRange: DateRange | undefined
    subscriptions: SubscriptionPickerValue[]
    emails: string[]
    nodeIDs: number[]
}

export const PRESETS: { key: RangePreset; label: string; ms: number | null }[] = [
    { key: "24h", label: "24h", ms: 24 * 60 * 60 * 1000 },
    { key: "7d", label: "7d", ms: 7 * 24 * 60 * 60 * 1000 },
    { key: "14d", label: "14d", ms: 14 * 24 * 60 * 60 * 1000 },
    { key: "30d", label: "30d", ms: 30 * 24 * 60 * 60 * 1000 },
    { key: "custom", label: "Custom", ms: null },
]

interface Props {
    value: FilterRailValue
    onChange: (v: FilterRailValue) => void
    onClearAll?: () => void
    className?: string
    retentionDays?: number
}

export function FilterRail({ value, onChange, onClearAll, className, retentionDays = 30 }: Props) {
    const nodes = useNodes().data ?? []
    const [emailDraft, setEmailDraft] = useState("")

    useEffect(() => { setEmailDraft("") }, [value.emails.length])

    const update = (patch: Partial<FilterRailValue>) => onChange({ ...value, ...patch })

    // Disable days outside the retention window in the custom picker —
    // selecting them only ever returns empty results.
    const minDate = useMemo(() => {
        const d = new Date()
        d.setDate(d.getDate() - retentionDays)
        d.setHours(0, 0, 0, 0)
        return d
    }, [retentionDays])
    const maxDate = useMemo(() => new Date(), [retentionDays])

    const activeCount = value.subscriptions.length + value.emails.length + value.nodeIDs.length + (value.preset !== "7d" ? 1 : 0)

    const addEmail = () => {
        const e = emailDraft.trim()
        if (!e) return
        if (value.emails.includes(e)) { setEmailDraft(""); return }
        update({ emails: [...value.emails, e] })
    }

    const toggleNode = (id: number) => {
        update({
            nodeIDs: value.nodeIDs.includes(id)
                ? value.nodeIDs.filter(n => n !== id)
                : [...value.nodeIDs, id],
        })
    }

    return (
        <Card className={cn("p-4 md:p-5 space-y-5 text-sm", className)}>
            <div className="flex items-center justify-between">
                <h3 className="text-xs uppercase tracking-wider font-semibold text-muted-foreground">Filters</h3>
                {activeCount > 0 && (
                    <button
                        type="button"
                        onClick={onClearAll}
                        className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                    >
                        Clear all
                    </button>
                )}
            </div>

            <RailSection
                label="Range"
                badge={value.preset !== "7d" ? value.preset : undefined}
            >
                <div className="grid grid-cols-5 gap-1">
                    {PRESETS.map(p => (
                        <Button
                            key={p.key}
                            size="sm"
                            variant={value.preset === p.key ? "default" : "outline"}
                            className="h-8 px-0 text-xs font-medium"
                            onClick={() => update({ preset: p.key })}
                        >
                            {p.label}
                        </Button>
                    ))}
                </div>
                {value.preset === "custom" && (
                    <div className="pt-1">
                        <DateRangePicker
                            value={value.customRange}
                            onChange={r => update({ customRange: r })}
                            minDate={minDate}
                            maxDate={maxDate}
                        />
                    </div>
                )}
            </RailSection>

            <Separator />

            <RailSection
                label="Subscriptions"
                badge={value.subscriptions.length > 0 ? String(value.subscriptions.length) : undefined}
                onClear={value.subscriptions.length > 0 ? () => update({ subscriptions: [] }) : undefined}
            >
                <SubscriptionPicker
                    value={value.subscriptions}
                    onChange={subs => update({ subscriptions: subs })}
                />
            </RailSection>

            <Separator />

            <RailSection
                label="Emails"
                badge={value.emails.length > 0 ? String(value.emails.length) : undefined}
                onClear={value.emails.length > 0 ? () => update({ emails: [] }) : undefined}
            >
                <div className="flex gap-1.5">
                    <Input
                        value={emailDraft}
                        onChange={e => setEmailDraft(e.target.value)}
                        onKeyDown={e => { if (e.key === "Enter") { e.preventDefault(); addEmail() } }}
                        placeholder="exact email…"
                        className="h-8 text-xs flex-1"
                    />
                    <Button size="sm" variant="outline" className="h-8 px-2.5 text-xs" onClick={addEmail}>Add</Button>
                </div>
                {value.emails.length > 0 && (
                    <div className="flex flex-wrap gap-1 pt-0.5">
                        {value.emails.map(e => (
                            <Badge
                                key={e}
                                variant="secondary"
                                className="gap-1 pr-1 text-[10px] font-mono"
                            >
                                <span className="truncate max-w-[140px]">{e}</span>
                                <button
                                    type="button"
                                    onClick={() => update({ emails: value.emails.filter(x => x !== e) })}
                                    className="hover:text-foreground text-muted-foreground"
                                    aria-label={`Remove ${e}`}
                                >
                                    ×
                                </button>
                            </Badge>
                        ))}
                    </div>
                )}
            </RailSection>

            <Separator />

            <RailSection
                label="Nodes"
                badge={value.nodeIDs.length > 0 ? String(value.nodeIDs.length) : undefined}
                onClear={value.nodeIDs.length > 0 ? () => update({ nodeIDs: [] }) : undefined}
            >
                <div className="flex flex-wrap gap-1 max-h-40 overflow-auto">
                    {nodes.map(n => {
                        const active = value.nodeIDs.includes(n.id)
                        return (
                            <button
                                key={n.id}
                                type="button"
                                onClick={() => toggleNode(n.id)}
                                className={cn(
                                    "px-2 py-1 rounded-md text-xs border transition-colors",
                                    active
                                        ? "bg-foreground text-background border-foreground"
                                        : "bg-background text-foreground/70 border-border hover:border-foreground/40 hover:text-foreground"
                                )}
                            >
                                {n.name}
                            </button>
                        )
                    })}
                    {nodes.length === 0 && (
                        <p className="text-xs text-muted-foreground">No nodes available</p>
                    )}
                </div>
            </RailSection>
        </Card>
    )
}

function RailSection({
    label,
    badge,
    onClear,
    children,
}: {
    label: string
    badge?: string
    onClear?: () => void
    children: React.ReactNode
}) {
    return (
        <section className="space-y-2">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <Label className="text-xs uppercase tracking-wider font-semibold text-muted-foreground">{label}</Label>
                    {badge && (
                        <span className="px-1.5 py-px rounded text-[10px] font-mono font-semibold bg-foreground/10 text-foreground tabular-nums">
                            {badge}
                        </span>
                    )}
                </div>
                {onClear && (
                    <button
                        type="button"
                        onClick={onClear}
                        className="text-[10px] uppercase tracking-wider text-muted-foreground hover:text-foreground transition-colors"
                    >
                        Clear
                    </button>
                )}
            </div>
            {children}
        </section>
    )
}
