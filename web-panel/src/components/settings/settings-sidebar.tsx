import { useState } from "react"
import type { IconType } from "react-icons"
import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { HiOutlineChevronDown } from "react-icons/hi"
import { getCategoryLabel } from "./settings-constants"

export interface SidebarItem {
    key: string
    label: string
    icon: IconType
    dirty: boolean
    settingCount?: number
}

interface SettingsSidebarProps {
    items: SidebarItem[]
    active: string
    onSelect: (key: string) => void
    mode?: "desktop" | "mobile"
}

export function SettingsSidebar({ items, active, onSelect, mode = "desktop" }: SettingsSidebarProps) {
    const [sheetOpen, setSheetOpen] = useState(false)

    // Mobile mode: button trigger + bottom sheet
    if (mode === "mobile") {
        const activeItem = items.find(i => i.key === active)
        const ActiveIcon = activeItem?.icon
        const dirtyCount = items.filter(i => i.dirty).length

        return (
            <>
                <Button
                    variant="outline"
                    className="w-full justify-between h-11"
                    onClick={() => setSheetOpen(true)}
                >
                    <div className="flex items-center gap-2 min-w-0">
                        {ActiveIcon && <ActiveIcon className="w-4 h-4 shrink-0" />}
                        <span className="truncate">{activeItem ? getCategoryLabel(activeItem.key) : "Select category"}</span>
                        {dirtyCount > 0 && (
                            <Badge variant="warning" className="text-[10px] px-1.5 py-0">
                                {dirtyCount}
                            </Badge>
                        )}
                    </div>
                    <HiOutlineChevronDown className="w-4 h-4 shrink-0 text-muted-foreground" />
                </Button>

                <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
                    <SheetContent side="bottom" className="rounded-t-2xl px-0 pb-0">
                        <SheetTitle className="sr-only">Settings Categories</SheetTitle>
                        <SheetDescription className="sr-only">Select a settings category</SheetDescription>
                        <nav className="px-2 space-y-0.5 pb-4" style={{ paddingBottom: "max(1rem, env(safe-area-inset-bottom))" }}>
                            {items.map(item => {
                                const Icon = item.icon
                                const isActive = item.key === active
                                return (
                                    <button
                                        key={item.key}
                                        onClick={() => {
                                            onSelect(item.key)
                                            setSheetOpen(false)
                                        }}
                                        className={cn(
                                            "w-full flex items-center gap-3 px-4 py-3 rounded-lg text-sm transition-colors text-left min-h-[48px]",
                                            isActive
                                                ? "bg-primary/10 text-primary font-medium"
                                                : "text-foreground hover:bg-muted"
                                        )}
                                    >
                                        <Icon className="w-5 h-5 shrink-0" />
                                        <span className="flex-1">{getCategoryLabel(item.key)}</span>
                                        {item.settingCount !== undefined && (
                                            <Badge variant="secondary" className="text-[10px] px-1.5 py-0">
                                                {item.settingCount}
                                            </Badge>
                                        )}
                                        {item.dirty && (
                                            <Badge variant="warning" className="text-[10px] px-1.5 py-0">
                                                Modified
                                            </Badge>
                                        )}
                                    </button>
                                )
                            })}
                        </nav>
                    </SheetContent>
                </Sheet>
            </>
        )
    }

    // Desktop mode: vertical sidebar
    return (
        <nav className="w-[220px] shrink-0 space-y-0.5 sticky top-20">
            {items.map(item => {
                const Icon = item.icon
                const isActive = item.key === active
                return (
                    <button
                        key={item.key}
                        onClick={() => onSelect(item.key)}
                        className={cn(
                            "w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm transition-colors text-left relative",
                            isActive
                                ? "bg-primary/10 text-primary font-medium"
                                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                        )}
                    >
                        {isActive && (
                            <span className="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-5 bg-primary rounded-r" />
                        )}
                        <Icon className="w-4 h-4 shrink-0" />
                        <span className="flex-1">{getCategoryLabel(item.key)}</span>
                        {item.dirty && (
                            <Badge variant="warning" className="text-[10px] px-1.5 py-0">
                                Modified
                            </Badge>
                        )}
                    </button>
                )
            })}
        </nav>
    )
}
