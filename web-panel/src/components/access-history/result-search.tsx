import { useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Card } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ArrowUpRight, Search } from "lucide-react"
import { Heatmap } from "./heatmap"
import { cn } from "@/lib/utils"
import type {
    AccessHistoryGlobalHit,
    AccessHistoryGlobalSubAggregate,
    AccessHistoryGlobalValueAggregate,
    AccessHistorySearchKind,
} from "@/lib/types"

interface Props {
    hits: AccessHistoryGlobalHit[]
    bySubscription: AccessHistoryGlobalSubAggregate[]
    byValue: AccessHistoryGlobalValueAggregate[]
    nodes: { id: number; name: string }[]
    onPivotToSubscription: (subID: number, label: string) => void
}

const KIND_TONE: Record<AccessHistorySearchKind, string> = {
    domain: "text-emerald-400 bg-emerald-500/10 border-emerald-500/30",
    rejected_domain: "text-red-400 bg-red-500/10 border-red-500/30",
    source_ip: "text-sky-400 bg-sky-500/10 border-sky-500/30",
}

const KIND_LABEL: Record<AccessHistorySearchKind, string> = {
    domain: "accept",
    rejected_domain: "reject",
    source_ip: "ip",
}

function formatLargeNumber(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
    return n.toLocaleString()
}

function KindTag({ kind }: { kind: AccessHistorySearchKind }) {
    return (
        <span
            className={cn(
                "inline-flex items-center px-1.5 py-px rounded text-[10px] font-mono font-semibold uppercase tracking-wider border ml-2 align-middle",
                KIND_TONE[kind],
            )}
        >
            {KIND_LABEL[kind]}
        </span>
    )
}

export function ResultSearch(props: Props) {
    const [tab, setTab] = useState("by_value")
    return (
        <Tabs value={tab} onValueChange={setTab} className="space-y-3">
            <TabsList className="h-9">
                <TabsTrigger value="by_value" className="text-xs gap-1.5">
                    By value
                    <span className="text-muted-foreground tabular-nums">({props.byValue.length})</span>
                </TabsTrigger>
                <TabsTrigger value="by_subscription" className="text-xs gap-1.5">
                    By subscription
                    <span className="text-muted-foreground tabular-nums">({props.bySubscription.length})</span>
                </TabsTrigger>
                <TabsTrigger value="hits" className="text-xs gap-1.5">
                    Raw hits
                    <span className="text-muted-foreground tabular-nums">({props.hits.length})</span>
                </TabsTrigger>
                <TabsTrigger value="heatmap" className="text-xs">Heatmap</TabsTrigger>
            </TabsList>

            <TabsContent value="by_value" className="animate-in fade-in duration-300">
                <ByValueTable rows={props.byValue} />
            </TabsContent>
            <TabsContent value="by_subscription" className="animate-in fade-in duration-300">
                <BySubscriptionTable rows={props.bySubscription} onPivot={props.onPivotToSubscription} />
            </TabsContent>
            <TabsContent value="hits" className="animate-in fade-in duration-300">
                <RawHitsTable rows={props.hits} />
            </TabsContent>
            <TabsContent value="heatmap" className="animate-in fade-in duration-300">
                <Heatmap hits={props.hits} nodes={props.nodes} />
            </TabsContent>
        </Tabs>
    )
}

function ByValueTable({ rows }: { rows: AccessHistoryGlobalValueAggregate[] }) {
    if (rows.length === 0) return <EmptyState />
    return (
        <Card className="p-0 overflow-hidden">
            <div className="overflow-auto max-h-[60vh]">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Value</TableHead>
                            <TableHead className="text-right w-[110px]">Hits</TableHead>
                            <TableHead className="text-right w-[80px]">Subs</TableHead>
                            <TableHead className="text-right w-[80px]">Hours</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r, i) => (
                            <TableRow key={`${r.kind}-${r.value}-${i}`} className="hover:bg-muted/30">
                                <TableCell className="font-mono text-xs truncate max-w-[380px]">
                                    {r.value}
                                    <KindTag kind={r.kind} />
                                </TableCell>
                                <TableCell className="text-right font-mono tabular-nums font-semibold">{formatLargeNumber(r.count)}</TableCell>
                                <TableCell className="text-right font-mono tabular-nums text-muted-foreground">{r.subscriptions}</TableCell>
                                <TableCell className="text-right font-mono tabular-nums text-muted-foreground">{r.hours}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </Card>
    )
}

