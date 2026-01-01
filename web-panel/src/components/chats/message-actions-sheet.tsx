import * as Dialog from "@radix-ui/react-dialog"
import { cn } from "@/lib/utils"

export interface MessageAction {
    label: string
    icon: React.ReactNode
    onSelect: () => void
    danger?: boolean
}

interface Props {
    open: boolean
    onOpenChange: (v: boolean) => void
    actions: MessageAction[]
}

export function MessageActionsSheet({ open, onOpenChange, actions }: Props) {
    return (
        <Dialog.Root open={open} onOpenChange={onOpenChange}>
            <Dialog.Portal>
                <Dialog.Overlay className="fixed inset-0 bg-black/40 z-50" />
                <Dialog.Content className="fixed inset-x-0 bottom-0 z-50 rounded-t-2xl bg-background border-t border-border shadow-lg pb-[env(safe-area-inset-bottom)] sm:left-1/2 sm:bottom-auto sm:top-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2 sm:inset-x-auto sm:w-72 sm:rounded-2xl sm:border">
                    <div className="mx-auto my-2 h-1 w-10 rounded-full bg-muted-foreground/30 sm:hidden" />
                    <Dialog.Title className="sr-only">Message actions</Dialog.Title>
                    <ul className="py-2">
                        {actions.map((a) => (
                            <li key={a.label}>
                                <button
                                    onClick={() => {
                                        a.onSelect()
                                        onOpenChange(false)
                                    }}
                                    className={cn(
                                        "flex w-full items-center gap-3 px-5 py-3 text-sm text-left",
                                        "hover:bg-muted/60 active:bg-muted",
                                        a.danger && "text-destructive",
                                    )}
                                >
                                    <span className="shrink-0">{a.icon}</span>
                                    <span>{a.label}</span>
                                </button>
                            </li>
                        ))}
                    </ul>
                </Dialog.Content>
            </Dialog.Portal>
        </Dialog.Root>
    )
}
