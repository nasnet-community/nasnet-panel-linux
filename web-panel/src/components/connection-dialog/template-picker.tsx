import { useEffect, useMemo, useRef, useState } from "react"
import { Check, Search, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { FormSection } from "./section"

export interface TemplateItem {
    id: string
    name: string
    /** Stack tokens shown on the card and matched by search: "xhttp", "reality", "vision". */
    tokens: string[]
    description?: string
    group?: string
}

export interface TemplateGroup {
    id: string
    label: string
}

interface TemplatePickerProps {
    items: TemplateItem[]
    appliedId: string | null
    onApply: (id: string) => void
    /** How many cards the ungrouped, unfiltered view shows; the rest are reachable by search. */
    visibleCount?: number
    /** When set, the unfiltered view shows every item under these headings instead of the first N. */
    groups?: TemplateGroup[]
    /** Appended to the "N more" hint, e.g. "from XTLS/Xray-examples". */
    sourceHint?: string
    /** Example search terms shown in the hint. */
    searchExamples?: string[]
}

function Tok({ children }: { children: string }) {
    return (
        <span className="rounded-[5px] bg-accent px-1.5 py-px font-mono text-[11px] leading-4 text-text-secondary">
            {children}
        </span>
    )
}

function TemplateCard({ item, selected, onClick }: { item: TemplateItem; selected: boolean; onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            title={item.description}
            className={cn(
                "flex min-h-[58px] flex-col justify-center gap-1.5 rounded-lg border p-3 text-left transition-colors",
                selected
                    ? "border-primary bg-primary/[0.06] ring-1 ring-primary/30"
                    : "hover:bg-accent/50",
            )}
        >
            <span className="flex items-center gap-1.5 text-[13px] font-medium leading-[18px] [text-wrap:pretty]">
                {selected && <Check className="h-3.5 w-3.5 shrink-0 text-primary" />}
                {item.name}
            </span>
            <span className="flex flex-wrap items-center gap-1">
                {item.tokens.map((t) => <Tok key={t}>{t}</Tok>)}
            </span>
        </button>
    )
}

function matches(item: TemplateItem, q: string): boolean {
    const hay = [item.name, item.description ?? "", item.group ?? "", ...item.tokens].join(" ").toLowerCase()
    return q.split(/\s+/).filter(Boolean).every((word) => hay.includes(word))
}

// Compact template chooser: one row of cards, a search box that reveals the
// rest, and a collapsed "Started from …" row once a template is applied so
// the actual form fields sit above the fold.
export function TemplatePicker({
    items,
    appliedId,
    onApply,
    visibleCount = 8,
    groups,
    sourceHint,
    searchExamples = ["reality", "grpc", "tcp", "fallbacks"],
}: TemplatePickerProps) {
    const [query, setQuery] = useState("")
    const [expanded, setExpanded] = useState(false)
    const inputRef = useRef<HTMLInputElement>(null)

    // "/" focuses the search unless the user is already typing somewhere.
    useEffect(() => {
        const onKey = (e: globalThis.KeyboardEvent) => {
            if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return
            const t = e.target as HTMLElement | null
            if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return
            const el = inputRef.current
            if (!el) return
            e.preventDefault()
            el.focus()
        }
        document.addEventListener("keydown", onKey)
        return () => document.removeEventListener("keydown", onKey)
    }, [])

    const q = query.trim().toLowerCase()
    const applied = appliedId ? items.find((i) => i.id === appliedId) ?? null : null

    const filtered = useMemo(() => (q ? items.filter((i) => matches(i, q)) : items), [items, q])

    const apply = (id: string) => {
        onApply(id)
        setQuery("")
        setExpanded(false)
    }

    if (applied && !expanded && !q) {
        return (
            <div className="flex items-center gap-3 rounded-lg border bg-card py-2.5 pl-3.5 pr-3">
                <Check className="h-4 w-4 shrink-0 text-status-success" />
                <span className="text-xs text-text-tertiary">Started from</span>
                <span className="text-[13px] font-medium leading-[18px]">{applied.name}</span>
                <span className="ml-1 hidden items-center gap-1 sm:flex">
                    {applied.tokens.map((t) => <Tok key={t}>{t}</Tok>)}
                </span>
                <Button type="button" variant="ghost" size="sm" className="ml-auto" onClick={() => setExpanded(true)}>
                    Change template
                </Button>
            </div>
        )
    }

    const search = (
        <div className="relative w-full sm:w-60">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <input
                ref={inputRef}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === "Escape") {
                        e.preventDefault()
                        if (query) setQuery("")
                        else e.currentTarget.blur()
                    }
                }}
                placeholder={`Search ${items.length} templates`}
                aria-label="Search templates"
                className="h-8 w-full rounded-md border border-input bg-input/30 pl-8 pr-8 text-[13px] shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
            />
            {query ? (
                <button
                    type="button"
                    aria-label="Clear search"
                    onClick={() => setQuery("")}
                    className="absolute right-1.5 top-1/2 grid h-5 w-5 -translate-y-1/2 place-items-center rounded text-text-tertiary hover:text-foreground"
                >
                    <X className="h-3.5 w-3.5" />
                </button>
            ) : (
                <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded border bg-card px-1 font-mono text-[11px] leading-4 text-text-tertiary">/</kbd>
            )}
        </div>
    )

    const grid = (list: TemplateItem[]) => (
        <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
            {list.map((item) => (
                <TemplateCard key={item.id} item={item} selected={item.id === appliedId} onClick={() => apply(item.id)} />
            ))}
        </div>
    )

    let bodyContent: React.ReactNode
    let hint: React.ReactNode = null

    if (q) {
        bodyContent = filtered.length > 0
            ? grid(filtered)
            : <p className="py-3 text-sm text-muted-foreground">No templates match “{query.trim()}”.</p>
        hint = <>{filtered.length} of {items.length} · matches name and stack tokens. <span className="font-mono">Esc</span> clears.</>
    } else if (groups && groups.length > 0) {
        bodyContent = (
            <div className="flex flex-col gap-3">
                {groups.map((g) => {
                    const list = items.filter((i) => i.group === g.id)
                    if (list.length === 0) return null
                    return (
                        <div key={g.id} className="flex flex-col gap-2">
                            <span className="text-xs font-medium text-muted-foreground">{g.label}</span>
                            {grid(list)}
                        </div>
                    )
                })}
            </div>
        )
    } else {
        const shown = items.slice(0, visibleCount)
        const rest = items.length - shown.length
        bodyContent = grid(shown)
        if (rest > 0) {
            hint = (
                <>
                    {rest} more{sourceHint ? ` ${sourceHint}` : ""}. Type to search:{" "}
                    {searchExamples.map((ex, i) => (
                        <span key={ex}>
                            {i > 0 && ", "}
                            <button type="button" className="font-mono hover:text-foreground" onClick={() => { setQuery(ex); inputRef.current?.focus() }}>{ex}</button>
                        </span>
                    ))}…
                </>
            )
        }
    }

    return (
        <FormSection title="Start from a template" right={search}>
            {bodyContent}
            {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
        </FormSection>
    )
}
