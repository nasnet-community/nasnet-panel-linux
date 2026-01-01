import { useMemo } from "react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import type { AccessHistoryGlobalHit } from "@/lib/types"

interface Props {
    hits: AccessHistoryGlobalHit[]
    nodes: { id: number; name: string }[]
}

export function Heatmap({ hits, nodes }: Props) {
    const grid = useMemo(() => {
        const out = new Map<string, number>()
        let max = 0
        for (const h of hits) {
            const d = new Date(h.bucket)
            const hour = d.getUTCHours()
            const key = `${hour}-${h.node_id}`
            const next = (out.get(key) ?? 0) + h.count
            out.set(key, next)
            if (next > max) max = next
        }
        return { out, max }
    }, [hits])

    if (hits.length === 0) {
        return (
            <Card className="p-10 flex flex-col items-center justify-center gap-2 text-center">
                <p className="text-sm font-medium text-muted-foreground">No hits to plot</p>
                <p className="text-xs text-muted-foreground/70">Run a query to populate the heatmap.</p>
            </Card>
        )
    }

    const hours = Array.from({ length: 24 }, (_, i) => i)
    const visibleNodes = nodes.length > 0 ? nodes : Array.from(new Set(hits.map(h => h.node_id))).map(id => ({ id, name: `Node ${id}` }))

    return (
        <Card className="p-4 md:p-5 space-y-4">
            <div className="flex items-baseline justify-between">
                <div>
                    <h3 className="text-sm font-semibold">Activity heatmap</h3>
                    <p className="text-xs text-muted-foreground mt-0.5">Hits aggregated by hour (UTC) and node. Hover for exact counts.</p>
                </div>
                <Legend max={grid.max} />
            </div>
            <div className="overflow-x-auto">
                <table className="border-separate border-spacing-[3px]">
                    <thead>
                        <tr>
                            <th className="text-left pr-3 text-[10px] uppercase tracking-wider font-semibold text-muted-foreground">Hour</th>
                            {visibleNodes.map(n => (
                                <th key={n.id} className="px-1 text-[10px] uppercase tracking-wider font-semibold text-muted-foreground whitespace-nowrap text-center">
                                    {n.name}
                                </th>
                            ))}
                        </tr>
                    </thead>
                    <tbody>
                        {hours.map(h => (
                            <tr key={h}>
                                <td className="pr-3 text-[11px] text-muted-foreground tabular-nums font-mono">
                                    {String(h).padStart(2, "0")}
                                </td>
                                {visibleNodes.map(n => {
                                    const v = grid.out.get(`${h}-${n.id}`) ?? 0
                                    const intensity = grid.max > 0 ? v / grid.max : 0
                                    return (
                                        <td
                                            key={n.id}
                                            title={v === 0 ? `${n.name} @${String(h).padStart(2, "0")}:00 — no hits` : `${n.name} @${String(h).padStart(2, "0")}:00 — ${v.toLocaleString()}`}
                                            className={cn(
                                                "h-6 min-w-[24px] rounded-sm align-middle transition-all hover:ring-2 hover:ring-foreground/40 hover:scale-110 cursor-default",
                                                v === 0 && "bg-muted/30",
                                            )}
                                            style={{
                                                backgroundColor: v === 0
                                                    ? undefined
                                                    : `oklch(0.65 ${0.08 + intensity * 0.12} 230 / ${0.25 + intensity * 0.65})`,
                                            }}
                                        />
                                    )
                                })}
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </Card>
    )
}

function Legend({ max }: { max: number }) {
    const steps = 5
    return (
        <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
            <span className="uppercase tracking-wider font-semibold">Low</span>
            <div className="flex">
                {Array.from({ length: steps }, (_, i) => {
                    const intensity = (i + 1) / steps
                    return (
                        <div
                            key={i}
                            className="w-4 h-3"
                            style={{ backgroundColor: `oklch(0.65 ${0.08 + intensity * 0.12} 230 / ${0.25 + intensity * 0.65})` }}
                        />
                    )
                })}
            </div>
            <span className="uppercase tracking-wider font-semibold">{max.toLocaleString()}</span>
        </div>
    )
}
