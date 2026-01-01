import { useEffect, useState, useMemo, useCallback } from "react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { HiOutlineRefresh, HiOutlineSearch, HiOutlineX, HiOutlineCog } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { useSettings, useUpdateSettings } from "@/lib/queries"
import { restartServer } from "@/lib/admin-api"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { toast } from "sonner"
import { getApiBaseUrl } from '@/lib/config'
import { DangerZone } from "@/components/settings/danger-zone"
import { categoryMeta } from "@/components/settings/settings-constants"
import { SettingsSidebar, type SidebarItem } from "@/components/settings/settings-sidebar"
import { SettingsCategoryContent } from "@/components/settings/settings-category-content"
import { SettingsSearchResults, buildSearchResults } from "@/components/settings/settings-search-results"
import { SettingsSaveBar } from "@/components/settings/settings-save-bar"
import { SettingsImportExport } from "@/components/settings/settings-import-export"
import { SettingsHeaderMobileMenu } from "@/components/settings/settings-header-mobile-menu"
import { SettingsSkeleton } from "@/components/settings/settings-skeleton"
import type { SettingsGrouped } from "@/lib/domain/setting"

export default function SettingsPage() {
    const { data: serverSettings, isLoading, refetch, isRefetching } = useSettings()
    const updateSettings = useUpdateSettings()
    const confirmDialog = useConfirm()

    const [editedSettings, setEditedSettings] = useState<SettingsGrouped>({})
    const [activeCategory, setActiveCategory] = useState<string>("")
    const [search, setSearch] = useState("")
    const [highlightedKey, setHighlightedKey] = useState<string | null>(null)
    const [restarting, setRestarting] = useState(false)
    const [savingAll, setSavingAll] = useState(false)

    // Sync server data to local edit state
    useEffect(() => {
        if (serverSettings && Object.keys(serverSettings).length > 0) {
            setEditedSettings(serverSettings)
        }
    }, [serverSettings])

    // Sort categories by defined order
    const categories = useMemo(() =>
        Object.keys(editedSettings).sort((a, b) => {
            const orderA = categoryMeta[a]?.order ?? 99
            const orderB = categoryMeta[b]?.order ?? 99
            return orderA - orderB
        }),
        [editedSettings]
    )

    // Default to first category when categories load
    useEffect(() => {
        if (categories.length > 0 && !activeCategory) {
            setActiveCategory(categories[0])
        }
    }, [categories, activeCategory])

    const isCategoryDirty = useCallback((category: string): boolean => {
        if (!serverSettings?.[category]) return false
        const serverList = serverSettings[category]
        const editedList = editedSettings[category] || []
        for (let i = 0; i < serverList.length; i++) {
            if (serverList[i]?.value !== editedList[i]?.value) return true
        }
        return false
    }, [serverSettings, editedSettings])

    // Count dirty categories for save bar
    const dirtyCategories = useMemo(() =>
        categories.filter(c => isCategoryDirty(c)),
        [categories, isCategoryDirty]
    )

    const handleSettingChange = useCallback((category: string, key: string, value: string | boolean) => {
        setEditedSettings(prev => ({
            ...prev,
            [category]: prev[category].map(s =>
                s.key === key ? { ...s, value: value.toString() } : s
            ),
        }))
    }, [])

    const handleResetCategory = useCallback((category: string) => {
        if (!serverSettings?.[category]) return
        setEditedSettings(prev => ({
            ...prev,
            [category]: serverSettings[category],
        }))
    }, [serverSettings])

    const handleDiscardAll = useCallback(() => {
        if (!serverSettings) return
        setEditedSettings(serverSettings)
    }, [serverSettings])

    const saveCategorySettings = useCallback(async (category: string): Promise<boolean> => {
        const categorySettings = editedSettings[category]
        if (!categorySettings) return false

        // Filter out unchanged sensitive fields (still showing masked value)
        const serverList = serverSettings?.[category] || []
        const settingsToSave = categorySettings.filter(s => {
            if (s.sensitive) {
                const serverVal = serverList.find(sv => sv.key === s.key)?.value
                if (serverVal === s.value) return false
            }
            return true
        })

        try {
            await updateSettings.mutateAsync(settingsToSave)

            // Check for restart-required modifications
            const modifiedRestartKeys = settingsToSave.filter(s => {
                if (!s.requires_restart) return false
                const serverVal = serverList.find(sv => sv.key === s.key)?.value
                return serverVal !== s.value
            })

            if (modifiedRestartKeys.length > 0) {
                toast.warning("Server restart required", {
                    description: "Some changes require a server restart to take effect.",
                })
            }
            return true
        } catch {
            return false
        }
    }, [editedSettings, serverSettings, updateSettings])

    const handleSaveCategory = useCallback(async (category: string) => {
        await saveCategorySettings(category)
    }, [saveCategorySettings])

    const handleSaveAll = useCallback(async () => {
        if (dirtyCategories.length === 0) return

        const confirmed = await confirmDialog({
            title: "Save All Changes",
            description: `Save changes across ${dirtyCategories.length} ${dirtyCategories.length === 1 ? "category" : "categories"}?`,
            confirmLabel: "Save All",
        })
        if (!confirmed) return

        setSavingAll(true)
        for (const category of dirtyCategories) {
            await saveCategorySettings(category)
        }
        setSavingAll(false)
    }, [confirmDialog, dirtyCategories, saveCategorySettings])

    const handleNavigateToCategory = useCallback((category: string, key: string) => {
        setSearch("")
        setActiveCategory(category)
        setHighlightedKey(key)
        setTimeout(() => setHighlightedKey(null), 2500)
    }, [])

    const handleRestart = useCallback(async () => {
        const confirmed = await confirmDialog({
            title: "Restart Server",
            description: "This will restart the server. All active connections will be dropped.",
            confirmLabel: "Restart",
            variant: "destructive",
        })
        if (!confirmed) return

        try {
            const result = await restartServer()
            if (!result.success) {
                toast.error("Failed to restart server", { description: result.error })
                return
            }
            setRestarting(true)
            toast.success("Server restart initiated", {
                description: "The page will reload when the server is back.",
            })

            // Poll health endpoint until server is back
            const pollInterval = setInterval(async () => {
                try {
                    const base = getApiBaseUrl()
                    const resp = await fetch(`${base}/health`, { cache: "no-store" })
                    if (resp.ok) {
                        clearInterval(pollInterval)
                        window.location.reload()
                    }
                } catch {
                    // Expected during restart — server is down
                }
            }, 2000)
        } catch {
            // Network error likely means server is already restarting
            setRestarting(true)
            toast.success("Server restart initiated", {
                description: "The page will reload when the server is back.",
            })
            const pollInterval = setInterval(async () => {
                try {
                    const base = getApiBaseUrl()
                    const resp = await fetch(`${base}/health`, { cache: "no-store" })
                    if (resp.ok) {
                        clearInterval(pollInterval)
                        window.location.reload()
                    }
                } catch {
                    // Expected during restart
                }
            }, 2000)
        }
    }, [confirmDialog])

    // Sidebar items
    const sidebarItems: SidebarItem[] = useMemo(() =>
        categories.map(cat => ({
            key: cat,
            label: cat,
            icon: categoryMeta[cat]?.icon || HiOutlineCog,
            dirty: isCategoryDirty(cat),
            settingCount: editedSettings[cat]?.length,
        })),
        [categories, isCategoryDirty, editedSettings]
    )

    // Search results
    const searchResults = useMemo(() =>
        buildSearchResults(editedSettings, serverSettings, search),
        [editedSettings, serverSettings, search]
    )

    const loading = isLoading || isRefetching
    const saving = updateSettings.isPending || savingAll
    const isSearchActive = search.length > 0

    if (isLoading && Object.keys(editedSettings).length === 0) {
        return <SettingsSkeleton />
    }

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            {/* Header */}
            <div className="flex items-start justify-between gap-3 sm:flex-row">
                <div className="min-w-0 flex-1">
                    <h1 className="text-2xl sm:text-3xl font-bold tracking-tight">Settings</h1>
                    <p className="text-muted-foreground mt-1 text-sm sm:text-base">
                        Configure global system settings and preferences
                    </p>
                </div>

                {/* Desktop action row */}
                <div className="hidden md:flex gap-2 self-start flex-wrap">
                    <SettingsImportExport disabled={saving || loading} />
                    <DangerZone disabled={saving || loading} />
                    <Button variant="outline" onClick={handleRestart} disabled={saving || loading || restarting}>
                        <HiOutlineRefresh className={`w-4 h-4 mr-2 ${restarting ? "animate-spin" : ""}`} />
                        Restart Server
                    </Button>
                    <Button variant="outline" onClick={() => refetch()} disabled={saving || loading}>
                        <HiOutlineRefresh className={`w-4 h-4 mr-2 ${loading ? "animate-spin" : ""}`} />
                        Refresh
                    </Button>
                </div>

                {/* Mobile kebab */}
                <div className="md:hidden">
                    <SettingsHeaderMobileMenu
                        onRestart={handleRestart}
                        onRefresh={() => refetch()}
                        restarting={restarting}
                        loading={loading}
                        saving={saving}
                    />
                </div>
            </div>

            {/* Search */}
            <div className="relative max-w-md">
                <HiOutlineSearch className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                <Input
                    placeholder="Search settings by name, key, or description..."
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="pl-9 h-10"
                />
                {search && (
                    <Button
                        variant="ghost"
                        size="icon"
                        className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7"
                        onClick={() => setSearch("")}
                    >
                        <HiOutlineX className="w-3.5 h-3.5" />
                    </Button>
                )}
            </div>

            {/* Mobile category selector — hidden on md+ */}
            {!isSearchActive && (
                <div className="md:hidden">
                    <SettingsSidebar
                        items={sidebarItems}
                        active={activeCategory}
                        onSelect={setActiveCategory}
                        mode="mobile"
                    />
                </div>
            )}

            {/* Main layout */}
            {isSearchActive ? (
                <SettingsSearchResults
                    results={searchResults}
                    onSettingChange={handleSettingChange}
                    onNavigateToCategory={handleNavigateToCategory}
                    highlightedKey={highlightedKey}
                />
            ) : (
                <div className="md:flex md:gap-6">
                    {/* Desktop sidebar — hidden below md */}
                    <div className="hidden md:flex">
                        <SettingsSidebar
                            items={sidebarItems}
                            active={activeCategory}
                            onSelect={setActiveCategory}
                        />
                    </div>

                    {/* Content area */}
                    <div className="flex-1 min-w-0 rounded-xl border bg-card p-4 sm:p-6">
                        {activeCategory && editedSettings[activeCategory] ? (
                            <SettingsCategoryContent
                                category={activeCategory}
                                settings={editedSettings[activeCategory]}
                                serverSettings={serverSettings?.[activeCategory]}
                                isDirty={isCategoryDirty(activeCategory)}
                                isSaving={saving}
                                onSettingChange={handleSettingChange}
                                onSave={() => handleSaveCategory(activeCategory)}
                                onReset={() => handleResetCategory(activeCategory)}
                                highlightedKey={highlightedKey}
                            />
                        ) : (
                            <div className="flex items-center justify-center py-16 text-muted-foreground">
                                Select a category
                            </div>
                        )}
                    </div>
                </div>
            )}

            {/* Floating save bar */}
            <SettingsSaveBar
                dirtyCount={dirtyCategories.length}
                onSaveAll={handleSaveAll}
                onDiscardAll={handleDiscardAll}
                isSaving={saving}
            />
        </div>
    )
}
