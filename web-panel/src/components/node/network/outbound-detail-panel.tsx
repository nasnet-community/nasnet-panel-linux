import { useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { cn, copyToClipboard, countryFlag, formatDateTime, formatRelativeTime } from "@/lib/utils"
import { toast } from "sonner"
import { HiOutlineClipboardCopy, HiOutlineArrowRight, HiOutlineScale } from "react-icons/hi"
import type { Outbound } from "@/lib/types"
import { STATUS_LABELS } from "@/components/outbound/outbound-test-result-dialog"
import { hasStreamTransport, testEntryOf, type OutboundDetail, type OutboundUsage } from "./outbound-details"

interface OutboundDetailPanelProps {
    outbound: Outbound
    route: string
    details: OutboundDetail[]
    usage: OutboundUsage
    isDefault: boolean
    /** Jump to the Routing subtab's Rules pane. */
    onOpenRules: () => void
}

export function OutboundDetailPanel({
    outbound, route, details, usage, isDefault, onOpenRules,
}: OutboundDetailPanelProps) {
    const [tab, setTab] = useState("details")
    const usageTotal = usage.rules.length + usage.balancers.length

    return (
        <Tabs value={tab} onValueChange={setTab} className="space-y-3">
            <TabsList variant="line" className="gap-5">
                <TabsTrigger value="details">Details</TabsTrigger>
                <TabsTrigger value="rules">
                    Used by
                    {usageTotal > 0 && (
                        <Badge variant="secondary" className="ml-1.5 px-1.5 py-0 text-[10px]">{usageTotal}</Badge>
                    )}
                </TabsTrigger>
            </TabsList>

            <TabsContent value="details" className="mt-3">
                <OutboundDetails outbound={outbound} route={route} details={details} />
            </TabsContent>

            <TabsContent value="rules" className="mt-3">
                <OutboundUsageList usage={usage} isDefault={isDefault} onOpenRules={onOpenRules} />
            </TabsContent>
        </Tabs>
    )
}

// Everything the row cannot fit, at a size you can read.
function OutboundDetails({ outbound, route, details }: { outbound: Outbound; route: string; details: OutboundDetail[] }) {
    const streamed = hasStreamTransport(outbound.protocol)
    const test = testEntryOf(outbound)
    const lastTest = test
        ? [
            STATUS_LABELS[test.result.status ?? (test.result.success ? "passed" : "failed")],
            test.result.latency_ms > 0 ? `${test.result.latency_ms}ms` : null,
            test.result.ip ? `${countryFlag(test.result.country)} ${test.result.ip}`.trim() : null,
            formatRelativeTime(test.tested_at),
        ].filter(Boolean).join(" · ")
        : "Never"
    const domainStrategy = outbound.freedom_settings?.domainStrategy || outbound.wireguard_settings?.domainStrategy
    const mux = outbound.mux_settings?.enabled
        ? `On · ${outbound.mux_settings.concurrency ?? 8} streams`
        : "Off"

    const rows: { label: string; value: string; copy?: boolean }[] = [
        { label: "Protocol", value: outbound.protocol.toUpperCase() },
        { label: "Route", value: route, copy: true },
        ...(streamed ? [
            { label: "Transport", value: (outbound.network || "tcp").toUpperCase() },
            { label: "Security", value: (outbound.security || "none").toUpperCase() },
        ] : []),
        ...details.map(d => ({ label: d.label, value: d.value, copy: true })),
        ...(domainStrategy ? [{ label: "Domains", value: domainStrategy }] : []),
        ...(outbound.send_through ? [{ label: "Send through", value: outbound.send_through, copy: true }] : []),
        ...(outbound.sockopt_settings?.interface ? [{ label: "Interface", value: outbound.sockopt_settings.interface, copy: true }] : []),
        ...(outbound.sockopt_settings?.mark ? [{ label: "Mark", value: String(outbound.sockopt_settings.mark) }] : []),
        ...(streamed ? [{ label: "Mux", value: mux }] : []),
        ...(outbound.remark ? [{ label: "Remark", value: outbound.remark }] : []),
        { label: "Last test", value: lastTest },
        // Generated router entries have no persisted creation/update timestamps.
        ...(!outbound.managed ? [
            { label: "Created", value: formatDateTime(outbound.created_at) },
            { label: "Updated", value: formatDateTime(outbound.updated_at) },
        ] : []),
    ]

    return (
        <dl className="grid grid-cols-1 @xl:grid-cols-2 gap-x-8 gap-y-0 rounded-lg border bg-card px-4 py-1">
            {rows.map((r, i) => (
                <div key={`${r.label}-${i}`} className="flex items-baseline gap-3 py-2 border-b border-border/60 last:border-b-0 @xl:[&:nth-last-child(2)]:border-b-0">
                    <dt className="w-24 shrink-0 text-[11px] uppercase tracking-wide text-muted-foreground">{r.label}</dt>
                    <dd className="min-w-0 flex-1 font-mono text-xs break-all">{r.value}</dd>
                    {r.copy && (
                        <button
                            type="button"
                            aria-label={`Copy ${r.label}`}
                            onClick={async () => { await copyToClipboard(r.value); toast.success(`${r.label} copied`) }}
                            className={cn(
                                "shrink-0 h-6 w-6 rounded-md flex items-center justify-center text-muted-foreground",
                                "hover:text-foreground hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                            )}
                        >
                            <HiOutlineClipboardCopy className="w-3.5 h-3.5" />
                        </button>
                    )}
                </div>
            ))}
        </dl>
    )
}

// Who routes here. Rules first, balancers after; each row jumps to Routing.
function OutboundUsageList({ usage, isDefault, onOpenRules }: { usage: OutboundUsage; isDefault: boolean; onOpenRules: () => void }) {
    const empty = usage.rules.length === 0 && usage.balancers.length === 0

    return (
        <div className="space-y-2">
            {isDefault && (
                <p className="text-xs text-muted-foreground rounded-lg border border-primary/20 bg-primary/5 px-3 py-2">
                    <b className="text-foreground font-medium">Default outbound.</b> Traffic no routing rule matches ends up here.
                </p>
            )}

            {empty ? (
                <div className="flex flex-col items-start gap-2 rounded-lg border border-dashed px-4 py-4 text-sm text-muted-foreground">
                    <span>No routing rule or balancer targets this outbound{isDefault ? " directly" : ""}.</span>
                    <Button variant="outline" size="sm" onClick={onOpenRules}>
                        Open routing rules
                        <HiOutlineArrowRight className="w-3.5 h-3.5 ml-1.5" />
                    </Button>
                </div>
            ) : (
                <div className="rounded-lg border bg-card divide-y divide-border/60">
                    {usage.rules.map(rule => (
                        <button
                            key={`rule-${rule.id}`}
                            type="button"
                            onClick={onOpenRules}
                            className="w-full flex items-center gap-3 px-3 py-2 text-left hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring first:rounded-t-lg last:rounded-b-lg"
                        >
                            <span className={cn(
                                "w-2 h-2 rounded-full shrink-0",
                                rule.enabled ? "bg-emerald-500" : "border-[1.5px] border-muted-foreground/50",
                            )} />
                            <span className="font-mono text-xs font-medium truncate">{rule.rule_tag}</span>
                            {rule.remark && <span className="text-xs text-muted-foreground truncate">{rule.remark}</span>}
                            <span className="ml-auto text-[11px] text-muted-foreground shrink-0">rule</span>
                        </button>
                    ))}
                    {usage.balancers.map(b => (
                        <button
                            key={`bal-${b.id}`}
                            type="button"
                            onClick={onOpenRules}
                            className="w-full flex items-center gap-3 px-3 py-2 text-left hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring first:rounded-t-lg last:rounded-b-lg"
                        >
                            <HiOutlineScale className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                            <span className="font-mono text-xs font-medium truncate">{b.tag}</span>
                            <span className="text-xs text-muted-foreground">{b.strategy}</span>
                            <span className="ml-auto text-[11px] text-muted-foreground shrink-0">balancer</span>
                        </button>
                    ))}
                </div>
            )}
        </div>
    )
}
