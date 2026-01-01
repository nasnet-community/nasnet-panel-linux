import { useState, useMemo } from "react"
import { Button } from "@/components/ui/button"
import { HiOutlineExclamation, HiOutlineLockClosed, HiOutlineLockOpen } from "react-icons/hi"
import { SettingField } from "./setting-field"
import { TLSTestButton } from "./tls-test-button"
import type { Setting } from "@/lib/domain/setting"

type HttpsMode = "disabled" | "acme" | "manual"

interface HttpsModeSelectorProps {
    settings: Setting[]
    serverSettings: Setting[] | undefined
    highlightedKey: string | null
    onSettingChange: (category: string, key: string, value: string | boolean) => void
}

const ACME_KEYS = ["acme_email", "acme_staging", "acme_auto_renew"]
const MANUAL_KEYS = ["tls_cert_file", "tls_key_file"]
const ALL_HTTPS_KEYS = ["acme_enabled", ...ACME_KEYS, ...MANUAL_KEYS]

function extractDomain(url: string): string {
    try {
        const u = new URL(url)
        return u.hostname
    } catch {
        return ""
    }
}

export function HttpsModeSelector({ settings, serverSettings, highlightedKey, onSettingChange }: HttpsModeSelectorProps) {
    const getSetting = (key: string) => settings.find(s => s.key === key)
    const getVal = (key: string) => getSetting(key)?.value || ""

    // Derive mode from current settings
    const currentMode = useMemo((): HttpsMode => {
        if (getVal("acme_enabled") === "true") return "acme"
        if (getVal("tls_cert_file") && getVal("tls_key_file")) return "manual"
        return "disabled"
    }, [settings])

    // Allow user override of the displayed mode (before save)
    const [overrideMode, setOverrideMode] = useState<HttpsMode | null>(null)
    const mode = overrideMode ?? currentMode

    const [showUrlPrompt, setShowUrlPrompt] = useState(false)

    // Check if URLs use http://
    const urlKeys = ["app_base_url", "sub_panel_url"]
    const httpUrls = urlKeys.filter(k => getVal(k).startsWith("http://"))
    const hasHttpUrls = httpUrls.length > 0

    const handleModeChange = (newMode: HttpsMode) => {
        setOverrideMode(newMode)

        if (newMode === "disabled") {
            onSettingChange("server", "acme_enabled", false)
            onSettingChange("server", "tls_cert_file", "")
            onSettingChange("server", "tls_key_file", "")
            setShowUrlPrompt(false)
        } else if (newMode === "acme") {
            onSettingChange("server", "acme_enabled", true)
            onSettingChange("server", "tls_cert_file", "")
            onSettingChange("server", "tls_key_file", "")
            if (hasHttpUrls) setShowUrlPrompt(true)
        } else if (newMode === "manual") {
            onSettingChange("server", "acme_enabled", false)
            if (hasHttpUrls) setShowUrlPrompt(true)
        }
    }

    const switchUrlsToHttps = () => {
        for (const key of urlKeys) {
            const val = getVal(key)
            if (val.startsWith("http://")) {
                onSettingChange("server", key, val.replace("http://", "https://"))
            }
        }
        setShowUrlPrompt(false)
    }

    const isModified = (key: string) => {
        const server = serverSettings?.find(s => s.key === key)
        const local = settings.find(s => s.key === key)
        return server?.value !== local?.value
    }

    // Domain mismatch detection for TLS test results
    const certDomains = useMemo(() => {
        return [] as string[]
    }, [])

    return (
        <div className="space-y-4">
            {/* Mode selector */}
            <div className="flex flex-col sm:flex-row sm:items-center gap-1 p-1 rounded-lg bg-muted/50 border border-border/50">
                {([
                    { value: "disabled" as HttpsMode, label: "Disabled", icon: HiOutlineLockOpen },
                    { value: "acme" as HttpsMode, label: "ACME (Let's Encrypt)", icon: HiOutlineLockClosed },
                    { value: "manual" as HttpsMode, label: "Manual Certificate", icon: HiOutlineLockClosed },
                ]).map(({ value, label, icon: Icon }) => (
                    <button
                        key={value}
                        onClick={() => handleModeChange(value)}
                        className={`flex-1 flex items-center justify-center gap-2 px-3 py-2 rounded-md text-sm font-medium whitespace-nowrap transition-all ${
                            mode === value
                                ? "bg-background text-foreground shadow-sm border border-border/80"
                                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                        }`}
                    >
                        <Icon className="h-4 w-4 shrink-0" />
                        <span>{label}</span>
                    </button>
                ))}
            </div>

            {/* URL update prompt */}
            {showUrlPrompt && hasHttpUrls && (
                <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 px-4 py-3">
                    <div className="flex items-start gap-3">
                        <HiOutlineExclamation className="h-5 w-5 text-yellow-600 dark:text-yellow-400 mt-0.5 shrink-0" />
                        <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium text-yellow-700 dark:text-yellow-400">
                                Your URLs still use HTTP
                            </p>
                            <p className="text-xs text-yellow-600/80 dark:text-yellow-400/70 mt-0.5">
                                {httpUrls.length === 1 ? `${httpUrls[0].replace(/_/g, " ")} uses` : `${httpUrls.length} URLs use`} http:// but the server will serve HTTPS. Subscription links and agent connections may fail.
                            </p>
                        </div>
                        <div className="flex gap-2 shrink-0">
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => setShowUrlPrompt(false)}
                                className="text-xs"
                            >
                                Skip
                            </Button>
                            <Button
                                size="sm"
                                onClick={switchUrlsToHttps}
                                className="text-xs"
                            >
                                Switch to HTTPS
                            </Button>
                        </div>
                    </div>
                </div>
            )}

            {/* ACME settings */}
            {mode === "acme" && (
                <div className="space-y-2 pl-1">
                    {ACME_KEYS.map(key => {
                        const setting = getSetting(key)
                        if (!setting) return null
                        return (
                            <SettingField
                                key={key}
                                setting={setting}
                                isModified={isModified(key)}
                                isHighlighted={highlightedKey === key}
                                onChange={(k, v) => onSettingChange("server", k, v)}
                            />
                        )
                    })}
                </div>
            )}

            {/* Manual TLS settings */}
            {mode === "manual" && (
                <div className="space-y-2 pl-1">
                    {MANUAL_KEYS.map(key => {
                        const setting = getSetting(key)
                        if (!setting) return null
                        return (
                            <SettingField
                                key={key}
                                setting={setting}
                                isModified={isModified(key)}
                                isHighlighted={highlightedKey === key}
                                onChange={(k, v) => onSettingChange("server", k, v)}
                            />
                        )
                    })}
                    <TLSTestButton settings={settings} onSettingChange={onSettingChange} />
                </div>
            )}

            {/* Disabled state message */}
            {mode === "disabled" && (
                <p className="text-xs text-muted-foreground pl-1">
                    The server will listen on plain HTTP. Use a reverse proxy (nginx, Caddy) for HTTPS, or enable ACME/manual certificates above.
                </p>
            )}
        </div>
    )
}

// Keys consumed by this component — used to hide them from default rendering
HttpsModeSelector.CONSUMED_KEYS = ALL_HTTPS_KEYS
