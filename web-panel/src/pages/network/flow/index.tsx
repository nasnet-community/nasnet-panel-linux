import { useMemo, useState } from "react"
import { Link } from "react-router"
import { ArrowLeft, Download, Network, RefreshCw, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { EmptyState } from "@/components/ui/empty-state"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { FlowConnsTable } from "@/components/flow/flow-conns-table"
import { FlowDetailPanel } from "@/components/flow/flow-detail-panel"
import { FlowGraph } from "@/components/flow/flow-graph"
import { FlowTimeline } from "@/components/flow/flow-timeline"
import { MismatchStrip } from "@/components/flow/mismatch-strip"
import { TraceBar } from "@/components/flow/trace-bar"
import { exportDebugBundle } from "@/components/flow/flow-export"
import { useRates } from "@/components/flow/use-rates"
import { useFlowConns, useFlowEvents, useFlowGraph } from "@/lib/queries/use-flow"
import { usePageTitle } from "@/hooks/use-page-title"
import { cn } from "@/lib/utils"
import type { TraceView } from "@/lib/types/flow"

const DNS_OVERLAY_KEY = "flow-dns-overlay"

export default function TrafficFlowPage() {
    usePageTitle("Traffic flow")
    const flow = useFlowGraph()
    const conns = useFlowConns()
    const events = useFlowEvents()
    const rates = useRates(flow.data)

    const [selected, setSelected] = useState<string | null>(null)
    const [trace, setTrace] = useState<TraceView | null>(null)
    const [showDNS, setShowDNS] = useState(
        () => localStorage.getItem(DNS_OVERLAY_KEY) === "true",
    )

    const selectedNode = useMemo(
        () => flow.data?.nodes.find((n) => n.id === selected) ?? null,
        [flow.data, selected],
    )
    const selectedMismatches = useMemo(
        () => (flow.data?.mismatches ?? []).filter((m) => m.node_id === selected),
        [flow.data, selected],
    )

    function toggleDNS(on: boolean) {
        setShowDNS(on)
        localStorage.setItem(DNS_OVERLAY_KEY, String(on))
    }

    // Router mode off 404s every route. Any other error is a real failure, and
    // this is the page you open to diagnose one.
    if (flow.isError) {
        const status = (flow.error as { status?: number } | null)?.status
        return status === 404 ? (
            <EmptyState
                icon={Network}
                title="Router mode is not enabled"
                description="The traffic flow view only exists when this box is doing the routing."
            />
        ) : (
            <EmptyState
                icon={TriangleAlert}
                title="The flow view could not be loaded"
                description={flow.error?.message ?? "The panel could not read this box's routing state."}
            />
        )
    }

    if (flow.isLoading) {
        return (
            <div className="space-y-4">
                <Skeleton className="h-10 w-full max-w-md" />
                <Skeleton className="h-[420px] w-full" />
                <Skeleton className="h-40 w-full" />
            </div>
        )
    }

    return (
        <div className="mx-auto max-w-7xl space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                    <Button variant="ghost" size="sm" asChild>
                        <Link to="/network">
                            <ArrowLeft className="mr-1 h-3.5 w-3.5" />
                            Network
                        </Link>
                    </Button>
                    <h1 className="text-xl font-semibold">Traffic flow</h1>
                </div>
                <div className="flex items-center gap-3">
                    <div className="flex items-center gap-2">
                        <Switch id="dns-overlay" checked={showDNS} onCheckedChange={toggleDNS} />
                        <Label htmlFor="dns-overlay" className="text-text-secondary text-xs">
                            DNS overlay
                        </Label>
                    </div>
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={() =>
                            exportDebugBundle({
                                flow: flow.data ?? null,
                                conns: conns.data ?? null,
                                events: events.data ?? null,
                                trace,
                            })
                        }
                    >
                        <Download className="mr-1.5 h-3.5 w-3.5" />
                        Export
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => void flow.refetch()}>
                        <RefreshCw
                            className={cn("h-3.5 w-3.5", flow.isFetching && "animate-spin")}
                        />
                        <span className="sr-only">Refresh</span>
                    </Button>
                </div>
            </div>

            {flow.data && (
                <MismatchStrip mismatches={flow.data.mismatches} onPick={setSelected} />
            )}

            <TraceBar onResult={setTrace} onClear={() => setTrace(null)} />

            {flow.data && (
                <FlowGraph
                    flow={flow.data}
                    rates={rates}
                    selected={selected}
                    onSelect={setSelected}
                    trace={trace}
                    showDNS={showDNS}
                />
            )}

            <FlowTimeline events={events.data ?? []} />

            <FlowConnsTable view={conns.data ?? null} loading={conns.isLoading} />

            <FlowDetailPanel
                node={selectedNode}
                mismatches={selectedMismatches}
                onClose={() => setSelected(null)}
            />
        </div>
    )
}
