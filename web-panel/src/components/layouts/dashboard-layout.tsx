import { useNavigate, Link, useLocation, Outlet } from "react-router"
import { useAuthStore } from "@/store/auth-store"
import { Button } from "@/components/ui/button"
import {
    HiOutlineHome, HiOutlineUsers, HiOutlineServer,
    HiOutlineCog, HiOutlineGlobeAlt,
    HiOutlineChevronLeft, HiOutlineChevronRight, HiOutlineShieldCheck,
    HiOutlineUserGroup, HiOutlineClipboardList,
    HiDotsHorizontal, HiOutlineArchive, HiOutlineLink,
    HiOutlineBell, HiOutlineSun, HiOutlineMoon, HiOutlineLogout,
    HiOutlineSearch, HiOutlineCube,
} from "react-icons/hi"
import { useTheme } from "@/components/providers/theme-provider"
import { cn } from "@/lib/utils"
import { EventsProvider } from "@/components/providers/events-provider"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { ErrorBoundary } from "@/components/ui/error-boundary"
import { useState, useEffect } from "react"
import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { Breadcrumbs } from "@/components/shared/breadcrumbs"
import { useSettings } from "@/lib/queries"
import { Wrench } from "lucide-react"
// import { MessageSquare } from "lucide-react"
// import { useUnreadChatCount } from "@/lib/queries/use-chat"
import { SidebarHeader } from "@/components/sidebar/sidebar-header"
import { SidebarContextPanel } from "@/components/sidebar/sidebar-context-panel"
import { SidebarNav, type NavItem, type NavSection } from "@/components/sidebar/sidebar-nav"
import { SidebarFooter } from "@/components/sidebar/sidebar-footer"

const NAV_SECTIONS: NavSection[] = [
    {
        label: "Overview",
        items: [
            { label: "Dashboard", href: "/dashboard", icon: HiOutlineHome, mobileQuickAccess: true, mobileLabel: "Home" },
        ],
    },
    {
        label: "Customers",
        items: [
            { label: "Users", href: "/users", icon: HiOutlineUsers, mobileQuickAccess: true },
            {
                label: "Subscriptions", href: "/subscriptions", icon: HiOutlineGlobeAlt, mobileQuickAccess: true, mobileLabel: "Subs", collapsedLabel: "Subs",
                children: [
                    { label: "Accounts", href: "/accounts", icon: HiOutlineUserGroup },
                ],
            },
            // Chat support removed (frontend only)
            // { label: "Chats", href: "/chats", icon: MessageSquare, badgeKey: "chats" as const },
        ],
    },
    {
        label: "Infrastructure",
        items: [
            {
                label: "Server", href: "/server", icon: HiOutlineServer, mobileQuickAccess: true,
                children: [
                    { label: "Hosts", href: "/hosts", icon: HiOutlineLink },
                    { label: "Access Logs", href: "/access-logs", icon: HiOutlineGlobeAlt },
                    { label: "Access History", href: "/access-history", icon: HiOutlineSearch },
                    { label: "Xray Core", href: "/xray-binaries", icon: HiOutlineCube },
                ],
            },
            {
                label: "Certificates", href: "/certificates", icon: HiOutlineShieldCheck, collapsedLabel: "Certs",
                children: [
                    { label: "Domains", href: "/domains", icon: HiOutlineGlobeAlt },
                ],
            },
        ],
    },
    {
        label: "Operations",
        items: [
            { label: "Backup", href: "/backup", icon: HiOutlineArchive },
            { label: "Alerts", href: "/alerts", icon: HiOutlineBell },
            { label: "Audit Log", href: "/audit", icon: HiOutlineClipboardList },
            { label: "Settings", href: "/settings", icon: HiOutlineCog },
        ],
    },
]

// Flatten all items (including children) for mobile use
const flattenItems = (sections: NavSection[]): NavItem[] => {
    const items: NavItem[] = []
    for (const section of sections) {
        for (const item of section.items) {
            items.push(item)
            if (item.children) {
                items.push(...item.children)
            }
        }
    }
    return items
}

const ALL_NAV_ITEMS = flattenItems(NAV_SECTIONS)
const MOBILE_QUICK_ITEMS = ALL_NAV_ITEMS.filter(item => item.mobileQuickAccess)
const MOBILE_MORE_ITEMS = ALL_NAV_ITEMS.filter(item => !item.mobileQuickAccess)
const MOBILE_QUICK_HREFS = MOBILE_QUICK_ITEMS.map(item => item.href)

const SIDEBAR_COLLAPSED_KEY = "sidebar-collapsed"

