import { useEffect, useState } from "react"
import { Link, useLocation } from "react-router"
import { Ellipsis, LogOut, Moon, Search, Sun } from "lucide-react"
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet"
import { SidebarContextPanel } from "@/components/sidebar/sidebar-context-panel"
import { NavBadge, type NavBadgeValue } from "@/components/sidebar/nav-badge"
import { useTheme } from "@/components/providers/theme-provider"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useCommandPalette } from "@/store/command-palette-store"
import { cn } from "@/lib/utils"
import { MOBILE_TABS, NAV_SECTIONS, SETTINGS_ITEM, isItemActive, type NavItem } from "./nav-config"
import type { NavBadges } from "./use-nav-badges"

const TAB_HREFS = new Set(MOBILE_TABS.map((t) => t.href))

// Every page not on the tab bar, children flattened in place. Anything left
// in the unlabelled top block reads best among the customer pages.
const SHEET_SECTIONS = (() => {
    const [top, ...rest] = NAV_SECTIONS.map((s) => ({
        label: s.label ?? "",
        items: s.items.flatMap((i) => [i, ...(i.children ?? [])]).filter((i) => !TAB_HREFS.has(i.href)),
    }))
    rest[0] = { ...rest[0], items: [...top.items, ...rest[0].items] }
    return rest.filter((s) => s.items.length > 0)
})()

const TAB_CLASS = "relative flex-1 min-h-12 flex flex-col items-center justify-center gap-0.5 transition-colors"

interface MobileNavProps {
    badges: NavBadges
    username?: string
    onLogout: () => void
}

export function MobileNav({ badges, username, onLogout }: MobileNavProps) {
    const { pathname } = useLocation()
    const isMobile = useIsMobile()
    const [open, setOpen] = useState(false)
    const { resolvedTheme, setTheme } = useTheme()
    const openPalette = useCommandPalette((s) => s.setOpen)
    useEffect(() => setOpen(false), [pathname])

    const badgeFor = (item: NavItem) => (item.badgeKey ? badges[item.badgeKey] : undefined)
    const inMore = !MOBILE_TABS.some((t) => isItemActive(t, pathname))
    const alertCount = badges.alerts?.count ?? 0
    // Settings sits under More; its dot shows there when no alert count does
    const moreBadge: NavBadgeValue = alertCount > 0 || !badges.settings?.count
        ? { count: alertCount, tone: badges.alerts?.tone === "danger" && alertCount > 0 ? "danger" : "warning" }
        : badges.settings
    const display = username || "Admin"
    const themeLabel = resolvedTheme === "dark" ? "Switch to light mode" : "Switch to dark mode"
    const close = () => setOpen(false)

    return (
        <>
            <nav
                aria-label="Main navigation"
                className="md:hidden fixed bottom-0 inset-x-0 z-50 bg-card/95 backdrop-blur-xl border-t border-border/50"
                style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
            >
                <div className="flex items-stretch justify-around h-14">
                    {MOBILE_TABS.map((tab) => (
                        <TabLink key={tab.href} item={tab} active={isItemActive(tab, pathname)} badge={badgeFor(tab)} />
                    ))}
                    <button type="button" onClick={() => setOpen(true)} aria-label="More" className={cn(TAB_CLASS, inMore ? "text-primary" : "text-muted-foreground")}>
                        {inMore && <TabIndicator />}
                        <span className="relative flex">
                            <Ellipsis className="size-[22px]" strokeWidth={1.75} />
                            <TabBadge value={moreBadge} />
                        </span>
                        <span className="text-[11px] font-medium lowercase">more</span>
                    </button>
                </div>
            </nav>

            <Sheet open={open} onOpenChange={setOpen}>
                <SheetContent side="bottom" className="rounded-t-2xl px-0 pb-0 gap-0 max-h-[90dvh]">
                    <SheetTitle className="sr-only">More</SheetTitle>
                    <SheetDescription className="sr-only">All pages and your account</SheetDescription>
                    <div className="flex-1 min-h-0 overflow-y-auto overscroll-contain px-4 pb-2">
                        {/* right padding clears the sheet's built-in close button */}
                        <div className="pt-2.5 pb-3 pr-9">
                            <button
                                type="button"
                                onClick={() => { close(); openPalette(true) }}
                                className="flex h-10 w-full items-center gap-2 rounded-lg border border-border/60 bg-muted/20 px-3 text-[14.5px] text-muted-foreground"
                            >
                                <Search className="size-4" />
                                search or jump to…
                            </button>
                        </div>
                        <div className="-mx-3 mb-3">
                            <SidebarContextPanel collapsed={false} visible={isMobile && open} placement="bottom" />
                        </div>
                        {SHEET_SECTIONS.map((section) => (
                            <section key={section.label} className="mb-3">
                                <h3 className="px-1 pb-1.5 text-[12px] font-medium leading-4 tracking-[0.08em] uppercase text-muted-foreground/80">
                                    {section.label}
                                </h3>
                                <div className="grid grid-cols-2 gap-1">
                                    {section.items.map((item) => (
                                        <SheetCell key={item.href} item={item} active={isItemActive(item, pathname)} badge={badgeFor(item)} onNavigate={close} />
                                    ))}
                                </div>
                            </section>
                        ))}
                    </div>
                    <div className="border-t border-border/50 px-4 pt-2 space-y-1" style={{ paddingBottom: "max(0.75rem, env(safe-area-inset-bottom))" }}>
                        <SheetCell item={SETTINGS_ITEM} active={isItemActive(SETTINGS_ITEM, pathname)} badge={badgeFor(SETTINGS_ITEM)} onNavigate={close} wide />
                        <div className="flex items-center gap-3 h-12 pl-2">
                            <span className="size-9 shrink-0 rounded-full bg-primary/10 flex items-center justify-center text-sm font-semibold text-primary">
                                {display.charAt(0).toUpperCase()}
                            </span>
                            <div className="flex-1 min-w-0">
                                <p className="text-[14.5px] font-medium leading-[18px] truncate">{display}</p>
                                <p className="font-mono text-[10px] leading-[14px] tracking-[0.08em] uppercase text-muted-foreground">Administrator</p>
                            </div>
                            <button type="button" onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")} aria-label={themeLabel} title={themeLabel} className="size-10 flex items-center justify-center rounded-lg text-muted-foreground hover:text-foreground">
                                {resolvedTheme === "dark" ? <Sun className="size-[18px]" /> : <Moon className="size-[18px]" />}
                            </button>
                            <button type="button" onClick={onLogout} aria-label="Log out" title="Log out" className="size-10 flex items-center justify-center rounded-lg text-muted-foreground hover:text-destructive">
                                <LogOut className="size-[18px]" />
                            </button>
                        </div>
                    </div>
                </SheetContent>
            </Sheet>
        </>
    )
}

