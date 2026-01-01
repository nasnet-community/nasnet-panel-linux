import { useEffect, useState } from "react"
import { useLocation } from "react-router"
import { ChevronDown } from "lucide-react"
import { cn, formatSpeed } from "@/lib/utils"
import {
    useDashboardStats,
    useOnlineUsersHistory,
} from "@/lib/queries/use-dashboard"
// import { useUnreadChatCount } from "@/lib/queries/use-chat"
import { useNodeAggregateStats } from "@/lib/queries/use-dashboard-widgets"
import { useSubscriptionCounts, useSubsExpiringWithin } from "@/lib/queries/use-subscriptions"
import { useChartPalette } from "@/lib/design/palette"
import {
    getContextConfig,
    type ContextConfig,
    type ContextStatus,
} from "./sidebar-context-config"

// Per-context persisted expand state. Each route remembers its own collapse
// decision across sessions; default is collapsed so first-time users get the
// terse "chart-only" view and opt in to details.
const EXPANDED_STORAGE_PREFIX = "sidebar-ctx-expanded:"
function useExpandedState(contextId: string): [boolean, () => void] {
    const key = EXPANDED_STORAGE_PREFIX + contextId
    const [expanded, setExpanded] = useState<boolean>(() => {
        if (typeof window === "undefined") return false
        return window.localStorage.getItem(key) === "true"
    })
    // Re-read on route change so each route uses its own stored preference.
    useEffect(() => {
        if (typeof window === "undefined") return
        setExpanded(window.localStorage.getItem(key) === "true")
    }, [key])
    const toggle = () => {
        setExpanded((prev) => {
            const next = !prev
            if (typeof window !== "undefined") {
                window.localStorage.setItem(key, String(next))
            }
            return next
        })
    }
    return [expanded, toggle]
}

type Tone = "neutral" | "ok" | "warn" | "danger"

interface HeroData {
    value: string
    unit?: string
    delta?: { text: string; tone: "up" | "down" | "flat" }
}

interface SparklineData {
    points: { ts: number; value: number }[]
    accent: "primary" | "amber" | "red"
}

interface Tile {
    label: string
    value: string
    tone: Tone
    delta?: string
}

interface DistributionSegment {
    key: string
    label: string
    value: number
    color: string
}

interface Distribution {
    total: number
    totalLabel: string
    segments: DistributionSegment[]
}

interface PanelData {
    loading: boolean
    status: ContextStatus
    hero: HeroData | null
    sparkline: SparklineData | null
    tiles: Tile[]
    distribution: Distribution | null
    /** Legacy — used by the collapsed-mode single-value strip. */
    headline: string
    /** Legacy — used by the current PanelBody render. Task 10 replaces this. */
    rows: { label: string; value: string }[]
}

const EMPTY_PLACEHOLDER = "—"

