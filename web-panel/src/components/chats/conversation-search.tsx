import { useState, useEffect, useRef } from "react"
import { Search, X, Loader2 } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { useSearchMessages } from "@/lib/queries/use-chat"
import { cn } from "@/lib/utils"
import { format } from "date-fns"

interface Props {
    subscriptionId: number
    onJump: (messageId: number) => void
}

export function ConversationSearch({ subscriptionId, onJump }: Props) {
    const [open, setOpen] = useState(false)
    const [q, setQ] = useState("")
    const [debounced, setDebounced] = useState("")
    const inputRef = useRef<HTMLInputElement>(null)

    useEffect(() => {
        const t = setTimeout(() => setDebounced(q), 250)
        return () => clearTimeout(t)
    }, [q])

    useEffect(() => { if (open) inputRef.current?.focus() }, [open])

    const { data, isFetching } = useSearchMessages(subscriptionId, debounced)
    const hits = data?.hits ?? []

    if (!open) {
        return (
            <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                onClick={() => setOpen(true)}
                aria-label="Search in conversation"
                title="Search in conversation"
            >
                <Search className="h-4 w-4" />
            </Button>
        )
    }

    return (
        <div className="border-b border-border bg-background shrink-0 w-full">
            <div className="flex items-center gap-2 p-2">
                <div className="relative flex-1">
                    <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                    <Input
                        ref={inputRef}
                        value={q}
                        onChange={(e) => setQ(e.target.value)}
                        placeholder="Search in this conversation..."
                        className="pl-9 h-8"
                        aria-label="Search messages"
                    />
                    {isFetching && (
                        <Loader2 className="absolute right-2.5 top-1/2 -translate-y-1/2 h-4 w-4 animate-spin text-muted-foreground" />
                    )}
                </div>
                <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8"
                    onClick={() => { setOpen(false); setQ("") }}
                    aria-label="Close search"
                >
                    <X className="h-4 w-4" />
                </Button>
            </div>
            {debounced.length >= 2 && (
                <div className="max-h-64 overflow-y-auto border-t border-border" role="listbox">
                    {hits.length === 0 && !isFetching && (
                        <div className="p-3 text-xs text-muted-foreground text-center">No matches</div>
                    )}
                    {hits.map((h) => (
                        <button
                            key={h.id}
                            onClick={() => { onJump(h.id); setOpen(false) }}
                            role="option"
                            aria-selected={false}
                            className={cn(
                                "w-full text-left px-3 py-2 text-xs border-b border-border/50",
                                "hover:bg-muted/60 transition-colors flex flex-col gap-0.5",
                            )}
                        >
                            <span className="text-muted-foreground">
                                {h.sender_type === "admin" ? "Admin" : "User"} · {format(new Date(h.created_at), "MMM d, h:mm a")}
                            </span>
                            <span className="line-clamp-2">{h.content}</span>
                        </button>
                    ))}
                </div>
            )}
        </div>
    )
}
