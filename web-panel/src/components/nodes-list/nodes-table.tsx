import { useRef, type MouseEvent, type PointerEvent } from "react"
import { ArrowDown, ArrowUp, ChevronRight, Router, SatelliteDish, Send } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { CountryFlag } from "@/components/ui/country-flag"
import { Skeleton } from "@/components/ui/skeleton"
import { HOST_COLORS } from "@/components/node/overview/overview-helpers"
import { chart } from "@/lib/design/palette"
import { nodeAddress, outageReasonLabel } from "@/lib/node-kind"
import { useLongPress } from "@/lib/hooks/use-long-press"
import { cn, formatBytes, formatSpeed } from "@/lib/utils"
import {
    agentVersionOf, clockTime, downUp, outagesText, problemsOf, shortDuration, speedOf, uptimeText, usageTone, usersOf, usersOnline,
    type NodeView, type Sort, type SortKey, type Tone,
} from "./facts"
import { CopyIP, IssueChip, NodeTags, StatusDot, statusText } from "./node-card"
import { NodeMenu, type NodeActions } from "./node-menu"
import { UptimeStrip } from "./uptime-strip"

interface Column {
    key: SortKey
    label: string
    className: string
}

// The table drops columns by its own width: Agent first, the strip last.
const COLUMNS: Column[] = [
    { key: "name", label: "Node", className: "min-w-[200px]" },
    { key: "status", label: "Status", className: "w-[128px]" },
    { key: "users", label: "Users", className: "w-[92px]" },
    { key: "today", label: "Today", className: "w-[132px]" },
    { key: "now", label: "Now", className: "hidden w-[96px] @min-[1020px]:table-cell" },
    { key: "cpu", label: "CPU", className: "hidden w-[60px] @min-[760px]:table-cell" },
    { key: "memory", label: "Mem", className: "hidden w-[60px] @min-[760px]:table-cell" },
    { key: "disk", label: "Disk", className: "hidden w-[60px] @min-[760px]:table-cell" },
    { key: "uptime", label: "30 days", className: "hidden w-[150px] @min-[900px]:table-cell" },
    { key: "agent", label: "Agent", className: "hidden w-[96px] @min-[1100px]:table-cell" },
]

const TONE_TEXT: Record<Tone, string> = { "": "", warn: "text-amber-500", crit: "text-red-500" }

function Mini({ value, color, tone }: { value?: number; color: string; tone: Tone }) {
    if (value === undefined) return <span className="text-muted-foreground">—</span>
    return (
        <div className="grid gap-1.5 pr-1">
            <span className={cn("text-[13px] font-medium tabular-nums", TONE_TEXT[tone])}>{Math.round(value)}%</span>
            <span className="h-[3px] overflow-hidden rounded-full bg-foreground/8">
                <span className="block h-full" style={{ width: `${Math.min(value, 100)}%`, background: tone === "crit" ? chart.red : tone === "warn" ? chart.amber : color }} />
            </span>
        </div>
    )
}

/** The second line under a row's status: why it's down, or its worst problem. */
function statusNote(v: NodeView, now: number): { text: string; className?: string } | null {
    const n = v.node
    if (v.status === "down") return { text: n.last_offline_reason ? outageReasonLabel(n.last_offline_reason, v.local) : "Offline" }
    if (v.status === "maintenance") return n.maintenance_since ? { text: `for ${shortDuration(now - Date.parse(n.maintenance_since))}` } : null
    const worst = problemsOf(v).find((i) => i.tone === "crit") ?? problemsOf(v)[0]
    if (worst) return { text: worst.text, className: worst.tone === "crit" ? "text-red-500" : "text-amber-600 dark:text-amber-400" }
    if (v.stale && v.stats?.received_at) return { text: `Last reading ${clockTime(v.stats.received_at)}` }
    return null
}

export interface NodesTableProps {
    views: NodeView[]
    now: number
    loading: boolean
    sort: Sort
    onSort: (key: SortKey) => void
    actions: NodeActions
    selectMode: boolean
    selected: ReadonlySet<number>
    onToggle: (view: NodeView) => void
    onToggleAll: (select: boolean) => void
    onActivate: (view: NodeView, e: MouseEvent) => void
}

