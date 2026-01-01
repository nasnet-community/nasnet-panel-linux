import { useState } from "react"
import { HiChevronDown } from "react-icons/hi"
import { cn } from "@/lib/utils"

interface CollapsiblePanelProps {
    title: string
    children: React.ReactNode
    defaultOpen?: boolean
    className?: string
    badge?: React.ReactNode
}

export function CollapsiblePanel({ title, children, defaultOpen = true, className, badge }: CollapsiblePanelProps) {
    const [open, setOpen] = useState(defaultOpen)

    return (
        <div className={cn("border rounded-lg overflow-hidden", className)}>
            <button
                onClick={() => setOpen(!open)}
                className="w-full flex items-center justify-between px-3 py-2.5 text-sm font-medium text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
            >
                <div className="flex items-center gap-2">
                    <span>{title}</span>
                    {badge}
                </div>
                <HiChevronDown className={cn("w-4 h-4 transition-transform duration-200", open && "rotate-180")} />
            </button>
            {open && <div className="px-3 pb-3">{children}</div>}
        </div>
    )
}
