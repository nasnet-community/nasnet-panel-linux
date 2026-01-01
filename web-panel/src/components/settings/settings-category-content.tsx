import { useMemo } from "react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { HiOutlineSave, HiOutlineCog, HiOutlineExclamation, HiOutlineTrash } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { categoryMeta, categorySubGroups, getCategoryLabel, notificationChannels, notificationMatrixSections } from "./settings-constants"
import { SettingField } from "./setting-field"
import { NotificationMatrix } from "./notification-matrix"
import { HttpsModeSelector } from "./https-mode-selector"
import { ChangePasswordForm } from "./change-password-card"
import { MaintenancePanel } from "./maintenance-panel"
import { useRetentionStats, useRunRetentionCleanup } from "@/lib/queries/use-settings"
import type { Setting } from "@/lib/domain/setting"
import type { RetentionStat } from "@/lib/api/settings"

interface SettingsCategoryContentProps {
    category: string
    settings: Setting[]
    serverSettings: Setting[] | undefined
    isDirty: boolean
    isSaving: boolean
    onSettingChange: (category: string, key: string, value: string | boolean) => void
    onSave: () => void
    onReset: () => void
    highlightedKey: string | null
}

interface GroupedSettings {
    label: string
    description?: string
    component?: string
    settings: Array<{
        setting: Setting
        isModified: boolean
    }>
}

