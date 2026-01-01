import { useState, useEffect } from "react"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"

interface Props {
    open: boolean
    onOpenChange: (v: boolean) => void
    messageId: number
    initialContent: string
    onSave: (args: { messageId: number; content: string }) => void
    isPending?: boolean
}

export function EditMessageDialog({ open, onOpenChange, messageId, initialContent, onSave, isPending }: Props) {
    const [content, setContent] = useState(initialContent)
    useEffect(() => {
        if (open) setContent(initialContent)
    }, [open, initialContent])

    const trimmed = content.trim()
    const unchanged = trimmed === initialContent.trim()

    const handleSave = () => {
        if (!trimmed || unchanged) {
            onOpenChange(false)
            return
        }
        onSave({ messageId, content: trimmed })
    }

    return (
        <ResponsiveDialog
            open={open}
            onOpenChange={onOpenChange}
            title="Edit message"
            saveLabel={isPending ? "Saving..." : "Save"}
            saveDisabled={!trimmed || unchanged || isPending}
            onSave={handleSave}
        >
            <div className="space-y-2">
                <textarea
                    value={content}
                    onChange={(e) => setContent(e.target.value.slice(0, 2000))}
                    className="w-full min-h-[140px] resize-none rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    autoFocus
                />
                {content.length > 1800 && (
                    <div className="text-[10px] text-muted-foreground text-right">{content.length}/2000</div>
                )}
            </div>
        </ResponsiveDialog>
    )
}
