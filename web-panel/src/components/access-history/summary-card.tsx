import type { ReactNode } from "react"
import { AlertTriangle } from "lucide-react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import { Figure, FieldLabel, Pill } from "./bits"
import { HitsSparkline, type HourPoint } from "./charts"
import { fmtAgo, fmtDayTime, fmtInt, fmtWindow } from "./format"

function Seen({ first, last, extra }: { first?: string; last?: string; extra?: ReactNode }) {
    return (
        <dl className="m-0 grid grid-cols-[92px_minmax(0,1fr)] items-center gap-y-1.5 text-sm sm:ml-auto">
            <dt><FieldLabel>First seen</FieldLabel></dt>
            <dd className="m-0">{first ? fmtDayTime(first) : "—"}</dd>
            <dt><FieldLabel>Last seen</FieldLabel></dt>
            <dd className="m-0">
                {last ? <>{fmtAgo(last)} <span className="text-muted-foreground">{fmtDayTime(last)}</span></> : "—"}
            </dd>
            {extra}
        </dl>
    )
}

function Shell({ children, className }: { children: ReactNode; className?: string }) {
    return <Card className={cn("gap-4 px-4 py-4 sm:px-6 sm:pb-4 sm:pt-[18px]", className)}>{children}</Card>
}

export function SearchSummary({
    scope,
    from,
    to,
    subscriptions,
    values,
    hits,
    firstSeen,
    lastSeen,
    points,
    approximate,
    updating,
    onPickDay,
}: {
    scope: ReactNode
    from: Date
    to: Date
    subscriptions: number
    values: number
    hits: number
    firstSeen?: string
    lastSeen?: string
    points: HourPoint[]
    approximate: boolean
    updating: boolean
    onPickDay?: (dayStart: number) => void
}) {
    return (
        <Shell className={cn(updating && "opacity-70 transition-opacity")}>
            <div className="flex flex-wrap items-center gap-2.5">
                {scope}
                <span className="font-mono text-[13px] text-muted-foreground">{fmtWindow(from, to)}</span>
                {approximate && (
                    <span
                        className="inline-flex items-center gap-1 rounded-full border border-amber-500/35 bg-amber-500/10 px-2 py-0.5 text-[12px] text-amber-600 dark:text-amber-300"
                        title="More than 50,000 records matched. The figures cover the newest 50,000; narrow the range or the search for exact numbers."
                    >
                        <AlertTriangle className="size-3" />
                        Approximate
                    </span>
                )}
            </div>
            <div className="flex flex-wrap items-center gap-x-7 gap-y-4 sm:gap-x-12">
                <Figure label="Subscriptions" value={fmtInt(subscriptions)} />
                <Figure label="Values" value={fmtInt(values)} />
                <Figure label="Hits" value={fmtInt(hits)} />
                <Seen first={firstSeen} last={lastSeen} />
            </div>
            {points.length > 1 && (
                <div className="space-y-1.5 border-t border-border/60 pt-3.5">
                    <div className="flex justify-between text-[13px] text-muted-foreground">
                        <span>Hits per hour</span>
                        {onPickDay && <span className="hidden sm:inline">Click a day to zoom in</span>}
                    </div>
                    <HitsSparkline points={points} onPickDay={onPickDay} />
                </div>
            )}
        </Shell>
    )
}

export function ProfileSummary({
    subId,
    label,
    from,
    to,
    activeHours,
    windowHours,
    emails,
    sourceIps,
    firstSeen,
    lastSeen,
    nodes,
    actions,
}: {
    subId: number
    label: string
    from: Date
    to: Date
    activeHours?: number
    windowHours: number
    emails: number
    sourceIps?: number
    firstSeen?: string
    lastSeen?: string
    nodes: string
    actions: ReactNode
}) {
    return (
        <Shell>
            <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex flex-wrap items-center gap-2.5">
                    <Pill className="h-[26px] text-sm">
                        <span className="font-mono font-normal text-muted-foreground">#{subId}</span>
                        <span className="font-semibold">{label}</span>
                    </Pill>
                    <span className="font-mono text-[13px] text-muted-foreground">{fmtWindow(from, to)}</span>
                </div>
                <div className="flex flex-wrap items-center gap-1.5">{actions}</div>
            </div>
            <div className="flex flex-wrap items-center gap-x-7 gap-y-4 sm:gap-x-12">
                {activeHours !== undefined && <Figure label="Active hours" value={fmtInt(activeHours)} suffix={`of ${fmtInt(windowHours)}`} />}
                <Figure label="Emails" value={fmtInt(emails)} />
                {sourceIps !== undefined && <Figure label="Source IPs" value={fmtInt(sourceIps)} />}
                <Seen
                    first={firstSeen}
                    last={lastSeen}
                    extra={
                        <>
                            <dt><FieldLabel>Nodes</FieldLabel></dt>
                            <dd className="m-0 truncate">{nodes || "—"}</dd>
                        </>
                    }
                />
            </div>
        </Shell>
    )
}
