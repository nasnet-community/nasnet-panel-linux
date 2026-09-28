import { useState } from "react"
import { Link } from "react-router"
import { ChevronDown, ChevronRight, ExternalLink, Search } from "lucide-react"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type {
    AccessHistoryGlobalHit,
    AccessHistoryGlobalSubscription,
    AccessHistoryGlobalValueAggregate,
    AccessHistorySearchKind,
} from "@/lib/types"
import { fmtAgo, fmtDayTime, fmtInt, fmtShortTime, fmtUTC } from "./format"

const KIND_TAG: Partial<Record<AccessHistorySearchKind, { label: string; className: string }>> = {
    rejected_domain: { label: "rejected", className: "text-red-600 bg-red-500/10 border-red-500/30 dark:text-red-400" },
    source_ip: { label: "source IP", className: "text-sky-600 bg-sky-500/10 border-sky-500/30 dark:text-sky-400" },
}

/** Accepted values stay untagged; only the unusual kinds get a tag. */
function KindTag({ kind }: { kind: AccessHistorySearchKind }) {
    const tag = KIND_TAG[kind]
    if (!tag) return null
    return <span className={cn("ml-2 inline-flex shrink-0 items-center rounded border px-1.5 py-px align-middle text-[11px] font-medium", tag.className)}>{tag.label}</span>
}

function subName(row: { subscription_id: number; subscription_label?: string }): string {
    if (row.subscription_id === 0) return "Unattached"
    return row.subscription_label || `Sub #${row.subscription_id}`
}

function ShareBar({ pct }: { pct: number }) {
    return (
        <span className="flex h-[3px] w-full justify-end rounded-full bg-muted">
            <span className="h-[3px] rounded-full bg-muted-foreground/55" style={{ width: `${Math.max(1.5, pct)}%` }} />
        </span>
    )
}

function EmptyCard({ title, hint }: { title: string; hint: string }) {
    return (
        <Card className="items-center gap-1.5 p-10 text-center">
            <p className="text-sm font-medium">{title}</p>
            <p className="text-[13px] text-muted-foreground">{hint}</p>
        </Card>
    )
}

// ─── Subscriptions ──────────────────────────────────────────────────

interface SubscriptionActions {
    onOpenProfile: (id: number, label: string) => void
    onSearchWithin: (id: number, label: string) => void
    onOpenSubscription: (id: number) => void
}

