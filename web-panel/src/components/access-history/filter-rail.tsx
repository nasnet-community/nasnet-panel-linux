import { useMemo, useState } from "react"
import type { DateRange } from "react-day-picker"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import type { Node } from "@/lib/types"
import { DateRangePicker } from "@/components/subscription/access-history/date-range-picker"
import { FieldLabel } from "./bits"
import { WhoPicker, type PickedSubscription } from "./who-picker"
import { syncDotClass, syncTitle, type NodeSync } from "./sync-badge"

export type RangePreset = "24h" | "7d" | "14d" | "30d" | "custom"

export const DEFAULT_PRESET: RangePreset = "7d"

export const PRESETS: { key: Exclude<RangePreset, "custom">; label: string; ms: number }[] = [
    { key: "24h", label: "24h", ms: 24 * 60 * 60 * 1000 },
    { key: "7d", label: "7d", ms: 7 * 24 * 60 * 60 * 1000 },
    { key: "14d", label: "14d", ms: 14 * 24 * 60 * 60 * 1000 },
    { key: "30d", label: "30d", ms: 30 * 24 * 60 * 60 * 1000 },
]

export interface FilterRailValue {
    preset: RangePreset
    customRange: DateRange | undefined
    subscriptions: PickedSubscription[]
    emails: string[]
    nodeIDs: number[]
}

export const EMPTY_RAIL: FilterRailValue = { preset: DEFAULT_PRESET, customRange: undefined, subscriptions: [], emails: [], nodeIDs: [] }

export function activeFilterCount(v: FilterRailValue): number {
    return v.subscriptions.length + v.emails.length + v.nodeIDs.length + (v.preset !== DEFAULT_PRESET ? 1 : 0)
}

interface Props {
    value: FilterRailValue
    onChange: (v: FilterRailValue) => void
    nodes: Node[]
    syncs: NodeSync[]
    retentionDays: number
    /** Bigger touch targets for the phone sheet. */
    touch?: boolean
    className?: string
}

export function FilterRail({ value, onChange, nodes, syncs, retentionDays, touch, className }: Props) {
    const update = (patch: Partial<FilterRailValue>) => onChange({ ...value, ...patch })
    // A range with only its start picked stays here until the end is chosen,
    // so the window on screen never goes empty mid-pick.
    const [draft, setDraft] = useState<DateRange>()
    const syncById = useMemo(() => new Map(syncs.map(s => [s.node.id, s])), [syncs])
    const listed = nodes.filter(n => n.enable_access_log || value.nodeIDs.includes(n.id))

    // Days outside retention only ever come back empty.
    const minDate = useMemo(() => {
        const d = new Date()
        d.setDate(d.getDate() - retentionDays)
        d.setHours(0, 0, 0, 0)
        return d
    }, [retentionDays])

    const who = value.subscriptions.length + value.emails.length
    const btn = touch ? "h-11 text-sm" : "h-8 text-[13px]"

    return (
        <Card className={cn("gap-[18px] p-5", touch && "border-0 bg-transparent p-0 shadow-none", className)}>
            {!touch && (
                <div className="flex items-center justify-between">
                    <FieldLabel as="h3">Filters</FieldLabel>
                    {activeFilterCount(value) > 0 && (
                        <button type="button" onClick={() => onChange(EMPTY_RAIL)} className="text-[13px] text-muted-foreground transition-colors hover:text-foreground">
                            Clear all
                        </button>
                    )}
                </div>
            )}

            <section className="space-y-2">
                <FieldLabel>Range</FieldLabel>
                <div className="grid grid-cols-4 gap-1">
                    {PRESETS.map(p => (
                        <button
                            key={p.key}
                            type="button"
                            aria-pressed={value.preset === p.key}
                            onClick={() => { setDraft(undefined); update({ preset: p.key, customRange: undefined }) }}
                            className={cn(
                                "rounded-md border font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                                btn,
                                value.preset === p.key
                                    ? "border-primary bg-primary font-semibold text-primary-foreground"
                                    : "border-input bg-transparent hover:bg-accent dark:bg-input/30 dark:hover:bg-input/50",
                            )}
                        >
                            {p.label}
                        </button>
                    ))}
                </div>
                <DateRangePicker
                    value={draft ?? (value.preset === "custom" ? value.customRange : undefined)}
                    onChange={r => {
                        if (r?.from && r?.to) {
                            setDraft(undefined)
                            update({ preset: "custom", customRange: r })
                        } else {
                            setDraft(r)
                        }
                    }}
                    minDate={minDate}
                    maxDate={new Date()}
                    placeholder="Custom range"
                    className={cn(
                        "w-full justify-start font-normal",
                        touch ? "h-11" : "h-8 text-[13px]",
                        value.preset === "custom" ? "border-primary" : "border-dashed text-muted-foreground",
                    )}
                />
            </section>

            <div className="h-px bg-border" />

            <section className="space-y-2">
                <div className="flex items-center justify-between">
                    <span className="flex items-center gap-2">
                        <FieldLabel as="label" htmlFor={touch ? "who-sheet" : "who-rail"}>Who</FieldLabel>
                        {who > 0 && <span className="rounded bg-muted px-1.5 font-mono text-[11px] font-semibold">{who}</span>}
                    </span>
                    {who > 0 && (
                        <button type="button" onClick={() => update({ subscriptions: [], emails: [] })} className="text-[12px] text-muted-foreground hover:text-foreground">
                            Clear
                        </button>
                    )}
                </div>
                <WhoPicker
                    touch={touch}
                    inputId={touch ? "who-sheet" : "who-rail"}
                    subscriptions={value.subscriptions}
                    emails={value.emails}
                    onSubscriptions={subscriptions => update({ subscriptions })}
                    onEmails={emails => update({ emails })}
                />
                {who === 0 && <p className="text-[13px] text-muted-foreground">Everyone. Pick someone to narrow the search or open their profile.</p>}
            </section>

            <div className="h-px bg-border" />

            <section className="space-y-2">
                <div className="flex items-center justify-between">
                    <FieldLabel>Nodes</FieldLabel>
                    {value.nodeIDs.length > 0 && (
                        <button type="button" onClick={() => update({ nodeIDs: [] })} className="text-[12px] text-muted-foreground hover:text-foreground">
                            Clear
                        </button>
                    )}
                </div>
                <div className="flex flex-wrap gap-1.5">
                    {listed.map(n => {
                        const on = value.nodeIDs.includes(n.id)
                        const s = syncById.get(n.id)
                        return (
                            <button
                                key={n.id}
                                type="button"
                                aria-pressed={on}
                                title={syncTitle(s, n.is_online)}
                                onClick={() => update({ nodeIDs: on ? value.nodeIDs.filter(id => id !== n.id) : [...value.nodeIDs, n.id] })}
                                className={cn(
                                    "inline-flex items-center gap-1.5 rounded-md border px-2.5 transition-colors",
                                    touch ? "h-10 text-sm" : "h-7 text-[13px]",
                                    on ? "border-primary bg-primary text-primary-foreground" : "border-border bg-background hover:border-foreground/40",
                                )}
                            >
                                <span className={cn("size-[7px] rounded-full", syncDotClass(s?.state, n.is_online))} />
                                {n.name}
                            </button>
                        )
                    })}
                    {listed.length === 0 && <p className="text-[13px] text-muted-foreground">No node collects access logs.</p>}
                </div>
            </section>
        </Card>
    )
}
