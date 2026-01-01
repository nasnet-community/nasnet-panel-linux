import { cn } from "@/lib/utils"
import { Settings, Network, ArrowRightLeft, Shield, FileCode, Wrench } from "lucide-react"
import type { TabId, TabDefinition } from "./use-outbound-form"
import type { LucideIcon } from "lucide-react"

const tabIcons: Record<TabId, LucideIcon> = {
    general: Settings,
    network: Network,
    transport: ArrowRightLeft,
    security: Shield,
    protocol: FileCode,
    advanced: Wrench,
}

interface OutboundTabBarMobileProps {
    tabs: TabDefinition[]
    activeTab: TabId
    onTabChange: (tab: TabId) => void
}

export function OutboundTabBarMobile({ tabs, activeTab, onTabChange }: OutboundTabBarMobileProps) {
    return (
        <div className="border-t bg-background flex overflow-x-auto pb-[env(safe-area-inset-bottom)]">
            {tabs.map((tab) => {
                const Icon = tabIcons[tab.id]
                const isActive = activeTab === tab.id
                return (
                    <button
                        key={tab.id}
                        type="button"
                        onClick={() => onTabChange(tab.id)}
                        className={cn(
                            "flex flex-col items-center gap-0.5 px-3 py-2 min-w-[64px] text-xs font-medium transition-colors relative",
                            isActive
                                ? "text-primary"
                                : "text-muted-foreground"
                        )}
                    >
                        <div className="relative">
                            <Icon className="h-4.5 w-4.5" />
                            {tab.hasErrors && (
                                <span className="absolute -top-0.5 -right-0.5 h-1.5 w-1.5 rounded-full bg-red-500" />
                            )}
                            {!tab.hasErrors && tab.badges.length > 0 && (
                                <span className="absolute -top-0.5 -right-0.5 h-1.5 w-1.5 rounded-full bg-emerald-500" />
                            )}
                        </div>
                        <span className="truncate max-w-[56px]">{tab.label}</span>
                        {isActive && (
                            <div className="absolute top-0 left-2 right-2 h-0.5 bg-primary rounded-b" />
                        )}
                    </button>
                )
            })}
        </div>
    )
}
