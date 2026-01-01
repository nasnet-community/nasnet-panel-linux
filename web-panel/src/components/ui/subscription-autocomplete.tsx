import { useState, useEffect, useRef, useMemo, useCallback } from "react"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Search, Loader2 } from "lucide-react"
import { listSubscriptions } from "@/lib/admin-api"
import { cn } from "@/lib/utils"

interface Suggestion {
    email: string
    label: string
    username: string
    source: "api" | "local"
}

interface SubscriptionAutocompleteProps {
    value: string
    onChange: (value: string) => void
    /** Called when user picks a suggestion — bypasses debounce */
    onSelect?: (email: string) => void
    /** Emails already known from loaded data (shown instantly) */
    localEmails?: string[]
    placeholder?: string
    className?: string
}

export function SubscriptionAutocomplete({
    value,
    onChange,
    onSelect,
    localEmails = [],
    placeholder = "Filter subscription...",
    className,
}: SubscriptionAutocompleteProps) {
    const [open, setOpen] = useState(false)
    const [apiResults, setApiResults] = useState<Suggestion[]>([])
    const [loading, setLoading] = useState(false)
    const [activeIndex, setActiveIndex] = useState(-1)
    const containerRef = useRef<HTMLDivElement>(null)
    const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined)
    const inputRef = useRef<HTMLInputElement>(null)
    // Track the last selected email to avoid re-searching after selection
    const justSelectedRef = useRef<string | null>(null)

    // Merge local + API suggestions, deduplicate by email
    const suggestions = useMemo(() => {
        const query = value.toLowerCase().trim()
        if (!query) return []

        const map = new Map<string, Suggestion>()

        // Local emails: filter client-side by email match
        for (const email of localEmails) {
            if (email && email.toLowerCase().includes(query)) {
                map.set(email, { email, label: "", username: "", source: "local" })
            }
        }

        // API results: already matched server-side (by email, label, username, first_name)
        // so include ALL of them — don't re-filter by email
        for (const s of apiResults) {
            if (s.email) {
                map.set(s.email, s)
            }
        }

        // Remove exact match from suggestions
        if (map.size === 1 && map.has(value)) return []

        return [...map.values()]
            .sort((a, b) => {
                // Prioritize starts-with matches on email
                const aStarts = a.email.toLowerCase().startsWith(query) ? 0 : 1
                const bStarts = b.email.toLowerCase().startsWith(query) ? 0 : 1
                if (aStarts !== bStarts) return aStarts - bStarts
                return a.email.localeCompare(b.email)
            })
            .slice(0, 15)
    }, [localEmails, apiResults, value])

    // Search API on input change — skip if value was just set by selection
    useEffect(() => {
        if (debounceRef.current) clearTimeout(debounceRef.current)

        // Skip search if this value came from selecting a suggestion
        if (justSelectedRef.current === value) {
            justSelectedRef.current = null
            return
        }

        if (!value || value.length < 2) {
            setApiResults([])
            return
        }
        debounceRef.current = setTimeout(async () => {
            setLoading(true)
            try {
                const res = await listSubscriptions({ search: value, per_page: 10 })
                if (res.success && res.data) {
                    const results: Suggestion[] = res.data
                        .filter(s => s.config_email)
                        .map(s => ({
                            email: s.config_email!,
                            label: s.label || "",
                            username: s.user?.username || s.user?.first_name || "",
                            source: "api" as const,
                        }))
                    setApiResults(results)
                }
            } finally {
                setLoading(false)
            }
        }, 300)
        return () => {
            if (debounceRef.current) clearTimeout(debounceRef.current)
        }
    }, [value])

    // Click outside to close
    useEffect(() => {
        function handleClickOutside(e: MouseEvent) {
            if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
                setOpen(false)
            }
        }
        document.addEventListener("mousedown", handleClickOutside)
        return () => document.removeEventListener("mousedown", handleClickOutside)
    }, [])

    // Reset active index when suggestions change
    useEffect(() => {
        setActiveIndex(-1)
    }, [suggestions.length])

    const selectEmail = useCallback((email: string) => {
        justSelectedRef.current = email
        if (onSelect) {
            onSelect(email)
        } else {
            onChange(email)
        }
        setOpen(false)
    }, [onChange, onSelect])

    const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
        if (!open || suggestions.length === 0) return

        switch (e.key) {
            case "ArrowDown":
                e.preventDefault()
                setActiveIndex(prev => (prev + 1) % suggestions.length)
                break
            case "ArrowUp":
                e.preventDefault()
                setActiveIndex(prev => (prev <= 0 ? suggestions.length - 1 : prev - 1))
                break
            case "Enter":
                e.preventDefault()
                if (activeIndex >= 0 && activeIndex < suggestions.length) {
                    selectEmail(suggestions[activeIndex].email)
                }
                break
            case "Escape":
                setOpen(false)
                break
        }
    }, [open, suggestions, activeIndex, selectEmail])

    const showDropdown = open && value.length >= 1 && suggestions.length > 0

    // Build the display context for a suggestion
    const renderContext = (s: Suggestion) => {
        const parts: string[] = []
        if (s.username) parts.push(s.username)
        if (s.label && s.label !== s.username) parts.push(s.label)
        return parts.join(" · ")
    }

    return (
        <div ref={containerRef} className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
            <Input
                ref={inputRef}
                placeholder={placeholder}
                value={value}
                onChange={e => { onChange(e.target.value); setOpen(true) }}
                onFocus={() => setOpen(true)}
                onKeyDown={handleKeyDown}
                className={cn("pl-8", className)}
                autoComplete="off"
            />
            {loading && (
                <Loader2 className="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 animate-spin text-muted-foreground" />
            )}

            {showDropdown && (
                <div className="absolute z-50 top-full left-0 min-w-[280px] mt-1 rounded-md border bg-popover shadow-md overflow-hidden">
                    <ScrollArea className="max-h-[220px]">
                        <div className="py-1">
                            {suggestions.map((s, i) => {
                                const query = value.toLowerCase()
                                const emailLower = s.email.toLowerCase()
                                const idx = emailLower.indexOf(query)
                                const context = renderContext(s)

                                return (
                                    <button
                                        key={s.email}
                                        type="button"
                                        className={cn(
                                            "w-full flex flex-col gap-0.5 px-3 py-1.5 text-left transition-colors",
                                            i === activeIndex
                                                ? "bg-accent text-accent-foreground"
                                                : "hover:bg-muted/50"
                                        )}
                                        onMouseEnter={() => setActiveIndex(i)}
                                        onMouseDown={e => {
                                            e.preventDefault() // prevent blur before click
                                            selectEmail(s.email)
                                        }}
                                    >
                                        <span className="font-mono text-xs truncate">
                                            {idx >= 0 ? (
                                                <>
                                                    {s.email.slice(0, idx)}
                                                    <span className="text-primary font-semibold">
                                                        {s.email.slice(idx, idx + query.length)}
                                                    </span>
                                                    {s.email.slice(idx + query.length)}
                                                </>
                                            ) : (
                                                s.email
                                            )}
                                        </span>
                                        {context && (
                                            <span className="text-[10px] text-muted-foreground truncate">
                                                {context}
                                            </span>
                                        )}
                                    </button>
                                )
                            })}
                        </div>
                    </ScrollArea>
                </div>
            )}
        </div>
    )
}
