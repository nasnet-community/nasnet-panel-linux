import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Settings, Network, ArrowRightLeft, Shield, FileCode, Wrench } from "lucide-react"
import { AnimatePresence, motion } from "framer-motion"
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

interface OutboundTabSidebarProps {
    tabs: TabDefinition[]
    activeTab: TabId
    onTabChange: (tab: TabId) => void
}

export function OutboundTabSidebar({ tabs, activeTab, onTabChange }: OutboundTabSidebarProps) {
    return (
        <div className="w-48 shrink-0 border-r pr-1">
            <nav className="flex flex-col gap-1 py-1">
                <AnimatePresence initial={false}>
                    {tabs.map((tab) => {
                        const Icon = tabIcons[tab.id]
                        const isActive = activeTab === tab.id
                        return (
                            <motion.button
                                key={tab.id}
                                initial={{ opacity: 0, x: -8 }}
                                animate={{ opacity: 1, x: 0 }}
                                exit={{ opacity: 0, x: -8 }}
                                transition={{ duration: 0.15 }}
                                type="button"
                                onClick={() => onTabChange(tab.id)}
                                className={cn(
                                    "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium transition-colors text-left w-full",
                                    isActive
                                        ? "bg-accent text-accent-foreground"
                                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                                )}
                            >
                                <Icon className="h-4 w-4 shrink-0" />
                                <span className="flex-1 truncate">{tab.label}</span>
                                {/* Status dot */}
                                {tab.hasErrors ? (
                                    <span className="h-2 w-2 rounded-full bg-red-500 shrink-0" />
                                ) : tab.badges.length > 0 ? (
                                    <span className="h-2 w-2 rounded-full bg-emerald-500 shrink-0" />
                                ) : null}
                            </motion.button>
                        )
                    })}
                </AnimatePresence>
            </nav>
            {/* Badge summaries below active tab */}
            {tabs.map((tab) => {
                if (tab.badges.length === 0 || activeTab !== tab.id) return null
                return (
                    <div key={tab.id} className="flex flex-wrap gap-1 px-3 pb-1 mt-1">
                        {tab.badges.slice(0, 2).map((badge, i) => (
                            <Badge key={i} variant="secondary" className="text-[10px] font-normal px-1.5 py-0">
                                {badge}
                            </Badge>
                        ))}
                    </div>
                )
            })}
        </div>
    )
}
