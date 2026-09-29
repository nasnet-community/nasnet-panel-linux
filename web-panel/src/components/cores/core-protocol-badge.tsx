import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { ACCENTS, coreProtocolMeta, type CoreProtocol } from "@/lib/types/cores"

/** Icon + accent + label badge — the signature protocol identity. */
export function CoreProtocolBadge({
    protocol,
    showIcon = true,
    className,
}: {
    protocol: CoreProtocol
    showIcon?: boolean
    className?: string
}) {
    const meta = coreProtocolMeta(protocol)
    const accent = ACCENTS[meta.accent]
    const Icon = meta.icon
    return (
        <Badge variant="outline" className={cn("gap-1.5 font-medium", accent.badge, className)}>
            {showIcon && <Icon className="size-3.5" aria-hidden />}
            {meta.label}
        </Badge>
    )
}

/** Square accent-tinted icon tile for card/modal headers. */
export function CoreProtocolIcon({
    protocol,
    className,
    size = "md",
}: {
    protocol: CoreProtocol
    className?: string
    size?: "sm" | "md" | "lg"
}) {
    const meta = coreProtocolMeta(protocol)
    const accent = ACCENTS[meta.accent]
    const Icon = meta.icon
    const box = size === "lg" ? "size-11" : size === "sm" ? "size-8" : "size-9"
    const icon = size === "lg" ? "size-6" : size === "sm" ? "size-4" : "size-5"
    return (
        <div
            className={cn("flex shrink-0 items-center justify-center rounded-lg", box, accent.softBg, className)}
        >
            <Icon className={cn(icon, accent.text)} aria-hidden />
        </div>
    )
}
