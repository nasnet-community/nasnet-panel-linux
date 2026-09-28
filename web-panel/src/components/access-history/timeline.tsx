import { useMemo, useState } from "react"
import { Card } from "@/components/ui/card"
import { useChartPalette } from "@/lib/design/palette"
import type { Node } from "@/lib/types"
import { FieldLabel, Segmented } from "./bits"
import { HitsChart, type HourPoint } from "./charts"
import { fmtInt, startOfLocalDay, tzName } from "./format"

const ramp = (t: number) => `oklch(0.65 ${(0.08 + t * 0.12).toFixed(3)} 230 / ${(0.25 + t * 0.65).toFixed(2)})`
const hh = (h: number) => String(h).padStart(2, "0")

interface HeatRow {
    key: string
    name: string
    note?: string
    cells: { v: number | null; tip: string }[]
}

export function Timeline({ points, nodes, staleNodes }: { points: HourPoint[]; nodes: Node[]; staleNodes: Map<number, string> }) {
    const c = useChartPalette()
    // With one node, per-node rows are a single stripe; start on days.
    const [rows, setRows] = useState<"node" | "day">(() => nodes.length <= 1 ? "day" : "node")
    const nodeNames = useMemo(() => new Map(nodes.map(n => [n.id, n.name])), [nodes])

    const heat = useMemo<HeatRow[]>(() => {
        if (rows === "node") {
            const ids = new Set<number>(nodes.filter(n => n.enable_access_log !== false).map(n => n.id))
            for (const p of points) for (const id of Object.keys(p.nodes ?? {})) ids.add(Number(id))
            return [...ids].map(id => {
                const byHour = new Array<number>(24).fill(0)
                for (const p of points) byHour[new Date(p.t).getHours()] += p.nodes?.[id] ?? 0
                const name = nodeNames.get(id) ?? `Node ${id}`
                const any = byHour.some(v => v > 0)
                return {
                    key: `n${id}`,
                    name,
                    note: !any && staleNodes.has(id) ? staleNodes.get(id) : undefined,
                    cells: byHour.map((v, h) => ({ v, tip: `${name}, ${hh(h)}:00, ${v ? `${fmtInt(v)} hits` : "no hits"}` })),
                }
            })
        }
        const days = new Map<number, (number | null)[]>()
        for (const p of points) {
            const day = startOfLocalDay(p.t)
            if (!days.has(day)) days.set(day, new Array<number | null>(24).fill(null))
            const cells = days.get(day)!
            const h = new Date(p.t).getHours()
            cells[h] = (cells[h] ?? 0) + p.accepted + p.rejected + (p.sourceIp ?? 0)
        }
        return [...days.entries()].sort((a, b) => a[0] - b[0]).map(([day, cells]) => {
            const name = new Date(day).toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" })
            return {
                key: `d${day}`,
                name,
                cells: cells.map((v, h) => ({ v, tip: v === null ? `${name}, ${hh(h)}:00, outside the window` : `${name}, ${hh(h)}:00, ${fmtInt(v)} hits` })),
            }
        })
    }, [rows, points, nodes, nodeNames, staleNodes])

    const max = Math.max(1, ...heat.flatMap(r => r.cells.map(c => c.v ?? 0)))

    return (
        <div className="space-y-4">
            <Card className="gap-3.5 p-4 sm:p-5">
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <div className="flex flex-wrap items-center gap-4">
                        <FieldLabel as="h3">Hits per hour</FieldLabel>
                        <Legend color={c.success} label="Accepted" />
                        <Legend color={c.danger} label="Rejected" />
                    </div>
                    <span className="text-[13px] text-muted-foreground">Hourly, {tzName()}</span>
                </div>
                <HitsChart points={points} nodeNames={nodeNames} />
            </Card>

            <Card className="gap-3.5 p-4 sm:p-5">
                <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="space-y-1">
                        <FieldLabel as="h3">When in the day</FieldLabel>
                        <p className="text-sm text-muted-foreground">
                            {rows === "node"
                                ? "Hits by hour of day and node, all days combined. Hover a cell for the count."
                                : "Hits by hour and day. Hover a cell for the count."}
                        </p>
                    </div>
                    <div className="flex items-center gap-3">
                        <span className="hidden items-center gap-1.5 text-[12px] text-muted-foreground sm:flex">
                            Fewer
                            <span className="flex gap-0.5">{[0.1, 0.3, 0.5, 0.75, 1].map(t => <span key={t} className="h-2.5 w-3.5 rounded-[2px]" style={{ background: ramp(t) }} />)}</span>
                            More
                        </span>
                        <Segmented
                            ariaLabel="Heatmap rows"
                            value={rows}
                            onChange={setRows}
                            options={[{ value: "node", label: "Nodes" }, { value: "day", label: "Days" }]}
                        />
                    </div>
                </div>
                <div className="overflow-x-auto">
                    <div className="min-w-[560px] space-y-[3px]">
                        <div className="grid grid-cols-[92px_repeat(24,minmax(0,1fr))] gap-[3px]">
                            <span />
                            {Array.from({ length: 24 }, (_, h) => (
                                <span key={h} className="text-center font-mono text-[11px] text-muted-foreground">{hh(h)}</span>
                            ))}
                        </div>
                        {heat.map(r => (
                            <div key={r.key} className="grid grid-cols-[92px_repeat(24,minmax(0,1fr))] items-center gap-[3px]">
                                <span className="flex min-w-0 flex-col leading-tight">
                                    <span className="truncate text-[13px]">{r.name}</span>
                                    {r.note && <span className="truncate text-[11px] text-amber-600 dark:text-amber-300">{r.note}</span>}
                                </span>
                                {r.cells.map((c, h) => (
                                    <span
                                        key={h}
                                        title={c.tip}
                                        className="h-6 rounded-[4px] transition-shadow hover:ring-2 hover:ring-foreground/40"
                                        style={{ background: c.v === null ? "transparent" : c.v === 0 ? "color-mix(in oklab, var(--muted) 45%, transparent)" : ramp(c.v / max) }}
                                    />
                                ))}
                            </div>
                        ))}
                    </div>
                </div>
            </Card>
        </div>
    )
}

function Legend({ color, label }: { color: string; label: string }) {
    return (
        <span className="inline-flex items-center gap-1.5 text-[13px] text-muted-foreground">
            <span className="h-[3px] w-2.5 rounded-full" style={{ background: color }} />
            {label}
        </span>
    )
}
