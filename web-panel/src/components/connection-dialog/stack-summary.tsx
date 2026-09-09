import { Fragment } from "react"
import { cn } from "@/lib/utils"

// One token per layer of the configured stack: protocol · network · security
// · flow · endpoint. Rendered in the dialog header so the user sees what the
// form currently describes without visiting every tab; a token with `tab`
// set jumps there on click.
export interface StackToken {
    label: string
    tab?: string
    /** Secondary token (endpoint, flow) rendered without a chip background. */
    dim?: boolean
}

interface StackSummaryProps {
    tokens: StackToken[]
    onJump?: (tab: string) => void
    className?: string
}

export function StackSummary({ tokens, onJump, className }: StackSummaryProps) {
    if (tokens.length === 0) return null
    return (
        <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
            {tokens.map((t, i) => {
                const clickable = !!t.tab && !!onJump
                const chip = (
                    <span
                        className={cn(
                            "font-mono text-[11px] leading-4",
                            t.dim
                                ? "text-text-tertiary"
                                : "rounded-[5px] bg-accent px-1.5 py-px text-text-secondary",
                            clickable && "transition-colors hover:text-foreground",
                        )}
                    >
                        {t.label}
                    </span>
                )
                return (
                    <Fragment key={`${t.label}-${i}`}>
                        {i > 0 && <span className="text-[11px] leading-4 text-text-disabled">·</span>}
                        {clickable ? (
                            <button type="button" onClick={() => onJump?.(t.tab!)} className="rounded-[5px] outline-none focus-visible:ring-2 focus-visible:ring-ring/50">
                                {chip}
                            </button>
                        ) : chip}
                    </Fragment>
                )
            })}
        </div>
    )
}