export function NodesTable(p: NodesTableProps) {
    const allChecked = p.views.length > 0 && p.views.every((v) => p.selected.has(v.node.id))
    const some = p.views.some((v) => p.selected.has(v.node.id))
    const click = (v: NodeView, e: MouseEvent) => {
        // Portaled content (the ⋯ menu) bubbles here through React, not the DOM.
        if (!e.currentTarget.contains(e.target as globalThis.Node)) return
        if ((e.target as HTMLElement).closest("a, button, input, label, [role=checkbox], [role=menuitem]")) return
        p.onActivate(v, e)
    }
    return (
        <div className="@container overflow-x-auto rounded-2xl border bg-card/50 backdrop-blur-sm">
            <table className="w-full border-collapse text-left">
                <thead>
                    <tr className="h-10 shadow-[inset_0_-1px_0_var(--color-border)]">
                        {p.selectMode && (
                            <th className="w-10 pl-4">
                                <Checkbox
                                    aria-label="Select all nodes"
                                    checked={allChecked ? true : some ? "indeterminate" : false}
                                    onCheckedChange={() => p.onToggleAll(!allChecked)}
                                />
                            </th>
                        )}
                        {COLUMNS.map((c, i) => {
                            const active = p.sort.key === c.key
                            return (
                                <th
                                    key={c.key}
                                    className={cn("pr-3 font-medium", i === 0 && !p.selectMode && "pl-4", c.className)}
                                    aria-sort={active ? (p.sort.dir === "asc" ? "ascending" : "descending") : undefined}
                                >
                                    <button
                                        type="button"
                                        onClick={() => p.onSort(c.key)}
                                        title={c.key === "status" ? "Problems first" : undefined}
                                        className={cn(
                                            "inline-flex items-center gap-1 text-[11.5px] uppercase tracking-[0.08em] text-muted-foreground/80 hover:text-foreground",
                                            active && "text-foreground",
                                        )}
                                    >
                                        {c.label}
                                        {active && (p.sort.dir === "asc" ? <ArrowUp className="size-3" /> : <ArrowDown className="size-3" />)}
                                    </button>
                                </th>
                            )
                        })}
                        <th className="w-12"><span className="sr-only">Actions</span></th>
                    </tr>
                </thead>
                <tbody>
                    {p.loading
                        ? Array.from({ length: 3 }, (_, i) => (
                            <tr key={i} className="h-16 border-t border-border/40">
                                <td colSpan={COLUMNS.length + 2} className="px-4"><Skeleton className="h-8 w-full" /></td>
                            </tr>
                        ))
                        : p.views.map((v) => {
                            const n = v.node
                            const st = statusText(v, p.now)
                            const note = statusNote(v, p.now)
                            const faded = v.status === "down" || v.status === "maintenance" ? "opacity-45" : ""
                            const speed = v.status === "online" ? speedOf(v) : undefined
                            const live = speed ? speed.up + speed.down : 0
                            const users = usersOnline(v)
                            const usersTotal = usersOf(v)
                            const agent = agentVersionOf(v)
                            const agentDue = v.agentDue || !!v.mtproto?.agent_update
                            const xray = v.lite ? "" : v.stats?.xray_version || n.xray_version
                            const today = v.digest ? v.digest.today.uplink + v.digest.today.downlink : undefined
                            const selected = p.selected.has(n.id)
                            return (
                                <tr
                                    key={n.id}
                                    tabIndex={0}
                                    onClick={(e) => click(v, e)}
                                    onAuxClick={(e) => e.button === 1 && e.currentTarget.contains(e.target as globalThis.Node) && p.onActivate(v, e)}
                                    onKeyDown={(e) => { if (e.key === "Enter" && e.target === e.currentTarget) p.onActivate(v, e as unknown as MouseEvent) }}
                                    className={cn(
                                        "h-16 cursor-pointer border-t border-border/40 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                                        v.status === "down" && "bg-red-500/[0.07] hover:bg-red-500/10",
                                        selected && "bg-muted/60",
                                    )}
                                >
                                    {p.selectMode && (
                                        <td className="pl-4">
                                            <Checkbox checked={selected} aria-label={`Select ${n.name}`} onCheckedChange={() => p.onToggle(v)} />
                                        </td>
                                    )}
                                    <td className={cn("max-w-0 py-2 pr-3", !p.selectMode && "pl-4")}>
                                        <div className="flex min-w-0 items-center gap-2">
                                            <StatusDot view={v} />
                                            <span className="truncate text-[14.5px] font-semibold">{n.name}</span>
                                            <NodeTags view={v} iconsOnly />
                                            {v.issues.map((i) => <IssueChip key={i.kind} issue={i} iconOnly />)}
                                        </div>
                                        <div className="mt-0.5 flex min-w-0 items-center gap-2 pl-[18px] text-[12.5px] text-muted-foreground">
                                            <CountryFlag code={n.country_code} className="h-3 w-[18px]" />
                                            {nodeAddress(n) === n.ip && <CopyIP ip={n.ip} />}
                                            <span className="truncate" title={n.datacenter}>{n.datacenter || (v.local ? "This router" : "")}</span>
                                        </div>
                                    </td>
                                    <td className="py-2 pr-3">
                                        <div className={cn("whitespace-nowrap text-sm tabular-nums", st.className, v.status === "online" && "font-medium text-foreground")}>{st.text}</div>
                                        {note && <div className={cn("truncate text-[12px] text-muted-foreground", note.className)}>{note.text}</div>}
                                    </td>
                                    <td className={cn("py-2 pr-3", faded)}>
                                        <div className="text-sm font-semibold tabular-nums">{users ?? "—"}</div>
                                        {usersTotal !== undefined && <div className="whitespace-nowrap text-[12px] text-muted-foreground">of {usersTotal} {v.lite ? "users" : "active"}</div>}
                                    </td>
                                    <td className={cn("py-2 pr-3", faded)}>
                                        {today === undefined ? <Skeleton className="h-4 w-16" /> : (
                                            <>
                                                <div className="whitespace-nowrap text-sm font-semibold tabular-nums">{today ? formatBytes(today, 1) : "—"}</div>
                                                {today > 0 && <div className="whitespace-nowrap text-[12px] tabular-nums text-muted-foreground">{downUp(v.digest!.today.downlink, v.digest!.today.uplink)}</div>}
                                            </>
                                        )}
                                    </td>
                                    <td className={cn("hidden py-2 pr-3 @min-[1020px]:table-cell", faded)}>
                                        <span className="whitespace-nowrap text-sm font-semibold tabular-nums">{live ? formatSpeed(live) : "—"}</span>
                                    </td>
                                    <td className={cn("hidden py-2 pr-3 @min-[760px]:table-cell", faded)}><Mini value={v.stats?.cpu_percent} color={HOST_COLORS.cpu} tone={usageTone("cpu", v.stats?.cpu_percent)} /></td>
                                    <td className={cn("hidden py-2 pr-3 @min-[760px]:table-cell", faded)}><Mini value={v.stats?.memory_percent} color={HOST_COLORS.memory} tone={usageTone("memory", v.stats?.memory_percent)} /></td>
                                    <td className={cn("hidden py-2 pr-3 @min-[760px]:table-cell", faded)}><Mini value={v.stats?.disk_percent} color={HOST_COLORS.disk} tone={usageTone("disk", v.stats?.disk_percent)} /></td>
                                    <td className="hidden py-2 pr-3 @min-[900px]:table-cell">
                                        {v.digest ? (
                                            <div className="grid gap-1">
                                                <UptimeStrip days={v.digest.uptime.days} compact />
                                                <span className="whitespace-nowrap text-[12px] tabular-nums text-muted-foreground">
                                                    {v.digest.uptime.days.length ? `${uptimeText(v.digest.uptime.pct)}, ${outagesText(v.digest.uptime.outages)}` : "No record yet"}
                                                </span>
                                            </div>
                                        ) : <Skeleton className="h-6 w-28" />}
                                    </td>
                                    <td className="hidden py-2 pr-3 @min-[1100px]:table-cell">
                                        <div className="font-mono text-[12.5px]">{v.local ? "Built in" : agent || "—"}</div>
                                        <div className={cn("whitespace-nowrap text-[12px] text-muted-foreground", agentDue && "text-blue-500")}>
                                            {agentDue ? "Update ready" : xray ? `Xray ${xray}` : ""}
                                        </div>
                                    </td>
                                    <td className="py-2 pr-2 text-right"><NodeMenu view={v} actions={p.actions} /></td>
                                </tr>
                            )
                        })}
                </tbody>
            </table>
        </div>
    )
}