export function SubscriptionsTable({
    rows,
    windowHours,
    nodeNames,
    ...actions
}: { rows: AccessHistoryGlobalSubscription[]; windowHours: number; nodeNames: Map<number, string> } & SubscriptionActions) {
    const [open, setOpen] = useState<number | null>(null)
    if (rows.length === 0) return <EmptyCard title="No subscription matched" hint="Try a wider range, another match mode or fewer filters." />
    const top = rows[0].hits || 1
    const cols = "grid-cols-[minmax(0,1.2fr)_minmax(0,2fr)_120px_64px_92px_32px]"
    return (
        <Card className="gap-0 overflow-hidden py-0">
            <div className={cn("hidden h-[38px] items-center gap-x-3 border-b border-border px-3.5 text-[13px] font-medium text-muted-foreground md:grid", cols)}>
                <span>Subscription</span>
                <span>Matched values</span>
                <span className="text-right">Hits</span>
                <span className="text-right">Active</span>
                <span className="text-right">Last seen</span>
                <span />
            </div>
            {rows.map(r => {
                const isOpen = open === r.subscription_id
                const name = subName(r)
                const vals = r.top_values ?? []
                const more = r.values - Math.min(1, vals.length)
                const toggle = () => setOpen(isOpen ? null : r.subscription_id)
                return (
                    <div key={r.subscription_id} className="border-b border-border/50 last:border-b-0">
                        <div
                            role="button"
                            tabIndex={0}
                            aria-expanded={isOpen}
                            onClick={toggle}
                            onKeyDown={e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggle() } }}
                            className={cn("cursor-pointer px-3.5 transition-colors hover:bg-muted/30 focus-visible:bg-muted/30 focus-visible:outline-none", isOpen && "bg-muted/30")}
                        >
                            {/* Desktop row */}
                            <div className={cn("hidden min-h-[46px] items-center gap-x-3 md:grid", cols)}>
                                <span className="flex min-w-0 items-baseline gap-1.5">
                                    {r.subscription_id > 0 && <span className="font-mono text-[13px] text-muted-foreground">#{r.subscription_id}</span>}
                                    <span className={cn("truncate text-sm font-medium", r.subscription_id === 0 && "italic text-muted-foreground")}>{name}</span>
                                </span>
                                <span className="flex min-w-0 items-center gap-1.5 overflow-hidden">
                                    {vals[0] && <span className="min-w-0 shrink truncate rounded-[5px] bg-muted px-1.5 py-0.5 font-mono text-[13px]">{vals[0].value}</span>}
                                    {more > 0 && <span className="shrink-0 whitespace-nowrap text-[13px] text-muted-foreground">+{fmtInt(more)} more</span>}
                                </span>
                                <span className="flex flex-col items-end gap-[5px]">
                                    <span className="font-mono text-sm font-semibold tabular-nums">{fmtInt(r.hits)}</span>
                                    <ShareBar pct={(r.hits / top) * 100} />
                                </span>
                                <span className="text-right font-mono text-[13px] text-muted-foreground tabular-nums">{fmtInt(r.hours)} h</span>
                                <span className="text-right text-[13px] text-muted-foreground" title={fmtDayTime(r.last_seen)}>{fmtAgo(r.last_seen)}</span>
                                <span className="flex justify-end text-muted-foreground">
                                    {isOpen ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                                </span>
                            </div>
                            {/* Phone row */}
                            <div className="flex flex-col gap-1.5 py-3 md:hidden">
                                <span className="flex items-baseline justify-between gap-3">
                                    <span className="flex min-w-0 items-baseline gap-1.5">
                                        {r.subscription_id > 0 && <span className="font-mono text-[13px] text-muted-foreground">#{r.subscription_id}</span>}
                                        <span className="truncate text-[15px] font-medium">{name}</span>
                                    </span>
                                    <span className="font-mono text-sm font-semibold tabular-nums">{fmtInt(r.hits)}</span>
                                </span>
                                <span className="flex min-w-0 items-center gap-1.5">
                                    {vals[0] && <span className="min-w-0 shrink truncate rounded-[5px] bg-muted px-1.5 py-0.5 font-mono text-[13px]">{vals[0].value}</span>}
                                    {more > 0 && <span className="shrink-0 text-[13px] text-muted-foreground">+{fmtInt(more)} more</span>}
                                </span>
                                <span className="text-[13px] text-muted-foreground">{fmtInt(r.hours)} h active, last seen {fmtAgo(r.last_seen)}</span>
                            </div>
                        </div>
                        {isOpen && (
                            <SubscriptionDetail row={r} name={name} windowHours={windowHours} nodeNames={nodeNames} {...actions} />
                        )}
                    </div>
                )
            })}
        </Card>
    )
}

function SubscriptionDetail({
    row,
    name,
    windowHours,
    nodeNames,
    onOpenProfile,
    onSearchWithin,
    onOpenSubscription,
}: { row: AccessHistoryGlobalSubscription; name: string; windowHours: number; nodeNames: Map<number, string> } & SubscriptionActions) {
    const vals = row.top_values ?? []
    const nodes = (row.node_ids ?? []).map(id => nodeNames.get(id) ?? `Node ${id}`).join(", ")
    const attached = row.subscription_id > 0
    return (
        <div className="grid gap-7 border-t border-border/50 bg-muted/30 px-3.5 pb-[18px] pt-4 md:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
            <div>
                <div className="grid grid-cols-[minmax(0,1fr)_84px_56px] pb-1.5 text-[13px] text-muted-foreground">
                    <span>Top matched values</span><span className="text-right">Hits</span><span className="text-right">Active</span>
                </div>
                {vals.map(v => (
                    <div key={`${v.kind}-${v.value}`} className="grid h-[30px] grid-cols-[minmax(0,1fr)_84px_56px] items-center border-t border-border/50">
                        <span className="flex min-w-0 items-center"><span className="truncate font-mono text-[13px]">{v.value}</span><KindTag kind={v.kind} /></span>
                        <span className="text-right font-mono text-[13px] font-semibold tabular-nums">{fmtInt(v.count)}</span>
                        <span className="text-right font-mono text-[13px] text-muted-foreground tabular-nums">{fmtInt(v.hours)} h</span>
                    </div>
                ))}
                {attached && row.values > vals.length && (
                    <button
                        type="button"
                        onClick={() => onSearchWithin(row.subscription_id, name)}
                        className="mt-2.5 text-[13px] text-foreground/80 underline underline-offset-[3px] hover:text-foreground"
                    >
                        Show all {fmtInt(row.values)} values
                    </button>
                )}
            </div>
            <div className="flex flex-col gap-4">
                <dl className="m-0 grid grid-cols-[100px_minmax(0,1fr)] gap-y-[7px] text-sm">
                    <dt className="text-muted-foreground">First seen</dt><dd className="m-0">{fmtDayTime(row.first_seen)}</dd>
                    <dt className="text-muted-foreground">Last seen</dt><dd className="m-0">{fmtDayTime(row.last_seen)}</dd>
                    <dt className="text-muted-foreground">Active</dt><dd className="m-0">{fmtInt(row.hours)} of {fmtInt(windowHours)} hours</dd>
                    <dt className="text-muted-foreground">Client emails</dt><dd className="m-0">{fmtInt(row.emails)}</dd>
                    <dt className="text-muted-foreground">Nodes</dt><dd className="m-0 truncate">{nodes || "—"}</dd>
                </dl>
                {attached && (
                    <div className="flex flex-wrap gap-2">
                        <Button size="sm" onClick={() => onOpenProfile(row.subscription_id, name)}>Open profile</Button>
                        <Button size="sm" variant="outline" onClick={() => onSearchWithin(row.subscription_id, name)}>
                            <Search className="size-3.5" />Search within
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => onOpenSubscription(row.subscription_id)}>
                            Subscription<ExternalLink className="size-3.5" />
                        </Button>
                        {row.user_id > 0 && (
                            <Button size="sm" variant="ghost" asChild>
                                <Link to={`/users/${row.user_id}`}>User<ExternalLink className="size-3.5" /></Link>
                            </Button>
                        )}
                    </div>
                )}
            </div>
        </div>
    )
}

