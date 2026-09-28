import { Search } from "lucide-react"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { MOD_KEY } from "@/lib/platform"

const HINT = `${MOD_KEY}K`

export function SidebarSearchButton({ collapsed, onOpen }: { collapsed: boolean; onOpen: () => void }) {
    if (collapsed) {
        return (
            <Tooltip>
                <TooltipTrigger asChild>
                    <button
                        type="button"
                        onClick={onOpen}
                        aria-label="Search or jump to"
                        className="mx-auto mt-2 mb-1 size-10 shrink-0 flex items-center justify-center rounded-lg border border-border/60 bg-muted/20 text-muted-foreground hover:text-foreground transition-colors"
                    >
                        <Search className="size-[18px]" strokeWidth={1.75} />
                    </button>
                </TooltipTrigger>
                <TooltipContent side="right">
                    search <span className="ml-2 font-mono text-muted-foreground">{HINT}</span>
                </TooltipContent>
            </Tooltip>
        )
    }
    return (
        <button
            type="button"
            onClick={onOpen}
            className="mx-3 mt-2 mb-2 h-9 shrink-0 flex items-center gap-2 rounded-lg border border-border/60 bg-muted/20 pl-2.5 pr-2 text-[13.5px] text-muted-foreground hover:text-foreground transition-colors"
        >
            <Search className="size-[15px]" />
            <span className="flex-1 text-left">search or jump to…</span>
            <kbd className="rounded border border-border/60 px-1.5 font-mono text-[11px] leading-4">{HINT}</kbd>
        </button>
    )
}
