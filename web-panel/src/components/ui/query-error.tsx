import { AlertTriangle, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"

interface QueryErrorProps {
    title: string
    description: string
    onRetry: () => void
    isRetrying?: boolean
}

export function QueryError({ title, description, onRetry, isRetrying = false }: QueryErrorProps) {
    return (
        <div role="alert" className="flex flex-col gap-3 rounded-lg border border-status-warning bg-status-warning-soft p-4 sm:flex-row sm:items-center">
            <AlertTriangle aria-hidden="true" className="h-5 w-5 shrink-0 text-status-warning" />
            <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{title}</p>
                <p className="mt-1 text-sm text-muted-foreground">{description}</p>
            </div>
            <Button variant="outline" size="sm" onClick={onRetry} disabled={isRetrying}>
                <RefreshCw aria-hidden="true" className={`mr-1.5 h-4 w-4${isRetrying ? " animate-spin" : ""}`} />
                {isRetrying ? "Retrying…" : "Try again"}
            </Button>
        </div>
    )
}
