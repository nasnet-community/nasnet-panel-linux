import { ResponsiveDialog } from "@/components/ui/responsive-dialog"

interface Props {
    open: boolean
    onOpenChange: (v: boolean) => void
    messageId: number
    onConfirm: (messageId: number) => void
    isPending?: boolean
}

export function DeleteMessageDialog({ open, onOpenChange, messageId, onConfirm, isPending }: Props) {
    return (
        <ResponsiveDialog
            open={open}
            onOpenChange={onOpenChange}
            title="Delete message?"
            description="The message will be removed from the conversation. This cannot be undone."
            saveLabel={isPending ? "Deleting..." : "Delete"}
            saveVariant="destructive"
            saveDisabled={isPending}
            onSave={() => onConfirm(messageId)}
        >
            <div className="text-sm text-muted-foreground">
                Are you sure you want to delete this message?
            </div>
        </ResponsiveDialog>
    )
}
