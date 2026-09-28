import { useEffect, useRef, useState } from "react"
import { useDebouncedCallback } from "use-debounce"
import { ArrowDown, ArrowUp, Search, SlidersHorizontal, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { isTypingTarget } from "@/hooks/use-global-shortcuts"
import { cn } from "@/lib/utils"
import { FiltersPanel } from "./filters-panel"
import { NATURAL_ORDER, NO_TIME_LEFT, SORT_LABEL, type SortKey, type SortOrder } from "./groups"
import { filterCount, type UsersFilters, type UsersView } from "./use-users-view"

export function SearchBox({ value, onChange, placeholder, className, shortcut = false }: {
    value: string
    onChange: (q: string) => void
    placeholder: string
    className?: string
    shortcut?: boolean
}) {
    const [text, setText] = useState(value)
    const inputRef = useRef<HTMLInputElement>(null)
    const debounced = useDebouncedCallback(onChange, 300)

    // Back/forward rewrites the URL under us; follow it unless the user is typing.
    useEffect(() => {
        if (document.activeElement !== inputRef.current) setText(value)
    }, [value])

    useEffect(() => {
        if (!shortcut) return
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented || isTypingTarget(e.target)) return
            e.preventDefault()
            inputRef.current?.focus()
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [shortcut])

    const clear = () => {
        setText("")
        debounced.cancel()
        onChange("")
        inputRef.current?.focus()
    }

    return (
        <label className={cn("flex h-9 items-center gap-2.5 rounded-lg border border-input bg-card/50 px-3 text-muted-foreground focus-within:border-ring", className)}>
            <Search className="size-4 shrink-0" aria-hidden />
            <input
                ref={inputRef}
                type="search"
                value={text}
                placeholder={placeholder}
                aria-label="Search users"
                onChange={(e) => {
                    setText(e.target.value)
                    debounced(e.target.value)
                }}
                onKeyDown={(e) => {
                    if (e.key === "Escape" && text) {
                        e.preventDefault()
                        clear()
                    }
                }}
                className="min-w-0 flex-1 bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground [&::-webkit-search-cancel-button]:hidden"
            />
            {text ? (
                <button type="button" onClick={clear} aria-label="Clear search" className="flex size-6 items-center justify-center rounded-md bg-muted text-text-secondary hover:text-foreground">
                    <X className="size-3" />
                </button>
            ) : shortcut ? (
                <kbd className="rounded border border-border px-1.5 font-mono text-[11px] text-muted-foreground">/</kbd>
            ) : null}
        </label>
    )
}

const TELEGRAM_LABEL = { linked: "Telegram: linked", unlinked: "Telegram: not linked" }
const SEEN_LABEL = { online: "Last seen: online now", "24h": "Last seen: 24h", "7d": "Last seen: 7d", "30d": "Last seen: 30d", over30d: "Last seen: over 30d", never: "Last seen: never" }
const JOINED_LABEL = { "7d": "Joined: 7d", "30d": "Joined: 30d", "90d": "Joined: 90d", custom: "Joined: custom" }

function activeChips(f: UsersFilters): { key: string; label: string; clear: UsersFilters }[] {
    const chips: { key: string; label: string; clear: UsersFilters }[] = []
    if (f.telegram) chips.push({ key: "telegram", label: TELEGRAM_LABEL[f.telegram], clear: { telegram: undefined } })
    if (f.seen) chips.push({ key: "seen", label: SEEN_LABEL[f.seen], clear: { seen: undefined } })
    if (f.joined) {
        const label = f.joined === "custom" && (f.from || f.to) ? `Joined: ${f.from ?? "…"} to ${f.to ?? "…"}` : JOINED_LABEL[f.joined]
        chips.push({ key: "joined", label, clear: { joined: undefined, from: undefined, to: undefined } })
    }
    if (f.admin) chips.push({ key: "admin", label: "Admins only", clear: { admin: undefined } })
    return chips
}

export function UsersToolbar({ view, onSearch, onFilters, onClearFilters }: {
    view: UsersView
    onSearch: (q: string) => void
    onFilters: (f: UsersFilters) => void
    onClearFilters: () => void
}) {
    const [open, setOpen] = useState(false)
    const count = filterCount(view)
    return (
        <div className="flex flex-wrap items-center gap-2.5">
            <SearchBox value={view.q} onChange={onSearch} shortcut placeholder="Search by name, @username, Telegram ID or config link" className="min-w-[280px] flex-1" />
            <Popover open={open} onOpenChange={setOpen}>
                <PopoverTrigger asChild>
                    <Button variant="outline" size="sm" className={cn("h-9 gap-2", count > 0 && "border-foreground/30 bg-muted")}>
                        <SlidersHorizontal className="size-4" />
                        Filters
                        {count > 0 && <span className="flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-foreground px-1 font-mono text-[11px] text-background">{count}</span>}
                    </Button>
                </PopoverTrigger>
                <PopoverContent align="start" className="w-[380px] p-4">
                    <FiltersPanel value={view} onChange={onFilters} onClear={onClearFilters} footer={<Button size="sm" onClick={() => setOpen(false)}>Done</Button>} />
                </PopoverContent>
            </Popover>
            {activeChips(view).map((chip) => (
                <span key={chip.key} className="inline-flex h-8 items-center gap-1 rounded-lg border border-border bg-card pl-2.5 pr-1 text-[13px]">
                    {chip.label}
                    <button type="button" aria-label={`Remove filter ${chip.label}`} onClick={() => onFilters(chip.clear)} className="flex size-6 items-center justify-center rounded-md text-muted-foreground hover:text-foreground">
                        <X className="size-3" />
                    </button>
                </span>
            ))}
        </div>
    )
}

/** Sort control for the phone's filter sheet. */
export function PhoneSort({ view, onSortTo }: { view: UsersView; onSortTo: (sort: SortKey, order: SortOrder) => void }) {
    const keys = (Object.keys(SORT_LABEL) as SortKey[]).filter((k) => !(k === "time_left" && NO_TIME_LEFT.includes(view.group)))
    return (
        <div className="flex flex-col gap-1.5">
            <span className="text-[12.5px] font-medium text-muted-foreground">Sort</span>
            <div className="flex gap-2">
                <Select value={view.sort} onValueChange={(k) => onSortTo(k as SortKey, NATURAL_ORDER[k as SortKey])}>
                    <SelectTrigger className="h-10 flex-1" aria-label="Sort by"><SelectValue /></SelectTrigger>
                    <SelectContent>
                        {keys.map((k) => <SelectItem key={k} value={k}>{SORT_LABEL[k]}</SelectItem>)}
                    </SelectContent>
                </Select>
                <Button
                    variant="outline"
                    size="icon"
                    className="size-10"
                    onClick={() => onSortTo(view.sort, view.order === "asc" ? "desc" : "asc")}
                    aria-label={view.order === "asc" ? "Ascending; switch to descending" : "Descending; switch to ascending"}
                >
                    {view.order === "asc" ? <ArrowUp className="size-4" /> : <ArrowDown className="size-4" />}
                </Button>
            </div>
        </div>
    )
}
