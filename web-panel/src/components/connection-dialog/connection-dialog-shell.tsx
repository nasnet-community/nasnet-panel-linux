import { type KeyboardEvent, type ReactNode } from "react"
import { AnimatePresence, motion } from "framer-motion"
import { ArrowLeft } from "lucide-react"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    Sheet,
    SheetContent,
    SheetTitle,
    SheetDescription,
} from "@/components/ui/sheet"
import { StackSummary, type StackToken } from "./stack-summary"
import { DialogRail, type RailItem } from "./dialog-rail"
import { DialogTabBarMobile } from "./dialog-tab-bar-mobile"

export interface SecondaryAction {
    label: string
    onClick: () => void
    disabled?: boolean
}

interface ConnectionDialogShellProps {
    open: boolean
    /** Already guarded by the caller (dirty-form confirm lives there). */
    onOpenChange: (open: boolean) => void
    title: ReactNode
    /** Screen-reader description; not rendered visibly. */
    description: string
    stack: StackToken[]
    rail: RailItem[]
    activeTab: string
    onTabChange: (id: string) => void
    /** Footer left slot: unsaved indicator or error summary. */
    statusSlot?: ReactNode
    primaryLabel: string
    onPrimary: () => void
    loading?: boolean
    secondaryAction?: SecondaryAction
    children: ReactNode
}

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)

// One chrome for both the inbound and the outbound dialog: header with a live
// stack summary, a fixed rail (or bottom bar on phones), a scrolling tab body
// and a footer that carries status + actions. Cmd/Ctrl+Enter saves.
export function ConnectionDialogShell({
    open,
    onOpenChange,
    title,
    description,
    stack,
    rail,
    activeTab,
    onTabChange,
    statusSlot,
    primaryLabel,
    onPrimary,
    loading = false,
    secondaryAction,
    children,
}: ConnectionDialogShellProps) {
    const isMobile = useIsMobile()

    const handleKeyDown = (e: KeyboardEvent<HTMLElement>) => {
        if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && !loading) {
            e.preventDefault()
            onPrimary()
        }
    }

    const body = (
        <AnimatePresence mode="wait">
            <motion.div
                key={activeTab}
                initial={{ opacity: 0, x: 8 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: -8 }}
                transition={{ duration: 0.15 }}
            >
                {children}
            </motion.div>
        </AnimatePresence>
    )

    if (isMobile) {
        return (
            <Sheet open={open} onOpenChange={onOpenChange}>
                <SheetContent
                    side="bottom"
                    className="flex h-[100dvh] flex-col gap-0 rounded-t-xl p-0 [&>button:last-child]:hidden"
                    onKeyDown={handleKeyDown}
                >
                    <div className="sticky top-0 z-10 flex items-center justify-between border-b bg-background px-2 py-2">
                        <button
                            type="button"
                            onClick={() => onOpenChange(false)}
                            aria-label="Back"
                            className="grid h-9 w-9 place-items-center rounded-lg hover:bg-accent"
                        >
                            <ArrowLeft className="h-5 w-5" />
                        </button>
                        <SheetTitle className="flex-1 truncate text-center text-base tracking-[-0.3px]">
                            {title}
                        </SheetTitle>
                        <button
                            type="button"
                            onClick={onPrimary}
                            disabled={loading}
                            className="rounded-lg px-2 py-2 text-sm font-semibold text-primary hover:bg-accent disabled:pointer-events-none disabled:opacity-50"
                        >
                            {loading ? "Saving…" : primaryLabel}
                        </button>
                    </div>
                    <SheetDescription className="sr-only">{description}</SheetDescription>
                    {stack.length > 0 && (
                        <div className="flex justify-center px-4 pt-2">
                            <StackSummary tokens={stack} onJump={onTabChange} />
                        </div>
                    )}

                    <div className="flex-1 overflow-y-auto px-4 py-4">
                        {body}
                        {statusSlot && <div className="mt-4">{statusSlot}</div>}
                        {secondaryAction && (
                            <Button
                                type="button"
                                variant="outline"
                                className="mt-4 w-full"
                                onClick={secondaryAction.onClick}
                                disabled={secondaryAction.disabled || loading}
                            >
                                {secondaryAction.label}
                            </Button>
                        )}
                    </div>

                    <DialogTabBarMobile items={rail} activeId={activeTab} onChange={onTabChange} />
                </SheetContent>
            </Sheet>
        )
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                className="flex h-[85vh] max-h-[85vh] max-w-4xl flex-col gap-0 p-0"
                onKeyDown={handleKeyDown}
            >
                <div className="flex items-start justify-between gap-4 px-6 pb-4 pt-5">
                    <div className="min-w-0">
                        <DialogTitle className="text-lg leading-6">{title}</DialogTitle>
                        <DialogDescription className="sr-only">{description}</DialogDescription>
                        <StackSummary tokens={stack} onJump={onTabChange} className="mt-1.5" />
                    </div>
                </div>

                <div className="flex min-h-0 flex-1 border-t">
                    <DialogRail items={rail} activeId={activeTab} onChange={onTabChange} />
                    <ScrollArea className="flex-1">
                        <div className="p-6">{body}</div>
                    </ScrollArea>
                </div>

                <div className="flex items-center justify-between gap-3 border-t px-6 py-4">
                    <div className="min-w-0 flex-1 text-[13px] leading-[18px] text-text-tertiary">
                        {statusSlot}
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                            Cancel
                        </Button>
                        {secondaryAction && (
                            <Button
                                type="button"
                                variant="outline"
                                onClick={secondaryAction.onClick}
                                disabled={secondaryAction.disabled || loading}
                            >
                                {secondaryAction.label}
                            </Button>
                        )}
                        <Button type="button" onClick={onPrimary} disabled={loading}>
                            {loading ? "Saving…" : primaryLabel}
                            {!loading && (
                                <kbd className="rounded border border-black/10 bg-gray-300 px-1 font-mono text-[11px] font-normal leading-4 text-gray-600">
                                    {isMac ? "⌘↵" : "Ctrl↵"}
                                </kbd>
                            )}
                        </Button>
                    </div>
                </div>
            </DialogContent>
        </Dialog>
    )
}
