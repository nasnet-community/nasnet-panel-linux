import { useState, useMemo, useEffect } from "react"
import { Calendar as CalendarIcon, Infinity as InfinityIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { Calendar } from "@/components/ui/calendar"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { format } from "date-fns"
import { cn, formatBytes } from "@/lib/utils"
import { BANDWIDTH_OPTIONS } from "@/lib/types"
import type { Subscription } from "@/lib/types"
import type { SubscriptionDerived } from "@/lib/subscription-derived"
import { SectionHeader } from "./section-header"

interface LimitsSectionProps {
    subscription: Subscription
    derived: SubscriptionDerived
    onExtend: (days: number) => void
    onSetExpiry: (endDateISO: string | null, unlimited?: boolean) => void
    onSetDataLimit: (limitGb: number | null) => void
    onSetBandwidth: (limitMbps: number | null) => void
    onSetMaxDevices: (maxDevices: number) => void
    mutationsPending: {
        extend: boolean
        setExpiry: boolean
        setDataLimit: boolean
        setBandwidth: boolean
        setMaxDevices: boolean
    }
}

/**
 * Converts a user-picked date to an RFC3339 timestamp at end-of-day in the
 * admin's local timezone. Prevents the "expires 12h early" bug caused by
 * sending a bare YYYY-MM-DD that the server interprets as UTC midnight.
 */
function endOfDayLocalISO(d: Date): string {
    // date-fns `xxx` token emits e.g. "+09:00"; "XXX" emits "Z" for UTC.
    return format(new Date(d.getFullYear(), d.getMonth(), d.getDate(), 23, 59, 59), "yyyy-MM-dd'T'HH:mm:ssxxx")
}

export function LimitsSection({
    subscription,
    derived,
    onExtend,
    onSetExpiry,
    onSetDataLimit,
    onSetBandwidth,
    onSetMaxDevices,
    mutationsPending,
}: LimitsSectionProps) {
    const [expiry, setExpiry] = useState<Date | undefined>(
        derived.effectiveEndDate ? new Date(derived.effectiveEndDate) : undefined,
    )
    const [dataLimitGb, setDataLimitGb] = useState<string>(
        subscription.is_data_limit_custom && subscription.custom_data_limit != null
            ? (subscription.custom_data_limit / 1_073_741_824).toString()
            : "",
    )
    const [bwLimit, setBwLimit] = useState<string>(String(derived.effectiveBandwidth))
    // sub.max_devices: 0 = inherit plan, >0 = explicit per-sub override.
    const [maxDevices, setMaxDevices] = useState<string>(
        subscription.max_devices && subscription.max_devices > 0 ? String(subscription.max_devices) : "",
    )

    // Resync when server values change (e.g. user reopens the sheet for a different sub)
    useEffect(() => {
        setExpiry(derived.effectiveEndDate ? new Date(derived.effectiveEndDate) : undefined)
        setDataLimitGb(
            subscription.is_data_limit_custom && subscription.custom_data_limit != null
                ? (subscription.custom_data_limit / 1_073_741_824).toString()
                : "",
        )
        setBwLimit(String(derived.effectiveBandwidth))
        setMaxDevices(
            subscription.max_devices && subscription.max_devices > 0 ? String(subscription.max_devices) : "",
        )
    }, [subscription.id, subscription.is_data_limit_custom, subscription.custom_data_limit, subscription.max_devices, derived.effectiveBandwidth, derived.effectiveEndDate])

    const today = useMemo(() => new Date(), [])
    const calendarModifiers = useMemo(() => {
        if (!expiry) return {}
        const now = new Date()
        now.setHours(0, 0, 0, 0)
        const exp = new Date(expiry)
        exp.setHours(0, 0, 0, 0)
        const activeRange: Date[] = []
        if (exp > now) {
            const maxDays = 365
            const cursor = new Date(now)
            cursor.setDate(cursor.getDate() + 1)
            let count = 0
            while (cursor < exp && count < maxDays) {
                activeRange.push(new Date(cursor))
                cursor.setDate(cursor.getDate() + 1)
                count++
            }
        }
        return { expiry: exp, activeRange }
    }, [expiry])

    const currentDataLimitGb = derived.effectiveDataLimit > 0 ? derived.effectiveDataLimit / 1_073_741_824 : 0
    const dataLimitDraftNum = dataLimitGb ? parseFloat(dataLimitGb) : null
    const dataLimitDirty = dataLimitDraftNum != null && Number.isFinite(dataLimitDraftNum) && dataLimitDraftNum !== currentDataLimitGb

    return (
        <div className="space-y-4">
            {/* Extend Duration — simplified to two paths (quick buttons + calendar) */}
            <div className="space-y-2">
                <SectionHeader tone="default">Expiration</SectionHeader>
                <div className="flex gap-2 flex-wrap">
                    {[7, 30, 90].map((d) => (
                        <Button
                            key={d}
                            variant="outline"
                            size="sm"
                            className="h-7 text-xs"
                            onClick={() => onExtend(d)}
                            disabled={mutationsPending.extend}
                        >
                            +{d} days
                        </Button>
                    ))}
                </div>
                <div className="flex gap-2 items-center">
                    <Label className="text-xs text-muted-foreground whitespace-nowrap">Set date</Label>
                    <Popover>
                        <PopoverTrigger asChild>
                            <Button
                                variant="outline"
                                size="sm"
                                className={cn(
                                    "flex-1 h-7 text-xs justify-start font-normal",
                                    !expiry && "text-muted-foreground",
                                )}
                            >
                                <CalendarIcon className="w-3 h-3 mr-1.5" />
                                {expiry ? format(expiry, "MMM d, yyyy") : "Pick a date"}
                            </Button>
                        </PopoverTrigger>
                        <PopoverContent className="w-auto p-0" align="start">
                            {!derived.isUnlimitedExpiry && expiry && (
                                <div className="flex items-center justify-between gap-3 px-3 pt-3 pb-1 text-xs">
                                    <div className="flex flex-col items-start">
                                        <span className="text-muted-foreground">Today</span>
                                        <span className="font-semibold text-foreground">{format(today, "MMM d")}</span>
                                    </div>
                                    <div
                                        className={cn(
                                            "px-2.5 py-0.5 rounded-full font-bold text-[11px]",
                                            derived.daysRemaining > 7
                                                ? "bg-emerald-500/15 text-emerald-500"
                                                : derived.daysRemaining > 3
                                                    ? "bg-amber-500/15 text-amber-500"
                                                    : "bg-red-500/15 text-red-500",
                                        )}
                                    >
                                        {Math.max(0, derived.daysRemaining)}{" "}
                                        {derived.daysRemaining === 1 ? "day" : "days"} left
                                    </div>
                                    <div className="flex flex-col items-end">
                                        <span className="text-muted-foreground">Expires</span>
                                        <span className="font-semibold text-foreground">{format(expiry, "MMM d")}</span>
                                    </div>
                                </div>
                            )}
                            {derived.isUnlimitedExpiry && (
                                <div className="flex items-center justify-center gap-2 px-3 pt-3 pb-1 text-xs">
                                    <InfinityIcon className="w-3.5 h-3.5 text-muted-foreground" />
                                    <span className="text-muted-foreground font-medium">No expiry set</span>
                                </div>
                            )}
                            <Calendar
                                mode="single"
                                selected={expiry}
                                onSelect={setExpiry}
                                defaultMonth={expiry}
                                today={today}
                                modifiers={calendarModifiers}
                                modifiersClassNames={{
                                    expiry: "!bg-red-500/20 !text-red-400 !font-bold rounded-md",
                                    activeRange: "!bg-blue-500/10 !text-blue-300 rounded-sm",
                                }}
                                classNames={{
                                    today: "ring-2 ring-blue-500 rounded-md text-blue-400 font-semibold",
                                }}
                            />
                        </PopoverContent>
                    </Popover>
                    <Button
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() =>
                            onSetExpiry(expiry ? endOfDayLocalISO(expiry) : null)
                        }
                        disabled={mutationsPending.setExpiry || !expiry}
                    >
                        Set
                    </Button>
                    <Button
                        variant={derived.isUnlimitedExpiry ? "secondary" : "outline"}
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() => onSetExpiry(null, true)}
                        disabled={mutationsPending.setExpiry || derived.isUnlimitedExpiry}
                        aria-label="Set unlimited expiry"
                        title="Set unlimited"
                    >
                        <InfinityIcon className="w-3.5 h-3.5" />
                    </Button>
                </div>
            </div>

            <Separator />

            {/* Data Limit Override */}
            <div className="space-y-1.5">
                <SectionHeader tone="default">Data Limit</SectionHeader>
                <div className="flex gap-2 items-center">
                    <Input
                        type="number"
                        step="0.1"
                        min="0"
                        placeholder={formatBytes(subscription.data_limit)}
                        value={dataLimitGb}
                        onChange={(e) => setDataLimitGb(e.target.value)}
                        className={cn(
                            "flex-1 h-7 text-sm",
                            dataLimitDirty && "border-amber-500/70 bg-amber-500/10",
                        )}
                        aria-label="Custom data limit in GB"
                    />
                    <span className="text-xs text-muted-foreground">GB</span>
                    <Button
                        size="sm"
                        className="h-7"
                        onClick={() => {
                            if (!dataLimitGb) {
                                onSetDataLimit(null)
                                return
                            }
                            const val = parseFloat(dataLimitGb)
                            if (!Number.isFinite(val) || val < 0) return
                            onSetDataLimit(val)
                        }}
                        disabled={mutationsPending.setDataLimit}
                    >
                        Apply
                    </Button>
                </div>
                {dataLimitDraftNum != null && Number.isFinite(dataLimitDraftNum) && dataLimitDraftNum !== currentDataLimitGb && (
                    <p className="text-[10px]">
                        <span className="text-muted-foreground">
                            {currentDataLimitGb > 0 ? `${currentDataLimitGb} GB` : "Unlimited"}
                        </span>
                        <span className="text-muted-foreground mx-1">→</span>
                        <span className="text-foreground font-medium">
                            {dataLimitDraftNum === 0 ? "Unlimited" : `${dataLimitDraftNum} GB`}
                        </span>
                        {currentDataLimitGb > 0 && dataLimitDraftNum > 0 && (
                            <span className={cn(
                                "ml-1 font-medium",
                                dataLimitDraftNum - currentDataLimitGb > 0 ? "text-emerald-500" : "text-red-500",
                            )}>
                                ({dataLimitDraftNum - currentDataLimitGb > 0 ? "+" : ""}
                                {(dataLimitDraftNum - currentDataLimitGb).toFixed(1)} GB)
                            </span>
                        )}
                    </p>
                )}
                <p className="text-[10px] text-muted-foreground">
                    Empty = plan default. 0 = unlimited.
                    {subscription.is_data_limit_custom && (
                        <span className="text-amber-500 ml-1">(custom override active)</span>
                    )}
                </p>
            </div>

            {/*  Speed limit feature - disabled
            <Separator />

            <div className="space-y-1.5">
                <SectionHeader tone="default">Speed Limit</SectionHeader>
                <div className="flex gap-2 items-center">
                    <select
                        value={bwLimit}
                        onChange={(e) => setBwLimit(e.target.value)}
                        className="flex-1 h-7 rounded-md border border-input bg-background px-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        aria-label="Speed limit"
                    >
                        {BANDWIDTH_OPTIONS.map((opt) => (
                            <option key={opt.value} value={opt.value}>{opt.label}</option>
                        ))}
                    </select>
                    <Button
                        size="sm"
                        className="h-7"
                        onClick={() => {
                            const val = parseInt(bwLimit)
                            onSetBandwidth(Number.isFinite(val) ? val : null)
                        }}
                        disabled={mutationsPending.setBandwidth}
                    >
                        Apply
                    </Button>
                    <button
                        type="button"
                        onClick={() => onSetBandwidth(null)}
                        disabled={mutationsPending.setBandwidth}
                        className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                        title="Reset"
                    >
                        Reset
                    </button>
                </div>
                <p className="text-[10px] text-muted-foreground">
                    0 = unlimited.
                    {subscription.is_bandwidth_custom && (
                        <span className="text-amber-500 ml-1">(custom override active)</span>
                    )}
                </p>
            </div>
            */}

            <Separator />

            {/* Device Limit Override (WireGuard) */}
            <div className="space-y-1.5">
                <SectionHeader tone="default">Device Limit</SectionHeader>
                <div className="flex gap-2 items-center">
                    <Input
                        type="number"
                        step="1"
                        min="0"
                        placeholder="0 = unlimited"
                        value={maxDevices}
                        onChange={(e) => setMaxDevices(e.target.value)}
                        className="flex-1 h-7 text-sm"
                        aria-label="Device limit for this subscription"
                    />
                    <span className="text-xs text-muted-foreground">devices</span>
                    <Button
                        size="sm"
                        className="h-7"
                        onClick={() => {
                            if (!maxDevices) {
                                onSetMaxDevices(0)
                                return
                            }
                            const val = parseInt(maxDevices)
                            if (!Number.isFinite(val) || val < 0) return
                            onSetMaxDevices(val)
                        }}
                        disabled={mutationsPending.setMaxDevices}
                    >
                        Apply
                    </Button>
                    <button
                        type="button"
                        onClick={() => {
                            setMaxDevices("")
                            onSetMaxDevices(0)
                        }}
                        disabled={mutationsPending.setMaxDevices}
                        className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                        title="Reset"
                    >
                        Reset
                    </button>
                </div>
                <p className="text-[10px] text-muted-foreground">
                    Empty or 0 = unlimited. Caps
                    concurrent WireGuard devices for this subscription only.
                    {subscription.max_devices != null && subscription.max_devices > 0 && (
                        <span className="text-amber-500 ml-1">(override active)</span>
                    )}
                </p>
            </div>
        </div>
    )
}
