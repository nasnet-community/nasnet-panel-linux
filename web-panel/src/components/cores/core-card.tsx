import { Settings2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { ACCENTS, coreProtocolMeta, type CoreInbound } from "@/lib/types/cores"
import { CoreProtocolIcon } from "./core-protocol-badge"
import { CoreStatusChip, deriveCoreStatus, type CoreStatus } from "./core-status-chip"
import { CoreMemberAvatars } from "./core-member-avatars"

/** Two or three summary chips derived from the protocol settings. */
export function coreSummaryChips(inbound: CoreInbound): string[] {
    const s = (inbound.settings ?? {}) as Record<string, unknown>
    const chips: string[] = []
    const push = (v?: string | false | null) => {
        if (v) chips.push(v)
    }
    const str = (k: string) => (typeof s[k] === "string" ? (s[k] as string) : "")
    switch (inbound.protocol) {
        case "ssh":
            push(inbound.egress_mode === "xray" ? "Xray egress" : "userspace")
            break
        case "mtproto": {
            const modes = (s.modes ?? {}) as Record<string, boolean>
            const on = ["classic", "secure", "tls"].filter((m) => modes[m])
            push(on.length ? on.join("·") : "all modes")
            push(str("tls_domain") && `SNI ${str("tls_domain")}`)
            push(inbound.egress_mode === "xray" && "Xray egress")
            break
        }
    }
    return chips
}

export function CoreCard({
    inbound,
    onOpen,
    onEdit,
    lastWarning,
    setupRequired,
}: {
    inbound: CoreInbound
    onOpen: (inbound: CoreInbound) => void
    onEdit: (inbound: CoreInbound) => void
    lastWarning?: string
    /** the node is missing this protocol's binary */
    setupRequired?: boolean
}) {
    const meta = coreProtocolMeta(inbound.protocol)
    const accent = ACCENTS[meta.accent]
    const status: CoreStatus = lastWarning
        ? "push-failed"
        : setupRequired && !inbound.is_disabled
          ? "setup-required"
          : deriveCoreStatus(inbound)
    const chips = coreSummaryChips(inbound)
    const users = inbound.users ?? []

    return (
        <div
            role="button"
            tabIndex={0}
            onClick={() => onOpen(inbound)}
            onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault()
                    onOpen(inbound)
                }
            }}
            className={cn(
                "group relative flex flex-col overflow-hidden rounded-xl border bg-card text-left shadow-sm transition-all",
                "hover:border-foreground/20 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                inbound.is_disabled && "opacity-70 saturate-50",
            )}
        >
            {/* Accent stripe */}
            <span className={cn("absolute inset-y-0 left-0 w-1", accent.stripe)} aria-hidden />

            <div className="flex items-start gap-3 p-4 pl-5">
                <CoreProtocolIcon protocol={inbound.protocol} />
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                        <span className="truncate font-semibold">{meta.label}</span>
                        <span className="font-mono text-sm text-muted-foreground">:{inbound.port}</span>
                    </div>
                    <p className="truncate text-xs text-muted-foreground">
                        {inbound.remark || meta.family}
                    </p>
                </div>
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Button
                            variant="ghost"
                            size="icon"
                            className="size-8 shrink-0 opacity-60 group-hover:opacity-100"
                            onClick={(e) => {
                                e.stopPropagation()
                                onEdit(inbound)
                            }}
                            aria-label="Tunnel settings"
                        >
                            <Settings2 className="size-4" />
                        </Button>
                    </TooltipTrigger>
                    <TooltipContent>Settings</TooltipContent>
                </Tooltip>
            </div>

            <div className="flex flex-wrap items-center gap-2 px-4 pl-5">
                <CoreStatusChip
                    status={status}
                    tooltip={
                        status === "push-failed"
                            ? lastWarning
                            : status === "setup-required"
                              ? "Install its binary under Dependencies"
                              : undefined
                    }
                />
            </div>

            {chips.length > 0 && (
                <div className="flex flex-wrap gap-1.5 px-4 pl-5 pt-3">
                    {chips.map((c) => (
                        <span
                            key={c}
                            className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[12px] text-muted-foreground"
                        >
                            {c}
                        </span>
                    ))}
                </div>
            )}

            <div className="mt-3 flex items-center border-t px-4 py-2.5 pl-5">
                <div className="flex items-center gap-2">
                    <CoreMemberAvatars users={users} />
                    {users.length > 0 && (
                        <span className="text-xs text-muted-foreground">
                            {users.length} {users.length === 1 ? "user" : "users"}
                        </span>
                    )}
                </div>
            </div>
        </div>
    )
}