function BySubscriptionTable({ rows, onPivot }: { rows: AccessHistoryGlobalSubAggregate[]; onPivot: (id: number, label: string) => void }) {
    if (rows.length === 0) return <EmptyState />
    return (
        <Card className="p-0 overflow-hidden">
            <div className="overflow-auto max-h-[60vh]">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Subscription</TableHead>
                            <TableHead>Value</TableHead>
                            <TableHead className="text-right w-[110px]">Hits</TableHead>
                            <TableHead className="text-right w-[80px]">Hours</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r, i) => {
                            const label = r.subscription_label || `Sub #${r.subscription_id}`
                            return (
                                <TableRow key={`${r.subscription_id}-${r.kind}-${r.value}-${i}`} className="hover:bg-muted/30">
                                    <TableCell>
                                        <button
                                            className="group flex items-center gap-1.5 text-left hover:text-foreground"
                                            onClick={() => onPivot(r.subscription_id, label)}
                                            title="Pin to filter rail and switch to Overview"
                                        >
                                            <span className="text-muted-foreground font-mono text-xs">#{r.subscription_id}</span>
                                            <span className="text-sm truncate max-w-[180px]">{label}</span>
                                            <ArrowUpRight className="w-3 h-3 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                                        </button>
                                    </TableCell>
                                    <TableCell className="font-mono text-xs truncate max-w-[320px]">
                                        {r.value}
                                        <KindTag kind={r.kind} />
                                    </TableCell>
                                    <TableCell className="text-right font-mono tabular-nums font-semibold">{formatLargeNumber(r.count)}</TableCell>
                                    <TableCell className="text-right font-mono tabular-nums text-muted-foreground">{r.hours}</TableCell>
                                </TableRow>
                            )
                        })}
                    </TableBody>
                </Table>
            </div>
        </Card>
    )
}

function RawHitsTable({ rows }: { rows: AccessHistoryGlobalHit[] }) {
    if (rows.length === 0) return <EmptyState />
    return (
        <Card className="p-0 overflow-hidden">
            <div className="overflow-auto max-h-[60vh]">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[150px]">Bucket (UTC)</TableHead>
                            <TableHead>Subscription</TableHead>
                            <TableHead className="w-[80px]">Node</TableHead>
                            <TableHead>Value</TableHead>
                            <TableHead className="text-right w-[100px]">Count</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((h, i) => (
                            <TableRow key={`${h.bucket}-${h.email}-${h.value}-${i}`} className="hover:bg-muted/30">
                                <TableCell className="font-mono tabular-nums text-xs">
                                    {new Date(h.bucket).toISOString().replace("T", " ").slice(0, 16)}
                                </TableCell>
                                <TableCell className="text-xs">
                                    {h.subscription_id > 0
                                        ? <><span className="text-muted-foreground font-mono">#{h.subscription_id}</span> {h.subscription_label || ""}</>
                                        : <span className="text-muted-foreground italic">unattached</span>
                                    }
                                </TableCell>
                                <TableCell className="font-mono tabular-nums text-xs text-muted-foreground">{h.node_id}</TableCell>
                                <TableCell className="font-mono text-xs truncate max-w-[320px]">
                                    {h.value}
                                    <KindTag kind={h.kind} />
                                </TableCell>
                                <TableCell className="text-right font-mono tabular-nums font-semibold">{formatLargeNumber(h.count)}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </Card>
    )
}

function EmptyState() {
    return (
        <Card className="p-10 flex flex-col items-center justify-center gap-3 text-center">
            <div className="w-12 h-12 rounded-full bg-muted/50 flex items-center justify-center">
                <Search className="w-5 h-5 text-muted-foreground" />
            </div>
            <div className="space-y-1">
                <p className="text-sm font-medium">No results in this slice</p>
                <p className="text-xs text-muted-foreground">Try widening the date range or removing filters.</p>
            </div>
        </Card>
    )
}
