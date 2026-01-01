import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from "@/components/ui/dialog"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import type { OutboundTestResult } from "@/lib/types"
import { HiOutlineCheckCircle, HiOutlineXCircle, HiOutlineClock, HiOutlineGlobeAlt, HiOutlineStatusOnline } from "react-icons/hi"

interface OutboundTestResultDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    result: OutboundTestResult | null
    outboundTag: string
}

export function OutboundTestResultDialog({
    open,
    onOpenChange,
    result,
    outboundTag,
}: OutboundTestResultDialogProps) {
    if (!result) return null

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-sm">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        {result.success ? (
                            <HiOutlineCheckCircle className="w-5 h-5 text-emerald-500" />
                        ) : (
                            <HiOutlineXCircle className="w-5 h-5 text-red-500" />
                        )}
                        Test Result
                    </DialogTitle>
                    <DialogDescription>
                        Outbound: <span className="font-mono font-medium text-foreground">{outboundTag}</span>
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-3">
                    {/* Status */}
                    <div className="flex items-center justify-between py-2 px-3 rounded-lg bg-muted/50">
                        <span className="text-sm text-muted-foreground">Status</span>
                        <Badge variant={result.success ? "success" : "danger"}>
                            {result.success ? "Success" : "Failed"}
                        </Badge>
                    </div>

                    {/* Latency */}
                    <div className="flex items-center justify-between py-2 px-3 rounded-lg bg-muted/50">
                        <span className="text-sm text-muted-foreground flex items-center gap-1.5">
                            <HiOutlineClock className="w-4 h-4" />
                            Latency
                        </span>
                        <span className={cn(
                            "font-mono text-sm font-semibold",
                            result.latency_ms > 0 && result.latency_ms < 300 ? "text-emerald-500" :
                            result.latency_ms >= 300 && result.latency_ms < 800 ? "text-amber-500" :
                            result.latency_ms >= 800 ? "text-red-500" : "text-muted-foreground"
                        )}>
                            {result.latency_ms > 0 ? `${result.latency_ms} ms` : "—"}
                        </span>
                    </div>

                    {/* HTTP Status */}
                    {result.status_code > 0 && (
                        <div className="flex items-center justify-between py-2 px-3 rounded-lg bg-muted/50">
                            <span className="text-sm text-muted-foreground flex items-center gap-1.5">
                                <HiOutlineStatusOnline className="w-4 h-4" />
                                HTTP Status
                            </span>
                            <span className={cn(
                                "font-mono text-sm font-medium",
                                result.status_code >= 200 && result.status_code < 300 ? "text-emerald-500" : "text-amber-500"
                            )}>
                                {result.status_code}
                            </span>
                        </div>
                    )}

                    {/* IP */}
                    {result.ip && (
                        <div className="flex items-center justify-between py-2 px-3 rounded-lg bg-muted/50">
                            <span className="text-sm text-muted-foreground flex items-center gap-1.5">
                                <HiOutlineGlobeAlt className="w-4 h-4" />
                                IP Address
                            </span>
                            <span className="font-mono text-sm">{result.ip}</span>
                        </div>
                    )}

                    {/* Country */}
                    {result.country && (
                        <div className="flex items-center justify-between py-2 px-3 rounded-lg bg-muted/50">
                            <span className="text-sm text-muted-foreground">Country</span>
                            <span className="text-sm">{result.country}</span>
                        </div>
                    )}

                    {/* Message */}
                    {result.message && (
                        <div className="flex items-center justify-between py-2 px-3 rounded-lg bg-muted/50">
                            <span className="text-sm text-muted-foreground">Message</span>
                            <span className="text-sm text-right max-w-[180px] truncate">{result.message}</span>
                        </div>
                    )}

                    {/* Error */}
                    {result.error && (
                        <div className="py-2 px-3 rounded-lg bg-red-500/10 border border-red-500/20">
                            <span className="text-sm text-red-500 break-all">{result.error}</span>
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    )
}
