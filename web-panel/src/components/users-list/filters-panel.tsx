import type { ReactNode } from "react"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"
import type { JoinedFilter, SeenFilter, TelegramFilter, UsersFilters } from "./use-users-view"

interface Option<T extends string> { value: T | undefined; label: string }

function Segmented<T extends string>({ label, value, options, onChange }: { label: string; value: T | undefined; options: Option<T>[]; onChange: (v: T | undefined) => void }) {
    return (
        <fieldset>
            <legend className="mb-1.5 text-[12.5px] font-medium text-muted-foreground">{label}</legend>
            <div className="grid rounded-lg border border-border bg-background/60 p-0.5" style={{ gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))` }}>
                {options.map((o) => (
                    <button
                        key={o.label}
                        type="button"
                        aria-pressed={o.value === value}
                        onClick={() => onChange(o.value)}
                        className={cn("h-7 rounded-md text-[13px] transition-colors", o.value === value ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground")}
                    >
                        {o.label}
                    </button>
                ))}
            </div>
        </fieldset>
    )
}

function Chips<T extends string>({ label, value, options, onChange }: { label: string; value: T | undefined; options: Option<T>[]; onChange: (v: T | undefined) => void }) {
    return (
        <fieldset>
            <legend className="mb-1.5 text-[12.5px] font-medium text-muted-foreground">{label}</legend>
            <div className="flex flex-wrap gap-1.5">
                {options.map((o) => (
                    <button
                        key={o.label}
                        type="button"
                        aria-pressed={o.value === value}
                        onClick={() => onChange(o.value)}
                        className={cn(
                            "flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-[13px] transition-colors",
                            o.value === value ? "border-foreground/40 bg-muted font-medium text-foreground" : "border-border text-text-secondary hover:text-foreground",
                        )}
                    >
                        {o.value === "online" && <span aria-hidden className="size-1.5 rounded-full bg-status-success" />}
                        {o.label}
                    </button>
                ))}
            </div>
        </fieldset>
    )
}

const TELEGRAM: Option<TelegramFilter>[] = [{ value: undefined, label: "Any" }, { value: "linked", label: "Linked" }, { value: "unlinked", label: "Not linked" }]
const SEEN: Option<SeenFilter>[] = [
    { value: undefined, label: "Any" }, { value: "online", label: "Online now" }, { value: "24h", label: "24h" }, { value: "7d", label: "7d" },
    { value: "30d", label: "30d" }, { value: "over30d", label: "Over 30d" }, { value: "never", label: "Never" },
]
const JOINED: Option<JoinedFilter>[] = [
    { value: undefined, label: "Any" }, { value: "7d", label: "7d" }, { value: "30d", label: "30d" }, { value: "90d", label: "90d" }, { value: "custom", label: "Custom…" },
]

export function FiltersPanel({ value, onChange, onClear, footer }: { value: UsersFilters; onChange: (f: UsersFilters) => void; onClear: () => void; footer?: ReactNode }) {
    return (
        <div className="flex flex-col gap-4">
            <Segmented label="Telegram" value={value.telegram} options={TELEGRAM} onChange={(telegram) => onChange({ telegram })} />
            <Chips label="Last seen" value={value.seen} options={SEEN} onChange={(seen) => onChange({ seen })} />
            <Chips label="Joined" value={value.joined} options={JOINED} onChange={(joined) => onChange({ joined, from: undefined, to: undefined })} />
            {value.joined === "custom" && (
                <div className="grid grid-cols-2 gap-2">
                    <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                        From
                        <Input type="date" value={value.from ?? ""} onChange={(e) => onChange({ from: e.target.value || undefined })} className="h-8" />
                    </label>
                    <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                        To
                        <Input type="date" value={value.to ?? ""} onChange={(e) => onChange({ to: e.target.value || undefined })} className="h-8" />
                    </label>
                </div>
            )}
            <label className="flex items-center gap-2.5 text-[13.5px]">
                <Checkbox checked={!!value.admin} onCheckedChange={(c) => onChange({ admin: c === true || undefined })} />
                Admins only
            </label>
            <div className="-mx-4 flex items-center justify-between border-t border-border/60 px-4 pt-3">
                <button type="button" onClick={onClear} className="text-[13px] text-text-secondary hover:text-foreground">Clear all</button>
                {footer}
            </div>
        </div>
    )
}
