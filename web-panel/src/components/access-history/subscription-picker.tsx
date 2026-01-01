import { useEffect, useMemo, useState } from "react"
import { Search, X } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { useSubscriptions } from "@/lib/queries/use-subscriptions"
import { subscriptionLabel } from "@/lib/subscription-label"

export interface SubscriptionPickerValue {
    id: number
    label: string
}

interface Props {
    value: SubscriptionPickerValue[]
    onChange: (v: SubscriptionPickerValue[]) => void
    className?: string
    placeholder?: string
}

export function SubscriptionPicker({ value, onChange, className, placeholder }: Props) {
    const [raw, setRaw] = useState("")
    const [debounced, setDebounced] = useState("")
    const [open, setOpen] = useState(false)

    useEffect(() => {
        const id = window.setTimeout(() => setDebounced(raw.trim()), 250)
        return () => window.clearTimeout(id)
    }, [raw])

    const { data } = useSubscriptions({
        page: 1,
        perPage: 20,
        search: debounced || undefined,
    })

    const selectedIds = useMemo(() => new Set(value.map(v => v.id)), [value])

    const options = useMemo(() => {
        const list = data ?? []
        return list
            .filter(s => !selectedIds.has(s.id))
            .map(s => ({
                id: s.id,
                label: subscriptionLabel(s) || `Sub #${s.id}`,
            }))
    }, [data, selectedIds])

    const add = (opt: SubscriptionPickerValue) => {
        onChange([...value, opt])
        setRaw("")
        setOpen(false)
    }
    const remove = (id: number) => onChange(value.filter(v => v.id !== id))

    return (
        <div className={cn("space-y-2", className)}>
            <div className="relative">
                <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
                <Input
                    value={raw}
                    onChange={e => { setRaw(e.target.value); setOpen(true) }}
                    onFocus={() => setOpen(true)}
                    onBlur={() => window.setTimeout(() => setOpen(false), 150)}
                    placeholder={placeholder ?? "Search subscriptions…"}
                    className="pl-8 h-8 text-xs"
                />
                {open && options.length > 0 && (
                    <div className="absolute z-30 mt-1 w-full rounded-md border bg-popover shadow-lg max-h-60 overflow-auto">
                        {options.map(opt => (
                            <button
                                key={opt.id}
                                type="button"
                                onMouseDown={e => { e.preventDefault(); add(opt) }}
                                className="w-full text-left px-2.5 py-2 text-xs hover:bg-accent flex items-center gap-2 transition-colors"
                            >
                                <span className="text-muted-foreground font-mono">#{opt.id}</span>
                                <span className="truncate">{opt.label}</span>
                            </button>
                        ))}
                    </div>
                )}
            </div>
            {value.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                    {value.map(v => (
                        <Badge key={v.id} variant="secondary" className="gap-1.5 pr-1 text-xs">
                            <span className="text-muted-foreground font-mono">#{v.id}</span>
                            <span className="truncate max-w-[140px]">{v.label}</span>
                            <button
                                type="button"
                                onClick={() => remove(v.id)}
                                className="hover:text-foreground text-muted-foreground transition-colors rounded-sm hover:bg-foreground/10 p-0.5"
                                aria-label={`Remove ${v.label}`}
                            >
                                <X className="w-3 h-3" />
                            </button>
                        </Badge>
                    ))}
                </div>
            )}
        </div>
    )
}
