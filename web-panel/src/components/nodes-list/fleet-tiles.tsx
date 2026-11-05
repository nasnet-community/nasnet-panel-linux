import type { ReactNode } from "react"
import { Activity, Globe, Server, Users } from "lucide-react"
import { Sparkline } from "@/components/ui/sparkline"
import { T } from "@/components/node/node-ui"
import { chart } from "@/lib/design/palette"
import type { NodeStatsHistoryBulkMap } from "@/lib/api/nodes"
import { cn, formatBytes } from "@/lib/utils"
import { clockTime, downUp, problemsOf, shortDuration, speedOf, splitBytes, usersOnline, worstTone, type NodeView } from "./facts"

interface FleetFigures {
    online: number
    total: number
    anyDown: boolean
    line: { text: string; className: string }
    extra: string[]
    users: number
    usersSeries: number[]
    peak?: { users: number; at: number }
    today?: { down: number; up: number }
    yesterday: number
    live: { down: number; up: number }
    liveSeries: number[]
}

function figures(views: NodeView[], history: NodeStatsHistoryBulkMap | undefined, now: number): FleetFigures {
    const down = views.filter((v) => v.status === "down")
    const looks = views.filter((v) => v.group === 2)
    const crit = looks.filter((v) => worstTone(v) === "crit")
    const maint = views.filter((v) => v.status === "maintenance")
    const plural = (n: number) => `${n} need${n === 1 ? "s" : ""} a look`

    let line: FleetFigures["line"]
    if (down.length === 1) line = { text: `${down[0].node.name} down${down[0].node.last_disconnected_at ? ` ${shortDuration(now - Date.parse(down[0].node.last_disconnected_at))}` : ""}`, className: "text-red-500" }
    else if (down.length > 1) line = { text: `${down.length} down`, className: "text-red-500" }
    else if (looks.length === 1 && crit.length === 1) line = { text: `${crit[0].node.name}: ${problemsOf(crit[0])[0].text}`, className: "text-red-500" }
    else if (looks.length) line = { text: plural(looks.length), className: crit.length ? "text-red-500" : "text-amber-600 dark:text-amber-400" }
    else line = { text: views.length ? "All healthy" : "No nodes yet", className: "" }
    const extra = [down.length && looks.length ? plural(looks.length) : "", maint.length ? `${maint.length} in maintenance` : ""].filter(Boolean)

    // Maintenance pauses sales, not traffic: those users still count.
    const online = views.filter((v) => v.node.is_online)
    const serving = online
    const users = serving.reduce((s, v) => s + (usersOnline(v) ?? 0), 0)

    // Peak users per 15-minute bucket, summed across nodes.
    const byT = new Map<number, number>()
    for (const v of views) for (const p of v.digest?.series ?? []) byT.set(p.t, (byT.get(p.t) ?? 0) + p.users)
    const buckets = [...byT.entries()].sort((a, b) => a[0] - b[0])
    const best = buckets.reduce<[number, number] | undefined>((m, b) => (!m || b[1] > m[1] ? b : m), undefined)

    const withDigest = views.filter((v) => v.digest)
    const today = withDigest.length
        ? withDigest.reduce((s, v) => ({ down: s.down + v.digest!.today.downlink, up: s.up + v.digest!.today.uplink }), { down: 0, up: 0 })
        : undefined
    const yesterday = withDigest.reduce((s, v) => s + v.digest!.yesterday, 0)

    const live = serving.reduce((s, v) => {
        const sp = speedOf(v)
        return { down: s.down + (sp?.down ?? 0), up: s.up + (sp?.up ?? 0) }
    }, { down: 0, up: 0 })
    const rows = serving.map((v) => history?.[String(v.node.id)] ?? []).filter((r) => r.length)
    const len = Math.min(...rows.map((r) => r.length))
    const liveSeries = rows.length ? Array.from({ length: len }, (_, i) => rows.reduce((s, r) => s + r[r.length - len + i].up_speed + r[r.length - len + i].down_speed, 0)) : []

    return {
        online: online.length,
        total: views.length,
        anyDown: down.length > 0,
        line,
        extra,
        users,
        usersSeries: buckets.map((b) => b[1]),
        peak: best && best[1] > 0 ? { users: best[1], at: best[0] * 1000 } : undefined,
        today,
        yesterday,
        live,
        liveSeries,
    }
}