function useResolvedPanelData(config: ContextConfig | null): PanelData | null {
    // All hooks run unconditionally; consumers pick the right fields below.
    const c = useChartPalette()
    const COLORS = {
        active: c.success,
        expiring: c.warning,
        exhausted: c.danger,
        expired: c.chart6,
        paused: c.neutral,
        cancelled: c.mutedForeground,
    } as const
    const dashboard = useDashboardStats()
    // Chat support removed (frontend only)
    // const unread = useUnreadChatCount()
    const aggregate = useNodeAggregateStats()
    const onlineHistory = useOnlineUsersHistory(15)
    const subCounts = useSubscriptionCounts()
    const expiring7d = useSubsExpiringWithin(7)

    if (!config) return null

    const dash = dashboard.data
    const agg = aggregate.data

    // Shared helpers
    const online = dash?.online_users ?? 0
    const onlinePoints = (onlineHistory.data ?? []).map((p) => ({
        ts: new Date(p.created_at).getTime(),
        value: p.count,
    }))
    const onlineDelta = (() => {
        if (onlinePoints.length < 2) return undefined
        const oldest = onlinePoints[0]!.value
        const diff = online - oldest
        if (diff === 0) return { text: `· 15m`, tone: "flat" as const }
        const arrow = diff > 0 ? "↑" : "↓"
        return {
            text: `${arrow}${Math.abs(diff)} · 15m`,
            tone: diff > 0 ? ("up" as const) : ("down" as const),
        }
    })()

    const portfolio: Distribution | null = subCounts.data
        ? {
              total: subCounts.data.all,
              totalLabel: `${subCounts.data.all} SUBS`,
              segments: [
                  {
                      key: "active",
                      label: "active",
                      value: Math.max(0, subCounts.data.active - expiring7d.count),
                      color: COLORS.active,
                  },
                  {
                      key: "expiring",
                      label: "expiring",
                      value: expiring7d.count,
                      color: COLORS.expiring,
                  },
                  {
                      key: "exhausted",
                      label: "exhausted",
                      value: subCounts.data.traffic_exhausted,
                      color: COLORS.exhausted,
                  },
                  {
                      key: "expired",
                      label: "expired",
                      value: subCounts.data.expired,
                      color: COLORS.expired,
                  },
                  {
                      key: "paused",
                      label: "paused",
                      value: subCounts.data.paused,
                      color: COLORS.paused,
                  },
                  {
                      key: "cancelled",
                      label: "cancelled",
                      value: subCounts.data.cancelled,
                      color: COLORS.cancelled,
                  },
              ].filter((s) => s.value > 0),
          }
        : null

    switch (config.id) {
        case "system":
        case "customers": {
            const healthy = agg.onlineCount
            const total = agg.totalCount
            const tiles: Tile[] =
                config.id === "customers"
                    ? [
                          {
                              label: "expiring 7d",
                              value: String(expiring7d.count),
                              tone: expiring7d.count > 0 ? "warn" : "neutral",
                          },
                          {
                              label: "exhausted",
                              value: String(subCounts.data?.traffic_exhausted ?? 0),
                              tone: (subCounts.data?.traffic_exhausted ?? 0) > 0 ? "danger" : "neutral",
                          },
                          {
                              label: "expired",
                              value: String(subCounts.data?.expired ?? 0),
                              tone: (subCounts.data?.expired ?? 0) > 0 ? "danger" : "neutral",
                          },
                      ]
                    : [
                          {
                              label: "healthy",
                              value: total === 0 ? "—" : `${healthy}/${total}`,
                              tone: total > 0 && healthy < total ? "warn" : "ok",
                          },
                          {
                              label: "active subs",
                              value: String(subCounts.data?.active ?? 0),
                              tone: "neutral",
                          },
                      ]
            return {
                loading: dashboard.isLoading || onlineHistory.isLoading,
                status: total > 0 && healthy < total ? "warn" : "ok",
                hero: { value: String(online), unit: "online", delta: onlineDelta },
                sparkline: onlinePoints.length > 0 ? { points: onlinePoints, accent: "primary" } : null,
                tiles,
                distribution: portfolio,
                headline: String(online),
                rows: [
                    { label: "online", value: String(online) },
                    { label: "healthy", value: total === 0 ? "—" : `${healthy}/${total}` },
                ],
            }
        }

        case "nodes": {
            const healthy = agg.onlineCount
            const total = agg.totalCount
            const up = formatSpeed(agg.totalUpSpeed)
            const down = formatSpeed(agg.totalDownSpeed)
            return {
                loading: dashboard.isLoading || aggregate.isLoading,
                status: total > 0 && healthy < total ? "warn" : "ok",
                hero: { value: String(online), unit: "online", delta: onlineDelta },
                sparkline: onlinePoints.length > 0 ? { points: onlinePoints, accent: "primary" } : null,
                tiles: [
                    {
                        label: "healthy",
                        value: total === 0 ? "—" : `${healthy}/${total}`,
                        tone: total > 0 && healthy < total ? "warn" : "ok",
                    },
                    { label: "↑ up", value: up, tone: "neutral" },
                    { label: "↓ down", value: down, tone: "neutral" },
                ],
                distribution: null,
                headline: total === 0 ? "—" : String(healthy),
                rows: [
                    { label: "online", value: String(online) },
                    { label: "healthy", value: total === 0 ? "—" : `${healthy}/${total}` },
                    { label: "↑ up", value: up },
                    { label: "↓ down", value: down },
                ],
            }
        }

        // case "chats": {
        //     const unreadCount = unread.data ?? 0
        //     return {
        //         loading: unread.isLoading,
        //         status: "ok",
        //         hero: { value: String(unreadCount), unit: "unread" },
        //         sparkline: null,
        //         tiles: [
        //             {
        //                 label: "unread",
        //                 value: String(unreadCount),
        //                 tone: unreadCount > 0 ? "warn" : "neutral",
        //             },
        //             { label: "online", value: String(online), tone: "neutral" },
        //         ],
        //         distribution: null,
        //         headline: String(unreadCount),
        //         rows: [{ label: "unread", value: String(unreadCount) }],
        //     }
        // }

        case "certs": {
            const totalCerts = dash?.total_certificates ?? 0
            const expiring30 = dash?.certificates_expiring_30d ?? 0
            return {
                loading: dashboard.isLoading,
                status: expiring30 > 0 ? "warn" : "ok",
                hero: { value: String(totalCerts), unit: "certs" },
                sparkline: null,
                tiles: [
                    {
                        label: "expiring.30d",
                        value: String(expiring30),
                        tone: expiring30 > 0 ? "warn" : "neutral",
                    },
                    { label: "total", value: String(totalCerts), tone: "neutral" },
                ],
                distribution: null,
                headline: String(totalCerts),
                rows: [
                    { label: "total", value: String(totalCerts) },
                    { label: "expiring.30d", value: String(expiring30) },
                ],
            }
        }

        case "alerts": {
            const errors = dash?.alerts_error ?? 0
            const warns = dash?.alerts_warn ?? 0
            const infos = dash?.alerts_info ?? 0
            return {
                loading: dashboard.isLoading,
                status: errors > 0 ? "warn" : "ok",
                hero: { value: String(errors + warns), unit: "unresolved" },
                sparkline: null,
                tiles: [
                    {
                        label: "error",
                        value: String(errors),
                        tone: errors > 0 ? "danger" : "neutral",
                    },
                    {
                        label: "warn",
                        value: String(warns),
                        tone: warns > 0 ? "warn" : "neutral",
                    },
                    { label: "info", value: String(infos), tone: "neutral" },
                ],
                distribution:
                    errors + warns + infos > 0
                        ? {
                              total: errors + warns + infos,
                              totalLabel: `${errors + warns + infos} EVENTS`,
                              segments: [
                                  { key: "error", label: "error", value: errors, color: c.danger },
                                  { key: "warn", label: "warn", value: warns, color: c.warning },
                                  { key: "info", label: "info", value: infos, color: c.info },
                              ].filter((s) => s.value > 0),
                          }
                        : null,
                headline: String(errors + warns),
                rows: [
                    { label: "error", value: String(errors) },
                    { label: "warn", value: String(warns) },
                    { label: "info", value: String(infos) },
                ],
            }
        }
    }
}

