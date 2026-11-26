import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

interface EmptyStateProps {
    icon?: React.ComponentType<{ className?: string }>
    title?: string
    description?: ReactNode
    className?: string
}

export function EmptyState({ icon: Icon, title, description, className }: EmptyStateProps) {
    return (
        <div className={cn("text-center py-12 text-muted-foreground", className)}>
            {Icon && <Icon className="w-12 h-12 mx-auto mb-4 opacity-50" />}
            {title && <p className="font-medium">{title}</p>}
            {description && <div className="text-sm mt-1 mx-auto max-w-xl space-y-2">{description}</div>}
            {!title && !description && <p>No results found</p>}
        </div>
    )
}
