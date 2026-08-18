import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { KeyboardEvent as ReactKeyboardEvent, ReactNode } from "react"
import { Check, ChevronRight, Copy, Download, ExternalLink } from "lucide-react"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { toast } from "sonner"
import { copyToClipboard, cn } from "@/lib/utils"
import { motion, useReducedMotion } from "framer-motion"
import { QRCodeSVG } from "qrcode.react"
import { QRDialog } from "./qr-dialog"

import v2rayngIcon from "@/assets/app-icons/v2rayng.png"
import v2raynIcon from "@/assets/app-icons/v2rayn.png"
import shadowrocketIcon from "@/assets/app-icons/shadowrocket.png"
import streisandIcon from "@/assets/app-icons/streisand.png"
import v2boxIcon from "@/assets/app-icons/v2box.png"
import quantumultxIcon from "@/assets/app-icons/quantumultx.png"

type Platform = "android" | "ios" | "desktop"
type DesktopOS = "windows" | "macos" | "linux" | "unknown"

interface AppInfo {
    id: string
    name: string
    platform: Platform
    /** Where the app comes from — "App Store", "GitHub". */
    store: string
    /** What it costs — "Free", "$2.99". */
    price: string
    icon: string
    fallbackColor: string
    /** Lazy so it can read `navigator` at render time, not import time. */
    getUrl?: () => string
    buildLink: (subscriptionUrl: string, label: string) => string
}

const PLATFORM_LABEL: Record<Platform, string> = {
    ios: "iOS",
    android: "Android",
    desktop: "Desktop",
}

const PLATFORM_ORDER: Platform[] = ["ios", "android", "desktop"]

const STORAGE_KEY = "sub-panel:import-platform"

function detectDesktopOS(): DesktopOS {
    if (typeof navigator === "undefined") return "unknown"
    const uaData = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData
    const src = uaData?.platform || navigator.userAgent || ""
    if (/Win/i.test(src)) return "windows"
    if (/Mac/i.test(src)) return "macos"
    if (/Linux|X11/i.test(src)) return "linux"
    return "unknown"
}

function detectPlatform(): Platform {
    if (typeof navigator === "undefined") return "desktop"
    const ua = navigator.userAgent
    if (/Android/i.test(ua)) return "android"
    if (/iPhone|iPad|iPod/i.test(ua)) return "ios"
    // iPadOS 13+ reports itself as a Mac — touch points disambiguate.
    if (/Mac/i.test(ua) && navigator.maxTouchPoints > 1) return "ios"
    return "desktop"
}

function readStoredPlatform(): Platform | null {
    try {
        const v = window.localStorage.getItem(STORAGE_KEY)
        return v === "ios" || v === "android" || v === "desktop" ? v : null
    } catch {
        return null
    }
}

function storePlatform(p: Platform) {
    try {
        window.localStorage.setItem(STORAGE_KEY, p)
    } catch {
        // Private mode / storage disabled — the choice just won't persist.
    }
}

