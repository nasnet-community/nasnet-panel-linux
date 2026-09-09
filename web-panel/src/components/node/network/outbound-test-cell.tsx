import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn, countryFlag, formatRelativeTime } from "@/lib/utils"
import {
    HiOutlineCheckCircle,
    HiOutlineXCircle,
    HiOutlineExclamationCircle,
    HiOutlineStatusOnline,
} from "react-icons/hi"
import { Loader2 } from "lucide-react"
import type { OutboundTestEntry } from "@/lib/types"

interface OutboundTestCellProps {
    entry: OutboundTestEntry | null
    isTesting: boolean
    /** False for blackhole/dns/loopback/http — nothing upstream to probe. */
    canTest: boolean
    /** True while Test All runs; a single test would only queue behind it. */
    disabled: boolean
    protocol: string
    onTest: () => void
    onView: () => void
    align?: "left" | "right"
}

export function latencyTone(ms: number): string {
    if (ms < 300) return "text-emerald-600 dark:text-emerald-400"
    if (ms < 800) return "text-amber-600 dark:text-amber-400"
    return "text-red-600 dark:text-red-400"
}

// One cell for the whole test story: nothing yet, running, or a result you can
// open. The old table spent two columns on it and left both mostly empty.
export function OutboundTestCell({
    entry, isTesting, canTest, disabled, protocol, onTest, onView, align = "right",
}: OutboundTestCellProps) {
    const wrap = cn("flex items-center", align === "right" ? "justify-end" : "justify-start")

    if (!canTest) {
        return (
            <div className={wrap}>
                <Tooltip>
                    <TooltipTrigger asChild>
                        <span className="text-[11px] font-mono text-muted-foreground/50 cursor-help">n/a</span>
                    </TooltipTrigger>
                    <TooltipContent>A {protocol} outbound has no upstream to probe</TooltipContent>
                </Tooltip>
            </div>
        )
    }

    if (isTesting) {
        return (
            <div className={cn(wrap, "gap-1.5 text-[11px] text-muted-foreground")}>
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                Testing…
            </div>
        )
    }

    if (!entry) {
        return (
            <div className={wrap}>
                <Button
                    variant="ghost"
                    size="xs"
                    disabled={disabled}
                    onClick={(e) => { e.stopPropagation(); onTest() }}
                    className="text-muted-foreground hover:text-foreground"
                >
                    <HiOutlineStatusOnline className="w-3.5 h-3.5" />
                    Test
                </Button>
            </div>
        )
    }

    const { result } = entry
    const partial = result.status === "semi-passed"
    const Icon = result.success ? (partial ? HiOutlineExclamationCircle : HiOutlineCheckCircle) : HiOutlineXCircle
    const iconTone = result.success ? (partial ? "text-amber-500" : "text-emerald-500") : "text-red-500"
    const flag = countryFlag(result.country)
    const tip = result.ip ? `Exit IP: ${result.ip}` : result.error || result.message || "No details"

    return (
        <div className={wrap}>
            <Tooltip>
                <TooltipTrigger asChild>
                    <button
                        type="button"
                        onClick={(e) => { e.stopPropagation(); onView() }}
                        aria-label={`Test result: ${result.success ? "passed" : "failed"}, open details`}
                        className={cn(
                            "inline-flex flex-col gap-0.5 rounded-md px-1 -mx-1 hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                            align === "right" ? "items-end" : "items-start",
                        )}
                    >
                        <span className="inline-flex items-center gap-1 font-mono text-xs tabular-nums">
                            <Icon className={cn("w-4 h-4", iconTone)} />
                            {flag && <span className="text-sm leading-none">{flag}</span>}
                            {result.latency_ms > 0 && (
                                <span className={cn("font-medium", latencyTone(result.latency_ms))}>{result.latency_ms}ms</span>
                            )}
                        </span>
                        <span className="text-[10px] text-muted-foreground/70">{formatRelativeTime(entry.tested_at)}</span>
                    </button>
                </TooltipTrigger>
                <TooltipContent>{tip}</TooltipContent>
            </Tooltip>
        </div>
    )
}
