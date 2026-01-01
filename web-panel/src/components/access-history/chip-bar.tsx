import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { cn } from "@/lib/utils"
import type { AccessHistorySearchKind } from "@/lib/types"

export interface ChipBarValue {
    kinds: AccessHistorySearchKind[]
    includeIPs: boolean
    limit: number
}

const KIND_BUTTONS: {
    v: AccessHistorySearchKind
    label: string
    activeClass: string
}[] = [
    {
        v: "domain",
        label: "Accepted",
        activeClass: "bg-emerald-500/15 text-emerald-300 border-emerald-500/40",
    },
    {
        v: "rejected_domain",
        label: "Rejected",
        activeClass: "bg-red-500/15 text-red-300 border-red-500/40",
    },
    {
        v: "source_ip",
        label: "Source IP",
        activeClass: "bg-sky-500/15 text-sky-300 border-sky-500/40",
    },
]

interface Props {
    value: ChipBarValue
    onChange: (v: ChipBarValue) => void
    className?: string
}

export function ChipBar({ value, onChange, className }: Props) {
    // Source-IP kind is gated server-side by include_ips. We keep a single
    // chip per kind here and derive include_ips from kinds.includes("source_ip"),
    // so the UI has one toggle per concept.
    const toggleKind = (k: AccessHistorySearchKind) => {
        const nextKinds = value.kinds.includes(k)
            ? value.kinds.filter(x => x !== k)
            : [...value.kinds, k]
        onChange({
            ...value,
            kinds: nextKinds,
            includeIPs: nextKinds.includes("source_ip"),
        })
    }

    return (
        <div className={cn("flex flex-wrap items-center gap-3", className)}>
            <div className="flex items-center gap-2">
                <Label className="text-xs uppercase tracking-wider font-semibold text-muted-foreground">
                    Kinds
                </Label>
                <div className="flex items-center gap-1">
                    {KIND_BUTTONS.map(b => {
                        const active = value.kinds.includes(b.v)
                        return (
                            <button
                                key={b.v}
                                type="button"
                                aria-pressed={active}
                                onClick={() => toggleKind(b.v)}
                                className={cn(
                                    "px-2.5 py-1 rounded-md text-xs font-medium border transition-colors",
                                    active
                                        ? b.activeClass
                                        : "bg-background text-muted-foreground border-border hover:text-foreground hover:border-foreground/30",
                                )}
                            >
                                {b.label}
                            </button>
                        )
                    })}
                </div>
            </div>

            <div className="h-5 w-px bg-border" aria-hidden />

            <div className="flex items-center gap-1.5">
                <Label className="text-xs uppercase tracking-wider font-semibold text-muted-foreground">
                    Limit
                </Label>
                <Input
                    type="number"
                    min={50}
                    max={2000}
                    step={50}
                    value={value.limit}
                    onChange={e => {
                        const n = Number(e.target.value)
                        if (Number.isFinite(n) && n > 0) onChange({ ...value, limit: n })
                    }}
                    className="h-8 w-20 text-xs font-mono tabular-nums"
                />
            </div>
        </div>
    )
}