function toBase64(input: string): string {
    const bytes = new TextEncoder().encode(input)
    let bin = ""
    bytes.forEach((b) => {
        bin += String.fromCharCode(b)
    })
    return btoa(bin)
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
        store: "GitHub",
        price: "Free",
        icon: v2rayngIcon,
        fallbackColor: "from-green-400 to-green-600",
        getUrl: () => "https://github.com/2dust/v2rayNG/releases/download/2.0.15/v2rayNG_2.0.15_arm64-v8a.apk",
        buildLink: (url, label) =>
            `v2rayng://install-sub/?url=${encodeURIComponent(url)}#${encodeURIComponent(label)}`,
    },
    {
        id: "v2rayn",
        name: "v2rayN",
        platform: "desktop",
        store: "GitHub",
        price: "Free",
        icon: v2raynIcon,
        fallbackColor: "from-purple-400 to-purple-600",
        getUrl: () => V2RAYN_DOWNLOADS[detectDesktopOS()],
        buildLink: (url, label) =>
            `v2rayn://install-sub/?url=${encodeURIComponent(url)}#${encodeURIComponent(label)}`,
    },
    {
        id: "streisand",
        name: "Streisand",
        platform: "ios",
        store: "App Store",
        price: "Free",
        icon: streisandIcon,
        fallbackColor: "from-pink-400 to-pink-600",
        getUrl: () => "https://apps.apple.com/app/streisand/id6450534064",
        buildLink: (url, label) => `streisand://import/${url}#${encodeURIComponent(label)}`,
    },
    {
        id: "shadowrocket",
        name: "Shadowrocket",
        platform: "ios",
        store: "App Store",
        price: "$2.99",
        icon: shadowrocketIcon,
        fallbackColor: "from-indigo-400 to-indigo-600",
        getUrl: () => "https://apps.apple.com/app/shadowrocket/id932747118",
        buildLink: (url) => `sub://${toBase64(url)}`,
    },
    {
        id: "v2box",
        name: "V2Box",
        platform: "ios",
        store: "App Store",
        price: "Free",
        icon: v2boxIcon,
        fallbackColor: "from-amber-400 to-amber-600",
        getUrl: () => "https://apps.apple.com/app/v2box-v2ray-client/id6446814690",
        buildLink: (url, label) =>
            `v2box://install-sub?url=${encodeURIComponent(url)}&name=${encodeURIComponent(label)}`,
    },
    {
        id: "quantumultx",
        name: "Quantumult X",
        platform: "ios",
        store: "App Store",
        price: "$9.99",
        icon: quantumultxIcon,
        fallbackColor: "from-cyan-400 to-cyan-600",
        getUrl: () => "https://apps.apple.com/app/quantumult-x/id1443988620",
        buildLink: (url, label) => {
            const json = JSON.stringify({ server_remote: [`${url}#${label}`] })
            return `quantumult-x:///add-resource?remote-resource=${encodeURIComponent(json)}`
        },
    },
]

const RECOMMENDED: Record<Platform, string> = {
    android: "v2rayng",
    ios: "streisand",
    desktop: "v2rayn",
}

function getAppLabel(app: AppInfo): string {
    return app.platform === "ios"
        ? "Get on App Store"
        : app.platform === "android"
          ? "Download APK"
          : "Download app"
}

const FOCUS_RING =
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 focus-visible:ring-offset-2 focus-visible:ring-offset-background"

// --- Sub-components ---

function AppIcon({ app, size }: { app: AppInfo; size: "hero" | "row" }) {
    const [failed, setFailed] = useState(false)

    const cls = size === "hero" ? "w-12 h-12 rounded-xl" : "w-8 h-8 rounded-lg"

    if (failed) {
        return (
            <div
                className={cn(
                    cls,
                    "bg-gradient-to-br flex items-center justify-center text-white font-bold shrink-0",
                    app.fallbackColor,
                    size === "hero" ? "text-lg" : "text-sm"
                )}
            >
                {app.name[0]}
            </div>
        )
    }

    return (
        <img
            src={app.icon}
            alt=""
            aria-hidden="true"
            className={cn(cls, "object-cover shrink-0")}
            onError={() => setFailed(true)}
        />
    )
}

function SectionLabel({ children }: { children: ReactNode }) {
    return (
        <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground px-1 mb-2">
            {children}
        </h3>
    )
}

