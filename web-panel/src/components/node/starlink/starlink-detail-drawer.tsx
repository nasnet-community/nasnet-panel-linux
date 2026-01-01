import type { ReactNode } from "react"
import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { useIsMobile } from "@/hooks/use-is-mobile"

interface StarlinkDetailDrawerProps {
    isOpen: boolean
    onClose: () => void
    title: string
    children: ReactNode
}

export function StarlinkDetailDrawer({ isOpen, onClose, title, children }: StarlinkDetailDrawerProps) {
    const isMobile = useIsMobile()

    return (
        <Sheet open={isOpen} onOpenChange={(open) => !open && onClose()}>
            <SheetContent
                side={isMobile ? "bottom" : "right"}
                className={`
                    bg-card/95 backdrop-blur-xl border-white/10 overflow-y-auto p-6
                    ${isMobile ? "h-[85vh] rounded-t-2xl pb-[max(1.5rem,env(safe-area-inset-bottom))]" : "w-[440px] sm:max-w-[440px]"}
                `}
            >
                <SheetHeader className="pr-6">
                    <SheetTitle className="text-xs uppercase font-bold text-foreground tracking-[0.15em]">
                        {title}
                    </SheetTitle>
                </SheetHeader>
                <div className="mt-4 space-y-5">
                    {children}
                </div>
            </SheetContent>
        </Sheet>
    )
}
