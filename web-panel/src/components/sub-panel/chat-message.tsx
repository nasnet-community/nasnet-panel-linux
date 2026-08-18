import { memo } from "react"
import ReactMarkdown from "react-markdown"
import { Pin, Pencil, Check, CheckCheck } from "lucide-react"
import { cn } from "@/lib/utils"
import { format, isToday, isYesterday } from "date-fns"
import { QuotedSnippet } from "@/components/chats/quoted-snippet"

export interface ReplyTo {
    senderLabel: string
    content: string
    onJump?: () => void
}

interface ChatMessageProps {
    content: string
    senderType: "user" | "admin"
    createdAt: string
    isOptimistic?: boolean
    isPinned?: boolean
    editedAt?: string | null
    /** Caller controls alignment. Defaults to senderType === "user" so the public widget keeps its prior layout. */
    isMine?: boolean
    isDeleted?: boolean
    /** Read receipt for own messages: false = single check; true = double check. */
    isRead?: boolean
    /** Optional quoted message rendered above content. */
    replyTo?: ReplyTo | null
}

function formatMessageTime(dateStr: string): string {
    const date = new Date(dateStr)
    if (isToday(date)) return format(date, "h:mm a")
    if (isYesterday(date)) return "Yesterday " + format(date, "h:mm a")
    return format(date, "MMM d, h:mm a")
}

const SAFE_PROTOCOLS = /^(https?|mailto):/i

const markdownComponents = {
    a: ({ href, children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement>) => {
        const isSafe = href && SAFE_PROTOCOLS.test(href)
        if (!isSafe) return <span>{children}</span>
        return (
            <a href={href} target="_blank" rel="noopener noreferrer" {...props}>
                {children}
            </a>
        )
    },
}

export const ChatMessageBubble = memo(function ChatMessageBubble({
    content,
    senderType,
    createdAt,
    isOptimistic,
    isPinned,
    editedAt,
    isMine,
    isDeleted,
    isRead,
    replyTo,
}: ChatMessageProps) {
    const mine = isMine ?? senderType === "user"

    if (isDeleted) {
        return (
            <div className={cn("flex w-full", mine ? "justify-end" : "justify-start")}>
                <div className="max-w-[80%] rounded-2xl px-4 py-2 text-xs italic text-muted-foreground bg-muted/40">
                    Message deleted
                </div>
            </div>
        )
    }

    return (
        <div className={cn("flex w-full", mine ? "justify-end" : "justify-start")}>
            <div
                className={cn(
                    "max-w-[80%] rounded-2xl px-4 py-2 text-sm",
                    mine ? "bg-primary text-primary-foreground rounded-br-md" : "bg-muted rounded-bl-md",
                    isOptimistic && "opacity-70",
                )}
            >
                {!mine && (
                    <div className="text-xs font-medium text-muted-foreground mb-1">
                        {senderType === "admin" ? "Admin" : "User"}
                    </div>
                )}
                {replyTo && (
                    <QuotedSnippet
                        senderLabel={replyTo.senderLabel}
                        content={replyTo.content}
                        onJump={replyTo.onJump}
                        isMine={mine}
                    />
                )}
                <div className="prose prose-sm dark:prose-invert max-w-none break-words [&>p]:m-0 [&>p:not(:last-child)]:mb-1">
                    <ReactMarkdown
                        allowedElements={["p", "strong", "em", "code", "pre", "a", "ul", "ol", "li", "br"]}
                        unwrapDisallowed
                        components={markdownComponents}
                    >
                        {content}
                    </ReactMarkdown>
                </div>
                <div
                    className={cn(
                        "text-xs mt-1 flex items-center gap-1",
                        mine ? "text-primary-foreground/70" : "text-muted-foreground",
                    )}
                >
                    {isPinned && <Pin className="h-2.5 w-2.5" />}
                    {editedAt && <Pencil className="h-2.5 w-2.5" aria-label="Edited" />}
                    {isOptimistic ? "Sending..." : formatMessageTime(createdAt)}
                    {mine && !isOptimistic && (
                        isRead ? (
                            <CheckCheck className="h-3 w-3" aria-label="Seen" />
                        ) : (
                            <Check className="h-3 w-3" aria-label="Sent" />
                        )
                    )}
                </div>
            </div>
        </div>
    )
})