const STATUS_LABEL: Record<ContextStatus, string> = {
    ok: "OK",
    warn: "WARN",
    down: "DOWN",
}

const STATUS_DOT: Record<ContextStatus, string> = {
    ok: "bg-primary",
    warn: "bg-amber-500",
    down: "bg-red-500",
}

export interface SidebarContextPanelProps {
    collapsed: boolean
}

export function SidebarContextPanel({ collapsed }: SidebarContextPanelProps) {
    const location = useLocation()
    const pathname = location.pathname
    const config = getContextConfig(pathname)
    // Hook must run unconditionally; safe default id keeps the storage key
    // consistent even when config is briefly null (between route changes).
    const [expanded, toggleExpanded] = useExpandedState(config?.id ?? "unknown")
    const data = useResolvedPanelData(config)

    if (!config || !data) return null

    if (collapsed) {
        return (
            <div className="group relative py-2.5 px-2 border-b border-border/50">
                <div
                    className="flex flex-col items-center gap-0.5 rounded-[4px] py-1.5 px-1 cursor-default"
                    title={`${config.label} · ${data.headline}`}
                >
                    <span className="font-mono text-sm font-semibold tabular-nums text-primary leading-none">
                        {data.loading ? "·" : data.headline}
                    </span>
                    <span className={cn("w-1 h-1 rounded-full", STATUS_DOT[data.status])} />
                </div>
                {/* Hover popover: full panel to the right. Always force-expanded
                    because the user is actively hovering for detail. Solid bg
                    so the sidebar-card tint doesn't bleed through. */}
                <div className="absolute left-full top-0 ml-2 w-[220px] pointer-events-none opacity-0 translate-x-1 group-hover:opacity-100 group-hover:translate-x-0 transition-all duration-150 z-30 rounded-[4px] bg-popover border border-border shadow-lg">
                    <PanelBody config={config} data={data} expanded forceExpanded />
                </div>
            </div>
        )
    }

    return (
        <div className="mx-3 mt-3 mb-1">
            <PanelBody
                config={config}
                data={data}
                expanded={expanded}
                onToggle={toggleExpanded}
            />
        </div>
    )
}

interface PanelBodyProps {
    config: ContextConfig
    data: PanelData
    expanded: boolean
    /** When set, the toggle control is omitted — used by the hover popover
     *  where collapsing would defeat the purpose of the hover. */
    forceExpanded?: boolean
    onToggle?: () => void
}

