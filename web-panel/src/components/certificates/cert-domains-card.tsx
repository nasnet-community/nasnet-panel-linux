import { Link } from "react-router"
import { ArrowRight, RefreshCw } from "lucide-react"
import { toast } from "@/lib/toast"
import { Button } from "@/components/ui/button"
import { useRefreshDomainExpiry, useRenewSNICert } from "@/lib/queries"
import type { DomainCertRow } from "@/lib/types"
import { cn, formatRelativeTime } from "@/lib/utils"
import { certErrorMessage, daysLabel, expiryTone, sourceLabel, toneText } from "./cert-helpers"
import { CertCard, T } from "./cert-ui"

export function CertDomainsCard({ domains }: { domains: DomainCertRow[] }) {
    const renew = useRenewSNICert()
    const refresh = useRefreshDomainExpiry()
    const hasFiles = domains.some(d => d.source === "file")

    return (
        <CertCard
            title={`Domains · ${domains.length}`}
            aside={
                <div className="flex items-center gap-1">
                    {hasFiles && (
                        <Button variant="ghost" size="sm" className="h-8" disabled={refresh.isPending}
                            onClick={() => refresh.mutate(undefined, { onError: (e) => toast.error(certErrorMessage(e)) })}>
                            <RefreshCw className={cn("mr-1 h-3.5 w-3.5", refresh.isPending && "animate-spin")} />Re-check
                        </Button>
                    )}
                    <Button variant="ghost" size="sm" className="h-8" asChild>
                        <Link to="/domains">Manage<ArrowRight className="ml-1 h-3.5 w-3.5" /></Link>
                    </Button>
                </div>
            }
        >
            {domains.length === 0 ? (
                <p className={T.meta}>No domain certificates. Add one on the Domains page.</p>
            ) : (
                <ul className="flex flex-col divide-y divide-border/60">
                    {domains.map(d => {
                        const tone = expiryTone(d.days_left)
                        return (
                            <li key={d.id} className="flex flex-col gap-1 py-2.5 first:pt-0 last:pb-0">
                                <div className="flex items-center gap-2">
                                    <span className="truncate font-mono text-sm">{d.domain}</span>
                                    <span className={cn("ml-auto shrink-0 text-xs tabular-nums", toneText[tone])}>{daysLabel(d.days_left)}</span>
                                </div>
                                <div className={cn(T.meta, "flex flex-wrap items-center gap-x-1.5")}>
                                    <span>{sourceLabel(d.source, d.node_names)}</span>
                                    {d.auto_renew && <span>· auto-renew</span>}
                                    <span>· {d.usage_count} inbound{d.usage_count === 1 ? "" : "s"}</span>
                                    {d.checked_on && d.checked_at && <span>· read from {d.checked_on} {formatRelativeTime(d.checked_at)}</span>}
                                </div>
                                {d.note && <p className={T.meta}>{d.note}</p>}
                                {d.issue_error && <p className="text-xs text-red-500">{d.issue_error}</p>}
                                {d.is_auto_issued && (
                                    <Button variant="outline" size="sm" className="h-8 self-start" disabled={renew.isPending} onClick={() => renew.mutate(d.id)}>
                                        Renew
                                    </Button>
                                )}
                            </li>
                        )
                    })}
                </ul>
            )}
        </CertCard>
    )
}
