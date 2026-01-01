import * as Tabs from "@radix-ui/react-tabs"
import { cn } from "@/lib/utils"

export type InboxTab = "all" | "unread" | "mine" | "pinned"

const TABS: { id: InboxTab; label: string }[] = [
    { id: "all", label: "All" },
    { id: "unread", label: "Unread" },
    { id: "mine", label: "Mine" },
    { id: "pinned", label: "Pinned" },
]

export function InboxTabs({
    value,
    onChange,
    counts,
}: {
    value: InboxTab
    onChange: (t: InboxTab) => void
    counts?: Partial<Record<InboxTab, number>>
}) {
    return (
        <Tabs.Root value={value} onValueChange={(v) => onChange(v as InboxTab)}>
            <Tabs.List
                className="flex border-b border-border bg-background"
                aria-label="Conversation inbox"
            >
                {TABS.map((t) => (
                    <Tabs.Trigger
                        key={t.id}
                        value={t.id}
                        className={cn(
                            "flex-1 px-2 py-2 text-xs font-medium border-b-2 border-transparent",
                            "data-[state=active]:border-primary data-[state=active]:text-foreground",
                            "text-muted-foreground hover:text-foreground transition-colors outline-none",
                            "focus-visible:ring-2 focus-visible:ring-ring",
                        )}
                    >
                        {t.label}
                        {typeof counts?.[t.id] === "number" && counts[t.id]! > 0 && (
                            <span className="ml-1 inline-flex h-4 min-w-4 px-1 rounded-full bg-primary/15 text-[10px] items-center justify-center">
                                {counts[t.id]}
                            </span>
                        )}
                    </Tabs.Trigger>
                ))}
            </Tabs.List>
        </Tabs.Root>
    )
}