/** Shown when a deep link fired but the browser never lost focus. */
function ImportFallback({
    app,
    onCopy,
    copied,
}: {
    app: AppInfo
    onCopy: () => void
    copied: boolean
}) {
    const url = app.getUrl?.()
    return (
        <div className="rounded-xl border border-amber-500/25 bg-amber-500/[0.07] px-3 py-2.5">
            <p className="text-[13px] leading-snug text-foreground/85">
                Nothing opened. {app.name} may not be installed on this device.
            </p>
            <div className="mt-2 flex flex-wrap items-center gap-2">
                {url && (
                    <a
                        href={url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className={cn(
                            "inline-flex items-center gap-1.5 rounded-lg border border-border/60 bg-background/40 px-3 py-2 text-xs font-medium text-foreground/80 hover:text-foreground hover:bg-background/70 transition-colors",
                            FOCUS_RING
                        )}
                    >
                        <ExternalLink className="w-3.5 h-3.5" />
                        {getAppLabel(app)}
                    </a>
                )}
                <button
                    type="button"
                    onClick={onCopy}
                    className={cn(
                        "inline-flex items-center gap-1.5 rounded-lg border border-border/60 bg-background/40 px-3 py-2 text-xs font-medium text-foreground/80 hover:text-foreground hover:bg-background/70 transition-colors",
                        FOCUS_RING
                    )}
                >
                    {copied ? <Check className="w-3.5 h-3.5 text-emerald-500" /> : <Copy className="w-3.5 h-3.5" />}
                    {copied ? "Copied" : "Copy link instead"}
                </button>
            </div>
        </div>
    )
}

function HeroCard({
    app,
    onImport,
    reduceMotion,
}: {
    app: AppInfo
    onImport: (app: AppInfo) => void
    reduceMotion: boolean
}) {
    const url = app.getUrl?.()

    return (
        <div
            className="rounded-2xl border border-emerald-500/20 p-3.5"
            style={{
                background: "linear-gradient(135deg, rgba(52,211,153,0.10), rgba(52,211,153,0.02))",
            }}
        >
            <div className="flex items-center gap-3 mb-3">
                <AppIcon app={app} size="hero" />
                <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-[15px] font-semibold text-foreground">{app.name}</span>
                        <span className="text-xs font-medium uppercase tracking-wide bg-emerald-500/15 text-emerald-400 px-2 py-0.5 rounded-md">
                            Best for {PLATFORM_LABEL[app.platform]}
                        </span>
                    </div>
                    <div className="text-xs text-muted-foreground mt-1">
                        {app.price} · {app.store}
                    </div>
                </div>
            </div>

            <motion.button
                type="button"
                onClick={() => onImport(app)}
                whileTap={reduceMotion ? undefined : { scale: 0.985 }}
                className={cn(
                    "w-full h-12 rounded-xl bg-emerald-500/15 hover:bg-emerald-500/25 text-emerald-400 text-[13px] font-semibold tracking-wide transition-colors",
                    FOCUS_RING
                )}
            >
                Import to {app.name}
            </motion.button>

            {url && (
                <a
                    href={url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className={cn(
                        "mt-1 flex items-center justify-center gap-1.5 h-11 rounded-xl text-xs font-medium text-muted-foreground hover:text-foreground transition-colors",
                        FOCUS_RING
                    )}
                >
                    <Download className="w-3.5 h-3.5" />
                    Don't have it? {getAppLabel(app)}
                </a>
            )}
        </div>
    )
}

function AppRow({
    app,
    onImport,
    reduceMotion,
}: {
    app: AppInfo
    onImport: (app: AppInfo) => void
    reduceMotion: boolean
}) {
    return (
        <motion.button
            type="button"
            onClick={() => onImport(app)}
            whileTap={reduceMotion ? undefined : { scale: 0.99 }}
            className={cn(
                "w-full flex items-center gap-3 min-h-14 px-3 py-2.5 text-left hover:bg-white/[0.035] transition-colors",
                FOCUS_RING
            )}
        >
            <AppIcon app={app} size="row" />
            <span className="flex-1 min-w-0">
                <span className="block text-[13px] font-medium text-foreground truncate">
                    {app.name}
                </span>
                <span className="block text-xs text-muted-foreground truncate">{app.store}</span>
            </span>
            <span className="text-xs text-muted-foreground shrink-0">{app.price}</span>
            <ChevronRight className="w-4 h-4 text-muted-foreground/70 shrink-0" />
        </motion.button>
    )
}

function PlatformSwitcher({
    value,
    onChange,
    reduceMotion,
}: {
    value: Platform
    onChange: (p: Platform) => void
    reduceMotion: boolean
}) {
    const refs = useRef<Array<HTMLButtonElement | null>>([])

    const handleKeyDown = (e: ReactKeyboardEvent, index: number) => {
        if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return
        e.preventDefault()
        const dir = e.key === "ArrowRight" ? 1 : -1
        const next = (index + dir + PLATFORM_ORDER.length) % PLATFORM_ORDER.length
        onChange(PLATFORM_ORDER[next])
        refs.current[next]?.focus()
    }

    return (
        <div
            role="tablist"
            aria-label="Device platform"
            className="grid grid-cols-3 gap-1 p-1 rounded-xl bg-white/[0.04] border border-border/40"
        >
            {PLATFORM_ORDER.map((p, i) => {
                const active = p === value
                return (
                    <button
                        key={p}
                        ref={(el) => {
                            refs.current[i] = el
                        }}
                        type="button"
                        role="tab"
                        aria-selected={active}
                        tabIndex={active ? 0 : -1}
                        onClick={() => onChange(p)}
                        onKeyDown={(e) => handleKeyDown(e, i)}
                        className={cn(
                            "relative h-9 rounded-lg text-[13px] font-medium transition-colors",
                            active ? "text-emerald-400" : "text-muted-foreground hover:text-foreground",
                            FOCUS_RING
                        )}
                    >
                        {active && (
                            <motion.span
                                layoutId={reduceMotion ? undefined : "import-platform-pill"}
                                transition={{ type: "spring", stiffness: 400, damping: 32 }}
                                className="absolute inset-0 rounded-lg bg-emerald-500/15 border border-emerald-500/25"
                            />
                        )}
                        <span className="relative">{PLATFORM_LABEL[p]}</span>
                    </button>
                )
            })}
        </div>
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
    const reduceMotion = useReducedMotion() ?? false

    const [platform, setPlatform] = useState<Platform>(
        () => readStoredPlatform() ?? detectPlatform()
    )
    const [copied, setCopied] = useState(false)
    const [qrOpen, setQrOpen] = useState(false)
    const [failedAppId, setFailedAppId] = useState<string | null>(null)

    const importTimer = useRef<number | null>(null)

    const clearImportWatch = useCallback(() => {
        if (importTimer.current !== null) {
            window.clearTimeout(importTimer.current)
            importTimer.current = null
        }
    }, [])

    // The app opening means the page goes to background — that's the success
    // signal. Cancel the "nothing happened" timer whenever we lose the page.
    useEffect(() => {
        const onHide = () => {
            if (document.visibilityState === "hidden") clearImportWatch()
        }
        const onPageHide = () => clearImportWatch()
        document.addEventListener("visibilitychange", onHide)
        window.addEventListener("pagehide", onPageHide)
        window.addEventListener("blur", onPageHide)
        return () => {
            document.removeEventListener("visibilitychange", onHide)
            window.removeEventListener("pagehide", onPageHide)
            window.removeEventListener("blur", onPageHide)
            clearImportWatch()
        }
    }, [clearImportWatch])

    // Reset transient state when the sheet closes.
    useEffect(() => {
        if (!open) {
            clearImportWatch()
            setFailedAppId(null)
            setCopied(false)
        }
    }, [open, clearImportWatch])

    const handlePlatformChange = (p: Platform) => {
        setPlatform(p)
        storePlatform(p)
        clearImportWatch()
        setFailedAppId(null)
    }

    const recommendedApp = useMemo(
        () => APPS.find((a) => a.id === RECOMMENDED[platform]) ?? null,
        [platform]
    )
    const otherApps = useMemo(
        () => APPS.filter((a) => a.platform === platform && a.id !== RECOMMENDED[platform]),
        [platform]
    )

    const handleCopyLink = useCallback(async () => {
        const ok = await copyToClipboard(subscriptionUrl)
        if (!ok) {
            toast.error("Couldn't copy the link. Select it and copy manually.")
            return
        }
        setCopied(true)
        toast.success("Subscription link copied")
        setTimeout(() => setCopied(false), 2000)
    }, [subscriptionUrl])

    const handleImport = useCallback(
        (app: AppInfo) => {
            clearImportWatch()
            setFailedAppId(null)
            window.location.href = app.buildLink(subscriptionUrl, label)
            importTimer.current = window.setTimeout(() => {
                importTimer.current = null
                if (document.visibilityState === "visible") setFailedAppId(app.id)
            }, 1500)
        },
        [subscriptionUrl, label, clearImportWatch]
    )

    const failedApp = failedAppId ? APPS.find((a) => a.id === failedAppId) ?? null : null

    return (
        <>
            <Sheet open={open} onOpenChange={onOpenChange}>
                <SheetContent
                    side="bottom"
                    className={cn(
                        "rounded-t-2xl gap-3 max-h-[88dvh] overflow-y-auto pb-[max(1.5rem,env(safe-area-inset-bottom))]",
                        // Radix ships a 16px close target — grow it to a real tap area.
                        "[&>button:last-child]:top-2 [&>button:last-child]:right-2 [&>button:last-child]:size-11 [&>button:last-child]:grid [&>button:last-child]:place-items-center [&>button:last-child]:rounded-lg"
                    )}
                    onPointerDownOutside={() => onOpenChange(false)}
                >
                    <div
                        aria-hidden="true"
                        className="mx-auto mt-1 h-1 w-9 rounded-full bg-muted-foreground/25"
                    />

                    <SheetHeader className="p-0 gap-1">
                        <SheetTitle className="text-base">Add to your app</SheetTitle>
                        <SheetDescription className="text-xs">
                            Choose the app you use, then import in one tap.
                        </SheetDescription>
                    </SheetHeader>

                    <PlatformSwitcher
                        value={platform}
                        onChange={handlePlatformChange}
                        reduceMotion={reduceMotion}
                    />

                    <div className="space-y-4 pb-2">
                        {recommendedApp && (
                            <div className="space-y-2">
                                <HeroCard
                                    app={recommendedApp}
                                    onImport={handleImport}
                                    reduceMotion={reduceMotion}
                                />
                                {failedApp?.id === recommendedApp.id && (
                                    <ImportFallback
                                        app={failedApp}
                                        onCopy={handleCopyLink}
                                        copied={copied}
                                    />
                                )}
                            </div>
                        )}

                        {otherApps.length > 0 && (
                            <div>
                                <SectionLabel>Other apps</SectionLabel>
                                <div className="rounded-xl border border-border/40 bg-white/[0.02] divide-y divide-border/40 overflow-hidden">
                                    {otherApps.map((app) => (
                                        <AppRow
                                            key={app.id}
                                            app={app}
                                            onImport={handleImport}
                                            reduceMotion={reduceMotion}
                                        />
                                    ))}
                                </div>
                                {failedApp && failedApp.id !== recommendedApp?.id && (
                                    <div className="mt-2">
                                        <ImportFallback
                                            app={failedApp}
                                            onCopy={handleCopyLink}
                                            copied={copied}
                                        />
                                    </div>
                                )}
                            </div>
                        )}

                        <div>
                            <SectionLabel>Set up another device</SectionLabel>
                            <div className="rounded-xl border border-border/40 bg-white/[0.02] p-3 space-y-3">
                                <button
                                    type="button"
                                    onClick={() => setQrOpen(true)}
                                    aria-label="Show subscription QR code full size"
                                    className={cn(
                                        "mx-auto flex flex-col items-center gap-2 rounded-xl",
                                        FOCUS_RING
                                    )}
                                >
                                    <span className="bg-white rounded-lg p-2.5 block">
                                        <QRCodeSVG
                                            value={subscriptionUrl}
                                            size={140}
                                            level="M"
                                            bgColor="#ffffff"
                                            fgColor="#000000"
                                        />
                                    </span>
                                    <span className="text-xs text-muted-foreground">
                                        Scan from your phone
                                    </span>
                                </button>

                                <div className="flex items-stretch gap-2">
                                    <div className="flex-1 min-w-0 flex items-center rounded-lg border border-border/50 bg-background/40 px-3">
                                        <span className="text-xs font-mono text-muted-foreground truncate">
                                            {subscriptionUrl}
                                        </span>
                                    </div>
                                    <button
                                        type="button"
                                        onClick={handleCopyLink}
                                        className={cn(
                                            "shrink-0 h-11 px-4 inline-flex items-center gap-1.5 rounded-lg border border-border/50 bg-white/[0.05] hover:bg-white/[0.09] text-[13px] font-medium transition-colors",
                                            copied ? "text-emerald-500" : "text-foreground/80",
                                            FOCUS_RING
                                        )}
                                    >
                                        {copied ? (
                                            <Check className="w-4 h-4" />
                                        ) : (
                                            <Copy className="w-4 h-4" />
                                        )}
                                        {copied ? "Copied" : "Copy"}
                                    </button>
                                </div>
                            </div>
                        </div>
                    </div>
                </SheetContent>
            </Sheet>

            <QRDialog open={qrOpen} onOpenChange={setQrOpen} value={subscriptionUrl} />
        </>
    )
}