export function DashboardLayout() {
    const navigate = useNavigate()
    const { pathname } = useLocation()
    const user = useAuthStore((state) => state.user)
    const logout = useAuthStore((state) => state.logout)
    const isLoading = useAuthStore((state) => state.isLoading)
    const [isCollapsed, setIsCollapsed] = useState(() => {
        if (typeof window !== "undefined") {
            return localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "true"
        }
        return false
    })
    const [mounted, setMounted] = useState(false)
    const [mobileOpen, setMobileOpen] = useState(false)
    // Data for badges and mini dashboard
    // const { data: unreadChatCount = 0 } = useUnreadChatCount()
    const unreadChatCount = 0
    const { data: settingsByCategory } = useSettings()
    const maintenanceSettings = settingsByCategory?.maintenance ?? []
    const globalMaintenanceActive =
        maintenanceSettings.find((s) => s.key === "maintenance_mode_enabled")?.value === "true"
    const maintenanceMessage =
        maintenanceSettings.find((s) => s.key === "maintenance_mode_message")?.value || ""

    useEffect(() => {
        setMounted(true)
    }, [])

    useEffect(() => {
        setMobileOpen(false)
    }, [pathname])

    const { resolvedTheme, setTheme } = useTheme()
    const toggleTheme = () => setTheme(resolvedTheme === "dark" ? "light" : "dark")

    const toggleSidebar = () => {
        const newState = !isCollapsed
        setIsCollapsed(newState)
        localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(newState))
    }

    const handleLogout = async () => {
        await logout()
        navigate("/login")
    }

    return (
        <EventsProvider>
        <ConfirmDialogProvider>
        {isLoading ? (
            <div className="min-h-screen flex items-center justify-center bg-background">
                <div className="w-8 h-8 border-4 border-primary border-t-transparent rounded-full animate-spin" />
            </div>
        ) : (
            <div className="min-h-screen flex flex-col md:flex-row bg-background">
                {/* Desktop Sidebar */}
                <aside
                    className={cn(
                        "hidden md:flex border-r border-border/50 bg-card/95 backdrop-blur-sm flex-col transition-all duration-200 ease-in-out relative sticky top-0 h-screen",
                        isCollapsed ? "w-20" : "w-64"
                    )}
                >
                    <button
                        onClick={toggleSidebar}
                        className="absolute -right-3 top-20 z-10 w-6 h-6 rounded-full bg-card border border-border/60 shadow-md flex items-center justify-center hover:bg-muted hover:scale-110 active:scale-95 transition-all duration-150"
                        title={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
                        aria-label={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
                    >
                        {isCollapsed ? (
                            <HiOutlineChevronRight className="w-3 h-3 text-muted-foreground" />
                        ) : (
                            <HiOutlineChevronLeft className="w-3 h-3 text-muted-foreground" />
                        )}
                    </button>
                    <div className="flex-1 flex flex-col min-h-0">
                        <SidebarHeader collapsed={isCollapsed} />
                        <SidebarContextPanel collapsed={isCollapsed} />
                        <SidebarNav
                            sections={NAV_SECTIONS}
                            collapsed={isCollapsed}
                            getBadge={() => unreadChatCount}
                        />
                        <SidebarFooter
                            collapsed={isCollapsed}
                            username={user?.username}
                            resolvedTheme={resolvedTheme}
                            onToggleTheme={toggleTheme}
                            onLogout={handleLogout}
                        />
                    </div>
                </aside>

                {/* Main content */}
                <main className="flex-1 overflow-auto w-full">
                    <div className="p-4 md:p-8 pb-24 md:pb-8 space-y-6">
                        <ErrorBoundary>
                            <Breadcrumbs />
                            {globalMaintenanceActive && (
                                <div
                                    role="status"
                                    className="mb-4 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 flex items-center gap-2"
                                >
                                    <Wrench className="h-4 w-4 text-amber-500 shrink-0" />
                                    <span className="text-sm flex-1">
                                        <strong>Global maintenance mode is active.</strong>
                                        {maintenanceMessage ? (
                                            <>
                                                {" "}
                                                <q dir="auto">{maintenanceMessage}</q>
                                            </>
                                        ) : null}{" "}
                                        Users cannot purchase or renew.
                                    </span>
                                </div>
                            )}
                            <Outlet />
                        </ErrorBoundary>
                    </div>
                </main>

                {/* Mobile Bottom Tab Bar */}
                <nav className="md:hidden fixed bottom-0 inset-x-0 z-50 bg-card/95 backdrop-blur-xl border-t border-border/50" style={{ paddingBottom: "env(safe-area-inset-bottom)" }}>
                    <div className="flex items-stretch justify-around" style={{ height: "56px" }}>
                        {MOBILE_QUICK_ITEMS.map((tab) => {
                            const isActive = pathname === tab.href || pathname.startsWith(`${tab.href}/`)
                            return (
                                <Link
                                    key={tab.label}
                                    to={tab.href}
                                    className={cn(
                                        "flex flex-col items-center justify-center gap-0.5 flex-1 transition-colors min-h-[48px] relative",
                                        isActive ? "text-primary" : "text-muted-foreground"
                                    )}
                                >
                                    {isActive && (
                                        <div className="absolute top-0 left-1/2 -translate-x-1/2 w-8 h-[2px] rounded-full bg-primary" />
                                    )}
                                    <tab.icon className="w-5 h-5" />
                                    <span className="text-[10px] font-medium lowercase">{tab.mobileLabel || tab.label}</span>
                                </Link>
                            )
                        })}
                        <button
                            onClick={() => setMobileOpen(true)}
                            className={cn(
                                "flex flex-col items-center justify-center gap-0.5 flex-1 transition-colors min-h-[48px] relative",
                                !MOBILE_QUICK_HREFS.some(
                                    href => pathname === href || pathname.startsWith(`${href}/`)
                                ) ? "text-primary" : "text-muted-foreground"
                            )}
                        >
                            {(unreadChatCount > 0) && (
                                <div className="absolute top-1.5 right-1/2 translate-x-3 w-2 h-2 rounded-full bg-amber-500" />
                            )}
                            <HiDotsHorizontal className="w-5 h-5" />
                            <span className="text-[10px] font-medium lowercase">More</span>
                        </button>
                    </div>
                </nav>

                {/* More Sheet (Mobile) */}
                <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
                    <SheetContent side="bottom" className="rounded-t-2xl px-0 pb-0 gap-0 max-h-[90dvh]">
                        <SheetTitle className="sr-only">More Options</SheetTitle>
                        <SheetDescription className="sr-only">Additional navigation and settings</SheetDescription>
                        <div className="flex-1 min-h-0 overflow-y-auto overscroll-contain">
                        <div className="px-4 pt-2 pb-1">
                            <SidebarContextPanel collapsed={false} />
                        </div>
                        <nav className="px-2 space-y-1">
                            {NAV_SECTIONS.map((section) => {
                                // Get items from this section that appear in MOBILE_MORE_ITEMS
                                const moreHrefs = new Set(MOBILE_MORE_ITEMS.map(i => i.href))
                                const sectionItems = section.items.flatMap(item => {
                                    const result: NavItem[] = []
                                    if (moreHrefs.has(item.href)) result.push(item)
                                    if (item.children) {
                                        result.push(...item.children.filter(c => moreHrefs.has(c.href)))
                                    }
                                    return result
                                })
                                if (sectionItems.length === 0) return null
                                return (
                                    <div key={section.label || "top"}>
                                        {section.label && (
                                            <div className="px-4 pt-3 pb-1">
                                                <span className="font-mono text-[10px] font-semibold tracking-[0.12em] uppercase text-muted-foreground/70">
                                                    // {section.label.toLowerCase()}
                                                </span>
                                            </div>
                                        )}
                                        {sectionItems.map((item) => {
                                            const isActive = pathname === item.href || pathname.startsWith(`${item.href}/`)
                                            const mobileBadgeCount = item.badgeKey === "chats" ? unreadChatCount : 0
                                            const showBadge = mobileBadgeCount > 0
                                            const isNested = NAV_SECTIONS.some(s => s.items.some(i => i.children?.some(c => c.href === item.href)))
                                            return (
                                                <Link
                                                    key={item.label}
                                                    to={item.href}
                                                    onClick={() => setMobileOpen(false)}
                                                    className={cn(
                                                        "flex items-center gap-3 py-3 rounded-lg transition-colors",
                                                        isNested ? "pl-10 pr-4" : "px-4",
                                                        isActive
                                                            ? "bg-primary/10 text-primary font-medium"
                                                            : "text-foreground hover:bg-muted"
                                                    )}
                                                >
                                                    <item.icon className={cn(isNested ? "w-4 h-4" : "w-5 h-5")} />
                                                    <span className={cn("flex-1 lowercase", isNested ? "text-[13px]" : "text-sm")}>{item.label}</span>
                                                    {showBadge && (
                                                        <span className="bg-amber-500 text-white text-[9px] font-bold rounded-full min-w-[18px] h-4.5 flex items-center justify-center px-1.5">
                                                            {mobileBadgeCount}
                                                        </span>
                                                    )}
                                                </Link>
                                            )
                                        })}
                                    </div>
                                )
                            })}
                        </nav>
                        </div>
                        <div className="border-t px-4 py-4" style={{ paddingBottom: "max(1rem, env(safe-area-inset-bottom))" }}>
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0">
                                    <span className="text-sm font-medium text-primary">
                                        {user?.username?.charAt(0).toUpperCase() || "A"}
                                    </span>
                                </div>
                                <div className="flex-1 min-w-0">
                                    <p className="text-sm font-medium truncate">{user?.username || "Admin"}</p>
                                    <p className="text-xs text-muted-foreground">Administrator</p>
                                </div>
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    onClick={toggleTheme}
                                    className="text-muted-foreground hover:text-foreground"
                                    title={resolvedTheme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
                                    aria-label={resolvedTheme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
                                >
                                    {resolvedTheme === "dark"
                                        ? <HiOutlineSun className="w-5 h-5" />
                                        : <HiOutlineMoon className="w-5 h-5" />}
                                </Button>
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    onClick={handleLogout}
                                    className="text-muted-foreground hover:text-foreground"
                                    title="Logout"
                                    aria-label="Logout"
                                >
                                    <HiOutlineLogout className="w-5 h-5" />
                                </Button>
                            </div>
                        </div>
                    </SheetContent>
                </Sheet>
            </div>
        )}
        </ConfirmDialogProvider>
        </EventsProvider>
    )
}
