import { useState } from "react"
import * as Popover from "@radix-ui/react-popover"
import { Plus } from "lucide-react"
import { cn } from "@/lib/utils"
import {
    useMessageReactions,
    useAddReaction,
    useRemoveReaction,
    useReplaceReaction,
    type ReactionScope,
} from "@/lib/queries/use-chat"

const PICKER = ["👍", "❤️", "😂", "🎉", "🤔", "😢"]

interface Props {
    messageId: number
    scope: ReactionScope
    selfReactor: "user" | "admin"
    selfAdminId?: number
}

export function ReactionsBar({ messageId, scope, selfReactor, selfAdminId }: Props) {
    const { data: reactions = [] } = useMessageReactions(scope, messageId)
    const add = useAddReaction(scope)
    const remove = useRemoveReaction(scope)
    const replace = useReplaceReaction(scope)
    const [pickerOpen, setPickerOpen] = useState(false)

    // Group counts; mark whether the viewer reacted with that emoji.
    const groups = new Map<string, { count: number; me: boolean }>()
    let myEmoji: string | null = null
    for (const r of reactions) {
        const me =
            r.reactor === selfReactor &&
            (selfAdminId ? r.admin_id === selfAdminId : r.admin_id == null)
        const cur = groups.get(r.emoji) ?? { count: 0, me: false }
        cur.count += 1
        cur.me = cur.me || me
        groups.set(r.emoji, cur)
        if (me) myEmoji = r.emoji
    }

    const onSelectEmoji = (emoji: string) => {
        if (myEmoji && myEmoji !== emoji) {
            replace.mutate({ messageId, oldEmoji: myEmoji, newEmoji: emoji })
        } else if (myEmoji === emoji) {
            remove.mutate({ messageId, emoji })
        } else {
            add.mutate({ messageId, emoji })
        }
        setPickerOpen(false)
    }

    if (groups.size === 0) {
        // Empty: don't reserve layout space. The "React" action in the message
        // toolbar (long-press / right-click / hover) is the entry-point.
        return null
    }

    return (
        <div className="flex flex-wrap gap-1 mt-0.5">
            {[...groups.entries()].map(([emoji, g]) => (
                <button
                    key={emoji}
                    onClick={() => onSelectEmoji(emoji)}
                    className={cn(
                        "px-1.5 py-0.5 rounded-full text-xs border transition-colors",
                        g.me
                            ? "bg-primary/10 border-primary/40"
                            : "bg-muted/60 border-border hover:bg-muted",
                    )}
                    title={g.me ? "Remove your reaction" : "Add reaction"}
                >
                    {emoji} {g.count}
                </button>
            ))}
            <Popover.Root open={pickerOpen} onOpenChange={setPickerOpen}>
                <Popover.Trigger asChild>
                    <button
                        className="px-1.5 py-0.5 rounded-full text-xs border border-dashed border-border text-muted-foreground hover:bg-muted/40 inline-flex items-center"
                        aria-label="Change reaction"
                        title={myEmoji ? "Change your reaction" : "Add reaction"}
                    >
                        <Plus className="h-3 w-3" />
                    </button>
                </Popover.Trigger>
                <Popover.Portal>
                    <Popover.Content sideOffset={4} className="z-50 rounded-md border border-border bg-background p-1 shadow-md flex gap-0.5">
                        {PICKER.map((e) => (
                            <button
                                key={e}
                                className={cn(
                                    "rounded-md px-1.5 py-1 text-base hover:bg-muted",
                                    myEmoji === e && "bg-primary/10",
                                )}
                                onClick={() => onSelectEmoji(e)}
                            >
                                {e}
                            </button>
                        ))}
                    </Popover.Content>
                </Popover.Portal>
            </Popover.Root>
        </div>
    )
}
