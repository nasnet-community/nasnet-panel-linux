import { Ellipsis, Keyboard, LogOut } from "lucide-react"
import { useLocation } from "react-router"
import {
    DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup,
    DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuShortcut, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useTheme } from "@/components/providers/theme-provider"
import { SETTINGS_ITEM, isItemActive } from "@/components/layouts/nav-config"
import { NavLinkRow, RailLink } from "./sidebar-nav"

type ThemeChoice = "system" | "light" | "dark"

export interface SidebarFooterProps {
    collapsed: boolean
    username: string | undefined
    onLogout: () => void
    onOpenShortcuts: () => void
}

export function SidebarFooter({ collapsed, username, onLogout, onOpenShortcuts }: SidebarFooterProps) {
    const { pathname } = useLocation()
    const display = username || "Admin"
    const avatar = (
        <span className="size-[30px] shrink-0 rounded-full bg-primary/10 flex items-center justify-center text-xs font-semibold text-primary">
            {display.charAt(0).toUpperCase()}
        </span>
    )

    if (collapsed) {
        return (
            <div className="border-t border-border/50 mt-auto py-2 flex flex-col items-center gap-1.5">
                <RailLink item={SETTINGS_ITEM} pathname={pathname} />
                <AccountMenu display={display} side="right" onLogout={onLogout} onOpenShortcuts={onOpenShortcuts}>
                    <button type="button" aria-label={`Account menu, ${display}`} className="rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                        {avatar}
                    </button>
                </AccountMenu>
            </div>
        )
    }

    return (
        <div className="border-t border-border/50 mt-auto px-3 pt-2 pb-2.5 space-y-1.5">
            <NavLinkRow item={SETTINGS_ITEM} active={isItemActive(SETTINGS_ITEM, pathname)} />
            <div className="flex items-center gap-2.5 pl-1.5">
                {avatar}
                <div className="flex-1 min-w-0">
                    <p className="text-[14px] font-medium leading-[18px] truncate">{display}</p>
                    <p className="font-mono text-[10px] leading-[14px] tracking-[0.08em] uppercase text-muted-foreground">Administrator</p>
                </div>
                <AccountMenu display={display} side="top" onLogout={onLogout} onOpenShortcuts={onOpenShortcuts}>
                    <button type="button" aria-label="Account menu" className="size-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors">
                        <Ellipsis className="size-4" />
                    </button>
                </AccountMenu>
            </div>
        </div>
    )
}

function AccountMenu({ display, side, onLogout, onOpenShortcuts, children }: {
    display: string
    side: "top" | "right"
    onLogout: () => void
    onOpenShortcuts: () => void
    children: React.ReactElement
}) {
    const { theme, setTheme } = useTheme()
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
            <DropdownMenuContent side={side} align="end" className="w-56">
                <DropdownMenuLabel className="font-normal">
                    <span className="block text-sm font-medium">{display}</span>
                    <span className="block font-mono text-[10px] tracking-[0.08em] uppercase text-muted-foreground">Administrator</span>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">theme</DropdownMenuLabel>
                <DropdownMenuRadioGroup value={theme} onValueChange={(value) => setTheme(value as ThemeChoice)}>
                    <DropdownMenuRadioItem value="system">system</DropdownMenuRadioItem>
                    <DropdownMenuRadioItem value="light">light</DropdownMenuRadioItem>
                    <DropdownMenuRadioItem value="dark">dark</DropdownMenuRadioItem>
                </DropdownMenuRadioGroup>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={onOpenShortcuts}>
                    <Keyboard className="size-4" />
                    keyboard shortcuts
                    <DropdownMenuShortcut>?</DropdownMenuShortcut>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={onLogout} className="text-destructive focus:text-destructive">
                    <LogOut className="size-4" />
                    log out
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    )
}