export function SettingsCategoryContent({
    category,
    settings,
    serverSettings,
    isDirty,
    isSaving,
    onSettingChange,
    onSave,
    onReset,
    highlightedKey,
}: SettingsCategoryContentProps) {
    const meta = categoryMeta[category]
    const Icon = meta?.icon || HiOutlineCog

    if (category === "maintenance") {
        return (
            <div className="space-y-6">
                <div className="flex items-center gap-3">
                    <Icon className="h-6 w-6 text-muted-foreground" />
                    <div>
                        <h2 className="text-xl font-semibold">Maintenance</h2>
                        {meta?.description && (
                            <p className="text-sm text-muted-foreground">{meta.description}</p>
                        )}
                    </div>
                </div>
                <MaintenancePanel settings={settings} />
            </div>
        )
    }

    // Retention stats only fetched for the Data Retention category so other
    // categories don't pay the query cost on mount.
    const isDataCategory = category === "data"
    const { data: retentionStats } = useRetentionStats()
    const runRetentionCleanup = useRunRetentionCleanup()

    // Indexed lookup so the per-field render is O(1) instead of O(N) per card.
    const retentionStatByKey = useMemo(() => {
        if (!isDataCategory || !retentionStats) return new Map<string, RetentionStat>()
        const m = new Map<string, RetentionStat>()
        for (const s of retentionStats) m.set(s.setting_key, s)
        return m
    }, [isDataCategory, retentionStats])

    // Group settings by sub-groups
    const groups = useMemo((): GroupedSettings[] => {
        const subGroups = categorySubGroups[category]
        if (!subGroups || subGroups.length === 0) {
            // No sub-groups: render all settings flat
            return [{
                label: "",
                settings: settings.map(setting => ({
                    setting,
                    isModified: serverSettings?.find(s => s.key === setting.key)?.value !== setting.value,
                })),
            }]
        }

        const grouped: GroupedSettings[] = []
        const usedKeys = new Set<string>()

        for (const sg of subGroups) {
            const groupSettings = sg.keys
                .map(key => settings.find(s => s.key === key))
                .filter((s): s is Setting => s !== undefined)
                .map(setting => ({
                    setting,
                    isModified: serverSettings?.find(s => s.key === setting.key)?.value !== setting.value,
                }))

            if (groupSettings.length > 0 || sg.component) {
                grouped.push({
                    label: sg.label,
                    description: sg.description,
                    component: sg.component,
                    settings: groupSettings,
                })
                sg.keys.forEach(k => usedKeys.add(k))
                // Mark all matrix setting keys as used so they don't appear in "Other"
                if (sg.component === "notification-matrix") {
                    for (const ch of notificationChannels) {
                        for (const section of notificationMatrixSections) {
                            for (const event of section.events) {
                                usedKeys.add(`notification_${ch.key}_${event.key}`)
                            }
                        }
                    }
                }
            }
        }

        // Collect ungrouped settings
        const ungrouped = settings
            .filter(s => !usedKeys.has(s.key))
            .map(setting => ({
                setting,
                isModified: serverSettings?.find(s => s.key === setting.key)?.value !== setting.value,
            }))

        if (ungrouped.length > 0) {
            grouped.push({
                label: "Other",
                settings: ungrouped,
            })
        }

        return grouped
    }, [category, settings, serverSettings])

    return (
        <div className="space-y-4">
            {/* Category header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-primary/10">
                    <Icon className="w-5 h-5 text-primary" />
                </div>
                <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                        <h2 className="text-lg font-semibold">{getCategoryLabel(category)} Settings</h2>
                        <Badge variant="secondary" className="text-xs">
                            {settings.length}
                        </Badge>
                        {isDirty && (
                            <Badge variant="warning" className="text-xs">Modified</Badge>
                        )}
                    </div>
                    {meta?.description && (
                        <p className="text-sm text-muted-foreground mt-0.5">{meta.description}</p>
                    )}
                </div>
                {/* Manual cleanup trigger — only on the Data Retention category.
                    Bypasses the 6h scheduler cadence so admins can apply a new
                    retention window immediately. The toast reports total deleted
                    rows; the retention-stats query is invalidated on success. */}
                {isDataCategory && (
                    <Button
                        variant="outline"
                        size="sm"
                        className="shrink-0"
                        disabled={runRetentionCleanup.isPending}
                        onClick={() => runRetentionCleanup.mutate()}
                        title="Run the retention sweep now instead of waiting for the next scheduled tick."
                    >
                        {runRetentionCleanup.isPending ? (
                            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        ) : (
                            <HiOutlineTrash className="w-4 h-4 mr-2" />
                        )}
                        Clean now
                    </Button>
                )}
            </div>

            <Separator />

            {/* Outbound proxy: toggles on but URL empty/invalid */}
            {category === "server" && (() => {
                const proxyURL = (settings.find(s => s.key === "outbound_proxy_url")?.value || "").trim()
                const proxyKeys = settings.filter(s => s.key.startsWith("proxy_use_"))
                const anyOn = proxyKeys.some(s => s.value === "true")
                if (!anyOn || proxyURL !== "") return null
                return (
                    <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 px-4 py-3">
                        <div className="flex items-start gap-3">
                            <HiOutlineExclamation className="h-5 w-5 text-yellow-600 dark:text-yellow-400 mt-0.5 shrink-0" />
                            <div className="flex-1 min-w-0">
                                <p className="text-sm font-medium text-yellow-700 dark:text-yellow-400">
                                    Outbound proxy URL is empty
                                </p>
                                <p className="text-xs text-yellow-600/80 dark:text-yellow-400/70 mt-0.5">
                                    One or more features are toggled to use the outbound proxy, but <code>outbound_proxy_url</code> is empty.
                                    Those features will fall back to direct connections (a warning will appear in the hub log).
                                </p>
                            </div>
                        </div>
                    </div>
                )
            })()}

            {/* Protocol mismatch warning for server category */}
            {category === "server" && (() => {
                const acmeOn = settings.find(s => s.key === "acme_enabled")?.value === "true"
                const hasCerts = !!(settings.find(s => s.key === "tls_cert_file")?.value && settings.find(s => s.key === "tls_key_file")?.value)
                const tlsConfigured = acmeOn || hasCerts
                const urlKeys = ["app_base_url", "sub_panel_url"]
                const httpUrls = urlKeys.filter(k => (settings.find(s => s.key === k)?.value || "").startsWith("http://"))
                if (!tlsConfigured || httpUrls.length === 0) return null
                return (
                    <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 px-4 py-3">
                        <div className="flex items-start gap-3">
                            <HiOutlineExclamation className="h-5 w-5 text-yellow-600 dark:text-yellow-400 mt-0.5 shrink-0" />
                            <div className="flex-1 min-w-0">
                                <p className="text-sm font-medium text-yellow-700 dark:text-yellow-400">
                                    URLs use HTTP but TLS is configured
                                </p>
                                <p className="text-xs text-yellow-600/80 dark:text-yellow-400/70 mt-0.5">
                                    Your server will serve HTTPS, but {httpUrls.length === 3 ? "all URLs" : httpUrls.length === 1 ? "one URL" : `${httpUrls.length} URLs`} still use http://. Subscription links and agent connections may fail.
                                </p>
                            </div>
                            <Button
                                size="sm"
                                variant="outline"
                                className="shrink-0 text-xs border-yellow-500/30 text-yellow-700 dark:text-yellow-400 hover:bg-yellow-500/10"
                                onClick={() => {
                                    for (const key of httpUrls) {
                                        const val = settings.find(s => s.key === key)?.value || ""
                                        if (val.startsWith("http://")) {
                                            onSettingChange("server", key, val.replace("http://", "https://"))
                                        }
                                    }
                                }}
                            >
                                Switch to HTTPS
                            </Button>
                        </div>
                    </div>
                )
            })()}

            {/* Setting fields grouped by sub-groups */}
            <div className="space-y-6">
                {groups.map((group, idx) => (
                    <div key={group.label || idx}>
                        {group.label && (
                            <div className="border-l-2 border-primary/20 pl-4 mb-3">
                                <h3 className="text-sm font-semibold text-foreground">{group.label}</h3>
                                {group.description && (
                                    <p className="text-xs text-muted-foreground mt-0.5">{group.description}</p>
                                )}
                            </div>
                        )}
                        {group.component === "notification-matrix" ? (
                            <NotificationMatrix
                                settings={settings}
                                serverSettings={serverSettings}
                                onSettingChange={onSettingChange}
                            />
                        ) : group.component === "https-mode" ? (
                            <HttpsModeSelector
                                settings={settings}
                                serverSettings={serverSettings}
                                highlightedKey={highlightedKey}
                                onSettingChange={onSettingChange}
                            />
                        ) : (
                            <div className="space-y-2">
                                {group.settings.map(({ setting, isModified }) => (
                                    <SettingField
                                        key={setting.key}
                                        setting={setting}
                                        isModified={isModified}
                                        isHighlighted={highlightedKey === setting.key}
                                        onChange={(key, value) => onSettingChange(category, key, value)}
                                        retentionStat={retentionStatByKey.get(setting.key)}
                                    />
                                ))}
                            </div>
                        )}
                    </div>
                ))}
            </div>

            {/* Change Password form for security category */}
            {category === "security" && (
                <>
                    <Separator />
                    <div className="space-y-3">
                        <h3 className="text-sm font-semibold">Change Password</h3>
                        <ChangePasswordForm />
                    </div>
                </>
            )}

            {/* Footer: Reset + Save */}
            <Separator />
            <div className="flex items-center justify-end gap-2 pt-1">
                <Button
                    variant="ghost"
                    size="sm"
                    onClick={onReset}
                    disabled={!isDirty || isSaving}
                >
                    Reset
                </Button>
                <Button
                    size="sm"
                    onClick={onSave}
                    disabled={!isDirty || isSaving}
                >
                    {isSaving ? (
                        <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    ) : (
                        <HiOutlineSave className="w-4 h-4 mr-2" />
                    )}
                    Save {getCategoryLabel(category)}
                </Button>
            </div>
        </div>
    )
}
