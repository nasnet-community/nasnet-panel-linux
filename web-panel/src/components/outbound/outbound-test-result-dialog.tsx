import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from "@/components/ui/dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { cn, countryFlag, formatRelativeTime } from "@/lib/utils"
import type { OutboundTestEntry, OutboundTestStatus } from "@/lib/types"
import {
    HiOutlineCheckCircle,
    HiOutlineXCircle,
    HiOutlineClock,
    HiOutlineGlobeAlt,
    HiOutlineStatusOnline,
    HiOutlineDownload,
    HiOutlineUpload,
    HiOutlineMinusCircle,
} from "react-icons/hi"

interface OutboundTestResultDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    entry: OutboundTestEntry | null
    outboundTag: string
    testing: boolean
    /** Freedom outbounds are probed directly, which has no throughput mode. */
    speedtestSupported: boolean
    onRetest: (speedtest: boolean) => void
}

const STATUS_LABELS: Record<OutboundTestStatus, string> = {
    passed: "Passed",
    "semi-passed": "Partially passed",
    failed: "Failed",
    timeout: "Timed out",
    broken: "Broken",
    not_applicable: "Not applicable",
}

function statusVariant(status: OutboundTestStatus): "success" | "warning" | "danger" | "secondary" {
    switch (status) {
        case "passed":
            return "success"
        case "semi-passed":
            return "warning"
        case "not_applicable":
            return "secondary"
        default:
            return "danger"
    }
}

/** One label/value row, skipped entirely when there is no value to show. */
function DetailRow({
    label,
    icon: Icon,
    children,
}: {
    label: string
    icon?: React.ComponentType<{ className?: string }>
    children: React.ReactNode
}) {
    return (
        <div className="flex items-center justify-between py-2 px-3 rounded-lg bg-muted/50">
            <span className="text-sm text-muted-foreground flex items-center gap-1.5">
                {Icon && <Icon className="w-4 h-4" />}
                {label}
            </span>
            {children}
        </div>
    )
}

export function OutboundTestResultDialog({
    open,
    onOpenChange,
    entry,
    outboundTag,
    testing,
    speedtestSupported,
    onRetest,
}: OutboundTestResultDialogProps) {
    if (!entry) return null

    const { result } = entry
    const status: OutboundTestStatus = result.status ?? (result.success ? "passed" : "failed")
    const isNA = status === "not_applicable"

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-sm">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        {isNA ? (
                            <HiOutlineMinusCircle className="w-5 h-5 text-muted-foreground" />
                        ) : result.success ? (
                            <HiOutlineCheckCircle className="w-5 h-5 text-emerald-500" />
                        ) : (
                            <HiOutlineXCircle className="w-5 h-5 text-red-500" />
                        )}
                        Test Result
                    </DialogTitle>
                    <DialogDescription>
                        Outbound: <span className="font-mono font-medium text-foreground">{outboundTag}</span>
                        <span className="text-muted-foreground"> · {formatRelativeTime(entry.tested_at)}</span>
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-3">
                    <DetailRow label="Status">
                        <Badge variant={statusVariant(status)}>{STATUS_LABELS[status]}</Badge>
                    </DetailRow>

                    {result.latency_ms > 0 && (
                        <DetailRow label="Latency" icon={HiOutlineClock}>
                            <span className={cn(
                                "font-mono text-sm font-semibold",
                                result.latency_ms < 300 ? "text-emerald-500" :
                                result.latency_ms < 800 ? "text-amber-500" : "text-red-500"
                            )}>
                                {result.latency_ms} ms
                            </span>
                        </DetailRow>
                    )}

                    {!!result.ttfb_ms && result.ttfb_ms > 0 && (
                        <DetailRow label="Time to first byte">
                            <span className="font-mono text-sm">{result.ttfb_ms} ms</span>
                        </DetailRow>
                    )}

                    {!!result.connect_time_ms && result.connect_time_ms > 0 && (
                        <DetailRow label="Connect time">
                            <span className="font-mono text-sm">{result.connect_time_ms} ms</span>
                        </DetailRow>
                    )}

                    {!!result.status_code && result.status_code > 0 && (
                        <DetailRow label="HTTP Status" icon={HiOutlineStatusOnline}>
                            <span className={cn(
                                "font-mono text-sm font-medium",
                                result.status_code >= 200 && result.status_code < 300 ? "text-emerald-500" : "text-amber-500"
                            )}>
                                {result.status_code}
                            </span>
                        </DetailRow>
                    )}

                    {result.ip && (
                        <DetailRow label="Exit IP" icon={HiOutlineGlobeAlt}>
                            <span className="font-mono text-sm">{result.ip}</span>
                        </DetailRow>
                    )}

                    {result.country && (
                        <DetailRow label="Exit country">
                            <span className="text-sm">
                                {countryFlag(result.country)} {result.country}
                            </span>
                        </DetailRow>
                    )}

                    {!!result.download_mbps && result.download_mbps > 0 && (
                        <DetailRow label="Download" icon={HiOutlineDownload}>
                            <span className="font-mono text-sm">{result.download_mbps.toFixed(1)} Mbps</span>
                        </DetailRow>
                    )}

                    {!!result.upload_mbps && result.upload_mbps > 0 && (
                        <DetailRow label="Upload" icon={HiOutlineUpload}>
                            <span className="font-mono text-sm">{result.upload_mbps.toFixed(1)} Mbps</span>
                        </DetailRow>
                    )}

                    {result.message && (
                        <div className="py-2 px-3 rounded-lg bg-muted/50">
                            <span className="text-sm text-muted-foreground break-words">{result.message}</span>
                        </div>
                    )}

                    {result.error && (
                        <div className="py-2 px-3 rounded-lg bg-red-500/10 border border-red-500/20">
                            <span className="text-sm text-red-500 break-all">{result.error}</span>
                        </div>
                    )}

                    {!isNA && (
                        <div className="flex gap-2 pt-1">
                            <Button
                                variant="outline"
                                size="sm"
                                className="flex-1"
                                disabled={testing}
                                onClick={() => onRetest(false)}
                            >
                                {testing ? "Testing…" : "Re-test"}
                            </Button>
                            {speedtestSupported && (
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="flex-1"
                                    disabled={testing}
                                    onClick={() => onRetest(true)}
                                >
                                    With speedtest
                                </Button>
                            )}
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    )
}
