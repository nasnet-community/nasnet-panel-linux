import { cn } from "@/lib/utils"
import { GripVertical } from "lucide-react"

interface WidgetWrapperProps {
    title: string
    icon?: React.ReactNode
    children: React.ReactNode
    isEditMode?: boolean
    headerRight?: React.ReactNode
    className?: string
    noPadding?: boolean
}

export function WidgetWrapper({
    title,
    icon,
    children,
    isEditMode = false,
    headerRight,
    className,
    noPadding = false,
}: WidgetWrapperProps) {
    return (
        <div className={cn(
            "h-full flex flex-col rounded-lg border border-border bg-card/80 backdrop-blur-sm overflow-hidden transition-all duration-200",
            isEditMode && "ring-2 ring-primary/20 ring-offset-1 ring-offset-background cursor-move",
            className
        )}>
            {/* Title bar */}
            <div className={cn(
                "flex items-center justify-between gap-2 px-4 py-2.5 border-b border-border/50 bg-muted/30 shrink-0",
                isEditMode && "drag-handle cursor-grab active:cursor-grabbing"
            )}>
                <div className="flex items-center gap-2 min-w-0">
                    {isEditMode && (
                        <GripVertical className="w-4 h-4 text-muted-foreground shrink-0" />
                    )}
                    {icon && <span className="shrink-0">{icon}</span>}
                    <h3 className="text-sm font-semibold truncate">{title}</h3>
                </div>
                <div className="flex items-center gap-1 shrink-0">
                    {headerRight}
                </div>
            </div>

            {/* Content */}
            <div className={cn("flex-1 overflow-auto", !noPadding && "p-4")}>
                {children}
            </div>
        </div>
    )
}