function TabIndicator() {
    return <span className="absolute top-0 left-1/2 -translate-x-1/2 w-8 h-[2px] rounded-full bg-primary" />
}

function TabBadge({ value }: { value?: NavBadgeValue }) {
    return <NavBadge value={value} className="absolute -top-1.5 left-3.5 min-w-[18px] h-[18px] px-1 text-[11px] ring-2 ring-card" />
}

function TabLink({ item, active, badge }: { item: NavItem; active: boolean; badge?: NavBadgeValue }) {
    const Icon = item.icon
    return (
        <Link to={item.href} aria-current={active ? "page" : undefined} className={cn(TAB_CLASS, active ? "text-primary" : "text-muted-foreground")}>
            {active && <TabIndicator />}
            <span className="relative flex">
                <Icon className="size-[22px]" strokeWidth={1.75} />
                <TabBadge value={badge} />
            </span>
            <span className="text-[11px] font-medium lowercase">{item.mobileLabel ?? item.label}</span>
        </Link>
    )
}

function SheetCell({ item, active, badge, onNavigate, wide = false }: {
    item: NavItem
    active: boolean
    badge?: NavBadgeValue
    onNavigate: () => void
    wide?: boolean
}) {
    const Icon = item.icon
    return (
        <Link
            to={item.href}
            onClick={onNavigate}
            aria-current={active ? "page" : undefined}
            className={cn(
                "flex h-11 min-w-0 items-center gap-2.5 rounded-lg px-3 text-[14.5px] font-medium lowercase transition-colors",
                active ? "bg-primary/10 text-primary" : wide ? "text-foreground hover:bg-muted" : "bg-muted/30 text-foreground hover:bg-muted",
            )}
        >
            <Icon className="size-[18px] shrink-0 text-muted-foreground" strokeWidth={1.75} />
            <span className="truncate">{item.label}</span>
            <NavBadge value={badge} className="ml-auto min-w-5 h-[18px] px-1.5 text-[11.5px]" />
        </Link>
    )
}
