import { useState } from "react"
import { Copy, Check } from "lucide-react"
import { cn, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"

interface CopyableTextProps {
    text: string
    className?: string
    iconSize?: number
    badge?: boolean
}

export function CopyableText({ text, className, iconSize = 12, badge = false }: CopyableTextProps) {
    const [copied, setCopied] = useState(false)

    const handleCopy = async (e: React.MouseEvent) => {
        e.preventDefault()
        e.stopPropagation()

        if (!text) return

        await copyToClipboard(text)
        setCopied(true)

        toast.success("Copied to clipboard", {
            description: text,
            duration: 2000,
        })

        setTimeout(() => setCopied(false), 2000)
    }

    return (
        <button
            onClick={handleCopy}
            className={cn(
                "group/copy flex items-center gap-1.5 transition-all outline-none",
                badge && "bg-muted/50 hover:bg-muted px-2 py-0.5 rounded-md",
                "hover:text-foreground text-left",
                className
            )}
            title="Click to copy"
        >
            <span className="truncate">{text}</span>
            <div className="shrink-0 transition-all duration-300">
                {copied ? (
                    <Check className="text-emerald-500 animate-in zoom-in duration-300" style={{ width: iconSize, height: iconSize }} />
                ) : (
                    <Copy
                        className="text-muted-foreground/30 group-hover/copy:text-muted-foreground/70 group-hover/copy:scale-110 transition-all"
                        style={{ width: iconSize, height: iconSize }}
                    />
                )}
            </div>
        </button>
    )
}