// Status tint migrates the card bg from a flat rgba to a top-weighted linear
// gradient so the card has subtle depth: most-saturated at the header label,
// fading to near-neutral by the bottom. Keeps the "OK=green, WARN=amber,
// DOWN=red" semantic without drowning the content below.
const STATUS_TINT: Record<ContextStatus, { top: string; bottom: string; border: string }> = {
    ok: {
        top: "rgba(34,197,94,0.06)",
        bottom: "rgba(34,197,94,0.015)",
        border: "rgba(34,197,94,0.18)",
    },
    warn: {
        top: "rgba(251,191,36,0.07)",
        bottom: "rgba(251,191,36,0.015)",
        border: "rgba(251,191,36,0.22)",
    },
    down: {
        top: "rgba(239,68,68,0.08)",
        bottom: "rgba(239,68,68,0.02)",
        border: "rgba(239,68,68,0.25)",
    },
}

function PanelBody({ config, data, expanded, forceExpanded, onToggle }: PanelBodyProps) {
    const tint = STATUS_TINT[data.status]
    // Collapsed view keeps label + hero + sparkline (the "live pulse" a user
    // wants at a glance); tiles + distribution hide behind the toggle.
    const showDetails = forceExpanded || expanded
    const hasDetails =
        (data.tiles.length > 0 || data.distribution !== null) && !forceExpanded
    return (
        <div
            className="rounded-[4px] border px-3.5 py-3"
            style={{
                borderColor: tint.border,
                backgroundImage: `linear-gradient(180deg, ${tint.top} 0%, ${tint.bottom} 100%)`,
            }}
        >
            <LabelRow
                config={config}
                status={data.status}
                expanded={showDetails}
                toggleable={hasDetails}
                onToggle={onToggle}
            />
            {data.hero && <HeroRow hero={data.hero} loading={data.loading} />}
            {data.sparkline && <SparklineRow data={data.sparkline} />}
            {/* Collapsible region: grid-rows animation from 0fr → 1fr gives
                auto-height transitions without measuring the content. */}
            <div
                className={cn(
                    "grid transition-[grid-template-rows,opacity] duration-200 ease-out",
                    showDetails
                        ? "grid-rows-[1fr] opacity-100"
                        : "grid-rows-[0fr] opacity-0",
                )}
                aria-hidden={!showDetails}
            >
                <div className="overflow-hidden min-h-0">
                    {data.tiles.length > 0 && <TilesRow tiles={data.tiles} loading={data.loading} />}
                    {data.distribution && <DistributionBar dist={data.distribution} />}
                </div>
            </div>
        </div>
    )
}

function LabelRow({
    config,
    status,
    expanded,
    toggleable,
    onToggle,
}: {
    config: ContextConfig
    status: ContextStatus
    expanded: boolean
    toggleable: boolean
    onToggle?: () => void
}) {
    // The whole row becomes a toggle when details are available, so the user
    // has a generous hit target. When nothing collapses (e.g. chat context
    // with no distribution) render as a static div — no chevron, no cursor.
    const content = (
        <>
            <span className="font-mono text-[10.5px] font-semibold tracking-[0.08em] uppercase text-primary">
                {config.label}
            </span>
            <span className="flex items-center gap-1.5">
                <span className={cn("w-1.5 h-1.5 rounded-full animate-pulse", STATUS_DOT[status])} />
                <span className="font-mono text-[10.5px] font-semibold tracking-[0.08em] uppercase text-primary">
                    {STATUS_LABEL[status]}
                </span>
                {toggleable && (
                    <ChevronDown
                        className={cn(
                            "w-3 h-3 ml-0.5 text-primary/50 transition-transform duration-200",
                            expanded ? "rotate-180" : "rotate-0",
                        )}
                        aria-hidden
                    />
                )}
            </span>
        </>
    )
    if (!toggleable) {
        return <div className="flex items-center justify-between mb-2.5">{content}</div>
    }
    return (
        <button
            type="button"
            onClick={onToggle}
            aria-expanded={expanded}
            aria-label={`${expanded ? "Collapse" : "Expand"} ${config.label} details`}
            className="flex items-center justify-between w-full mb-2.5 -mx-1 px-1 py-0.5 rounded-[3px] cursor-pointer hover:bg-white/[0.025] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/40 transition-colors"
        >
            {content}
        </button>
    )
}

