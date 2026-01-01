import { useState } from "react"
import { Copy, Check, Download } from "lucide-react"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { toast } from "sonner"
import { copyToClipboard } from "@/lib/utils"
import { motion } from "framer-motion"
import { QRCodeSVG } from "qrcode.react"

import v2rayngIcon from "@/assets/app-icons/v2rayng.png"
import v2raynIcon from "@/assets/app-icons/v2rayn.png"
import shadowrocketIcon from "@/assets/app-icons/shadowrocket.png"
import streisandIcon from "@/assets/app-icons/streisand.png"
import v2boxIcon from "@/assets/app-icons/v2box.png"
import quantumultxIcon from "@/assets/app-icons/quantumultx.png"

type Platform = "android" | "ios" | "desktop"

interface AppInfo {
    id: string
    name: string
    platform: "android" | "ios" | "desktop"
    store: string
    icon: string
    fallbackColor: string
    downloadUrl?: string
    buildLink: (subscriptionUrl: string, label: string) => string
}

type DesktopOS = "windows" | "macos" | "linux" | "unknown"

function detectDesktopOS(): DesktopOS {
    if (typeof navigator === "undefined") return "unknown"
    const platform = navigator.platform || ""
    if (/Win/i.test(platform)) return "windows"
    if (/Mac/i.test(platform)) return "macos"
    if (/Linux/i.test(platform)) return "linux"
    return "unknown"
}

const V2RAYN_DOWNLOADS: Record<DesktopOS, string> = {
    windows: "https://github.com/2dust/v2rayN/releases/download/7.19.5/v2rayN-windows-64-desktop.zip",
    macos: "https://github.com/2dust/v2rayN/releases/download/7.19.5/v2rayN-macos-arm64.dmg",
    linux: "https://github.com/2dust/v2rayN/releases/download/7.19.5/v2rayN-linux-64.deb",
    unknown: "https://github.com/2dust/v2rayN/releases/download/7.19.5/v2rayN-windows-64-desktop.zip",
}

const APPS: AppInfo[] = [
    {
        id: "v2rayng",
        name: "v2rayNG",
        platform: "android",
        store: "GitHub APK",
        icon: v2rayngIcon,
        fallbackColor: "from-green-400 to-green-600",
        downloadUrl: "https://github.com/2dust/v2rayNG/releases/download/2.0.15/v2rayNG_2.0.15_arm64-v8a.apk",
        buildLink: (url, label) =>
            `v2rayng://install-sub/?url=${encodeURIComponent(url)}#${encodeURIComponent(label)}`,
    },
    {
        id: "v2rayn",
        name: "v2rayN",
        platform: "desktop",
        store: "GitHub · Win / macOS / Linux",
        icon: v2raynIcon,
        fallbackColor: "from-purple-400 to-purple-600",
        downloadUrl: V2RAYN_DOWNLOADS[detectDesktopOS()],
        buildLink: (url, label) =>
            `v2rayn://install-sub/?url=${encodeURIComponent(url)}#${encodeURIComponent(label)}`,
    },
    {
        id: "shadowrocket",
        name: "Shadowrocket",
        platform: "ios",
        store: "App Store · $2.99",
        icon: shadowrocketIcon,
        fallbackColor: "from-indigo-400 to-indigo-600",
        buildLink: (url) => `sub://${btoa(unescape(encodeURIComponent(url)))}`,
    },
    {
        id: "streisand",
        name: "Streisand",
        platform: "ios",
        store: "App Store · Free",
        icon: streisandIcon,
        fallbackColor: "from-pink-400 to-pink-600",
        buildLink: (url, label) => `streisand://import/${url}#${encodeURIComponent(label)}`,
    },
    {
        id: "v2box",
        name: "V2Box",
        platform: "ios",
        store: "App Store · Free",
        icon: v2boxIcon,
        fallbackColor: "from-amber-400 to-amber-600",
        buildLink: (url, label) =>
            `v2box://install-sub?url=${encodeURIComponent(url)}&name=${encodeURIComponent(label)}`,
    },
    {
        id: "quantumultx",
        name: "Quantumult X",
        platform: "ios",
        store: "App Store · $9.99",
        icon: quantumultxIcon,
        fallbackColor: "from-cyan-400 to-cyan-600",
        buildLink: (url, label) => {
            const json = JSON.stringify({ server_remote: [`${url}#${label}`] })
            return `quantumult-x:///add-resource?remote-resource=${encodeURIComponent(json)}`
        },
    },
]

function detectPlatform(): Platform {
    if (typeof navigator === "undefined") return "desktop"
    const ua = navigator.userAgent
    if (/Android/i.test(ua)) return "android"
    if (/iPhone|iPad|iPod/i.test(ua)) return "ios"
    return "desktop"
}

const PLATFORM = detectPlatform()

