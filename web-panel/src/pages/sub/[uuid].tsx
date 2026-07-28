import { useEffect } from "react"
import { motion, AnimatePresence, useReducedMotion } from "framer-motion"
import { useParams } from "react-router"
import { useTheme } from "@/components/providers/theme-provider"
import { useSubPanel, AuthRequiredError, SubPanelNotFoundError } from "@/lib/queries/use-sub-panel"
// import { ChatWidget } from "@/components/sub-panel/chat-widget"
import { useSubPanelEvents } from "@/lib/hooks/use-sub-panel-events"
import { PanelHeader } from "@/components/sub-panel/panel-header"
import { PasswordGate } from "@/components/sub-panel/password-gate"
import { StatusBadges } from "@/components/sub-panel/status-banner"
import { StatsGrid } from "@/components/sub-panel/stats-grid"
import { ServerList } from "@/components/sub-panel/server-list"
import { WgDevices } from "@/components/sub-panel/wg-devices"
import { TelegramChatId } from "@/components/sub-panel/telegram-chat-id"
import { PanelSkeleton } from "@/components/sub-panel/panel-skeleton"
import { DataForecast } from "@/components/sub-panel/data-forecast"
import { UsageHeatmap } from "@/components/sub-panel/usage-heatmap"
import { cn, copyToClipboard, getRelativeTime } from "@/lib/utils"
import { Card, CardContent } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { AlertCircle, Copy, LinkIcon, Monitor, Wifi } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { toast } from "sonner"
import { useQueryClient } from "@tanstack/react-query"
import { useIpGeolocation } from "@/lib/hooks/use-ip-geolocation"
import { NetworkConstellationBg } from "@/components/sub-panel/backgrounds/network-constellation"
import { useMaintenanceStatus } from "@/hooks/use-maintenance-status"
import { MaintenanceBanner } from "@/components/maintenance/maintenance-banner"
import { UsageTrendChart } from "@/components/sub-panel/usage-trend-chart"

const CONFIG_ID_REGEX = /^[a-zA-Z0-9_-]{8,64}$/

function InvalidUuidView() {
    return (
        <div className="min-h-screen bg-background">
            <PanelHeader />
            <main className="max-w-2xl md:max-w-3xl mx-auto px-4 md:px-6 py-4">
                <div className="flex flex-col items-center justify-center py-20 text-center space-y-3">
                    <LinkIcon className="h-10 w-10 text-muted-foreground/50" />
                    <h2 className="text-lg font-semibold">Invalid Subscription Link</h2>
                    <p className="text-sm text-muted-foreground max-w-sm">
                        This subscription link format is invalid. Please check the URL and try again.
                    </p>
                </div>
            </main>
        </div>
    )
}

