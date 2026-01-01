import { cn } from "@/lib/utils"

interface EmptyStateProps {
    icon?: React.ComponentType<{ className?: string }>
    title?: string
    description?: string
    className?: string
}

export function EmptyState({ icon: Icon, title, description, className }: EmptyStateProps) {
    return (
        <div className={cn("text-center py-12 text-muted-foreground", className)}>
            {Icon && <Icon className="w-12 h-12 mx-auto mb-4 opacity-50" />}
            {title && <p className="font-medium">{title}</p>}
            {description && <p className="text-sm mt-1">{description}</p>}
            {!title && !description && <p>No results found</p>}
        </div>
    )
}
