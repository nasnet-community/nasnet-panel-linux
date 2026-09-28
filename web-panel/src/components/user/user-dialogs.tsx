import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import type { Subscription } from "@/lib/types"
import { deriveSubscription } from "@/lib/subscription-derived"
import { cn } from "@/lib/utils"
import { FieldLabel } from "@/components/access-history/bits"
import { fmtSize } from "./format"

/** One number field with a confirm button (extend days, device limit, Telegram ID). */
export function NumberDialog({
    open,
    onOpenChange,
    title,
    description,
    label,
    initial,
    suffix,
    min = 0,
    confirmLabel,
    pending,
    onSubmit,
}: {
    open: boolean
    onOpenChange: (open: boolean) => void
    title: string
    description?: string
    label: string
    initial: string
    suffix?: string
    min?: number
    confirmLabel: (value: number) => string
    pending?: boolean
    onSubmit: (value: number) => void
}) {
    const [value, setValue] = useState(initial)
    useEffect(() => {
        if (open) setValue(initial)
    }, [open, initial])
    const n = Number(value)
    const valid = value.trim() !== "" && Number.isFinite(n) && n >= min
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{title}</DialogTitle>
                    {description && <DialogDescription>{description}</DialogDescription>}
                </DialogHeader>
                <form
                    className="space-y-4"
                    onSubmit={(e) => {
                        e.preventDefault()
                        if (valid) onSubmit(n)
                    }}
                >
                    <div className="space-y-2">
                        <Label htmlFor="number-dialog-value">{label}</Label>
                        <div className="relative">
                            <Input id="number-dialog-value" type="number" inputMode="numeric" min={min} value={value} onChange={(e) => setValue(e.target.value)} autoFocus className={cn(suffix && "pr-14")} />
                            {suffix && <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 font-mono text-xs text-muted-foreground">{suffix}</span>}
                        </div>
                    </div>
                    <DialogFooter>
                        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
                        <Button type="submit" disabled={!valid || pending}>{valid ? confirmLabel(n) : confirmLabel(0)}</Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    )
}

const PRESETS = [10, 25, 50, 100]

/** Raises the data limit; an exhausted subscription comes back when there is room. */
export function AddDataDialog({
    subscription,
    open,
    onOpenChange,
    pending,
    onSubmit,
}: {
    subscription: Subscription | null
    open: boolean
    onOpenChange: (open: boolean) => void
    pending?: boolean
    onSubmit: (amountGb: number) => void
}) {
    const [preset, setPreset] = useState<number | null>(50)
    const [custom, setCustom] = useState("")
    useEffect(() => {
        if (open) {
            setPreset(50)
            setCustom("")
        }
    }, [open])
    const d = deriveSubscription(subscription)
    if (!subscription || !d) return null
    const amount = custom.trim() ? Number(custom) : preset ?? 0
    const valid = Number.isFinite(amount) && amount > 0
    const add = valid ? amount * 1024 ** 3 : 0
    const limit = d.effectiveDataLimit
    const newLimit = limit + add
    const left = Math.max(0, limit - subscription.data_used)
    const newLeft = Math.max(0, newLimit - subscription.data_used)
    const reactivates = subscription.status === "traffic_exhausted" && newLimit > subscription.data_used
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Add data</DialogTitle>
                    <DialogDescription>Raises the data limit of #{subscription.id}. Usage so far stays counted.</DialogDescription>
                </DialogHeader>
                <form
                    className="space-y-5"
                    onSubmit={(e) => {
                        e.preventDefault()
                        if (valid) onSubmit(amount)
                    }}
                >
                    <div className="space-y-2">
                        <FieldLabel>Amount</FieldLabel>
                        <div className="grid grid-cols-4 gap-1.5">
                            {PRESETS.map(gb => (
                                <button
                                    key={gb}
                                    type="button"
                                    aria-pressed={!custom && preset === gb}
                                    onClick={() => { setPreset(gb); setCustom("") }}
                                    className={cn(
                                        "h-9 rounded-md border font-mono text-[13px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                                        !custom && preset === gb ? "border-primary bg-primary font-semibold text-primary-foreground" : "border-input bg-background hover:bg-accent",
                                    )}
                                >
                                    {gb} GB
                                </button>
                            ))}
                        </div>
                        <Label htmlFor="add-data-custom" className="pt-1 text-xs font-normal text-muted-foreground">Or a custom amount</Label>
                        <div className="relative">
                            <Input id="add-data-custom" type="number" inputMode="decimal" min={0} step="any" placeholder="GB" value={custom} onChange={(e) => setCustom(e.target.value)} className="pr-12" />
                            <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 font-mono text-xs text-muted-foreground">GB</span>
                        </div>
                    </div>
                    <dl className="grid grid-cols-[96px_minmax(0,1fr)] gap-y-2 rounded-lg bg-muted/40 p-3.5 text-[13px]">
                        <dt className="self-center"><FieldLabel>Limit</FieldLabel></dt>
                        <dd className="font-mono">{fmtSize(limit)} <span className="text-muted-foreground">to</span> {fmtSize(newLimit)}</dd>
                        <dt className="self-center"><FieldLabel>Left</FieldLabel></dt>
                        <dd className="font-mono">{fmtSize(left)} <span className="text-muted-foreground">to</span> {fmtSize(newLeft)}</dd>
                        {reactivates && (
                            <>
                                <dt className="self-center"><FieldLabel>Status</FieldLabel></dt>
                                <dd>Back to <span className="text-emerald-600 dark:text-emerald-400">Active</span></dd>
                            </>
                        )}
                    </dl>
                    <DialogFooter>
                        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
                        <Button type="submit" disabled={!valid || pending}>{valid ? `Add ${+amount.toFixed(2)} GB` : "Add data"}</Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    )
}