function HeroRow({ hero, loading }: { hero: HeroData; loading: boolean }) {
    if (loading) {
        return <div className="h-9 mb-2 rounded bg-muted/40 animate-pulse w-24" />
    }
    // Number + unit share a baseline; the delta pill anchors to the top of the
    // number (self-start + matched leading) so it doesn't visually float next
    // to the smaller "online" text.
    return (
        <div className="flex items-start gap-2 mb-1">
            <div className="flex items-baseline gap-2 min-w-0">
                <span className="font-mono text-[32px] leading-none font-semibold tabular-nums text-foreground tracking-tight">
                    {hero.value}
                </span>
                {hero.unit && (
                    <span className="font-mono text-[10.5px] text-primary/80">{hero.unit}</span>
                )}
            </div>
            {hero.delta && (
                <span
                    className={cn(
                        "ml-auto self-start font-mono text-[10px] leading-none px-1.5 py-1 rounded-[3px] tabular-nums tracking-tight",
                        hero.delta.tone === "up" && "bg-primary/10 text-primary ring-1 ring-primary/20",
                        hero.delta.tone === "down" && "bg-red-500/8 text-red-400/90 ring-1 ring-red-500/20",
                        hero.delta.tone === "flat" && "bg-white/[0.04] text-muted-foreground ring-1 ring-white/5",
                    )}
                >
                    {hero.delta.text}
                </span>
            )}
        </div>
    )
}

function SparklineRow({ data }: { data: SparklineData }) {
    const c = useChartPalette()
    const { points, accent } = data
    if (points.length < 3) {
        return (
            <div className="h-8 mt-2 mb-2 flex items-center">
                <span className="font-mono text-[9.5px] text-muted-foreground/60 tracking-wider">
                    WARMING UP · collecting samples…
                </span>
            </div>
        )
    }

    const width = 220
    const height = 36
    const values = points.map((p) => p.value)
    const min = Math.min(...values)
    const max = Math.max(...values)
    const range = max - min || 1
    const step = width / (points.length - 1)
    const path = points
        .map((p, i) => {
            const x = i * step
            const y = height - ((p.value - min) / range) * (height - 4) - 2
            return `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`
        })
        .join(" ")

    const strokeColor =
        accent === "primary" ? c.success : accent === "amber" ? c.warning : c.danger
    const gradientId = `spark-grad-${accent}`

    return (
        <div className="h-9 mt-1 mb-2">
            <svg
                viewBox={`0 0 ${width} ${height}`}
                preserveAspectRatio="none"
                className="w-full h-full"
                aria-hidden
            >
                <defs>
                    <linearGradient id={gradientId} x1="0" x2="0" y1="0" y2="1">
                        <stop offset="0%" stopColor={strokeColor} stopOpacity="0.3" />
                        <stop offset="100%" stopColor={strokeColor} stopOpacity="0" />
                    </linearGradient>
                </defs>
                <path
                    d={path}
                    fill="none"
                    stroke={strokeColor}
                    strokeWidth="1.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                />
                <path
                    d={`${path} L${width},${height} L0,${height} Z`}
                    fill={`url(#${gradientId})`}
                />
            </svg>
        </div>
    )
}

// TileValue isolates a trailing unit ("KB/s", "MB") so the number stays big
// and the unit stacks below — avoids clipping "593.1 KB/s" in the narrow
// 3-col grid (~50px per tile).
const VALUE_SPLIT = /\s+(.+)$/
function TileValue({ value, tone }: { value: string; tone: Tone }) {
    const toneClass =
        tone === "ok"
            ? "text-primary"
            : tone === "warn"
            ? "text-amber-400"
            : tone === "danger"
            ? "text-red-400"
            : "text-foreground"
    const match = value.match(VALUE_SPLIT)
    const main = match ? value.slice(0, match.index) : value
    const unit = match ? match[1] : null
    if (!unit) {
        return (
            <span
                className={cn(
                    "font-mono text-[20px] font-semibold tabular-nums leading-none tracking-tight",
                    toneClass,
                )}
            >
                {main}
            </span>
        )
    }
    return (
        <div className="flex flex-col min-w-0 leading-none">
            <span
                className={cn(
                    "font-mono text-[18px] font-semibold tabular-nums tracking-tight truncate",
                    toneClass,
                )}
            >
                {main}
            </span>
            <span
                className={cn(
                    "font-mono text-[9px] font-medium tabular-nums tracking-[0.04em] opacity-55 mt-0.5 truncate",
                    toneClass,
                )}
            >
                {unit}
            </span>
        </div>
    )
}

