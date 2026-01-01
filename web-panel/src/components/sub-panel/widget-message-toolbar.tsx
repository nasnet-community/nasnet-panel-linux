import { useState } from "react"
import { Copy, Pencil, Trash2, Reply, Smile } from "lucide-react"
import { cn } from "@/lib/utils"
import { toast } from "sonner"
import { useEditSubChatMessage, useDeleteSubChatMessage } from "@/lib/queries/use-chat"
import { EditMessageDialog } from "@/components/chats/edit-message-dialog"
import { DeleteMessageDialog } from "@/components/chats/delete-message-dialog"
import { MessageActionsSheet, type MessageAction } from "@/components/chats/message-actions-sheet"
import { useLongPress } from "@/lib/hooks/use-long-press"
import type { ChatMessage } from "@/lib/api/chat"

const FIVE_MIN_MS = 5 * 60_000

interface Props {
    uuid: string
    message: ChatMessage
    isMine: boolean
    onQuote?: (m: ChatMessage) => void
    onReact?: (messageId: number) => void
    children: React.ReactNode
}

export function WidgetMessageToolbar({ uuid, message, isMine, onQuote, onReact, children }: Props) {
    const [editOpen, setEditOpen] = useState(false)
    const [delOpen, setDelOpen] = useState(false)
    const [sheetOpen, setSheetOpen] = useState(false)
    const edit = useEditSubChatMessage(uuid)
    const del = useDeleteSubChatMessage(uuid)

    const persisted = !!message.subscription_id
    const within5 =
        persisted && message.created_at && Date.now() - new Date(message.created_at).getTime() < FIVE_MIN_MS
    const canMutate = isMine && within5

    const actions: MessageAction[] = [
        {
            label: "Copy",
            icon: <Copy className="h-4 w-4" />,
            onSelect: () => {
                navigator.clipboard.writeText(message.content)
                toast.success("Copied")
            },
        },
        ...(onQuote && persisted
            ? [{ label: "Reply", icon: <Reply className="h-4 w-4" />, onSelect: () => onQuote(message) }]
            : []),
        ...(onReact && persisted
            ? [{ label: "React", icon: <Smile className="h-4 w-4" />, onSelect: () => onReact(message.id) }]
            : []),
        ...(canMutate
            ? [
                  { label: "Edit", icon: <Pencil className="h-4 w-4" />, onSelect: () => setEditOpen(true) },
                  {
                      label: "Delete",
                      icon: <Trash2 className="h-4 w-4" />,
                      danger: true,
                      onSelect: () => setDelOpen(true),
                  },
              ]
            : []),
    ]

    const longPress = useLongPress({ onLongPress: () => setSheetOpen(true) })

    return (
        <>
            <div
                {...longPress}
                onContextMenu={(e) => {
                    e.preventDefault()
                    setSheetOpen(true)
                }}
                onDoubleClick={() => setSheetOpen(true)}
                className="group relative"
            >
                {children}
                {/* Desktop hover toolbar */}
                <div
                    className={cn(
                        "hidden sm:flex absolute -top-3 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity",
                        "gap-0.5 rounded-md border border-border bg-background shadow-sm p-0.5",
                        isMine ? "right-2" : "left-2",
                    )}
                    role="toolbar"
                    aria-label="Message actions"
                >
                    <button
                        className="p-1 hover:bg-muted rounded"
                        onClick={() => {
                            navigator.clipboard.writeText(message.content)
                            toast.success("Copied")
                        }}
                        title="Copy"
                        aria-label="Copy"
                    >
                        <Copy className="h-3 w-3" />
                    </button>
                    {onReact && persisted && (
                        <button
                            className="p-1 hover:bg-muted rounded"
                            onClick={() => onReact(message.id)}
                            title="React"
                            aria-label="React"
                        >
                            <Smile className="h-3 w-3" />
                        </button>
                    )}
                    {onQuote && persisted && (
                        <button
                            className="p-1 hover:bg-muted rounded"
                            onClick={() => onQuote(message)}
                            title="Reply"
                            aria-label="Reply"
                        >
                            <Reply className="h-3 w-3" />
                        </button>
                    )}
                    {canMutate && (
                        <>
                            <button
                                className="p-1 hover:bg-muted rounded"
                                onClick={() => setEditOpen(true)}
                                title="Edit"
                                aria-label="Edit"
                            >
                                <Pencil className="h-3 w-3" />
                            </button>
                            <button
                                className="p-1 hover:bg-muted rounded text-destructive"
                                onClick={() => setDelOpen(true)}
                                title="Delete"
                                aria-label="Delete"
                            >
                                <Trash2 className="h-3 w-3" />
                            </button>
                        </>
                    )}
                </div>
            </div>
            <MessageActionsSheet open={sheetOpen} onOpenChange={setSheetOpen} actions={actions} />
            <EditMessageDialog
                open={editOpen}
                onOpenChange={setEditOpen}
                messageId={message.id}
                initialContent={message.content}
                onSave={(args) => edit.mutate(args, { onSuccess: () => setEditOpen(false) })}
                isPending={edit.isPending}
            />
            <DeleteMessageDialog
                open={delOpen}
                onOpenChange={setDelOpen}
                messageId={message.id}
                onConfirm={(id) => del.mutate(id, { onSuccess: () => setDelOpen(false) })}
                isPending={del.isPending}
            />
        </>
    )
}
