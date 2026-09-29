import { ChevronRight } from "lucide-react"
import { cn } from "@/lib/utils"
import {
    ACCENTS,
    CORE_PROTOCOLS,
    type CoreProtocol,
    type CoreProtocolMeta,
} from "@/lib/types/cores"

export function ProtocolPicker({
    onPick,
    disabledProtocols,
}: {
    onPick: (protocol: CoreProtocol) => void
    /** Protocols disabled on the target node — shown non-selectable with a hint. */
    disabledProtocols?: Set<CoreProtocol>
}) {
    return (
        <div className="grid gap-2.5 sm:grid-cols-2">
            {CORE_PROTOCOLS.map((meta) => (
                <ProtocolCard
                    key={meta.value}
                    meta={meta}
                    onPick={onPick}
                    disabled={disabledProtocols?.has(meta.value)}
                />
            ))}
        </div>
    )
}

function ProtocolCard({
    meta,
    onPick,
    disabled,
}: {
    meta: CoreProtocolMeta
    onPick: (protocol: CoreProtocol) => void
    disabled?: boolean
}) {
    const accent = ACCENTS[meta.accent]
    const Icon = meta.icon
    return (
        <button
            type="button"
            disabled={disabled}
            onClick={() => {
                if (!disabled) onPick(meta.value)
            }}
            className={cn(
                "group flex h-full items-start gap-3 rounded-xl border p-3 text-left transition-colors",
                "hover:border-foreground/20 hover:bg-muted/40",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                disabled &&
                    "cursor-not-allowed opacity-50 hover:border-border hover:bg-transparent",
            )}
        >
            <div
                className={cn(
                    "flex size-9 shrink-0 items-center justify-center rounded-lg",
                    accent.softBg,
                )}
            >
                <Icon className={cn("size-5", accent.text)} />
            </div>
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                    <span className="font-medium leading-tight">{meta.label}</span>
                    {meta.needsBinary && (
                        <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">
                            needs telemt
                        </span>
                    )}
                </div>
                <p className="mt-0.5 text-xs leading-snug text-muted-foreground">{meta.oneLiner}</p>
                {disabled && (
                    <p className="mt-1.5 text-[12px] font-medium text-muted-foreground">
                        Disabled on this node
                    </p>
                )}
            </div>
            <ChevronRight className="size-4 shrink-0 self-center text-muted-foreground/60 transition-transform group-hover:translate-x-0.5 group-hover:text-muted-foreground" />
        </button>
    )
}
