import { CornerUpLeft } from "lucide-react"
import { cn } from "@/lib/utils"

interface Props {
    content: string
    senderLabel: string
    onJump?: () => void
    isMine?: boolean
}

export function QuotedSnippet({ content, senderLabel, onJump, isMine }: Props) {
    const truncated = content.length > 120 ? content.slice(0, 120) + "…" : content
    return (
        <button
            onClick={onJump}
            className={cn(
                "flex items-start gap-2 mb-1.5 rounded-md px-2 py-1 text-left text-xs",
                "border-l-2 w-full",
                isMine
                    ? "bg-primary-foreground/10 border-primary-foreground/40"
                    : "bg-muted-foreground/10 border-muted-foreground/40",
            )}
        >
            <CornerUpLeft className="h-3 w-3 mt-0.5 shrink-0 opacity-70" />
            <span className="flex flex-col min-w-0">
                <span className="font-medium opacity-80">{senderLabel}</span>
                <span className="opacity-90 line-clamp-2 break-words">{truncated}</span>
            </span>
        </button>
    )
}
