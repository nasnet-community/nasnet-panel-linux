import { useNavigate, useLocation, Outlet } from "react-router"
import { useEffect, useState } from "react"
import { ChevronLeft, ChevronRight, Wrench } from "lucide-react"
import { useAuthStore } from "@/store/auth-store"
import { cn } from "@/lib/utils"
import { EventsProvider } from "@/components/providers/events-provider"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { ErrorBoundary } from "@/components/ui/error-boundary"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Breadcrumbs } from "@/components/shared/breadcrumbs"
import { useSettings } from "@/lib/queries"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { SidebarHeader } from "@/components/sidebar/sidebar-header"
import { SidebarContextPanel } from "@/components/sidebar/sidebar-context-panel"
import { SidebarNav } from "@/components/sidebar/sidebar-nav"
import { SidebarFooter } from "@/components/sidebar/sidebar-footer"
import { SidebarSearchButton } from "@/components/sidebar/sidebar-search-button"
import { CommandPalette } from "@/components/command-palette/command-palette"
import { useCommandPalette } from "@/store/command-palette-store"
import { ShortcutsDialog } from "@/components/command-palette/shortcuts-dialog"
import { useGlobalShortcuts } from "@/hooks/use-global-shortcuts"
import { NAV_SECTIONS } from "./nav-config"
import { MobileNav } from "./mobile-nav"
import { useNavBadges } from "./use-nav-badges"

const SIDEBAR_COLLAPSED_KEY = "sidebar-collapsed"

export function DashboardLayout() {
    const navigate = useNavigate()
    const { pathname, search } = useLocation()
    const terminalWorkspace = /^\/nodes\/[^/]+\/?$/.test(pathname) && new URLSearchParams(search).get("tab") === "terminal"
    const user = useAuthStore((state) => state.user)
    const logout = useAuthStore((state) => state.logout)
    const isLoading = useAuthStore((state) => state.isLoading)
    const [isCollapsed, setIsCollapsed] = useState(() => {
        if (typeof window !== "undefined") {
            return localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "true"
        }
        return false
    })
    // A workspace-sized rail without overwriting the user's usual navigation preference.
    const [terminalSidebarExpanded, setTerminalSidebarExpanded] = useState(false)
    const sidebarCollapsed = terminalWorkspace ? !terminalSidebarExpanded : isCollapsed
    const isMobile = useIsMobile()
    const badges = useNavBadges()
    const setPaletteOpen = useCommandPalette((s) => s.setOpen)
    const { data: settingsByCategory } = useSettings()
    const maintenanceSettings = settingsByCategory?.maintenance ?? []
    const globalMaintenanceActive =
        maintenanceSettings.find((s) => s.key === "maintenance_mode_enabled")?.value === "true"
    const maintenanceMessage =
        maintenanceSettings.find((s) => s.key === "maintenance_mode_message")?.value || ""

    useEffect(() => { if (!terminalWorkspace) setTerminalSidebarExpanded(false) }, [terminalWorkspace])

    const toggleSidebar = () => {
        if (terminalWorkspace) {
            setTerminalSidebarExpanded(value => !value)
            return
        }
        const newState = !isCollapsed
        setIsCollapsed(newState)
        localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(newState))
    }
    useGlobalShortcuts(toggleSidebar)

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
                <TooltipProvider delayDuration={300}>
                    <aside
                        className={cn(
                            "hidden md:flex border-r border-border/50 bg-card/95 backdrop-blur-sm flex-col transition-all duration-200 ease-in-out relative sticky top-0 h-screen",
                            sidebarCollapsed ? "w-16 shrink-0" : "w-64 shrink-0"
                        )}
                    >
                        <button
                            onClick={toggleSidebar}
                            className="absolute -right-3 top-5 z-10 w-6 h-6 rounded-full bg-card border border-border/60 shadow-md flex items-center justify-center hover:bg-muted hover:scale-110 active:scale-95 transition-all duration-150"
                            title={sidebarCollapsed ? "Expand sidebar ([)" : "Collapse sidebar ([)"}
                            aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
                        >
                            {sidebarCollapsed ? (
                                <ChevronRight className="w-3 h-3 text-muted-foreground" />
                            ) : (
                                <ChevronLeft className="w-3 h-3 text-muted-foreground" />
                            )}
                        </button>
                        <div className="flex-1 flex flex-col min-h-0">
                            <SidebarHeader collapsed={sidebarCollapsed} />
                            <SidebarSearchButton collapsed={sidebarCollapsed} onOpen={() => setPaletteOpen(true)} />
                            <SidebarContextPanel collapsed={sidebarCollapsed} visible={!isMobile} />
                            <SidebarNav sections={NAV_SECTIONS} collapsed={sidebarCollapsed} badges={badges} />
                            <SidebarFooter collapsed={sidebarCollapsed} username={user?.username} onLogout={handleLogout} onOpenShortcuts={() => useCommandPalette.getState().setShortcutsOpen(true)} />
                        </div>
                    </aside>
                </TooltipProvider>

                <main className="flex-1 min-w-0 overflow-auto w-full">
                    <div className={cn("p-4 md:p-8 space-y-6", terminalWorkspace ? "pb-[calc(72px+env(safe-area-inset-bottom,0px))] md:pb-6" : "pb-24 md:pb-8")}>
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

                <ShortcutsDialog />
                <CommandPalette onToggleSidebar={isMobile ? undefined : toggleSidebar} onLogout={handleLogout} />
                <MobileNav badges={badges} username={user?.username} onLogout={handleLogout} />
            </div>
        )}
        </ConfirmDialogProvider>
        </EventsProvider>
    )
}
