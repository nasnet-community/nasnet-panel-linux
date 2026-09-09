import type { ReactNode } from "react"
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { useIsMobile } from "@/hooks/use-is-mobile"

interface StarlinkDetailDrawerProps {
    isOpen: boolean
    onClose: () => void
    title: string
    /** Screen-reader summary. Radix warns on every open without one. */
    description?: string
    children: ReactNode
}

export function StarlinkDetailDrawer({ isOpen, onClose, title, description, children }: StarlinkDetailDrawerProps) {
    const isMobile = useIsMobile()

    return (
        <Sheet open={isOpen} onOpenChange={(open) => !open && onClose()}>
            <SheetContent
                side={isMobile ? "bottom" : "right"}
                // overflow-hidden on the x axis: the sky map is square and as
                // wide as the panel, so any stray sub-pixel overflow must not
                // turn into a horizontal scrollbar that clips the map.
                className={`
                    bg-card/95 backdrop-blur-xl border-white/10 overflow-x-hidden overflow-y-auto p-6 gap-0
                    ${isMobile
                        ? "h-[85vh] rounded-t-2xl pb-[max(1.5rem,env(safe-area-inset-bottom))]"
                        : "w-[min(520px,100vw)] max-w-none sm:max-w-none"}
                `}
            >
                <SheetHeader className="p-0 pr-8">
                    <SheetTitle className="text-[11px] font-medium uppercase leading-5 tracking-[0.08em] text-muted-foreground/70">
                        {title}
                    </SheetTitle>
                    <SheetDescription className="sr-only">
                        {description ?? `${title} for this Starlink terminal.`}
                    </SheetDescription>
                </SheetHeader>
                <div className="mt-4 space-y-5">
                    {children}
                </div>
            </SheetContent>
        </Sheet>
    )
}
