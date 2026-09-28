import { Copy, Download, Lock, Plus, RefreshCw, ShieldAlert, ShieldCheck } from "lucide-react"
import { toast } from "@/lib/toast"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { useInitializeCA } from "@/lib/queries"
import type { CertCA, CertOverview, StrictModeState } from "@/lib/types"
import { cn, copyToClipboard, formatDate } from "@/lib/utils"
import { blockerLabel, certErrorMessage, daysLabel, expiryTone, toneBadge, toneText } from "./cert-helpers"
import { cardShell, T } from "./cert-ui"
import { useCertActions } from "./use-cert-actions"

interface CertCAStripProps {
    ca: CertCA | null
    strict: StrictModeState
    nodeCount: number
    overview: CertOverview
}

export function CertCAStrip({ ca, strict, nodeCount, overview }: CertCAStripProps) {
    const initCA = useInitializeCA()
    const actions = useCertActions(overview)

    const copyFingerprint = async () => {
        if (!ca?.fingerprint_sha256) return
        await copyToClipboard(ca.fingerprint_sha256)
        toast.success("Fingerprint copied")
    }

    const strictLine = strict.enabled
        ? strict.blockers.length > 0
            ? `Refusing ${strict.blockers.map(b => b.node_name).join(", ")}`
            : "Revoked certificates and certificates of another node are refused."
        : strict.ready
            ? `${nodeCount}/${nodeCount} nodes ready`
            : `Not ready: ${strict.blockers.map(b => `${b.node_name} ${blockerLabel(b.reason)}`).join(", ")}`

    return (
        <Card className={cn(cardShell, "gap-0 p-0")}>
            <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
                {ca ? (
                    <>
                        <ShieldCheck className={cn("h-5 w-5 shrink-0", toneText[expiryTone(ca.days_left)])} />
                        <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-2">
                                <span className="truncate text-sm font-medium">{ca.common_name}</span>
                                <Badge variant="outline" className={cn("px-2 py-0 text-[12px]", toneBadge.ok)}>Active</Badge>
                            </div>
                            <div className={cn(T.meta, "mt-0.5 flex flex-wrap items-center gap-x-2")}>
                                <span className={toneText[expiryTone(ca.days_left)]}>{daysLabel(ca.days_left)} left</span>
                                <span>· expires {formatDate(ca.not_after, "long")}</span>
                                {ca.fingerprint_sha256 && (
                                    <button type="button" onClick={() => void copyFingerprint()} className="inline-flex items-center gap-1 font-mono hover:text-foreground" title={ca.fingerprint_sha256}>
                                        · SHA-256 {ca.fingerprint_sha256.slice(0, 11)}… <Copy className="h-3 w-3" />
                                    </button>
                                )}
                            </div>
                        </div>
                        <Button variant="outline" size="sm" className="h-11 sm:h-8" onClick={() => void actions.downloadCA()}>
                            <Download className="mr-1.5 h-3.5 w-3.5" />CA certificate
                        </Button>
                    </>
                ) : (
                    <>
                        <ShieldAlert className="h-5 w-5 shrink-0 text-amber-500" />
                        <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                                <span className="text-sm font-medium">Certificate authority</span>
                                <Badge variant="outline" className={cn("px-2 py-0 text-[12px]", toneBadge.warn)}>Not initialized</Badge>
                            </div>
                            <p className={cn(T.meta, "mt-0.5")}>Initialize the CA to issue node certificates.</p>
                        </div>
                        <Button size="sm" className="h-11 sm:h-8" disabled={initCA.isPending} onClick={() => initCA.mutate(undefined, { onError: (e) => toast.error(certErrorMessage(e)) })}>
                            {initCA.isPending ? <RefreshCw className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <Plus className="mr-1.5 h-3.5 w-3.5" />}
                            Initialize CA
                        </Button>
                    </>
                )}
            </div>
            {ca && (
                <div className="flex flex-col gap-3 border-t border-border/60 p-4 sm:flex-row sm:items-center">
                    <Lock className={cn("h-5 w-5 shrink-0", strict.enabled ? "text-emerald-500" : "text-muted-foreground")} />
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                            <span className="text-sm font-medium">Strict mode</span>
                            <Badge variant="outline" className={cn("px-2 py-0 text-[12px]", strict.enabled ? toneBadge.ok : toneBadge.muted)}>
                                {strict.enabled ? "On" : "Off"}
                            </Badge>
                        </div>
                        <p className={cn(T.meta, "mt-0.5", strict.enabled && strict.blockers.length > 0 && "text-red-500")}>{strictLine}</p>
                    </div>
                    <Button
                        size="sm"
                        variant={strict.enabled ? "outline" : "default"}
                        className="h-11 sm:h-8"
                        disabled={!strict.enabled && !strict.ready}
                        onClick={actions.toggleStrict}
                    >
                        {strict.enabled ? "Turn off" : "Turn on"}
                    </Button>
                </div>
            )}
        </Card>
    )
}
