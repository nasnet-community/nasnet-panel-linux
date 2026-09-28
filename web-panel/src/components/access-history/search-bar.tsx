import type { ReactNode, RefObject } from "react"
import { Search, X } from "lucide-react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import type { AccessHistoryMatch, AccessHistorySearchKind } from "@/lib/types"
import { FieldLabel, Kbd, Segmented } from "./bits"

const KINDS: { v: AccessHistorySearchKind; label: string; on: string }[] = [
    { v: "domain", label: "Accepted", on: "border-emerald-500/40 bg-emerald-500/12 text-emerald-700 dark:text-emerald-300" },
    { v: "rejected_domain", label: "Rejected", on: "border-red-500/40 bg-red-500/12 text-red-700 dark:text-red-300" },
    { v: "source_ip", label: "Source IP", on: "border-sky-500/40 bg-sky-500/12 text-sky-700 dark:text-sky-300" },
]

export const MATCH_OPTIONS = [
    { value: "contains" as const, label: "Contains", title: "Finds the text anywhere in a name" },
    { value: "domain" as const, label: "Domain", title: "Finds the name and its subdomains; IPs must match exactly" },
]

interface Props {
    inputRef: RefObject<HTMLInputElement | null>
    value: string
    onChange: (v: string) => void
    placeholder: string
    kinds: AccessHistorySearchKind[]
    onKinds: (k: AccessHistorySearchKind[]) => void
    match: AccessHistoryMatch
    onMatch: (m: AccessHistoryMatch) => void
    /** Phone-only filters trigger, shown in place of the rail. */
    filtersButton?: ReactNode
}

export function SearchBar({ inputRef, value, onChange, placeholder, kinds, onKinds, match, onMatch, filtersButton }: Props) {
    const toggle = (k: AccessHistorySearchKind) => onKinds(kinds.includes(k) ? kinds.filter(x => x !== k) : [...kinds, k])
    return (
        <Card className="gap-3 p-3 sm:p-4">
            <div className="relative">
                <label htmlFor="ah-search" className="sr-only">Search a domain, IP or part of a name</label>
                <Search className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <input
                    ref={inputRef}
                    id="ah-search"
                    value={value}
                    onChange={e => onChange(e.target.value)}
                    placeholder={placeholder}
                    autoComplete="off"
                    spellCheck={false}
                    className="h-11 w-full rounded-lg border border-input bg-transparent pl-10 pr-12 text-base shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:h-[42px] sm:text-[15px] dark:bg-input/30"
                />
                {value ? (
                    <button
                        type="button"
                        aria-label="Clear search"
                        onClick={() => { onChange(""); inputRef.current?.focus() }}
                        className="absolute right-2 top-1/2 flex size-7 -translate-y-1/2 items-center justify-center rounded-md bg-muted text-muted-foreground hover:text-foreground"
                    >
                        <X className="size-3.5" />
                    </button>
                ) : (
                    <span className="pointer-events-none absolute right-3 top-1/2 hidden -translate-y-1/2 sm:block"><Kbd>/</Kbd></span>
                )}
            </div>
            <div className="flex flex-wrap items-center gap-x-2.5 gap-y-2">
                {filtersButton}
                <FieldLabel className="hidden sm:inline">Kinds</FieldLabel>
                <div className="flex gap-1 max-sm:order-last max-sm:w-full">
                    {KINDS.map(k => {
                        const on = kinds.includes(k.v)
                        return (
                            <button
                                key={k.v}
                                type="button"
                                aria-pressed={on}
                                onClick={() => toggle(k.v)}
                                className={cn(
                                    "h-9 rounded-md border px-2.5 text-[13px] font-medium transition-colors sm:h-7",
                                    on ? k.on : "border-border bg-transparent text-muted-foreground hover:border-foreground/30 hover:text-foreground",
                                )}
                            >
                                {k.label}
                            </button>
                        )
                    })}
                </div>
                <span className="mx-1 hidden h-5 w-px bg-border sm:block" aria-hidden />
                <FieldLabel className="hidden sm:inline">Match</FieldLabel>
                <Segmented ariaLabel="Match" value={match} onChange={onMatch} options={MATCH_OPTIONS} className="max-sm:ml-auto" />
            </div>
        </Card>
    )
}