// ─── Values ─────────────────────────────────────────────────────────

const VALUES_PAGE = 20

export function ValuesTable({
    rows,
    total,
    onSearchValue,
}: { rows: AccessHistoryGlobalValueAggregate[]; total: number; onSearchValue: (value: string) => void }) {
    const [shown, setShown] = useState(VALUES_PAGE)
    if (rows.length === 0) return <EmptyCard title="No value matched" hint="Try a wider range or another match mode." />
    const top = rows[0].count || 1
    const visible = rows.slice(0, shown)
    const cols = "md:grid-cols-[minmax(0,1fr)_150px_104px_64px_36px]"
    const phoneCols = "grid-cols-[minmax(0,1fr)_88px]"
    return (
        <Card className="gap-0 overflow-hidden py-0">
            <div className={cn("hidden h-[38px] items-center gap-x-3.5 border-b border-border px-3.5 text-[13px] font-medium text-muted-foreground md:grid", cols)}>
                <span>Value</span><span className="text-right">Hits</span><span className="text-right">Subscriptions</span><span className="text-right">Active</span><span />
            </div>
            {visible.map(r => (
                <button
                    key={`${r.kind}-${r.value}`}
                    type="button"
                    title={`Search for ${r.value}`}
                    onClick={() => onSearchValue(r.value)}
                    className={cn("group grid w-full items-center gap-x-3.5 border-b border-border/50 px-3.5 py-2.5 text-left transition-colors hover:bg-muted/30 md:h-[43px] md:py-0", phoneCols, cols)}
                >
                    <span className="flex min-w-0 items-center"><span className="truncate font-mono text-sm">{r.value}</span><KindTag kind={r.kind} /></span>
                    <span className="flex flex-col items-end gap-[5px]">
                        <span className="font-mono text-sm font-semibold tabular-nums">{fmtInt(r.count)}</span>
                        <ShareBar pct={(r.count / top) * 100} />
                    </span>
                    <span className="hidden text-right font-mono text-[13px] text-muted-foreground tabular-nums md:block">{fmtInt(r.subscriptions)}</span>
                    <span className="hidden text-right font-mono text-[13px] text-muted-foreground tabular-nums md:block">{fmtInt(r.hours)} h</span>
                    <span className="hidden justify-end text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 md:flex"><Search className="size-4" /></span>
                </button>
            ))}
            <div className="flex flex-wrap items-center justify-between gap-3 px-3.5 py-3 text-sm text-muted-foreground">
                <span>Showing the top {fmtInt(visible.length)} of {fmtInt(total)} values. Click a value to search for it.</span>
                {shown < rows.length && (
                    <Button size="sm" variant="outline" onClick={() => setShown(s => s + 50)}>Show 50 more</Button>
                )}
            </div>
        </Card>
    )
}

