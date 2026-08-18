import * as React from "react"
import { HiArrowLeft } from "react-icons/hi"
import { useIsMobile } from "@/hooks/use-is-mobile"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    Sheet,
    SheetContent,
    SheetTitle,
    SheetDescription,
} from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface ResponsiveDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    title: string
    description?: string
    onSave: () => void
    saveLabel?: string
    saveDisabled?: boolean
    saveVariant?: "default" | "destructive"
    /** Compact badge/pill rendered next to the title — for state that scopes to the whole object. */
    headerExtra?: React.ReactNode
    /** Status line in the footer, left of the buttons (mobile: end of the scroll body). */
    footerNote?: React.ReactNode
    children: React.ReactNode
    className?: string
}

export function ResponsiveDialog({
    open,
    onOpenChange,
    title,
    description,
    onSave,
    saveLabel = "Save",
    saveDisabled = false,
    saveVariant = "default",
    headerExtra,
    footerNote,
    children,
    className,
}: ResponsiveDialogProps) {
    const isMobile = useIsMobile()

    if (isMobile) {
        return (
            <Sheet open={open} onOpenChange={onOpenChange}>
                <SheetContent
                    side="bottom"
                    className="h-[100dvh] rounded-t-xl flex flex-col p-0 [&>button:last-child]:hidden"
                >
                    {/* Sticky header */}
                    <div className="flex items-center justify-between px-4 py-3 border-b bg-background sticky top-0 z-10">
                        <button
                            type="button"
                            onClick={() => onOpenChange(false)}
                            className="p-2 -ml-2 rounded-lg hover:bg-accent"
                        >
                            <HiArrowLeft className="w-5 h-5" />
                        </button>
                        <SheetTitle className="flex-1 text-center text-base">
                            {title}
                        </SheetTitle>
                        <button
                            type="button"
                            onClick={onSave}
                            disabled={saveDisabled}
                            className={cn(
                                "font-semibold text-sm px-2 py-1 rounded-lg hover:bg-accent disabled:opacity-50 disabled:pointer-events-none",
                                saveVariant === "destructive" ? "text-destructive" : "text-primary",
                            )}
                        >
                            {saveLabel}
                        </button>
                    </div>
                    <SheetDescription className="sr-only">
                        {description || title}
                    </SheetDescription>
                    {/* Scrollable body */}
                    <div className="flex-1 overflow-y-auto px-4 py-4 space-y-6">
                        {headerExtra && <div className="flex">{headerExtra}</div>}
                        {children}
                        {footerNote && (
                            <div className="pt-2 border-t text-xs text-muted-foreground">{footerNote}</div>
                        )}
                    </div>
                </SheetContent>
            </Sheet>
        )
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className={cn("max-w-2xl max-h-[95vh] flex flex-col", className)}>
                <DialogHeader>
                    <div className="flex items-start gap-3">
                        <div className="flex-1 min-w-0">
                            <DialogTitle>{title}</DialogTitle>
                            {description && (
                                <DialogDescription>{description}</DialogDescription>
                            )}
                        </div>
                        {headerExtra && <div className="shrink-0 pr-8">{headerExtra}</div>}
                    </div>
                </DialogHeader>
                <div className="flex-1 overflow-y-auto space-y-6 py-4">
                    {children}
                </div>
                <DialogFooter className="border-t pt-4 sm:justify-between gap-3">
                    <div className="text-xs text-muted-foreground text-left leading-snug">{footerNote}</div>
                    <div className="flex gap-2 justify-end">
                        <Button variant="outline" onClick={() => onOpenChange(false)}>
                            Cancel
                        </Button>
                        <Button onClick={onSave} disabled={saveDisabled} variant={saveVariant === "destructive" ? "destructive" : "default"}>
                            {saveLabel}
                        </Button>
                    </div>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
