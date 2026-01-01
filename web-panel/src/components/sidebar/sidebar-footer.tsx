import { Button } from "@/components/ui/button"
import { HiOutlineLogout, HiOutlineSun, HiOutlineMoon } from "react-icons/hi"
import { cn } from "@/lib/utils"

export interface SidebarFooterProps {
    collapsed: boolean
    username: string | undefined
    resolvedTheme: "dark" | "light"
    onToggleTheme: () => void
    onLogout: () => void
}

export function SidebarFooter({
    collapsed,
    username,
    resolvedTheme,
    onToggleTheme,
    onLogout,
}: SidebarFooterProps) {
    const initial = username?.charAt(0).toUpperCase() || "A"
    const display = username || "Admin"
    const themeLabel =
        resolvedTheme === "dark" ? "Switch to light mode" : "Switch to dark mode"

    if (collapsed) {
        return (
            <div className="border-t border-border/50 mt-auto p-2">
                <div className="flex flex-col items-center gap-1.5">
                    <div
                        className="w-9 h-9 rounded-full bg-primary/10 flex items-center justify-center"
                        title={display}
                    >
                        <span className="text-xs font-semibold text-primary">{initial}</span>
                    </div>
                    <Button
                        variant="ghost"
                        size="icon"
                        onClick={onToggleTheme}
                        className="text-muted-foreground hover:text-foreground h-8 w-8 rounded-md"
                        title={themeLabel}
                        aria-label={themeLabel}
                    >
                        {resolvedTheme === "dark" ? (
                            <HiOutlineSun className="w-4 h-4" />
                        ) : (
                            <HiOutlineMoon className="w-4 h-4" />
                        )}
                    </Button>
                    <Button
                        variant="ghost"
                        size="icon"
                        onClick={onLogout}
                        className="text-muted-foreground hover:text-destructive h-8 w-8 rounded-md"
                        title="Logout"
                        aria-label="Logout"
                    >
                        <HiOutlineLogout className="w-4 h-4" />
                    </Button>
                </div>
            </div>
        )
    }

    return (
        <div className={cn("border-t border-border/50 mt-auto p-3")}>
            <div className="flex items-center gap-2.5 px-1">
                <div className="w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0">
                    <span className="text-xs font-medium text-primary">{initial}</span>
                </div>
                <div className="flex-1 min-w-0">
                    <p className="text-[13px] font-medium truncate">{display}</p>
                    <p className="font-mono text-[9px] text-muted-foreground tracking-[0.08em] uppercase">
                        Administrator
                    </p>
                </div>
                <Button
                    variant="ghost"
                    size="icon"
                    onClick={onToggleTheme}
                    className="text-muted-foreground hover:text-foreground h-8 w-8 rounded-md"
                    title={themeLabel}
                    aria-label={themeLabel}
                >
                    {resolvedTheme === "dark" ? (
                        <HiOutlineSun className="w-4 h-4" />
                    ) : (
                        <HiOutlineMoon className="w-4 h-4" />
                    )}
                </Button>
                <Button
                    variant="ghost"
                    size="icon"
                    onClick={onLogout}
                    className="text-muted-foreground hover:text-destructive h-8 w-8 rounded-md"
                    title="Logout"
                    aria-label="Logout"
                >
                    <HiOutlineLogout className="w-4 h-4" />
                </Button>
            </div>
        </div>
    )
}
