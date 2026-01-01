import { AnimatePresence, motion } from "framer-motion"
import { Badge } from "@/components/ui/badge"
import { HiChevronDown, HiOutlineInformationCircle } from "react-icons/hi"
import { cn } from "@/lib/utils"
import { countryCodeToFlag } from "@/lib/host-grouping"

interface HostGroupSectionProps {
    groupKey: string
    label: string
    countryCode?: string
    count: number
    isCollapsed: boolean
    onToggle: () => void
    children: React.ReactNode
}

export function HostGroupSection({
    groupKey,
    label,
    countryCode,
    count,
    isCollapsed,
    onToggle,
    children,
}: HostGroupSectionProps) {
    const isInfo = groupKey === "info"
    const flag = countryCode ? countryCodeToFlag(countryCode) : ""

    return (
        <div>
            <button
                onClick={onToggle}
                className="w-full flex items-center gap-2 px-1 py-2 text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
                <HiChevronDown
                    className={cn(
                        "h-4 w-4 transition-transform duration-200 shrink-0",
                        isCollapsed && "-rotate-90"
                    )}
                />
                {isInfo ? (
                    <HiOutlineInformationCircle className="h-4 w-4 text-amber-500 shrink-0" />
                ) : flag ? (
                    <span className="text-base leading-none">{flag}</span>
                ) : null}
                <span className={cn(isInfo && "text-amber-600 dark:text-amber-400")}>
                    {label}
                </span>
                <Badge variant="secondary" className="text-[10px] h-4 px-1.5 ml-1">
                    {count}
                </Badge>
            </button>

            <AnimatePresence initial={false}>
                {!isCollapsed && (
                    <motion.div
                        key="content"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.2, ease: "easeInOut" }}
                        className="overflow-hidden"
                    >
                        <div className="pb-2">
                            {children}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    )
}
