import { useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/ui/badge"
import { HostList } from "@/components/host/host-list"
import { InboundAccountsRow } from "@/components/node/inbound-accounts-row"
import { cn, copyToClipboard, formatDateTime } from "@/lib/utils"
import { toast } from "sonner"
import { HiOutlineClipboardCopy } from "react-icons/hi"
import type { Inbound } from "@/lib/types"
import type { Account } from "@/lib/api/accounts"
import type { InboundDetail } from "./desktop-inbound-row"

interface InboundDetailPanelProps {
    inbound: Inbound
    accounts: Account[]
    details: InboundDetail[]
    nodeId: number
    isOnline: boolean
    onAccountChange?: () => void
}

export function InboundDetailPanel({
    inbound, accounts, details, nodeId, isOnline, onAccountChange,
}: InboundDetailPanelProps) {
    const [tab, setTab] = useState("clients")
    const hostCount = inbound.hosts?.length ?? 0

    return (
        <Tabs value={tab} onValueChange={setTab} className="space-y-3">
            <TabsList variant="line" className="gap-5">
                <TabsTrigger value="hosts">
                    Hosts
                    {hostCount > 0 && (
                        <Badge variant="secondary" className="ml-1.5 px-1.5 py-0 text-[10px]">{hostCount}</Badge>
                    )}
                </TabsTrigger>
                <TabsTrigger value="clients">
                    Clients
                    {accounts.length > 0 && (
                        <Badge variant="secondary" className="ml-1.5 px-1.5 py-0 text-[10px]">{accounts.length}</Badge>
                    )}
                </TabsTrigger>
                <TabsTrigger value="details">Details</TabsTrigger>
            </TabsList>

            <TabsContent value="hosts" className="mt-3">
                <HostList
                    inboundId={inbound.id}
                    initialHosts={inbound.hosts}
                    inbound={inbound}
                    showHeader={false}
                />
            </TabsContent>

            <TabsContent value="clients" className="mt-3">
                <InboundAccountsRow
                    accounts={accounts}
                    nodeId={nodeId}
                    inboundId={inbound.id}
                    isOnline={isOnline}
                    onAccountChange={onAccountChange}
                />
            </TabsContent>

            <TabsContent value="details" className="mt-3">
                <InboundDetails inbound={inbound} details={details} />
            </TabsContent>
        </Tabs>
    )
}

// Everything the row used to squeeze onto one 10px line, at a size you can read.
function InboundDetails({ inbound, details }: { inbound: Inbound; details: InboundDetail[] }) {
    const rows: { label: string; value: string; copy?: boolean }[] = [
        { label: "Listen", value: `${inbound.listen || "0.0.0.0"}:${inbound.port}`, copy: true },
        ...(inbound.port_range ? [{ label: "Port range", value: inbound.port_range }] : []),
        { label: "Protocol", value: inbound.protocol.toUpperCase() },
        { label: "Transport", value: (inbound.network || "tcp").toUpperCase() },
        { label: "Security", value: (inbound.security || "none").toUpperCase() },
        { label: "Sniffing", value: inbound.sniffing_settings?.enabled ? "Enabled" : "Off" },
        ...details.map(d => ({ label: d.label, value: d.value, copy: true })),
        ...(inbound.remark ? [{ label: "Remark", value: inbound.remark }] : []),
        { label: "Created", value: formatDateTime(inbound.created_at) },
        { label: "Updated", value: formatDateTime(inbound.updated_at) },
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