const RECOMMENDED: Record<Platform, string | null> = {
    android: "v2rayng",
    ios: "streisand",
    desktop: "v2rayn",
}

// --- Sub-components ---

function AppIcon({ app, size = "default" }: { app: AppInfo; size?: "hero" | "compact" | "default" }) {
    const [failed, setFailed] = useState(false)

    const sizeClasses = {
        hero: "w-12 h-12 rounded-xl",
        default: "w-10 h-10 rounded-[10px]",
        compact: "w-[26px] h-[26px] rounded-[7px]",
    }
    const textSizes = { hero: "text-lg", default: "text-base", compact: "text-[10px]" }
    const cls = sizeClasses[size]

    if (failed) {
        return (
            <div
                className={`${cls} bg-gradient-to-br ${app.fallbackColor} flex items-center justify-center text-white ${textSizes[size]} font-bold shrink-0`}
                style={size === "hero" ? { boxShadow: "0 4px 12px rgba(52,211,153,0.2)" } : undefined}
            >
                {app.name[0]}
            </div>
        )
    }

    return (
        <img
            src={app.icon}
            alt={app.name}
            className={`${cls} object-cover shrink-0`}
            style={size === "hero" ? { boxShadow: "0 4px 12px rgba(52,211,153,0.2)" } : undefined}
            onError={() => setFailed(true)}
        />
    )
}

function HeroCard({
    app,
    subscriptionUrl,
    label,
    showBadge,
}: {
    app: AppInfo
    subscriptionUrl: string
    label: string
    showBadge: boolean
}) {
    const handleImport = () => {
        window.location.href = app.buildLink(subscriptionUrl, label)
    }

    return (
        <motion.div
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ type: "spring", stiffness: 300, damping: 24 }}
        >
            <button
                type="button"
                onClick={handleImport}
                className="w-full text-left rounded-2xl border border-emerald-500/15 p-3.5"
                style={{
                    background:
                        "linear-gradient(135deg, rgba(52,211,153,0.1), rgba(52,211,153,0.02))",
                }}
            >
                <div className="flex items-center gap-3 mb-3">
                    <AppIcon app={app} size="hero" />
                    <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-1.5">
                            <span className="text-sm font-bold">{app.name}</span>
                            {showBadge && (
                                <span className="text-[7px] font-semibold uppercase tracking-wide bg-emerald-500/15 text-emerald-400 px-1.5 py-0.5 rounded">
                                    Best for {app.platform === "android" ? "Android" : app.platform === "ios" ? "iOS" : "Desktop"}
                                </span>
                            )}
                        </div>
                        <div className="text-[10px] text-muted-foreground/40 mt-0.5">
                            {app.store}
                        </div>
                    </div>
                </div>
                <div className="flex gap-2">
                    <div className="flex-1 text-center text-xs font-semibold text-emerald-400 bg-emerald-500/15 rounded-[10px] py-2.5 tracking-wide">
                        Import Subscription
                    </div>
                    {app.downloadUrl && (
                        <a
                            href={app.downloadUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={(e) => e.stopPropagation()}
                            className="flex items-center gap-1.5 px-3 text-xs font-semibold text-muted-foreground/50 bg-white/[0.05] rounded-[10px] py-2.5 tracking-wide hover:text-muted-foreground/70 hover:bg-white/[0.08] transition-colors"
                        >
                            <Download className="w-3.5 h-3.5" />
                            App
                        </a>
                    )}
                </div>
            </button>
        </motion.div>
    )
}

function CompactAppTile({
    app,
    subscriptionUrl,
    label,
    index,
}: {
    app: AppInfo
    subscriptionUrl: string
    label: string
    index: number
}) {
    const handleImport = () => {
        window.location.href = app.buildLink(subscriptionUrl, label)
    }

    return (
        <motion.div
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ type: "spring", stiffness: 300, damping: 24, delay: index * 0.04 }}
            className="flex-1 flex flex-col items-center gap-1 rounded-xl border border-white/[0.04] bg-white/[0.025] p-2.5 opacity-60 hover:opacity-80 transition-all"
        >
            <button
                type="button"
                onClick={handleImport}
                className="flex flex-col items-center gap-1 active:scale-[0.97] transition-transform"
            >
                <AppIcon app={app} size="compact" />
                <span className="text-[8px] text-muted-foreground/45 font-medium">
                    {app.name.length > 10 ? app.name.slice(0, 8) + "..." : app.name}
                </span>
                <span className="text-[7px] text-muted-foreground/20">
                    {app.platform === "android" ? "Android" : app.platform === "ios" ? "iOS" : "Desktop"}
                </span>
            </button>
            {app.downloadUrl && (
                <a
                    href={app.downloadUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-0.5 text-[7px] text-muted-foreground/30 hover:text-muted-foreground/50 transition-colors mt-0.5"
                >
                    <Download className="w-2.5 h-2.5" />
                    Download
                </a>
            )}
        </motion.div>
    )
}

