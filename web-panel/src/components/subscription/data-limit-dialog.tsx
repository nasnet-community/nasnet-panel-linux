import { useEffect, useState, useCallback } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Slider } from "@/components/ui/slider"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { HiOutlineDatabase } from "react-icons/hi"
import { RotateCcw } from "lucide-react"
import type { Subscription } from "@/lib/types"
import { cn, formatBytes } from "@/lib/utils"
import { getUsageBarColor, getUsageTextColor } from "@/lib/constants/usage-thresholds"
import { useSetDataLimit } from "@/lib/queries"

const SLIDER_MAX = 500
const SLIDER_PRESETS = [10, 25, 50, 100, 200]

function bytesToGb(bytes: number): number {
    return bytes / (1024 * 1024 * 1024)
}

export function DataLimitDialog({
    open,
    onOpenChange,
    subscription,
}: {
    open: boolean
    onOpenChange: (open: boolean) => void
    subscription: Subscription | null
}) {
    const [limitValue, setLimitValue] = useState("")
    const [isUnlimited, setIsUnlimited] = useState(false)
    const setLimitMutation = useSetDataLimit()

    useEffect(() => {
        if (open && subscription) {
            const effectiveLimit = subscription.custom_data_limit ?? subscription.data_limit
            if (effectiveLimit === 0) {
                setIsUnlimited(true)
                setLimitValue("")
            } else {
                setIsUnlimited(false)
                const gbValue = bytesToGb(effectiveLimit).toFixed(2)
                setLimitValue(gbValue)
            }
        }
    }, [open, subscription])

    const handleSliderChange = useCallback((values: number[]) => {
        setLimitValue(values[0].toString())
    }, [])

    const handleInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
        setLimitValue(e.target.value)
    }, [])

    if (!subscription) return null

    const planDefaultGb = subscription.data_limit > 0
        ? bytesToGb(subscription.data_limit).toFixed(2)
        : "Unlimited"
    const isCustom = subscription.is_data_limit_custom
    const dataUsedGb = bytesToGb(subscription.data_used)

    // Compute usage percentage relative to the new/current limit
    const effectiveNewLimitGb = isUnlimited
        ? 0
        : limitValue ? parseFloat(limitValue) : 0
    const usagePercent = effectiveNewLimitGb > 0
        ? Math.min((dataUsedGb / effectiveNewLimitGb) * 100, 100)
        : 0

    const sliderValue = limitValue ? Math.min(parseFloat(limitValue) || 0, SLIDER_MAX) : 0

    const handleApply = () => {
        if (isUnlimited) {
            setLimitMutation.mutate(
                { id: subscription.id, limitGb: 0 },
                { onSuccess: () => onOpenChange(false) }
            )
        } else {
            const val = limitValue ? parseFloat(limitValue) : null
            setLimitMutation.mutate(
                { id: subscription.id, limitGb: val },
                { onSuccess: () => onOpenChange(false) }
            )
        }
    }

    const handleReset = () => {
        setLimitMutation.mutate(
            { id: subscription.id, limitGb: null },
            { onSuccess: () => onOpenChange(false) }
        )
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <HiOutlineDatabase className="w-5 h-5 text-primary" />
                        Data Limit
                    </DialogTitle>
                    <DialogDescription>
                        Subscription #{subscription.id}
                        {isCustom && (
                            <span className="ml-2 text-amber-500 text-xs font-medium">
                                (custom override)
                            </span>
                        )}
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4 py-1">
                    {/* Usage progress bar */}
                    <div className="space-y-2">
                        <div className="flex justify-between text-xs text-muted-foreground">
                            <span>{formatBytes(subscription.data_used)} used</span>
                            <span>
                                {isUnlimited || effectiveNewLimitGb === 0
                                    ? "Unlimited"
                                    : `${effectiveNewLimitGb.toFixed(1)} GB limit`}
                            </span>
                        </div>
                        <div className="h-2 bg-muted rounded-full overflow-hidden">
                            <div
                                className={cn(
                                    "h-full rounded-full transition-all duration-300",
                                    isUnlimited ? "bg-muted-foreground/20" : getUsageBarColor(usagePercent)
                                )}
                                style={{ width: isUnlimited ? "3%" : `${Math.max(usagePercent, 1)}%` }}
                            />
                        </div>
                        {!isUnlimited && effectiveNewLimitGb > 0 && (
                            <p className={cn("text-[10px] font-medium", getUsageTextColor(usagePercent))}>
                                {usagePercent.toFixed(1)}% used
                            </p>
                        )}
                    </div>

                    {/* Plan default info */}
                    <div className="flex items-center justify-between text-xs rounded-md bg-muted/50 px-3 py-2">
                        <span className="text-muted-foreground">Plan default</span>
                        <span className="font-medium">
                            {planDefaultGb === "Unlimited" ? planDefaultGb : `${planDefaultGb} GB`}
                        </span>
                    </div>

                    {/* Unlimited toggle */}
                    <div className="flex items-center justify-between">
                        <Label htmlFor="unlimited-toggle" className="text-sm font-medium cursor-pointer">
                            Unlimited
                        </Label>
                        <Switch
                            id="unlimited-toggle"
                            checked={isUnlimited}
                            onCheckedChange={setIsUnlimited}
                        />
                    </div>

                    {/* Slider + input (hidden when unlimited) */}
                    {!isUnlimited && (
                        <div className="space-y-3">
                            <div className="flex items-center gap-3">
                                <Slider
                                    value={[sliderValue]}
                                    onValueChange={handleSliderChange}
                                    max={SLIDER_MAX}
                                    min={1}
                                    step={1}
                                    className="flex-1"
                                />
                                <div className="flex items-center gap-1.5 shrink-0">
                                    <Input
                                        type="number"
                                        step="0.1"
                                        min="0.1"
                                        placeholder="GB"
                                        value={limitValue}
                                        onChange={handleInputChange}
                                        className="w-20 h-8 text-sm text-center"
                                    />
                                    <span className="text-xs text-muted-foreground">GB</span>
                                </div>
                            </div>

                            {/* Quick presets */}
                            <div className="flex flex-wrap gap-1.5">
                                {SLIDER_PRESETS.map((gb) => (
                                    <Button
                                        key={gb}
                                        variant={limitValue === gb.toString() ? "default" : "outline"}
                                        size="sm"
                                        className="h-6 text-xs px-2.5"
                                        onClick={() => setLimitValue(gb.toString())}
                                    >
                                        {gb} GB
                                    </Button>
                                ))}
                            </div>
                        </div>
                    )}
                </div>

                <DialogFooter className="gap-2 sm:gap-0">
                    <button
                        type="button"
                        onClick={handleReset}
                        disabled={setLimitMutation.isPending}
                        className="mr-auto inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
                    >
                        <RotateCcw className="w-3 h-3" />
                        Reset to default
                    </button>
                    <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        size="sm"
                        onClick={handleApply}
                        disabled={setLimitMutation.isPending}
                    >
                        {setLimitMutation.isPending ? "Applying..." : "Apply"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
