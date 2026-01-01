import { useMemo, useState } from "react"
import { PageHeader } from "@/components/shared/page-header"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { AlertTriangle, Bell, Clock, CheckCircle2, Send, Settings2, Info } from "lucide-react"
import { formatRelativeTime } from "@/lib/utils"
import {
    useAlertRules,
    useAlertEvents,
    useSetAlertRuleEnabled,
    useSetAlertRuleThreshold,
    useTestAlertRule,
} from "@/lib/queries"
import type { AlertRule, AlertEvent, AlertThreshold } from "@/lib/api/alerts"

// ruleTypeMeta centralises per-type display text so the RuleRow and
// threshold modal stay consistent. Adjust here when adding new rule
// types in the backend.
const RULE_TYPE_META: Record<string, { label: string; summary: (t: AlertThreshold) => string; fields: ("value" | "count" | "window_sec" | "duration_sec")[] }> = {
    node_offline: {
        label: "Node Offline",
        summary: () => "Fires when a node stops heartbeating.",
        fields: [],
    },
    node_crash_loop: {
        label: "Xray Crash Loop",
        summary: (t) => `${t.count ?? 5} crashes in ${t.window_sec ?? 300}s`,
        fields: ["count", "window_sec"],
    },
    high_cpu: {
        label: "High CPU",
        summary: (t) => `CPU > ${t.value ?? 90}% for ${t.duration_sec ?? 300}s`,
        fields: ["value", "duration_sec"],
    },
    high_disk: {
        label: "High Disk",
        summary: (t) => `Disk > ${t.value ?? 90}%`,
        fields: ["value", "duration_sec"],
    },
}

export default function AlertsPage() {
    const { data: rules, isLoading: rulesLoading } = useAlertRules()
    const { data: events, isLoading: eventsLoading } = useAlertEvents(100)
    const [editing, setEditing] = useState<AlertRule | null>(null)

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            <PageHeader
                title="Alerts"
                description="Fires on node offline, Xray crash loops, and resource thresholds. Delivery uses the Telegram/webhook channels configured in Settings → Notifications."
            />

            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                        <Bell className="w-4 h-4" /> Rules
                    </CardTitle>
                    <CardDescription>
                        Toggle rules on after confirming Telegram bot token is set. Disabled by default to prevent surprise notifications.
                    </CardDescription>
                </CardHeader>
                <CardContent className="space-y-3">
                    {rulesLoading ? (
                        [1, 2, 3, 4].map((i) => <Skeleton key={i} className="h-20 w-full" />)
                    ) : rules && rules.length > 0 ? (
                        rules.map((rule) => (
                            <RuleRow key={rule.id} rule={rule} onEdit={() => setEditing(rule)} />
                        ))
                    ) : (
                        <EmptyRules />
                    )}
                </CardContent>
            </Card>

            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                        <Clock className="w-4 h-4" /> Recent Events
                    </CardTitle>
                    <CardDescription>
                        Last 100 fires + resolutions. Refreshes every 15s.
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    {eventsLoading ? (
                        <div className="space-y-2">
                            {[1, 2, 3].map((i) => <Skeleton key={i} className="h-12 w-full" />)}
                        </div>
                    ) : events && events.length > 0 ? (
                        <EventsTimeline events={events} rules={rules ?? []} />
                    ) : (
                        <div className="text-sm text-muted-foreground text-center py-8">
                            No events yet. Enable a rule above and wait for it to fire.
                        </div>
                    )}
                </CardContent>
            </Card>

            <ThresholdDialog rule={editing} onClose={() => setEditing(null)} />
        </div>
    )
}

// ──────────────────── RuleRow ────────────────────

function RuleRow({ rule, onEdit }: { rule: AlertRule; onEdit: () => void }) {
    const toggleMut = useSetAlertRuleEnabled()
    const testMut = useTestAlertRule()
    const meta = RULE_TYPE_META[rule.rule_type] ?? { label: rule.rule_type, summary: () => "", fields: [] }

    return (
        <div className="flex flex-col md:flex-row md:items-center gap-3 border rounded-lg p-4 hover:border-border/80 transition-colors">
            <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                    <h3 className="font-semibold">{rule.name}</h3>
                    <Badge variant="outline" className="text-[10px] font-mono">{meta.label}</Badge>
                    {rule.last_fired_at && (
                        <Badge variant="secondary" className="text-[10px]">
                            last fired {formatRelativeTime(rule.last_fired_at)}
                        </Badge>
                    )}
                </div>
                <p className="text-xs text-muted-foreground mt-1">{rule.description || meta.summary(rule.threshold)}</p>
                <p className="text-xs text-muted-foreground mt-1 font-mono">
                    {meta.summary(rule.threshold)} · cooldown {rule.cooldown_sec}s
                </p>
            </div>
            <div className="flex items-center gap-2 shrink-0">
                {meta.fields.length > 0 && (
                    <Button variant="outline" size="sm" onClick={onEdit}>
                        <Settings2 className="w-3.5 h-3.5 mr-1.5" /> Threshold
                    </Button>
                )}
                <Button
                    variant="outline"
                    size="sm"
                    onClick={() => testMut.mutate(rule.id)}
                    disabled={testMut.isPending}
                    title="Send a test alert to verify delivery"
                >
                    <Send className="w-3.5 h-3.5 mr-1.5" /> Test
                </Button>
                <Switch
                    checked={rule.enabled}
                    onCheckedChange={(val) => toggleMut.mutate({ id: rule.id, enabled: val })}
                    aria-label={`Toggle ${rule.name}`}
                />
            </div>
        </div>
    )
}

