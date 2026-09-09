import { cn } from "@/lib/utils"
import type { RailItem } from "./dialog-rail"

interface DialogTabBarMobileProps {
    items: RailItem[]
    activeId: string
    onChange: (id: string) => void
}

// Four fixed tabs fit a 390px screen without truncation. Errors show as a
// count badge on the icon; the 6px status dots they replace were invisible.
export function DialogTabBarMobile({ items, activeId, onChange }: DialogTabBarMobileProps) {
    return (
        <div
            className="grid shrink-0 border-t bg-background pb-[env(safe-area-inset-bottom)]"
            style={{ gridTemplateColumns: `repeat(${items.length}, minmax(0, 1fr))` }}
        >
            {items.map((item) => {
                const Icon = item.icon
                const isActive = item.id === activeId
                return (
                    <button
                        key={item.id}
                        type="button"
                        onClick={() => onChange(item.id)}
                        className={cn(
                            "relative flex flex-col items-center gap-1 px-1 pb-1.5 pt-2.5 text-[11px] font-medium leading-[14px] transition-colors",
                            isActive ? "text-foreground" : "text-muted-foreground",
                            item.disabled && !isActive && "opacity-50",
                        )}
                    >
                        {isActive && <span className="absolute inset-x-4 top-0 h-0.5 rounded-b bg-primary" />}
                        <span className="relative">
                            <Icon className="h-5 w-5" />
                            {item.errorCount > 0 && (
                                <span className="absolute -right-2.5 -top-1.5 grid h-4 min-w-4 place-items-center rounded-full bg-status-danger px-1 text-[10px] font-semibold leading-[14px] text-white">
                                    {item.errorCount}
                                </span>
                            )}
                        </span>
                        <span className="truncate">{item.label}</span>
                    </button>
                )
            })}
        </div>
    )
}
