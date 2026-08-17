import { useCallback, useRef } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Badge } from "@/components/ui/badge"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { useEventListener } from "@/components/providers/events-provider"
import { eventTone, relativeTime } from "@/lib/flow-labels"
import { queryKeys } from "@/lib/queries/keys"
import type { FlowEvent } from "@/lib/types/flow"

function variantFor(type: string) {
    switch (eventTone(type)) {
        case "bad":
            return "danger" as const
        case "ok":
            return "success" as const
        default:
            return "outline" as const
    }
}

/** What changed just before it broke — newest first. */
export function FlowTimeline({ events }: { events: FlowEvent[] }) {
    const qc = useQueryClient()
    const debounce = useRef<ReturnType<typeof setTimeout> | null>(null)

    useEventListener(
        useCallback(
            (type: string) => {
                if (!type.startsWith("wan.") && !type.startsWith("vpn.") &&
                    !type.startsWith("interface.")) {
                    return
                }
                if (debounce.current) clearTimeout(debounce.current)
                debounce.current = setTimeout(() => {
                    void qc.invalidateQueries({ queryKey: queryKeys.networkFlowEvents() })
                }, 400)
            },
            [qc],
        ) as never,
    )

    if (events.length === 0) {
        return <p className="text-text-tertiary text-xs">no recent network events</p>
    }

    const newestFirst = [...events].reverse()

    return (
        <ScrollArea className="w-full">
            <div className="flex items-center gap-2 pb-2">
                {newestFirst.map((e, i) => (
                    <Badge
                        key={`${e.type}-${e.timestamp}-${i}`}
                        variant={variantFor(e.type)}
                        className="shrink-0 gap-1.5"
                        title={JSON.stringify(e.payload)}
                    >
                        {e.type}
                        <span className="text-[10px] opacity-70 tabular-nums">
                            {relativeTime(e.timestamp)}
                        </span>
                    </Badge>
                ))}
            </div>
            <ScrollBar orientation="horizontal" />
        </ScrollArea>
    )
}