// ─── Records ────────────────────────────────────────────────────────

export function RecordsTable({
    rows,
    total,
    truncated,
    limit,
    nodeNames,
    onRaiseLimit,
}: {
    rows: AccessHistoryGlobalHit[]
    total: number
    truncated: boolean
    limit: number
    nodeNames: Map<number, string>
    onRaiseLimit: () => void
}) {
    if (rows.length === 0) return <EmptyCard title="No records" hint="Try a wider range or fewer filters." />
    const cols = "md:grid-cols-[118px_minmax(0,1fr)_80px_minmax(0,1.4fr)_80px]"
    return (
        <Card className="gap-0 overflow-hidden py-0">
            <div className="flex flex-wrap items-center justify-between gap-2 px-3.5 pb-3 pt-3.5 text-sm text-muted-foreground">
                <span>Each record is one client's count for one value in one hour.</span>
                <span>Newest first</span>
            </div>
            <div className={cn("hidden h-9 items-center gap-x-3.5 border-y border-border px-3.5 text-[13px] font-medium text-muted-foreground md:grid", cols)}>
                <span>Hour</span><span>Subscription</span><span>Node</span><span>Value</span><span className="text-right">Count</span>
            </div>
            {rows.map((h, i) => (
                <div
                    key={`${h.bucket}-${h.node_id}-${h.email}-${h.kind}-${h.value}-${i}`}
                    className={cn("grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3.5 gap-y-0.5 border-b border-border/50 px-3.5 py-2 transition-colors hover:bg-muted/30 md:h-[41px] md:py-0", cols)}
                >
                    <span className="font-mono text-[13px] text-foreground/80" title={fmtUTC(h.bucket)}>{fmtShortTime(h.bucket)}</span>
                    <span className="flex min-w-0 items-baseline gap-1.5 max-md:order-last max-md:col-span-2">
                        {h.subscription_id > 0
                            ? <><span className="font-mono text-[13px] text-muted-foreground">#{h.subscription_id}</span><span className="truncate text-sm">{h.subscription_label || `Sub #${h.subscription_id}`}</span></>
                            : <span className="text-sm italic text-muted-foreground">Unattached</span>}
                    </span>
                    <span className="hidden text-[13px] text-muted-foreground md:block">{nodeNames.get(h.node_id) ?? `Node ${h.node_id}`}</span>
                    <span className="flex min-w-0 items-center max-md:col-span-2"><span className="truncate font-mono text-[13px]">{h.value}</span><KindTag kind={h.kind} /></span>
                    <span className="text-right font-mono text-sm font-semibold tabular-nums max-md:row-start-1 max-md:col-start-2">{fmtInt(h.count)}</span>
                </div>
            ))}
            <div className="flex flex-wrap items-center justify-between gap-3 px-3.5 py-3 text-sm text-muted-foreground">
                <span>
                    {truncated
                        ? <>Showing the newest {fmtInt(rows.length)} of {fmtInt(total)} records. Totals above count all of them.</>
                        : <>{fmtInt(rows.length)} records.</>}
                </span>
                {truncated && limit < 2000 && (
                    <Button size="sm" variant="outline" onClick={onRaiseLimit}>Show up to 2,000</Button>
                )}
            </div>
        </Card>
    )
}
