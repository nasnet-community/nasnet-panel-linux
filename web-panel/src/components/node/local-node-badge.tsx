import { Router as RouterIcon } from "lucide-react"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

/** Marks the built-in node, which runs inside this panel and has no agent. */
export function LocalNodeBadge({ className }: { className?: string }) {
    return (
        <TooltipProvider delayDuration={300}>
        <Tooltip>
            <TooltipTrigger asChild>
                <span
                    className={cn(
                        "inline-flex shrink-0 items-center gap-1 rounded-full bg-status-info-soft px-2 py-0.5 text-xs font-medium text-status-info",
                        className,
                    )}
                >
                    <RouterIcon className="h-3 w-3" aria-hidden />
                    This router
                </span>
            </TooltipTrigger>
            <TooltipContent>Runs inside this panel; no agent</TooltipContent>
        </Tooltip>
        </TooltipProvider>
    )
}
