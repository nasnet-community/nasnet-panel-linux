import { useState, type ReactNode } from "react"
import { Link } from "react-router"
import {
    CalendarPlus, ChevronDown, Gauge, KeyRound, Link2, MoreHorizontal, PanelRight,
    Pause, Play, Plus, RotateCcw, Search, Smartphone, TriangleAlert, X,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
    DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { Subscription, SubscriptionOverview } from "@/lib/types"
import { useSubscriptionUsageHistory, useSubscriptionUsagePattern } from "@/lib/queries"
import { deriveSubscription } from "@/lib/subscription-derived"
import { cn } from "@/lib/utils"
import { FieldLabel, Segmented } from "@/components/access-history/bits"
import { fmtAgo, fmtDayTime, fmtShortTime, fmtUTC } from "@/components/access-history/format"
import { HourStrip, TrafficChart, TrafficLegend, sumTraffic, toLocalHours } from "./subscription-charts"
import {
    daysUntil, fmtClock, fmtDay, fmtSize, fmtUTCDay, isEnded, panelState, plural, limitLine, subscriptionName, type PanelState,
} from "./format"
import { Tag } from "./tag"

export { Tag }

export interface SubscriptionActions {
    onToggle: (sub: Subscription, on: boolean) => void
    onCopyLink: (sub: Subscription) => void
    onExtend: (sub: Subscription) => void
    onAddData: (sub: Subscription) => void
    onDataLimit: (sub: Subscription) => void
    onResetUsage: (sub: Subscription) => void
    onDeviceLimit: (sub: Subscription) => void
    onRegenerateLink: (sub: Subscription) => void
    onOpenDetails: (sub: Subscription) => void
    onPause: (sub: Subscription) => void
    onResume: (sub: Subscription) => void
    onRevoke: (sub: Subscription) => void
}

type Range = "7" | "30" | "90"
const RANGES = [{ value: "7" as const, label: "7d" }, { value: "30" as const, label: "30d" }, { value: "90" as const, label: "90d" }]

export function StatusTag({ sub }: { sub: Subscription }) {
    switch (sub.status) {
        case "active": return <Tag kind="ok">Active</Tag>
        case "paused": return sub.pause_reason === "ban" ? <Tag kind="warn">Paused by ban</Tag> : <Tag kind="warn">Paused</Tag>
        case "traffic_exhausted": return <Tag kind="bad">Data exhausted</Tag>
        case "expired": return <Tag kind="bad">Expired</Tag>
        case "cancelled": return <Tag kind="muted">Cancelled</Tag>
        default: return <Tag kind="muted">{sub.status}</Tag>
    }
}

function Fig({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
    return (
        <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
            <FieldLabel>{label}</FieldLabel>
            {children}
        </div>
    )
}

const big = "font-mono text-[19px] font-bold leading-tight tracking-tight tabular-nums sm:text-[22px]"
const note = "text-xs text-muted-foreground"

function barColor(state: PanelState | "ended", pct: number): string {
    if (state === "paused" || state === "banPaused" || state === "ended") return "bg-zinc-500"
    if (state === "exhausted" || pct > 90) return "bg-red-500"
    if (pct > 70) return "bg-amber-500"
    return "bg-emerald-500"
}

function ActionButton({ primary, icon, children, onClick, disabled }: { primary?: boolean; icon: ReactNode; children: ReactNode; onClick: () => void; disabled?: boolean }) {
    return (
        <Button type="button" size="sm" variant={primary ? "default" : "outline"} onClick={onClick} disabled={disabled} className="h-10 flex-1 gap-1.5 sm:h-8 sm:flex-none">
            {icon}{children}
        </Button>
    )
}

interface PanelProps {
    sub: Subscription
    overview?: SubscriptionOverview
    busy: boolean
    actions: SubscriptionActions
    userBanned: boolean
    /** When the user was banned, for subscriptions the ban paused. */
    banAt?: string
}

export function SubscriptionPanel({ sub, overview, busy, actions, userBanned, banAt }: PanelProps) {
    const [range, setRange] = useState<Range>("30")
    const days = Number(range)
    const d = deriveSubscription(sub)!
    const ended = isEnded(sub)
    const state: PanelState | "ended" = ended ? "ended" : panelState(sub, userBanned)
    const { data: usage = [] } = useSubscriptionUsageHistory(sub.id, days)
    const { data: pattern = [] } = useSubscriptionUsagePattern(sub.id, Math.min(days, 30))
    const hours = toLocalHours(pattern)

    const used = sub.data_used
    const limit = d.effectiveDataLimit
    const pct = d.isUnlimitedData ? 0 : Math.min(100, (used / Math.max(limit, 1)) * 100)
    const left = Math.max(0, limit - used)
    const online = overview?.online_ips ?? []
    const end = d.effectiveEndDate
    const daysLeft = end ? daysUntil(end) : null
    const canToggle = (sub.status === "active" || sub.status === "paused") && state !== "banPaused"

    const icon = "size-3.5"
    const btn = {
        copy: <ActionButton key="copy" icon={<Link2 className={icon} />} onClick={() => actions.onCopyLink(sub)}>Copy link</ActionButton>,
        extend: (p?: boolean) => <ActionButton key="extend" primary={p} icon={<CalendarPlus className={icon} />} onClick={() => actions.onExtend(sub)} disabled={busy}>Extend</ActionButton>,
        add: (p?: boolean) => <ActionButton key="add" primary={p} icon={<Plus className={icon} />} onClick={() => actions.onAddData(sub)} disabled={busy || d.isUnlimitedData}>Add data</ActionButton>,
        reset: <ActionButton key="reset" icon={<RotateCcw className={icon} />} onClick={() => actions.onResetUsage(sub)} disabled={busy}>Reset usage</ActionButton>,
        resume: <ActionButton key="resume" primary icon={<Play className={icon} />} onClick={() => actions.onResume(sub)} disabled={busy}>Resume</ActionButton>,
    }
    const buttons: ReactNode[] =
        state === "exhausted" ? [btn.add(true), btn.reset, btn.extend()] :
        state === "expiring" ? [btn.extend(true), btn.add()] :
        state === "paused" ? [btn.resume] :
        state === "ended" ? [btn.extend(true)] :
        state === "banPaused" ? [] :
        [btn.copy, btn.extend(), btn.add()]

    return (
        <section id={`sub-${sub.id}`} aria-label={`Subscription ${sub.id}`} className="scroll-mt-4 overflow-hidden rounded-xl border border-border bg-card">
            <div className="flex flex-col gap-4 p-3.5 sm:gap-5 sm:px-5 sm:pb-5 sm:pt-[18px]">
                <div className="flex items-start gap-3 sm:gap-3.5">
                    <Switch
                        checked={sub.status === "active"}
                        onCheckedChange={(on) => actions.onToggle(sub, on)}
                        disabled={!canToggle || busy}
                        aria-label={sub.status === "active" ? `Pause subscription ${sub.id}` : `Resume subscription ${sub.id}`}
                        className="mt-0.5 data-[state=checked]:bg-emerald-500"
                    />
                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
                            <h2 className="text-base font-semibold leading-snug">{subscriptionName(sub)}</h2>
                            <StatusTag sub={sub} />
                            {state === "expiring" && daysLeft != null && <Tag kind="warn">Expires in {plural(Math.max(daysLeft, 0), "day")}</Tag>}
                            <span className="font-mono text-[13px] text-muted-foreground">#{sub.id}</span>
                        </div>
                        <span className="text-[13px] text-muted-foreground">{limitLine(sub)}</span>
                    </div>
                    <div className="hidden items-center gap-1.5 sm:flex">
                        {buttons}
                        {state === "banPaused" && <span className="px-1 text-[13px] text-muted-foreground">Resumes on unban</span>}
                        <PanelMenu sub={sub} state={state} busy={busy} actions={actions} unlimited={d.isUnlimitedData} />
                    </div>
                    <div className="-mr-1.5 -mt-1.5 sm:hidden"><PanelMenu sub={sub} state={state} busy={busy} actions={actions} unlimited={d.isUnlimitedData} /></div>
                </div>

                {buttons.length > 0 && <div className="flex gap-1.5 sm:hidden">{buttons}</div>}
                {state === "banPaused" && <p className="text-[13px] text-muted-foreground sm:hidden">Resumes when the user is unbanned.</p>}

                <div className="flex flex-col gap-4 sm:flex-row sm:flex-wrap sm:items-start sm:gap-x-12 sm:gap-y-4 sm:pl-[50px]">
                    <Fig label="Data" className="sm:w-[260px]">
                        <span className="flex items-baseline gap-1.5">
                            <span className={big}>{fmtSize(ended ? sub.lifetime_data_used : used)}</span>
                            <span className="font-mono text-[13px] text-muted-foreground">{ended ? "used in total" : d.isUnlimitedData ? "used" : `of ${fmtSize(limit)}`}</span>
                        </span>
                        {!ended && !d.isUnlimitedData && (
                            <>
                                <span className="mt-0.5 block h-1.5 overflow-hidden rounded-full bg-muted">
                                    <span className={cn("block h-full rounded-full", barColor(state, pct))} style={{ width: `${Math.max(pct, used > 0 ? 1 : 0)}%` }} />
                                </span>
                                <span className={note}><span className="font-mono">{fmtSize(left)}</span> left, <span className="font-mono">{pct.toFixed(1).replace(/\.0$/, "")}%</span> used</span>
                            </>
                        )}
                        {!ended && d.isUnlimitedData && <span className={note}>No data limit</span>}
                    </Fig>

                    <div className="grid grid-cols-3 gap-3 sm:contents">
                        <SecondFigure sub={sub} state={state} overview={overview} unlimited={d.isUnlimitedData} hasEnd={!!end} banAt={banAt} />
                        <Fig label={ended ? "Ended" : "Expires"}>
                            {end ? (
                                <span className={cn(big, state === "expiring" && "text-amber-600 dark:text-amber-400")} title={fmtDayTime(end)}>{fmtDay(end)}</span>
                            ) : (
                                <span className={cn(big, "text-muted-foreground")}>—</span>
                            )}
                            <span className={note}>
                                {!end ? "No expiry" : daysLeft != null && daysLeft >= 0 ? `In ${plural(daysLeft, "day")}` : `${plural(Math.abs(daysLeft ?? 0), "day")} ago`}
                                {state === "banPaused" && end ? ", not extended" : ""}
                            </span>
                        </Fig>
                        <Fig label={ended ? "Last active" : "Connections"}>
                            {ended ? (
                                <span className={big}>{sub.last_active_at ? fmtDay(sub.last_active_at) : "—"}</span>
                            ) : (
                                <span className="flex items-center gap-2">
                                    <span className={cn("size-2 shrink-0 rounded-full", online.length ? "bg-emerald-500 shadow-[0_0_0_3px_rgba(16,185,129,0.18)]" : "bg-zinc-500")} />
                                    <span className={big} title={online.join(", ") || undefined}>{online.length}</span>
                                    <span className="hidden text-[13px] text-muted-foreground sm:inline">online now</span>
                                </span>
                            )}
                            <span className={note}>
                                {ended || online.length > 0 || !sub.last_active_at
                                    ? `${overview?.ips_30d ?? 0} IPs in 30 days`
                                    : <span title={fmtUTC(sub.last_active_at)}>Last seen {fmtShortTime(sub.last_active_at)}</span>}
                            </span>
                            {!ended && <span className={cn(note, "hidden sm:inline")}>{sub.max_devices ? `Up to ${plural(sub.max_devices, "device")}` : "No device limit"}</span>}
                        </Fig>
                    </div>
                </div>

                <div className="flex flex-col gap-2.5 sm:gap-3">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex items-center gap-4">
                            <h3 className="text-[12px] font-semibold uppercase tracking-wider text-muted-foreground">Traffic</h3>
                            <span className="hidden sm:flex"><TrafficLegend /></span>
                        </div>
                        <div className="flex items-center gap-3.5">
                            <span className="hidden items-baseline gap-1.5 sm:flex">
                                <span className="font-mono text-[15px] font-bold tabular-nums">{fmtSize(sumTraffic(usage, days))}</span>
                                <span className="text-xs text-muted-foreground">in {days} days</span>
                            </span>
                            <Segmented value={range} options={RANGES} onChange={setRange} ariaLabel="Chart range" />
                        </div>
                    </div>
                    <div className="flex items-center justify-between sm:hidden">
                        <TrafficLegend />
                        <span className="font-mono text-[13px] font-bold tabular-nums">{fmtSize(sumTraffic(usage, days))}</span>
                    </div>
                    <TrafficChart points={usage} days={days} markDay={state === "exhausted" && overview?.exhausted_at ? new Date(overview.exhausted_at).toISOString().slice(0, 10) : undefined} className="h-[120px] sm:h-[150px]" />
                </div>

                <HourStrip hours={hours} />
            </div>

            <div className="flex flex-col gap-2 border-t border-border/60 bg-muted/[0.08] px-3.5 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4 sm:px-5">
                <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:gap-3.5">
                    <FieldLabel>Node accounts</FieldLabel>
                    {(overview?.accounts ?? []).length === 0 && <span className="text-[13px] text-muted-foreground">None</span>}
                    {(overview?.accounts ?? []).map(a => (
                        <span key={a.account_id} className="flex h-9 items-center gap-2 rounded-md border border-border px-2.5 text-[13px] sm:h-7">
                            <span className={cn("size-[7px] shrink-0 rounded-full", a.status === "active" ? "bg-emerald-500" : a.status === "disabled" ? "bg-red-500" : "bg-zinc-500")} title={a.status} />
                            <span className="flex-1 sm:flex-none">{a.node_name || `Node #${a.node_id}`}</span>
                            {a.node_country && <span className="font-mono text-[12px] uppercase text-muted-foreground">{a.node_country}</span>}
                            {a.protocol && <span className="rounded border border-border px-1.5 font-mono text-[12px] uppercase text-muted-foreground">{a.protocol}</span>}
                            {a.inbound_tag && <span className="font-mono text-[12px] text-muted-foreground">{a.inbound_tag}</span>}
                        </span>
                    ))}
                </div>
                <div className="grid grid-cols-2 gap-1.5 pt-1 sm:flex sm:items-center sm:gap-1 sm:pt-0">
                    <Button asChild variant="ghost" size="sm" className="h-10 gap-1.5 bg-muted sm:h-8 sm:bg-transparent">
                        <Link to={`/access-history?subscription_ids=${sub.id}`}><Search className={icon} /><span className="sm:hidden">History</span><span className="hidden sm:inline">Access history</span></Link>
                    </Button>
                    <Button variant="ghost" size="sm" className="h-10 gap-1.5 bg-muted sm:h-8 sm:bg-transparent" onClick={() => actions.onOpenDetails(sub)}>
                        <PanelRight className={icon} />Details
                    </Button>
                </div>
            </div>
        </section>
    )
}

function SecondFigure({ sub, state, overview, unlimited, hasEnd, banAt }: {
    sub: Subscription
    state: PanelState | "ended"
    overview?: SubscriptionOverview
    unlimited: boolean
    hasEnd: boolean
    banAt?: string
}) {
    if (state === "exhausted") {
        const at = overview?.exhausted_at
        return (
            <Fig label="Ran out">
                <span className={cn(big, "text-red-600 dark:text-red-400")} title={at ? fmtUTC(at) : undefined}>{at ? fmtDay(at) : "—"}</span>
                <span className={note}>{at ? `${fmtAgo(at)}, ${fmtClock(at)}` : "Out of data"}</span>
                {at && <span className={cn(note, "hidden sm:inline")}>Notice sent</span>}
            </Fig>
        )
    }
    if (state === "paused" || state === "banPaused") {
        // A ban's pause happened at the ban; other pauses come from the audit log.
        const at = sub.pause_reason === "ban" ? banAt ?? overview?.paused?.at : overview?.paused?.at
        const by = sub.pause_reason === "ban" ? "By the ban" : overview?.paused?.actor ? `By ${overview.paused.actor}` : "By admin"
        return (
            <Fig label="Paused">
                <span className={big} title={at ? fmtUTC(at) : undefined}>{at ? fmtDay(at) : "—"}</span>
                <span className={note}>{by}</span>
            </Fig>
        )
    }
    if (state === "ended") {
        return (
            <Fig label="Status">
                <span className="text-base font-semibold leading-[1.6] capitalize text-muted-foreground">{sub.status}</span>
            </Fig>
        )
    }
    const f = overview?.forecast
    if (unlimited || f?.unlimited) {
        return (
            <Fig label="Runs out">
                <span className="text-base font-semibold leading-[1.6] text-muted-foreground">Never</span>
                <span className={note}>No data limit</span>
            </Fig>
        )
    }
    if (!f) {
        return (
            <Fig label="Runs out">
                <span className="text-base font-semibold leading-[1.6] text-muted-foreground">—</span>
                <span className={note}>No forecast yet</span>
            </Fig>
        )
    }
    const perDay = <><span className="font-mono">{fmtSize(f.daily_avg_bytes)}</span> a day</>
    if (!f.exhaustion_date) {
        return (
            <Fig label="Runs out">
                <span className="text-base font-semibold leading-[1.6] text-muted-foreground">—</span>
                <span className={note}>No recent use</span>
            </Fig>
        )
    }
    // Without an end date the data runs out whenever it runs out.
    if (!f.will_exhaust_first && hasEnd) {
        return (
            <Fig label="Runs out">
                <span className="text-base font-semibold leading-[1.6] text-foreground/80">After expiry</span>
                <span className={note}>At {perDay}</span>
            </Fig>
        )
    }
    return (
        <Fig label="Runs out">
            <span className={cn(big, "text-amber-600 dark:text-amber-400")}>≈ {fmtUTCDay(f.exhaustion_date)}</span>
            <span className={note}>At {perDay}</span>
            {f.will_exhaust_first && <Tag kind="warn" className="hidden self-start sm:inline-flex"><TriangleAlert className="size-3" />Before expiry</Tag>}
        </Fig>
    )
}

function PanelMenu({ sub, state, busy, actions, unlimited }: { sub: Subscription; state: PanelState | "ended"; busy: boolean; actions: SubscriptionActions; unlimited?: boolean }) {
    const i = "mr-2 size-4"
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="icon" className="size-10 border-transparent sm:size-8 sm:border-input" aria-label={`More actions for subscription ${sub.id}`} disabled={busy}>
                    <MoreHorizontal className="size-4" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuItem onClick={() => actions.onCopyLink(sub)}><Link2 className={i} />Copy link</DropdownMenuItem>
                <DropdownMenuItem onClick={() => actions.onExtend(sub)}><CalendarPlus className={i} />Extend</DropdownMenuItem>
                <DropdownMenuItem onClick={() => actions.onAddData(sub)} disabled={unlimited}><Plus className={i} />Add data{unlimited && <span className="ml-auto text-xs text-muted-foreground">Unlimited</span>}</DropdownMenuItem>
                <DropdownMenuItem onClick={() => actions.onDataLimit(sub)}><Gauge className={i} />Data limit</DropdownMenuItem>
                <DropdownMenuItem onClick={() => actions.onResetUsage(sub)}><RotateCcw className={i} />Reset usage</DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => actions.onDeviceLimit(sub)}><Smartphone className={i} />Device limit<span className="ml-auto font-mono text-xs text-muted-foreground">{sub.max_devices || "∞"}</span></DropdownMenuItem>
                <DropdownMenuItem onClick={() => actions.onRegenerateLink(sub)}><KeyRound className={i} />Regenerate link</DropdownMenuItem>
                <DropdownMenuItem onClick={() => actions.onOpenDetails(sub)}><PanelRight className={i} />Open details</DropdownMenuItem>
                <DropdownMenuSeparator />
                {sub.status === "active" && <DropdownMenuItem onClick={() => actions.onPause(sub)}><Pause className={i} />Pause</DropdownMenuItem>}
                {sub.status === "paused" && (
                    <DropdownMenuItem onClick={() => actions.onResume(sub)} disabled={state === "banPaused"}>
                        <Play className={i} />Resume{state === "banPaused" && <span className="ml-auto text-xs text-muted-foreground">Banned</span>}
                    </DropdownMenuItem>
                )}
                {sub.status !== "active" && sub.status !== "paused" && (
                    <DropdownMenuItem disabled><Pause className={i} />Pause<span className="ml-auto text-xs text-muted-foreground">{state === "exhausted" ? "Out of data" : "Ended"}</span></DropdownMenuItem>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => actions.onRevoke(sub)} className="text-red-600 focus:text-red-600 dark:text-red-400"><X className={i} />Revoke</DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    )
}

/** Compact row for an expired or cancelled subscription; expands into the panel. */
export function EndedRow({ sub, overview, busy, actions }: { sub: Subscription; overview?: SubscriptionOverview; busy: boolean; actions: SubscriptionActions }) {
    const [open, setOpen] = useState(false)
    if (open) {
        return (
            <div className="space-y-1.5">
                <SubscriptionPanel sub={sub} overview={overview} busy={busy} actions={actions} userBanned={false} />
                <button type="button" aria-expanded onClick={() => setOpen(false)} className="text-xs text-muted-foreground hover:text-foreground">Collapse #{sub.id}</button>
            </div>
        )
    }
    return (
        <div id={`sub-${sub.id}`} className="flex scroll-mt-4 flex-wrap items-center gap-x-2.5 gap-y-2 rounded-xl border border-border bg-card py-2.5 pl-3.5 pr-1.5 sm:flex-nowrap sm:gap-4 sm:pl-4">
            <div className="flex min-w-0 basis-full flex-col gap-1 pr-2 sm:basis-auto sm:flex-1 sm:flex-row sm:items-center sm:gap-4 sm:pr-0">
                <span className="flex min-w-0 items-center gap-2 sm:gap-4">
                    <StatusTag sub={sub} />
                    <span className="font-mono text-[13px] text-muted-foreground">#{sub.id}</span>
                    <span className="truncate text-sm font-medium">{subscriptionName(sub)}</span>
                </span>
                <span className="flex gap-4 text-xs text-muted-foreground sm:ml-auto sm:text-[13px]">
                    <span className="whitespace-nowrap"><span className="font-mono text-foreground/80">{fmtSize(sub.lifetime_data_used)}</span> used</span>
                    {sub.last_active_at && <span className="whitespace-nowrap" title={fmtUTC(sub.last_active_at)}>Last active {fmtDay(sub.last_active_at)}</span>}
                </span>
            </div>
            <div className="ml-auto flex items-center gap-1.5">
                <Button type="button" variant="outline" size="sm" className="h-10 gap-1.5 sm:h-8" onClick={() => actions.onExtend(sub)} disabled={busy}>
                    <CalendarPlus className="size-3.5" />Extend
                </Button>
                <PanelMenu sub={sub} state="ended" busy={busy} actions={actions} />
                <Button type="button" variant="ghost" size="icon" className="size-10 text-muted-foreground sm:size-8" aria-label={`Show subscription ${sub.id}`} aria-expanded={false} onClick={() => setOpen(true)}>
                    <ChevronDown className="size-4" />
                </Button>
            </div>
        </div>
    )
}