function SubPanelContent({ uuid }: { uuid: string }) {
    const { data, isLoading, error, refetch } = useSubPanel(uuid)
    const queryClient = useQueryClient()
    const eventStatus = useSubPanelEvents(uuid, { enabled: !!data })

    // Fallback: while the SSE stream is not open, poll every 15s so data never
    // silently freezes. `hasData` is a stable boolean so this effect only
    // re-runs when the connection state changes (not on every tick).
    const hasData = !!data
    useEffect(() => {
        if (eventStatus === "open" || !hasData) return
        const id = setInterval(() => refetch(), 15_000)
        return () => clearInterval(id)
    }, [eventStatus, hasData, refetch])
    const reducedMotion = useReducedMotion()
    const { data: geoData } = useIpGeolocation(uuid, data?.online_ips)
    const { data: maintenance } = useMaintenanceStatus(uuid)
    const isUnderMaintenance = !!maintenance?.active
    const { setTheme } = useTheme()

    // Default to dark mode on public subscription page unless the visitor
    // already picked a theme (localStorage value other than "system").
    useEffect(() => {
        const stored = localStorage.getItem("theme")
        if (!stored || stored === "system") {
            setTheme("dark")
        }
    }, [setTheme])

    const isAuthRequired = error instanceof AuthRequiredError
    const authLabel = isAuthRequired ? error.label : ""

    if (isAuthRequired) {
        return (
            <PasswordGate
                uuid={uuid}
                label={authLabel}
                onAuthenticated={() => {
                    queryClient.removeQueries({ queryKey: ["sub-panel", uuid] })
                    refetch()
                }}
            />
        )
    }

    const containerVariants = {
        hidden: { opacity: 0 },
        visible: {
            opacity: 1,
            transition: { staggerChildren: 0.08 },
        },
    }

    const itemVariants = reducedMotion
        ? {
            hidden: { opacity: 0 },
            visible: {
                opacity: 1,
                transition: { duration: 0.15 },
            },
        }
        : {
            hidden: { opacity: 0, y: 12, scale: 0.98 },
            visible: {
                opacity: 1,
                y: 0,
                scale: 1,
                transition: { type: "spring" as const, stiffness: 300, damping: 24 },
            },
        }

    const scrollRevealProps = reducedMotion
        ? {
            initial: { opacity: 0 } as const,
            whileInView: { opacity: 1 } as const,
            viewport: { once: true } as const,
            transition: { duration: 0.15 },
        }
        : {
            initial: { opacity: 0, y: 20 } as const,
            whileInView: { opacity: 1, y: 0 } as const,
            viewport: { once: true, margin: "-50px" } as const,
            transition: { type: "spring" as const, stiffness: 300, damping: 24 },
        }

    return (
        <div className="min-h-screen bg-background relative overflow-x-hidden">
            <NetworkConstellationBg />

            <PanelHeader />

            <main className="relative z-[1] max-w-2xl md:max-w-3xl mx-auto px-4 md:px-6 py-4 md:py-6 space-y-3 md:space-y-4">
                <AnimatePresence mode="wait">
                    {isLoading && (
                        <motion.div
                            key="skeleton"
                            initial={{ opacity: 1 }}
                            exit={reducedMotion
                                ? { opacity: 0, transition: { duration: 0.15 } }
                                : { opacity: 0, scale: 0.98, transition: { duration: 0.2 } }
                            }
                        >
                            <PanelSkeleton />
                        </motion.div>
                    )}

                    {error && !isLoading && (
                        <motion.div
                            key="error"
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            className="flex flex-col items-center justify-center py-20 text-center space-y-3"
                        >
                            <AlertCircle className="h-10 w-10 text-muted-foreground/50" />
                            {error instanceof SubPanelNotFoundError ? (
                                <>
                                    <h2 className="text-lg font-semibold">Subscription Not Found</h2>
                                    <p className="text-sm text-muted-foreground max-w-sm">
                                        This subscription link is invalid or has been removed. Please check the URL or contact support.
                                    </p>
                                </>
                            ) : (
                                <>
                                    <h2 className="text-lg font-semibold">Couldn’t load subscription</h2>
                                    <p className="text-sm text-muted-foreground max-w-sm">
                                        Something went wrong reaching the server. Check your connection and try again.
                                    </p>
                                    <button
                                        type="button"
                                        onClick={() => refetch()}
                                        className="mt-1 inline-flex items-center gap-1.5 h-9 px-4 text-sm font-medium rounded-md bg-muted/50 hover:bg-muted transition-colors"
                                    >
                                        Retry
                                    </button>
                                </>
                            )}
                        </motion.div>
                    )}

                    {data && !isLoading && (
                        <motion.div
                            key="content"
                            className="space-y-3 md:space-y-4"
                            variants={containerVariants}
                            initial="hidden"
                            animate="visible"
                        >
                            {/* Maintenance banner */}
                            <MaintenanceBanner status={maintenance} />

                            {/* Subscription header: name + status badge */}
                            <motion.div variants={itemVariants} className="flex items-start justify-between gap-3">
                                <div className="min-w-0">
                                    <div className="flex items-center gap-2">
                                        <h1 className="text-lg md:text-xl font-bold tracking-tight truncate">{data.label}</h1>
                                        <span className="relative flex h-2.5 w-2.5 shrink-0" title={data.is_online ? "Online" : "Offline"}>
                                            {data.is_online && (
                                                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                                            )}
                                            <span className={cn(
                                                "relative inline-flex h-2.5 w-2.5 rounded-full",
                                                data.is_online ? "bg-emerald-500" : "bg-zinc-600"
                                            )} />
                                        </span>
                                    </div>
                                    {data.plan_name && data.plan_name !== data.label && (
                                        <p className="text-sm text-muted-foreground">{data.plan_name}</p>
                                    )}
                                    <button
                                        type="button"
                                        onClick={async () => {
                                            const ok = await copyToClipboard(data.subscription_url)
                                            if (ok) toast.success("Subscription URL copied")
                                            else toast.error("Couldn’t copy — long-press to copy manually")
                                        }}
                                        className="flex items-center gap-1.5 mt-1 text-[10px] md:text-xs text-muted-foreground/60 hover:text-muted-foreground transition-colors group cursor-pointer"
                                    >
                                        <Copy className="w-3 h-3" />
                                        <span>Copy Link</span>
                                    </button>
                                </div>
                                <div className="flex flex-col items-end gap-1.5 shrink-0">
                                    <StatusBadges status={data.status} />
                                    {data.last_active_at && (
                                        <span className="text-[10px] text-muted-foreground/50">
                                            {getRelativeTime(new Date(data.last_active_at).getTime())}
                                        </span>
                                    )}
                                </div>
                            </motion.div>

                            {/* Stats */}
                            <motion.div variants={itemVariants}>
                                <StatsGrid data={data} uuid={uuid} />
                            </motion.div>

                            {/* Server List — WireGuard is handled in its own card below.
                                Hide this entirely only when WG is the sole protocol, so a
                                WG-only sub doesn't show a misleading "No servers linked". */}
                            {(() => {
                                const servers = data.servers ?? []
                                const hasWg = servers.some((s) => s.protocol === "WIREGUARD")
                                const hasOther = servers.some((s) => s.protocol !== "WIREGUARD")
                                return (
                                    <>
                                        {(hasOther || !hasWg) && (
                                            <motion.div variants={itemVariants}>
                                                <ServerList
                                                    servers={servers}
                                                    uuid={uuid}
                                                    subscriptionUrl={data.subscription_url}
                                                    label={data.label}
                                                />
                                            </motion.div>
                                        )}
                                        {hasWg && (
                                            <motion.div variants={itemVariants}>
                                                <WgDevices uuid={uuid} />
                                            </motion.div>
                                        )}
                                    </>
                                )
                            })()}

                            {/* Connected IPs */}
                            {data.online_ips && data.online_ips.length > 0 && (
                                <>
                                    <motion.div {...scrollRevealProps}>
                                        <Separator className="opacity-50" />
                                    </motion.div>
                                    <motion.div {...scrollRevealProps}>
                                        <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
                                            <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-2">
                                                <div className="flex items-center justify-between">
                                                    <div className="flex items-center gap-2">
                                                        <Wifi className="w-4 h-4 text-emerald-400" />
                                                        <h3 className="text-xs md:text-sm font-medium text-muted-foreground uppercase tracking-wider">
                                                            Connected IPs
                                                        </h3>
                                                        <Badge variant="success" className="h-4.5 px-1.5 text-[10px] font-semibold">
                                                            {data.online_ips.length}
                                                        </Badge>
                                                    </div>
                                                    {data.online_ips.length > 1 && (
                                                        <button
                                                            type="button"
                                                            className="flex items-center gap-1 h-6 px-2 text-[10px] text-muted-foreground hover:text-foreground rounded-md hover:bg-muted/50 transition-colors"
                                                            onClick={async () => {
                                                                const allIPs = data.online_ips!.join("\n")
                                                                const ok = await copyToClipboard(allIPs)
                                                                if (ok) toast.success(`Copied ${data.online_ips!.length} IPs`)
                                                                else toast.error("Couldn’t copy")
                                                            }}
                                                        >
                                                            <Copy className="w-3 h-3" />
                                                            Copy all
                                                        </button>
                                                    )}
                                                </div>
                                                <div className="space-y-0.5 max-h-48 overflow-y-auto">
                                                    {data.online_ips.map((ip) => (
                                                        <button
                                                            key={ip}
                                                            type="button"
                                                            className="w-full flex items-center justify-between py-1.5 px-2 rounded-md hover:bg-muted/50 active:bg-muted/70 transition-colors cursor-pointer text-left group"
                                                            onClick={async () => {
                                                                const ok = await copyToClipboard(ip)
                                                                if (ok) toast.success(`Copied ${ip}`)
                                                                else toast.error("Couldn’t copy")
                                                            }}
                                                        >
                                                            <div className="flex items-center gap-2 min-w-0">
                                                                <Monitor className="w-3.5 h-3.5 shrink-0 text-emerald-400" />
                                                                <span className="text-sm font-mono truncate">{ip}</span>
                                                                {geoData?.[ip] && (
                                                                    <span className="text-[10px] text-muted-foreground/60 shrink-0 flex items-center gap-1">
                                                                        <span>{geoData[ip].flag}</span>
                                                                        <span className="hidden sm:inline">{geoData[ip].city}</span>
                                                                    </span>
                                                                )}
                                                                <Badge variant="success" className="h-4 px-1 text-[10px] font-semibold shrink-0">active</Badge>
                                                            </div>
                                                            <Copy className="w-3 h-3 shrink-0 ml-2 text-muted-foreground/30 sm:text-muted-foreground/0 group-hover:text-muted-foreground transition-colors" />
                                                        </button>
                                                    ))}
                                                </div>
                                            </CardContent>
                                        </Card>
                                    </motion.div>
                                </>
                            )}

                            {/* Data Forecast */}
                            {!data.is_unlimited && (
                                <>
                                    <motion.div {...scrollRevealProps}>
                                        <Separator className="opacity-50" />
                                    </motion.div>
                                    <motion.div {...scrollRevealProps}>
                                        <DataForecast uuid={uuid} />
                                    </motion.div>
                                </>
                            )}

                            {/* Activity Pattern */}
                            <motion.div {...scrollRevealProps}>
                                <UsageHeatmap uuid={uuid} />
                            </motion.div>

                            {/* Traffic Usage (up/down split, 7d or 30d) */}
                            <motion.div {...scrollRevealProps}>
                                <Separator className="opacity-50" />
                                <UsageTrendChart uuid={uuid} />
                            </motion.div>

                            {/* Telegram Chat ID (at end) */}
                            <motion.div {...scrollRevealProps}>
                                <TelegramChatId uuid={uuid} currentChatId={data.telegram_chat_id} connectedViaAccount={data.telegram_connected} botUsername={data.telegram_bot_username} />
                            </motion.div>

                            {/* Footer — reflects real SSE connection state */}
                            <motion.div {...scrollRevealProps} className="flex items-center justify-center gap-1.5 py-3 text-xs text-muted-foreground/70">
                                {eventStatus === "open" ? (
                                    <>
                                        <motion.span
                                            className="inline-block h-1.5 w-1.5 rounded-full bg-emerald-500"
                                            animate={{ opacity: [0.4, 1, 0.4] }}
                                            transition={{ duration: 2, repeat: Infinity, ease: "easeInOut" }}
                                        />
                                        Live updates enabled
                                    </>
                                ) : eventStatus === "connecting" ? (
                                    <>
                                        <span className="inline-block h-1.5 w-1.5 rounded-full bg-amber-500 animate-pulse" />
                                        Connecting…
                                    </>
                                ) : (
                                    <>
                                        <span className="inline-block h-1.5 w-1.5 rounded-full bg-zinc-500" />
                                        Live updates paused — refreshing periodically
                                    </>
                                )}
                            </motion.div>

                            {/* {data.chat_enabled && <ChatWidget uuid={uuid} />} */}
                        </motion.div>
                    )}
                </AnimatePresence>
            </main>
        </div>
    )
}

export default function SubPanelPage() {
    const { uuid } = useParams<{ uuid: string }>()

    if (!uuid || !CONFIG_ID_REGEX.test(uuid)) {
        return <InvalidUuidView />
    }

    return <SubPanelContent uuid={uuid} />
}