// ─── Phone ──────────────────────────────────────────────────────────

export interface NodeRowsPhoneProps {
    views: NodeView[]
    now: number
    selectMode: boolean
    selected: ReadonlySet<number>
    onActivate: (view: NodeView, e: MouseEvent) => void
    onToggle: (view: NodeView) => void
    onLongPress: (view: NodeView) => void
}

function PhoneRow({ view: v, now, selectMode, selected, onActivate, onToggle, onLongPress }: Omit<NodeRowsPhoneProps, "views" | "selected"> & { view: NodeView; selected: boolean }) {
    const n = v.node
    const pressed = useRef(false)
    const longPress = useLongPress({ onLongPress: () => { pressed.current = true; onLongPress(v) } })
    const pressHandlers = { ...longPress, onPointerDown: (e: PointerEvent) => { pressed.current = false; longPress.onPointerDown(e) } }
    const problems = problemsOf(v)
    const worst = problems.find((i) => i.tone === "crit") ?? problems[0] ?? v.issues[0]
    const today = v.digest ? v.digest.today.uplink + v.digest.today.downlink : undefined
    const users = usersOnline(v)
    const st = statusText(v, now)
    return (
        <div
            role="link"
            tabIndex={0}
            {...pressHandlers}
            onClick={(e) => {
                if (pressed.current) { pressed.current = false; return }
                if ((e.target as HTMLElement).closest("button, [role=checkbox]")) return
                onActivate(v, e)
            }}
            className={cn(
                "grid min-h-16 cursor-pointer select-none grid-cols-[auto_minmax(0,1fr)_auto_16px] items-center gap-2.5 border-t border-border/40 px-3.5 py-3 first:border-t-0",
                v.status === "down" && "bg-red-500/[0.07]",
                selected && "bg-muted/60",
            )}
        >
            {selectMode ? <Checkbox checked={selected} aria-label={`Select ${n.name}`} onCheckedChange={() => onToggle(v)} /> : <StatusDot view={v} />}
            <div className="grid min-w-0 gap-0.5">
                <div className="flex min-w-0 items-center gap-1.5 text-[15px] font-semibold">
                    <span className="truncate">{n.name}</span>
                    {v.local && <Router className="size-3.5 shrink-0 text-status-info" aria-label="This router" />}
                    {v.lite && <Send className="size-3.5 shrink-0 text-muted-foreground" aria-label="MTProto node" />}
                    {n.starlink_settings?.enabled && !n.is_stealth && <SatelliteDish className="size-3.5 shrink-0 text-muted-foreground" aria-label="On Starlink" />}
                    {worst && <IssueChip issue={worst} iconOnly />}
                </div>
                <div className="flex min-w-0 items-center gap-2 text-[12.5px] text-muted-foreground">
                    <CountryFlag code={n.country_code} className="h-3 w-[18px]" />
                    <span className="truncate">{n.datacenter || (v.local ? "This router" : "")}</span>
                </div>
            </div>
            <div className="grid justify-items-end gap-0.5 whitespace-nowrap">
                {v.status === "online" ? (
                    <>
                        <span className="text-sm font-semibold tabular-nums">{users !== undefined ? `${users} online` : "—"}</span>
                        <span className="text-[12px] tabular-nums text-muted-foreground">{today ? `${formatBytes(today, 1)} today` : " "}</span>
                    </>
                ) : (
                    <>
                        <span className={cn("text-sm tabular-nums", st.className)}>{st.text}</span>
                        <span className="text-[12px] text-muted-foreground">{statusNote(v, now)?.text ?? " "}</span>
                    </>
                )}
            </div>
            <ChevronRight className="size-4 text-muted-foreground/60" aria-hidden />
        </div>
    )
}

export function NodeRowsPhone({ views, selected, ...rest }: NodeRowsPhoneProps) {
    return (
        <div className="overflow-hidden rounded-[14px] border bg-card/50 backdrop-blur-sm">
            {views.map((v) => (
                <PhoneRow key={v.node.id} {...rest} view={v} selected={selected.has(v.node.id)} />
            ))}
        </div>
    )
}
