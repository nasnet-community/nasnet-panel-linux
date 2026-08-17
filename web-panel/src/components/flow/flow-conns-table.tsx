import { Info } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import { decodeMark, formatBytes } from "@/lib/flow-labels"
import type { FlowConnsView } from "@/lib/types/flow"

const ROWS = 50

interface FlowConnsTableProps {
    view: FlowConnsView | null
    loading: boolean
}

/** The tracer says where traffic would go; this says where it actually went. */
export function FlowConnsTable({ view, loading }: FlowConnsTableProps) {
    if (loading) return <Skeleton className="h-64 w-full" />
    if (!view) return null

    const rows = view.flows.slice(0, ROWS)

    return (
        <Card>
            <CardHeader>
                <CardTitle>Live connections</CardTitle>
                <CardDescription>
                    {view.total === 0
                        ? "nothing is flowing right now"
                        : `top ${rows.length} of ${view.total}, by volume`}
                </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
                {!view.acct_enabled && (
                    <Alert variant="info">
                        <Info className="h-4 w-4" />
                        <AlertDescription>
                            Byte counters are off (nf_conntrack_acct=0), so these rows read zero.
                            Applying any network change turns them on.
                        </AlertDescription>
                    </Alert>
                )}
                <div className="overflow-x-auto">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Proto</TableHead>
                                <TableHead>Source</TableHead>
                                <TableHead>Destination</TableHead>
                                <TableHead>Mark</TableHead>
                                <TableHead className="text-right">Up</TableHead>
                                <TableHead className="text-right">Down</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {rows.map((f, i) => {
                                const m = decodeMark(f.mark)
                                return (
                                    <TableRow key={`${f.src}-${f.dst}-${i}`}>
                                        <TableCell className="font-mono text-xs">{f.proto}</TableCell>
                                        <TableCell className="font-mono text-xs">
                                            {f.src}
                                            {f.device && (
                                                <span className="text-text-tertiary block font-sans">
                                                    {f.device}
                                                </span>
                                            )}
                                        </TableCell>
                                        <TableCell className="font-mono text-xs">{f.dst}</TableCell>
                                        <TableCell className="space-x-1">
                                            {m.group ? (
                                                <Badge
                                                    variant={
                                                        m.group === "foreign" ? "warning" : "secondary"
                                                    }
                                                >
                                                    {m.group}
                                                </Badge>
                                            ) : (
                                                <span className="text-text-tertiary font-mono text-xs">
                                                    {m.hex}
                                                </span>
                                            )}
                                            {m.pin > 0 && (
                                                <Badge variant="outline">pin {m.pin}</Badge>
                                            )}
                                        </TableCell>
                                        <TableCell className="text-right font-mono text-xs tabular-nums">
                                            {formatBytes(f.tx_bytes)}
                                        </TableCell>
                                        <TableCell className="text-right font-mono text-xs tabular-nums">
                                            {formatBytes(f.rx_bytes)}
                                        </TableCell>
                                    </TableRow>
                                )
                            })}
                        </TableBody>
                    </Table>
                </div>
            </CardContent>
        </Card>
    )
}
