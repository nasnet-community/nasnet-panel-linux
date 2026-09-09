import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { cn, formatBytes, formatRelativeTime } from "@/lib/utils"
import { HiOutlinePlus, HiOutlineStatusOnline, HiOutlineDotsVertical } from "react-icons/hi"
import { Stat } from "./section-stat"

export interface OutboundSummary {
    total: number
    disabled: number
    /** Enabled outbounds nothing routes to, the default excluded. */
    unused: number
    trafficBytes: number
    /** Newest last_tested_at across the outbounds, or null when none was tested. */
    lastTestedAt: string | null
}

interface OutboundSectionHeaderProps {
    summary: OutboundSummary
    testAllProgress: { done: number; total: number } | null
    canTestAll: boolean
    onTestAll: () => void
    onAdd: () => void
}

// Same shape as InboundSectionHeader. No "Last change" line here on purpose:
// traffic persistence bumps updated_at, so it would always say "just now".
export function OutboundSectionHeader({
    summary, testAllProgress, canTestAll, onTestAll, onAdd,
}: OutboundSectionHeaderProps) {
    const testLabel = testAllProgress ? `Testing ${testAllProgress.done}/${testAllProgress.total}…` : "Test All"
    const testDisabled = !canTestAll || !!testAllProgress

    return (
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0 space-y-1.5">
                <h3 className="text-lg font-semibold leading-tight">Outbounds</h3>
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 text-sm">
                    <Stat value={String(summary.total)} label="outbounds" />
                    {summary.disabled > 0 && <Stat value={String(summary.disabled)} label="disabled" tone="muted" />}
                    {summary.unused > 0 && <Stat value={String(summary.unused)} label="unused" tone="muted" />}
                    {summary.trafficBytes > 0 && <Stat value={formatBytes(summary.trafficBytes)} label="all-time" />}
                    <span className="text-muted-foreground whitespace-nowrap">
                        {summary.lastTestedAt ? `tested ${formatRelativeTime(summary.lastTestedAt)}` : "never tested"}
                    </span>
                </div>
            </div>

            <div className="flex items-start gap-2 shrink-0">
                <div className="hidden lg:flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={onTestAll} disabled={testDisabled}>
                        <HiOutlineStatusOnline className={cn("w-4 h-4 mr-2", testAllProgress && "animate-pulse")} />
                        {testLabel}
                    </Button>
                    <Button size="sm" onClick={onAdd}>
                        <HiOutlinePlus className="w-4 h-4 mr-2" />
                        Add Outbound
                    </Button>
                </div>

                {/* Narrow: Add stays out front, Test All folds into the kebab. */}
                <div className="flex lg:hidden items-center gap-2">
                    <Button size="sm" onClick={onAdd}>
                        <HiOutlinePlus className="w-4 h-4 mr-2" />
                        Add Outbound
                    </Button>
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button variant="outline" size="icon-sm" aria-label="More outbound actions">
                                <HiOutlineDotsVertical className="w-4 h-4" />
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={onTestAll} disabled={testDisabled}>
                                <HiOutlineStatusOnline className="w-4 h-4 mr-2" />
                                {testLabel}
                            </DropdownMenuItem>
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>
            </div>
        </div>
    )
}