function Tile({ icon: Icon, label, compact, children, aside, badge }: { icon: typeof Server; label: string; compact?: boolean; children: ReactNode; aside?: ReactNode; badge?: ReactNode }) {
    return (
        <div className={cn("relative grid min-w-0 content-start gap-1.5 overflow-hidden rounded-2xl border bg-card/50 backdrop-blur-sm", compact ? "gap-0.5 rounded-[14px] px-3.5 py-3" : "min-h-[124px] px-[18px] py-4")}>
            <div className="flex items-center gap-2 text-muted-foreground">
                {!compact && <Icon className="size-[15px] opacity-80" aria-hidden />}
                <span className={T.eyebrow}>{label}</span>
                {!compact && badge && <div className="ml-auto">{badge}</div>}
            </div>
            {children}
            {!compact && aside && <div className="pointer-events-none absolute bottom-[38px] right-[18px] w-[44%]">{aside}</div>}
        </div>
    )
}

function Value({ value, unit, className, compact }: { value: ReactNode; unit?: string; className?: string; compact?: boolean }) {
    return (
        <div className="whitespace-nowrap">
            <span className={cn(compact ? "text-[22px] leading-7 font-bold tracking-[-0.02em] tabular-nums" : T.value, className)}>{value}</span>
            {unit && <span className={cn(T.unit, "ml-1")}>{unit}</span>}
        </div>
    )
}

export function FleetTiles({ views, history, now, compact, loading }: { views: NodeView[]; history?: NodeStatsHistoryBulkMap; now: number; compact?: boolean; loading?: boolean }) {
    const f = figures(views, history, now)
    const today = f.today ? f.today.down + f.today.up : undefined
    const live = f.live.down + f.live.up
    const pip = (v: NodeView) => {
        const tone = worstTone(v)
        return cn(
            "size-2 rounded-full",
            v.status === "down" || tone === "crit" ? "bg-red-500"
                : v.status === "maintenance" ? "bg-blue-500"
                : v.status === "pending" ? "ring-[1.5px] ring-inset ring-amber-500"
                : tone === "warn" ? "bg-amber-500"
                : v.stale ? "bg-muted-foreground" : "bg-emerald-500",
        )
    }
    return (
        <div className={cn("grid grid-cols-2 gap-3 md:gap-4", !compact && "lg:grid-cols-4")}>
            <Tile icon={Server} label="Nodes online" compact={compact} badge={
                <div className="flex max-w-28 flex-wrap justify-end gap-1">{views.map((v) => <span key={v.node.id} className={pip(v)} title={v.node.name} />)}</div>
            }>
                <Value compact={compact} value={loading ? "—" : f.online} unit={`of ${f.total}`} className={cn(f.anyDown && "text-red-500")} />
                <p className={cn(T.meta, compact ? "truncate" : "line-clamp-2", f.line.className)}>
                    {loading ? " " : f.line.text}
                    {!compact && f.extra.map((e) => <span key={e} className="text-muted-foreground">, {e}</span>)}
                </p>
            </Tile>
            <Tile icon={Users} label="Users online" compact={compact} aside={f.usersSeries.length > 2 && <Sparkline data={f.usersSeries} color={chart.blue} height={34} />}>
                <Value compact={compact} value={loading ? "—" : f.users} />
                <p className={cn(T.meta, "truncate")}>{f.peak ? `Peak ${f.peak.users} at ${clockTime(f.peak.at)}` : " "}</p>
            </Tile>
            <Tile icon={Globe} label="Traffic today" compact={compact}>
                <Value compact={compact} value={today === undefined ? "—" : splitBytes(today)[0]} unit={today ? splitBytes(today)[1] : undefined} />
                <div className="flex min-w-0 items-center justify-between gap-2">
                    <p className={cn(T.meta, "truncate")}>{f.today && today ? downUp(f.today.down, f.today.up) : " "}</p>
                    {!compact && f.yesterday > 0 && <p className={cn(T.meta, "shrink-0")}>Yesterday {formatBytes(f.yesterday, 1)}</p>}
                </div>
            </Tile>
            <Tile icon={Activity} label="Throughput now" compact={compact} aside={f.liveSeries.length > 2 && <Sparkline data={f.liveSeries} color={chart.emerald} height={34} />}>
                <Value compact={compact} value={live ? splitBytes(live, true)[0] : "—"} unit={live ? splitBytes(live, true)[1] : undefined} />
                <p className={cn(T.meta, "truncate")}>{live ? downUp(f.live.down, f.live.up, true) : " "}</p>
            </Tile>
        </div>
    )
}