// ──────────────────── Threshold edit dialog ────────────────────

function ThresholdDialog({ rule, onClose }: { rule: AlertRule | null; onClose: () => void }) {
    const mut = useSetAlertRuleThreshold()
    const [draft, setDraft] = useState<AlertThreshold>({})
    const [cooldown, setCooldown] = useState<number>(0)

    // Reset draft state each time a new rule opens.
    useMemo(() => {
        if (rule) {
            setDraft({ ...rule.threshold })
            setCooldown(rule.cooldown_sec)
        }
    }, [rule])

    if (!rule) return null
    const meta = RULE_TYPE_META[rule.rule_type] ?? { fields: [] as ("value" | "count" | "window_sec" | "duration_sec")[] }

    const save = () => {
        mut.mutate(
            { id: rule.id, threshold: draft, cooldownSec: cooldown },
            { onSuccess: onClose },
        )
    }

    return (
        <Dialog open onOpenChange={(open) => !open && onClose()}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Edit threshold: {rule.name}</DialogTitle>
                    <DialogDescription>
                        Adjust when this rule fires. Changes apply on the next evaluation tick (within ~30s).
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4 py-2">
                    {meta.fields.includes("value") && (
                        <ThresholdField
                            label="Threshold (%)"
                            hint="Fires when the metric stays above this percentage."
                            value={draft.value ?? 0}
                            onChange={(v) => setDraft((d) => ({ ...d, value: v }))}
                            min={0}
                            max={100}
                        />
                    )}
                    {meta.fields.includes("count") && (
                        <ThresholdField
                            label="Count"
                            hint="Number of events within the window before firing."
                            value={draft.count ?? 0}
                            onChange={(v) => setDraft((d) => ({ ...d, count: v }))}
                            min={1}
                        />
                    )}
                    {meta.fields.includes("window_sec") && (
                        <ThresholdField
                            label="Window (seconds)"
                            hint="Sliding window over which events are counted."
                            value={draft.window_sec ?? 0}
                            onChange={(v) => setDraft((d) => ({ ...d, window_sec: v }))}
                            min={10}
                        />
                    )}
                    {meta.fields.includes("duration_sec") && (
                        <ThresholdField
                            label="Duration (seconds)"
                            hint="How long the condition must persist before firing. 0 = fire immediately."
                            value={draft.duration_sec ?? 0}
                            onChange={(v) => setDraft((d) => ({ ...d, duration_sec: v }))}
                            min={0}
                        />
                    )}

                    <Separator />

                    <ThresholdField
                        label="Cooldown (seconds)"
                        hint="Minimum time between repeat notifications once the rule is firing."
                        value={cooldown}
                        onChange={setCooldown}
                        min={0}
                    />
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={onClose}>Cancel</Button>
                    <Button onClick={save} disabled={mut.isPending}>
                        {mut.isPending ? "Saving…" : "Save"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}

function ThresholdField({ label, hint, value, onChange, min, max }: {
    label: string
    hint?: string
    value: number
    onChange: (v: number) => void
    min?: number
    max?: number
}) {
    return (
        <div className="space-y-1.5">
            <Label className="text-sm">{label}</Label>
            <Input
                type="number"
                value={value}
                min={min}
                max={max}
                onChange={(e) => onChange(Number(e.target.value))}
            />
            {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
        </div>
    )
}

// ──────────────────── Events timeline ────────────────────

function EventsTimeline({ events, rules }: { events: AlertEvent[]; rules: AlertRule[] }) {
    const ruleById = useMemo(() => {
        const m = new Map<number, AlertRule>()
        for (const r of rules) m.set(r.id, r)
        return m
    }, [rules])

    return (
        <div className="space-y-1">
            {events.map((ev) => {
                const rule = ruleById.get(ev.rule_id)
                const Icon = ev.status === "fired" ? AlertTriangle : CheckCircle2
                const iconClass = ev.status === "fired" ? "text-red-500" : "text-emerald-500"
                return (
                    <div key={ev.id} className="flex items-start gap-3 py-2 border-b border-border/40 last:border-0">
                        <Icon className={`w-4 h-4 shrink-0 mt-0.5 ${iconClass}`} />
                        <div className="flex-1 min-w-0">
                            <div className="flex items-center gap-2 flex-wrap">
                                <span className="font-medium text-sm">{ev.title}</span>
                                {rule && <Badge variant="outline" className="text-[10px]">{rule.name}</Badge>}
                                <Badge
                                    variant={ev.status === "fired" ? "danger" : "success"}
                                    className="text-[10px]"
                                >
                                    {ev.status}
                                </Badge>
                            </div>
                            <p className="text-xs text-muted-foreground mt-0.5 whitespace-pre-line">{ev.message}</p>
                        </div>
                        <span className="text-xs text-muted-foreground shrink-0">
                            {formatRelativeTime(ev.created_at)}
                        </span>
                    </div>
                )
            })}
        </div>
    )
}

function EmptyRules() {
    return (
        <div className="text-center py-8 text-muted-foreground">
            <Info className="w-8 h-8 mx-auto mb-3 opacity-50" />
            <p className="text-sm">No rules yet. Defaults seed automatically on next server restart.</p>
        </div>
    )
}
