import { X, CornerUpLeft } from "lucide-react"
import type { ChatMessage } from "@/lib/api/chat"

interface Props {
    target: ChatMessage
    onClear: () => void
    selfReactor?: "user" | "admin"
}

export function ReplyPreview({ target, onClear, selfReactor }: Props) {
    const senderIsMe = selfReactor && target.sender_type === selfReactor
    const senderLabel = senderIsMe
        ? "yourself"
        : target.sender_type === "admin"
        ? "Admin"
        : "User"
    return (
        <div className="flex items-start gap-2 px-3 py-2 border-t border-border bg-muted/40">
            <CornerUpLeft className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
            <div className="flex-1 min-w-0">
                <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
                    Replying to {senderLabel}
                </div>
                <div className="text-xs truncate">{target.content}</div>
            </div>
            <button onClick={onClear} className="text-muted-foreground hover:text-foreground" aria-label="Cancel reply">
                <X className="h-4 w-4" />
            </button>
        </div>
    )
}
