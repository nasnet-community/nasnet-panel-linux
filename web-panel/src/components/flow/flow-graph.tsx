import { useMemo } from "react"
import { useChartPalette } from "@/lib/design/palette"
import { formatRate } from "@/lib/flow-labels"
import type { FlowEdge, FlowNode, FlowView, TraceView } from "@/lib/types/flow"
import type { Rate } from "@/components/flow/use-rates"

// Fixed positions, not a layout engine: the picture must look the same every
// time it opens, so an operator builds a memory of where things are.
const POS: Record<string, { x: number; y: number }> = {
    dns: { x: 250, y: 16 },
    "src-lan": { x: 24, y: 128 },
    "src-xray": { x: 24, y: 236 },
    "src-router": { x: 24, y: 344 },
    "mark-domestic": { x: 250, y: 152 },
    "mark-foreign": { x: 250, y: 300 },
    "table-201": { x: 476, y: 100 },
    "table-203": { x: 476, y: 256 },
    "table-202": { x: 476, y: 404 },
    wg: { x: 702, y: 256 },
    killswitch: { x: 702, y: 380 },
    "uplink-domestic": { x: 928, y: 100 },
    "uplink-secondary": { x: 928, y: 328 },
    "world-domestic": { x: 1154, y: 100 },
    "world-foreign": { x: 1154, y: 328 },
}

const NODE_W = 152
const NODE_H = 52
const VIEW_W = 1330
const VIEW_H = 480

interface FlowGraphProps {
    flow: FlowView
    rates: Record<string, Rate>
    selected: string | null
    onSelect: (id: string | null) => void
    trace: TraceView | null
    showDNS: boolean
}

export function FlowGraph({ flow, rates, selected, onSelect, trace, showDNS }: FlowGraphProps) {
    const c = useChartPalette()

    const nodes = useMemo(
        () => flow.nodes.filter((n) => POS[n.id] && (showDNS || n.kind !== "dns")),
        [flow.nodes, showDNS],
    )
    const edges = useMemo(
        () =>
            flow.edges.filter(
                (e) => POS[e.from] && POS[e.to] && (showDNS || e.kind !== "dns"),
            ),
        [flow.edges, showDNS],
    )

    const traced = useMemo(() => {
        if (!trace) return null
        return new Set([...trace.path_nodes, ...trace.path_edges])
    }, [trace])

    const withMismatch = useMemo(
        () => new Set(flow.mismatches.map((m) => m.node_id)),
        [flow.mismatches],
    )

    const stroke = (status: string) => {
        switch (status) {
            case "ok":
                return c.success || "#10b981"
            case "warn":
                return c.warning || "#f59e0b"
            case "down":
                return c.danger || "#ef4444"
            default:
                return c.mutedForeground || "#94a3b8"
        }
    }

    const reduceMotion =
        typeof window !== "undefined" &&
        window.matchMedia?.("(prefers-reduced-motion: reduce)").matches

    return (
        <div className="overflow-x-auto rounded-lg border bg-card p-2">
            <svg
                viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
                className="w-full min-w-[900px]"
                role="group"
                aria-label="Traffic flow graph"
            >
                <defs>
                    <marker
                        id="flow-arrow"
                        viewBox="0 0 10 10"
                        refX="9"
                        refY="5"
                        markerWidth="5"
                        markerHeight="5"
                        orient="auto"
                    >
                        <path d="M0,0 L10,5 L0,10 z" fill={c.mutedForeground || "#94a3b8"} />
                    </marker>
                </defs>

                {edges.map((e) => (
                    <EdgeShape
                        key={e.id}
                        edge={e}
                        color={edgeColor(e, c)}
                        dimmed={!!traced && !traced.has(e.id)}
                        highlighted={!!traced && traced.has(e.id)}
                        rate={e.counter_key ? rates[e.counter_key] : undefined}
                        animate={!reduceMotion}
                        labelColor={c.mutedForeground || "#94a3b8"}
                    />
                ))}

                {nodes.map((n) => (
                    <NodeShape
                        key={n.id}
                        node={n}
                        stroke={stroke(n.status)}
                        palette={c}
                        selected={selected === n.id}
                        flagged={withMismatch.has(n.id)}
                        dimmed={!!traced && !traced.has(n.id)}
                        onSelect={onSelect}
                    />
                ))}
            </svg>
        </div>
    )
}

function edgeColor(e: FlowEdge, c: ReturnType<typeof useChartPalette>): string {
    if (e.status === "ghost") return c.mutedForeground || "#94a3b8"
    if (e.kind === "drop") return c.danger || "#ef4444"
    if (e.kind === "dns") return c.chart6 || "#8b5cf6"
    if (e.kind === "transport") return c.chart2 || "#3b82f6"
    return c.success || "#10b981"
}

