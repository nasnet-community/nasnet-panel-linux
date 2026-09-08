import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { cn, formatBytes, formatRelativeTime } from "@/lib/utils"
import {
    HiOutlineDownload,
    HiOutlineUpload,
    HiOutlinePlus,
    HiOutlineDotsVertical,
} from "react-icons/hi"

export interface InboundSummary {
    total: number
    disabled: number
    clients: number
    online: number
    expired: number
    trafficBytes: number
    /** Newest updated_at across the inbounds, or null when there are none. */
    lastChangedAt: string | null
}

interface InboundSectionHeaderProps {
    summary: InboundSummary
    actionLoading: string | null
    onDiscover: () => void
    onSync: () => void
    onAdd: () => void
}

function Stat({ value, label, tone }: { value: string; label: string; tone?: "good" | "bad" | "muted" }) {
    return (
        <span className="inline-flex items-baseline gap-1 whitespace-nowrap">
            <b className={cn(
                "font-semibold tabular-nums",
                tone === "good" && "text-emerald-600 dark:text-emerald-400",
                tone === "bad" && "text-red-600 dark:text-red-400",
                tone === "muted" && "text-muted-foreground",
            )}>
                {value}
            </b>
            <span className="text-muted-foreground">{label}</span>
        </span>
    )
}

export function InboundSectionHeader({
    summary, actionLoading, onDiscover, onSync, onAdd,
}: InboundSectionHeaderProps) {
    return (
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0 space-y-1.5">
                <h3 className="text-lg font-semibold leading-tight">Inbounds</h3>
                {/* What the old subtitle should have said: the numbers, not the noun. */}
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 text-sm">
                    <Stat value={String(summary.total)} label="inbounds" />
                    {summary.disabled > 0 && <Stat value={String(summary.disabled)} label="disabled" tone="muted" />}
                    <Stat value={String(summary.clients)} label="clients" />
                    <Stat value={String(summary.online)} label="online" tone={summary.online > 0 ? "good" : undefined} />
                    {summary.expired > 0 && <Stat value={String(summary.expired)} label="expired" tone="bad" />}
                    {summary.trafficBytes > 0 && <Stat value={formatBytes(summary.trafficBytes)} label="all-time" />}
                </div>
            </div>

            <div className="flex flex-col items-start sm:items-end gap-1.5 shrink-0">
                {/* Named by direction — "Discover" and "Sync" never said which way. */}
                <div className="hidden lg:flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={onDiscover} disabled={actionLoading === "discover"}>
                        <HiOutlineDownload className={cn("w-4 h-4 mr-2", actionLoading === "discover" && "animate-spin")} />
                        Import from Xray
                    </Button>
                    <Button variant="outline" size="sm" onClick={onSync} disabled={actionLoading === "sync"}>
                        <HiOutlineUpload className={cn("w-4 h-4 mr-2", actionLoading === "sync" && "animate-spin")} />
                        Push to Xray
                    </Button>
                    <Button size="sm" onClick={onAdd}>
                        <HiOutlinePlus className="w-4 h-4 mr-2" />
                        Add Inbound
                    </Button>
                </div>

                {/* Narrow: the transfer pair folds away, Add stays out front. */}
                <div className="flex lg:hidden items-center gap-2">
                    <Button size="sm" onClick={onAdd}>
                        <HiOutlinePlus className="w-4 h-4 mr-2" />
                        Add Inbound
                    </Button>
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button variant="outline" size="icon-sm" aria-label="Inbound transfer actions">
                                <HiOutlineDotsVertical className="w-4 h-4" />
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={onDiscover} disabled={actionLoading === "discover"}>
                                <HiOutlineDownload className="w-4 h-4 mr-2" />
                                Import from Xray
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={onSync} disabled={actionLoading === "sync"}>
                                <HiOutlineUpload className="w-4 h-4 mr-2" />
                                Push to Xray
                            </DropdownMenuItem>
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>
                {summary.lastChangedAt && (
                    <p className="text-xs text-muted-foreground">
                        Last change {formatRelativeTime(summary.lastChangedAt)}
                    </p>
                )}
            </div>
        </div>
    )
}