function TilesRow({ tiles, loading }: { tiles: Tile[]; loading: boolean }) {
    // Tailwind's JIT cannot see dynamic class interpolation from grid-cols-${n},
    // so pick among literal classes it will emit.
    const gridClass =
        tiles.length === 4
            ? "grid-cols-2"
            : tiles.length === 3
            ? "grid-cols-3"
            : tiles.length === 2
            ? "grid-cols-2"
            : "grid-cols-1"
    return (
        // Uniform tile chrome (Grafana/Datadog convention) so mismatched tones
        // don't break grid rhythm. Tone lives in label + value color only.
        // flex-col + justify-between + min-height bottom-aligns values regardless
        // of whether the label wraps (e.g. "expiring 7d" vs "expired").
        <div className={cn("grid gap-1.5 mt-2 mb-2.5", gridClass)}>
            {tiles.map((t) => (
                <div
                    key={t.label}
                    className="flex flex-col justify-between min-h-[48px] rounded-[3px] px-2 py-1.5 bg-white/[0.025] ring-1 ring-inset ring-white/[0.04]"
                >
                    <div
                        className={cn(
                            "font-mono text-[9px] tracking-[0.08em] uppercase leading-[1.3]",
                            t.tone === "neutral" && "text-muted-foreground/80",
                            t.tone === "ok" && "text-primary/90",
                            t.tone === "warn" && "text-amber-400/90",
                            t.tone === "danger" && "text-red-400/90",
                        )}
                    >
                        {t.label}
                    </div>
                    <div className="flex items-baseline gap-1 min-w-0">
                        <TileValue value={loading ? "·" : t.value} tone={t.tone} />
                        {t.delta && (
                            <span className="font-mono text-[9.5px] opacity-60 tabular-nums">
                                {t.delta}
                            </span>
                        )}
                    </div>
                </div>
            ))}
        </div>
    )
}

function DistributionBar({ dist }: { dist: Distribution }) {
    const totalValues = dist.segments.reduce((a, s) => a + s.value, 0)
    const denominator = Math.max(dist.total, totalValues, 1)
    const healthy = dist.segments.find((s) => s.key === "active")?.value ?? 0
    const pct = Math.round((healthy / denominator) * 100)
    return (
        <div className="mt-2.5 pt-2.5 border-t border-white/[0.05]">
            {/* Header: micro-label on the left, the headline metric on the right.
                Asymmetric weight: portfolio label is tertiary, % healthy is the
                takeaway and gets a bigger, emphasized treatment. */}
            <div className="flex items-end justify-between mb-1.5">
                <span className="font-mono text-[9px] tracking-[0.1em] uppercase text-muted-foreground/60">
                    portfolio · {dist.total}
                </span>
                <span className="font-mono text-[10.5px] font-semibold tabular-nums text-primary tracking-tight leading-none">
                    {pct}% <span className="text-primary/70 font-normal tracking-[0.08em] uppercase text-[9px]">healthy</span>
                </span>
            </div>
            {/* h-2.5 stacked bar with 1px inset dividers between segments for crisp
                read. Slight outer shadow-inner to seat it into the card. */}
            <div
                className="h-2.5 rounded-[2px] flex overflow-hidden bg-white/[0.035] shadow-[inset_0_0_0_1px_rgba(255,255,255,0.04)]"
                role="img"
                aria-label={`Portfolio distribution, total ${dist.total}`}
            >
                {dist.segments.map((s, i) => (
                    <span
                        key={s.key}
                        style={{
                            width: `${(s.value / denominator) * 100}%`,
                            background: s.color,
                            boxShadow: i > 0 ? "inset 1px 0 0 rgba(0,0,0,0.35)" : undefined,
                        }}
                    />
                ))}
            </div>
            {/* Two-column grid keeps the legend aligned instead of flex-wrap's
                3-then-1 orphaning. tabular-nums on the value locks the number
                columns so counts align visually. */}
            <div className="grid grid-cols-2 gap-x-3 gap-y-1 mt-2 font-mono text-[9.5px] text-muted-foreground/80">
                {dist.segments.map((s) => (
                    <span key={s.key} className="inline-flex items-center gap-1.5 min-w-0">
                        <span
                            className="w-1.5 h-1.5 rounded-[1px] shrink-0"
                            style={{ background: s.color }}
                        />
                        <span className="truncate">{s.label}</span>
                        <span className="ml-auto tabular-nums text-foreground/70">{s.value}</span>
                    </span>
                ))}
            </div>
        </div>
    )
}
