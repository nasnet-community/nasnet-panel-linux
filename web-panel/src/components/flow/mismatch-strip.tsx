import { useState } from "react"
import { CircleCheck, TriangleAlert } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import type { FlowMismatch } from "@/lib/types/flow"

const VISIBLE = 8

interface MismatchStripProps {
    mismatches: FlowMismatch[]
    onPick: (nodeId: string) => void
}

/** One glance at whether the kernel still matches what was asked for. */
export function MismatchStrip({ mismatches, onPick }: MismatchStripProps) {
    const [expanded, setExpanded] = useState(false)

    if (mismatches.length === 0) {
        return (
            <Badge variant="success" className="gap-1.5">
                <CircleCheck className="h-3 w-3" />
                the kernel matches the configuration
            </Badge>
        )
    }

    const shown = expanded ? mismatches : mismatches.slice(0, VISIBLE)
    const hidden = mismatches.length - shown.length

    return (
        <div className="flex flex-wrap items-center gap-2">
            {shown.map((m) => (
                <Badge
                    key={m.rule + m.message}
                    variant={m.severity === "error" ? "danger" : "warning"}
                    className="cursor-pointer gap-1.5"
                    onClick={() => onPick(m.node_id)}
                    title={m.expected ? `expected ${m.expected}, found ${m.actual}` : m.message}
                >
                    <TriangleAlert className="h-3 w-3" />
                    {m.message}
                </Badge>
            ))}
            {hidden > 0 && (
                <Badge
                    variant="outline"
                    className="cursor-pointer"
                    onClick={() => setExpanded(true)}
                >
                    +{hidden} more
                </Badge>
            )}
        </div>
    )
}
