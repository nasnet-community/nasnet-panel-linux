import { TooltipProvider } from "@/components/ui/tooltip"
import type { CertSummary, NodeCertRow } from "@/lib/types"
import { CertNodeRow } from "./cert-node-row"
import { CertCard, T } from "./cert-ui"

interface CertNodeListProps {
    nodes: NodeCertRow[]
    strictOn: boolean
    rotatingNodeId: number | null
    onRotate: (row: NodeCertRow) => void
    onView: (cert: CertSummary, row: NodeCertRow) => void
}

export function CertNodeList({ nodes, strictOn, rotatingNodeId, onRotate, onView }: CertNodeListProps) {
    return (
        <CertCard title={`Nodes · ${nodes.length}`}>
            {nodes.length === 0 ? (
                <p className={T.meta}>No nodes yet. Node certificates show up here once you add a node.</p>
            ) : (
                <TooltipProvider delayDuration={200}>
                    <div className="flex flex-col divide-y divide-border/60">
                        {nodes.map(row => (
                            <CertNodeRow
                                key={row.node_id}
                                row={row}
                                strictOn={strictOn}
                                rotating={rotatingNodeId === row.node_id}
                                onRotate={onRotate}
                                onView={onView}
                            />
                        ))}
                    </div>
                </TooltipProvider>
            )}
        </CertCard>
    )
}
