import { useEffect, useRef, useState } from "react"
import { useSearchParams } from "react-router"
import { CircleCheck, Info, OctagonX, Search, TriangleAlert } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { useTraceFlow } from "@/lib/queries/use-flow"
import { traceVerdictLabel } from "@/lib/flow-labels"
import { cn } from "@/lib/utils"
import type { TraceSource, TraceStep, TraceView } from "@/lib/types/flow"

const SOURCES: { value: TraceSource; label: string }[] = [
    { value: "lan", label: "a LAN client" },
    { value: "xray-foreign", label: "xray — foreign outbound" },
    { value: "xray-domestic", label: "xray — domestic outbound" },
    { value: "router", label: "the router itself" },
]

function isSource(v: string | null): v is TraceSource {
    return SOURCES.some((s) => s.value === v)
}

interface TraceBarProps {
    onResult: (v: TraceView) => void
    onClear: () => void
}

export function TraceBar({ onResult, onClear }: TraceBarProps) {
    const [params, setParams] = useSearchParams()
    const trace = useTraceFlow()

    const initialSource = params.get("source")
    const [dest, setDest] = useState(params.get("trace") ?? "")
    const [source, setSource] = useState<TraceSource>(
        isSource(initialSource) ? initialSource : "lan",
    )
    const autoRan = useRef(false)

    // A shared URL should show its answer without anyone pressing anything.
    useEffect(() => {
        if (autoRan.current) return
        autoRan.current = true
        const preset = params.get("trace")
        if (!preset) return
        trace.mutate(
            { dest: preset, source: isSource(initialSource) ? initialSource : "lan" },
            { onSuccess: onResult },
        )
    }, [params, initialSource, trace, onResult])

    function run() {
        const target = dest.trim()
        if (!target) return
        trace.mutate(
            { dest: target, source },
            {
                onSuccess: (v) => {
                    onResult(v)
                    const next = new URLSearchParams(params)
                    next.set("trace", target)
                    next.set("source", source)
                    setParams(next, { replace: true })
                },
            },
        )
    }

    function clear() {
        trace.reset()
        onClear()
        const next = new URLSearchParams(params)
        next.delete("trace")
        next.delete("source")
        setParams(next, { replace: true })
    }

    const result = trace.data

    return (
        <Card>
            <CardContent className="space-y-4 pt-4">
                <div className="flex flex-wrap items-center gap-2">
                    <Input
                        value={dest}
                        onChange={(e) => setDest(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && run()}
                        placeholder="IP or domain — where would it go?"
                        className="max-w-xs flex-1"
                        aria-label="Trace destination"
                    />
                    <Select value={source} onValueChange={(v) => setSource(v as TraceSource)}>
                        <SelectTrigger className="w-56" aria-label="Trace source">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {SOURCES.map((s) => (
                                <SelectItem key={s.value} value={s.value}>
                                    {s.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <Button size="sm" onClick={run} disabled={trace.isPending || !dest.trim()}>
                        <Search className="mr-1.5 h-3.5 w-3.5" />
                        Trace
                    </Button>
                    {(result || trace.isError) && (
                        <Button variant="ghost" size="sm" onClick={clear}>
                            Clear
                        </Button>
                    )}
                </div>

                {trace.isError && (
                    <Alert variant="warning">
                        <TriangleAlert className="h-4 w-4" />
                        <AlertDescription>{trace.error.message}</AlertDescription>
                    </Alert>
                )}

                {result && <TraceResult result={result} />}
            </CardContent>
        </Card>
    )
}

function TraceResult({ result }: { result: TraceView }) {
    const dropped = result.final_verdict === "dropped"
    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
                <Badge variant={dropped ? "danger" : "success"}>
                    {traceVerdictLabel(result.final_verdict)}
                </Badge>
                {result.resolved_ip && result.resolved_ip !== result.dest && (
                    <span className="text-text-tertiary font-mono text-xs">
                        {result.dest} → {result.resolved_ip}
                    </span>
                )}
            </div>
            <ol className="space-y-2">
                {result.steps.map((step, i) => (
                    <li key={`${step.title}-${i}`} className="flex gap-2.5">
                        <StepIcon verdict={step.verdict} />
                        <div className="min-w-0 space-y-0.5">
                            <div className="text-sm font-medium">{step.title}</div>
                            {step.evidence.map((line) => (
                                <div
                                    key={line}
                                    className="text-text-secondary font-mono text-xs break-all select-all"
                                >
                                    {line}
                                </div>
                            ))}
                        </div>
                    </li>
                ))}
            </ol>
        </div>
    )
}

function StepIcon({ verdict }: { verdict: TraceStep["verdict"] }) {
    const cls = "mt-0.5 h-4 w-4 shrink-0"
    switch (verdict) {
        case "drop":
            return <OctagonX className={cn(cls, "text-status-danger")} />
        case "warn":
            return <TriangleAlert className={cn(cls, "text-status-warning")} />
        case "ok":
            return <CircleCheck className={cn(cls, "text-status-success")} />
        default:
            return <Info className={cn(cls, "text-text-tertiary")} />
    }
}