interface EdgeShapeProps {
    edge: FlowEdge
    color: string
    dimmed: boolean
    highlighted: boolean
    rate?: Rate
    animate: boolean
    labelColor: string
}

function EdgeShape({
    edge,
    color,
    dimmed,
    highlighted,
    rate,
    animate,
    labelColor,
}: EdgeShapeProps) {
    const from = POS[edge.from]
    const to = POS[edge.to]
    const x1 = from.x + NODE_W
    const y1 = from.y + NODE_H / 2
    const x2 = to.x
    const y2 = to.y + NODE_H / 2
    const mx = (x1 + x2) / 2
    const d = `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`
    const pathId = `flow-edge-${edge.id}`

    const bps = rate ? rate.rxBps + rate.txBps : 0
    // Faster with more traffic; nothing at all when nothing is moving, which is
    // itself the signal.
    const dotDur = Math.max(0.8, Math.min(6, 6 - Math.log10(bps + 1)))

    return (
        <g opacity={dimmed ? 0.15 : 1} data-dimmed={dimmed || undefined} data-edge={edge.id}>
            <path
                id={pathId}
                d={d}
                fill="none"
                stroke={color}
                strokeWidth={highlighted ? 3.5 : 1.75}
                strokeDasharray={edge.status === "ghost" ? "6 5" : undefined}
                markerEnd="url(#flow-arrow)"
            />
            {edge.label && (
                <text
                    x={mx}
                    y={(y1 + y2) / 2 - 8}
                    textAnchor="middle"
                    fontSize="10"
                    fill={labelColor}
                >
                    {edge.label}
                </text>
            )}
            {rate && (
                <text
                    x={mx}
                    y={(y1 + y2) / 2 + 16}
                    textAnchor="middle"
                    fontSize="10"
                    fill={labelColor}
                    className="tabular-nums"
                >
                    {formatRate(bps)}
                </text>
            )}
            {animate &&
                bps > 0 &&
                edge.status !== "ghost" &&
                [0, dotDur / 2].map((begin) => (
                    <circle key={begin} r="3.5" fill={color} opacity="0">
                        <animate
                            attributeName="opacity"
                            values="0;1;1;0"
                            keyTimes="0;0.1;0.9;1"
                            dur={`${dotDur}s`}
                            begin={`${begin}s`}
                            repeatCount="indefinite"
                        />
                        <animateMotion
                            dur={`${dotDur}s`}
                            begin={`${begin}s`}
                            repeatCount="indefinite"
                        >
                            <mpath href={`#${pathId}`} />
                        </animateMotion>
                    </circle>
                ))}
        </g>
    )
}

interface NodeShapeProps {
    node: FlowNode
    stroke: string
    palette: ReturnType<typeof useChartPalette>
    selected: boolean
    flagged: boolean
    dimmed: boolean
    onSelect: (id: string) => void
}

function NodeShape({
    node,
    stroke,
    palette,
    selected,
    flagged,
    dimmed,
    onSelect,
}: NodeShapeProps) {
    const p = POS[node.id]
    const ghost = node.status === "ghost"

    return (
        <g
            transform={`translate(${p.x} ${p.y})`}
            role="button"
            tabIndex={0}
            aria-label={node.label}
            data-node={node.id}
            data-status={node.status}
            data-dimmed={dimmed || undefined}
            opacity={dimmed ? 0.18 : ghost ? 0.55 : 1}
            className="cursor-pointer"
            onClick={() => onSelect(node.id)}
            onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault()
                    onSelect(node.id)
                }
            }}
        >
            <title>{node.hint || node.label}</title>
            <rect
                width={NODE_W}
                height={NODE_H}
                rx="10"
                fill={palette.tooltipBg || "transparent"}
                stroke={selected ? palette.info || "#3b82f6" : stroke}
                strokeWidth={selected ? 2.5 : 1.5}
                strokeDasharray={ghost ? "5 4" : undefined}
            />
            <text x="12" y="21" fontSize="12.5" fontWeight="500" fill={palette.foreground}>
                {node.label}
            </text>
            {node.sublabel && (
                <text x="12" y="37" fontSize="10.5" fill={palette.mutedForeground}>
                    {truncate(node.sublabel, 22)}
                </text>
            )}
            {flagged && <circle cx={NODE_W - 12} cy="12" r="4.5" fill={palette.danger || "#ef4444"} />}
        </g>
    )
}

function truncate(s: string, max: number): string {
    return s.length <= max ? s : s.slice(0, max - 1) + "…"
}
