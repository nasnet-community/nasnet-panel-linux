import { useEventsStatus, type ConnectionStatus } from "@/components/providers/events-provider"
import { cn } from "@/lib/utils"
import logo from "@/assets/nasnet-logo.png"

const DOT_COLOR: Record<ConnectionStatus, string> = {
    connected: "bg-primary shadow-[0_0_6px_var(--primary-glow)]",
    connecting: "bg-yellow-500",
    disconnected: "bg-muted-foreground/50",
    error: "bg-red-500",
}

const DOT_LABEL: Record<ConnectionStatus, string> = {
    connected: "Live",
    connecting: "Connecting...",
    disconnected: "Disconnected",
    error: "Error",
}

export interface SidebarHeaderProps {
    collapsed: boolean
}

export function SidebarHeader({ collapsed }: SidebarHeaderProps) {
    const status = useEventsStatus()
    const dotClass = DOT_COLOR[status]
    const dotTitle = `Real-time: ${DOT_LABEL[status]}`

    return (
        <div
            className={cn(
                "h-16 flex items-center border-b border-border/50",
                collapsed ? "justify-center px-3" : "px-5 justify-start",
            )}
        >
            <div className={cn("flex items-center", collapsed ? "gap-0" : "gap-2.5")}>
                <div
                    className="w-6 h-6 rounded-full ring-1 ring-primary/60 overflow-hidden flex items-center justify-center shrink-0 relative"
                    title={collapsed ? dotTitle : undefined}
                >
                    <img src={logo} alt="NasNet" className="w-full h-full object-cover" />
                    {collapsed && (
                        <span
                            className={cn(
                                "absolute -top-0.5 -right-0.5 w-1.5 h-1.5 rounded-full",
                                dotClass,
                            )}
                        />
                    )}
                </div>
                {!collapsed && (
                    <>
                        <span className="font-mono text-[11px] font-semibold tracking-[0.06em] uppercase text-primary whitespace-nowrap">
                            NASNET·PANEL
                        </span>
                        <span
                            className={cn("w-1.5 h-1.5 rounded-full ml-1", dotClass)}
                            title={dotTitle}
                        />
                    </>
                )}
            </div>
        </div>
    )
}