// --- Main component ---

interface ImportToAppSheetProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    subscriptionUrl: string
    label: string
}

export function ImportToAppSheet({
    open,
    onOpenChange,
    subscriptionUrl,
    label,
}: ImportToAppSheetProps) {
    const platform = PLATFORM
    const [copied, setCopied] = useState(false)

    const recommendedId = RECOMMENDED[platform]
    const recommendedApp = recommendedId
        ? APPS.find((a) => a.id === recommendedId) ?? null
        : null
    const samePlatformApps = APPS.filter(
        (a) => a.platform === platform && a.id !== recommendedId
    )
    const otherPlatformApps = APPS.filter((a) => a.platform !== platform)

    const handleCopyLink = async () => {
        const ok = await copyToClipboard(subscriptionUrl)
        if (!ok) {
            toast.error("Failed to copy link")
            return
        }
        setCopied(true)
        toast.success("Subscription link copied")
        setTimeout(() => setCopied(false), 2000)
    }

    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent
                side="bottom"
                className="rounded-t-2xl [&>button:last-child]:hidden"
                onPointerDownOutside={() => onOpenChange(false)}
            >
                <SheetHeader className="pb-1">
                    <div className="flex items-center justify-between">
                        <div>
                            <SheetTitle className="text-[15px]">Import to App</SheetTitle>
                            <SheetDescription className="text-[10px]">
                                Select your app to import
                            </SheetDescription>
                        </div>
                        <span
                            className={`text-[9px] font-semibold px-2.5 py-1 rounded-full ${
                                platform === "android"
                                    ? "text-emerald-400 bg-emerald-500/10 border border-emerald-500/15"
                                    : platform === "ios"
                                      ? "text-blue-400 bg-blue-500/10 border border-blue-500/20"
                                      : "text-purple-400 bg-purple-500/10 border border-purple-500/20"
                            }`}
                        >
                            {platform === "android" ? "Android" : platform === "ios" ? "iOS" : "Desktop"}
                        </span>
                    </div>
                </SheetHeader>

                <div className="px-4 pb-6 space-y-3">
                    {/* Recommended hero card */}
                    {recommendedApp && (
                        <HeroCard
                            app={recommendedApp}
                            subscriptionUrl={subscriptionUrl}
                            label={label}
                            showBadge
                        />
                    )}

                    {/* Other same-platform apps */}
                    {samePlatformApps.length > 0 && (
                        <div className="flex gap-1.5">
                            {samePlatformApps.map((app, i) => (
                                <CompactAppTile
                                    key={app.id}
                                    app={app}
                                    subscriptionUrl={subscriptionUrl}
                                    label={label}
                                    index={i}
                                />
                            ))}
                        </div>
                    )}

                    {/* Cross-platform compact tiles */}
                    {otherPlatformApps.length > 0 && (
                        <div className="flex gap-1.5 flex-wrap">
                            {otherPlatformApps.map((app, i) => (
                                <CompactAppTile
                                    key={app.id}
                                    app={app}
                                    subscriptionUrl={subscriptionUrl}
                                    label={label}
                                    index={i}
                                />
                            ))}
                        </div>
                    )}

                    {/* Divider */}
                    <div className="h-px bg-gradient-to-r from-transparent via-white/[0.06] to-transparent" />

                    {/* QR + Copy section */}
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        transition={{ delay: 0.1, duration: 0.3 }}
                        className="flex gap-3"
                    >
                        <div className="shrink-0 rounded-xl border border-white/[0.05] bg-white/[0.03] p-2 flex items-center justify-center">
                            <div className="bg-white rounded-sm p-1">
                                <QRCodeSVG
                                    value={subscriptionUrl}
                                    size={56}
                                    level="M"
                                    bgColor="#ffffff"
                                    fgColor="#000000"
                                />
                            </div>
                        </div>
                        <div className="flex-1 flex flex-col justify-center gap-1.5 min-w-0">
                            <span className="text-[10px] text-muted-foreground/30 font-medium">
                                Manual Import
                            </span>
                            <span className="text-[8px] text-muted-foreground/20 font-mono truncate">
                                {subscriptionUrl}
                            </span>
                            <button
                                type="button"
                                onClick={handleCopyLink}
                                className="flex items-center justify-center gap-1.5 rounded-lg border border-white/[0.06] bg-white/[0.05] py-1.5 text-[9px] text-muted-foreground/45 font-medium hover:bg-white/[0.08] transition-colors"
                            >
                                {copied ? (
                                    <Check className="w-3 h-3 text-emerald-500" />
                                ) : (
                                    <Copy className="w-3 h-3" />
                                )}
                                {copied ? "Copied!" : "Copy Link"}
                            </button>
                        </div>
                    </motion.div>
                </div>
            </SheetContent>
        </Sheet>
    )
}
