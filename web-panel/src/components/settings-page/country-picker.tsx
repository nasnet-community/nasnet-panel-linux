import { useMemo, useState } from "react"
import { Command } from "cmdk"
import { Ban, Check, ChevronDown, Globe } from "lucide-react"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { CountryFlag, countryCodes, countryName } from "@/components/ui/country-flag"
import { NO_FLAG } from "@/lib/country"
import { cn } from "@/lib/utils"

/** A country with its flag; the blank choice is labelled by the caller. */
export function CountryLabel({ code, blank }: { code?: string; blank: string }) {
    if (code === NO_FLAG) return <span className="flex items-center gap-2 text-muted-foreground"><Ban className="size-3.5" aria-hidden />No flag</span>
    return code
        ? <span className="flex min-w-0 items-center gap-2"><CountryFlag code={code} /><span className="truncate">{countryName(code)}</span></span>
        : <span className="flex items-center gap-2 text-muted-foreground"><Globe className="size-3.5" aria-hidden />{blank}</span>
}

export function CountryPicker({ id, value, onChange, blank, noFlag, dirty, label = "Country" }: {
    id?: string
    value: string
    onChange: (code: string) => void
    /** what the empty choice means */
    blank: string
    /** offers "No flag", which stays blank for good */
    noFlag?: boolean
    dirty?: boolean
    label?: string
}) {
    const [open, setOpen] = useState(false)
    const codes = useMemo(() => countryCodes(), [])
    const pick = (code: string) => { onChange(code); setOpen(false) }
    const current = value.toUpperCase()

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
                <button
                    id={id}
                    type="button"
                    aria-label={label}
                    className={cn(
                        "flex h-9 w-full max-w-[360px] items-center gap-2 rounded-md border border-input bg-transparent px-3 text-left text-sm shadow-xs transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                        dirty && "border-amber-500/60",
                    )}
                >
                    <span className="min-w-0 flex-1"><CountryLabel code={current} blank={blank} /></span>
                    <ChevronDown className={cn("size-4 shrink-0 text-muted-foreground transition-transform", open && "rotate-180")} aria-hidden />
                </button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-(--radix-popover-trigger-width) min-w-[260px] p-0">
                <Command loop>
                    <Command.Input placeholder="Search countries" className="h-10 w-full border-b border-border bg-transparent px-3 text-sm outline-none placeholder:text-muted-foreground" />
                    <Command.List className="max-h-64 overflow-y-auto p-1">
                        <Command.Empty className="py-6 text-center text-xs text-muted-foreground">No match</Command.Empty>
                        <Option value={`${blank} auto`} selected={!current} onSelect={() => pick("")}>
                            <CountryLabel blank={blank} />
                        </Option>
                        {noFlag && (
                            <Option value="No flag none" selected={current === NO_FLAG} onSelect={() => pick(NO_FLAG)}>
                                <CountryLabel code={NO_FLAG} blank={blank} />
                            </Option>
                        )}
                        {codes.map(code => (
                            <Option key={code} value={`${countryName(code)} ${code}`} selected={code === current} onSelect={() => pick(code)}>
                                <CountryLabel code={code} blank={blank} />
                            </Option>
                        ))}
                    </Command.List>
                </Command>
            </PopoverContent>
        </Popover>
    )
}

function Option({ value, selected, onSelect, children }: { value: string; selected: boolean; onSelect: () => void; children: React.ReactNode }) {
    return (
        <Command.Item
            value={value}
            onSelect={onSelect}
            className="flex h-9 cursor-pointer items-center gap-2 rounded-sm px-2 text-sm data-[selected=true]:bg-muted"
        >
            <span className="min-w-0 flex-1">{children}</span>
            <Check className={cn("size-4", selected ? "opacity-100" : "opacity-0")} aria-hidden />
        </Command.Item>
    )
}
