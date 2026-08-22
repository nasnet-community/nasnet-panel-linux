import { useState } from "react"
import { Copy, Check, QrCode, Smartphone, ServerOff, Info } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { toast } from "sonner"
import { motion, AnimatePresence } from "framer-motion"
import { cn, copyToClipboard, formatRelativeTime } from "@/lib/utils"
import { QRDialog } from "./qr-dialog"
import type { SubPanelServer } from "@/lib/types/sub-panel"
import { ImportToAppSheet } from "./import-to-app-sheet"

const FOCUS_RING =
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 focus-visible:ring-inset"

/**
 * Keeps a 32px-tall control tappable at 44px: an invisible overlay extends the
 * hit area 6px past each edge without changing how big the button looks. The
 * row's own padding absorbs the overflow.
 */
const HIT_SLOP =
    "relative before:absolute before:content-[''] before:inset-x-0 before:-top-1.5 before:-bottom-1.5"

/**
 * Square version for 32px icon buttons: 6px on every side takes them to 44×44.
 * Pair it with `gap-3` so neighbouring hit areas tile edge-to-edge instead of
 * overlapping and stealing each other's taps.
 */
const HIT_SLOP_SQUARE = "relative before:absolute before:content-[''] before:-inset-1.5"

/**
 * Remark templates decorate the node label for VPN clients — "🇩🇪 Berlin | 📊 12 GB
 * left | ⏳ 9d". The panel already shows the flag, usage and expiry in their own
 * columns, so a row titled with the raw remark repeats them and doubles the flag.
 * Prefer the plain node name; fall back to stripping the decoration.
 */
function cleanServerName(server: SubPanelServer): string {
    if (server.node_name) return server.node_name
    const head = (server.name || "").split("|")[0]
    return (
        head
            .replace(/^(?:[\s\u{1F1E6}-\u{1F1FF}\p{Extended_Pictographic}]|\u{200D}|\u{FE0F})+/u, "")
            .replace(/[\s|\-–—]+$/u, "")
            .trim() || server.address
    )
}

const listContainerVariants = {
    hidden: {},
    visible: {
        transition: { staggerChildren: 0.05 },
    },
}

const nodeItemVariants = {
    hidden: { opacity: 0, y: 8 },
    visible: {
        opacity: 1,
        y: 0,
        transition: { type: "spring" as const, stiffness: 320, damping: 26 },
    },
}


function CopyIconMorph({ copied }: { copied: boolean }) {
    return (
        <AnimatePresence mode="wait" initial={false}>
            {copied ? (
                <motion.div
                    key="check"
                    initial={{ scale: 0, rotate: -90 }}
                    animate={{ scale: 1, rotate: 0 }}
                    exit={{ scale: 0, rotate: 90 }}
                    transition={{ type: "spring", stiffness: 500, damping: 15 }}
                    className="flex items-center justify-center"
                >
                    <Check className="h-3.5 w-3.5 md:h-4 md:w-4 text-emerald-500" />
                </motion.div>
            ) : (
                <motion.div
                    key="copy"
                    initial={{ scale: 0, rotate: 90 }}
                    animate={{ scale: 1, rotate: 0 }}
                    exit={{ scale: 0, rotate: -90 }}
                    transition={{ type: "spring", stiffness: 500, damping: 15 }}
                    className="flex items-center justify-center"
                >
                    <Copy className="h-3.5 w-3.5 md:h-4 md:w-4" />
                </motion.div>
            )}
        </AnimatePresence>
    )
}

/** Dot-separated technical detail. Skips the "none" transport/security values. */
function metaLine(server: SubPanelServer): string {
    const parts = [server.protocol]
    if (server.network && server.network !== "none") parts.push(server.network)
    if (server.security && server.security !== "none") parts.push(server.security)
    if (server.last_activity_at) parts.push(formatRelativeTime(server.last_activity_at))
    return parts.join(" · ")
}

function ServerNode({ server, isFirst }: { server: SubPanelServer; isFirst: boolean }) {
    const [copied, setCopied] = useState(false)
    const [qrOpen, setQrOpen] = useState(false)

    const handleCopy = async () => {
        const ok = await copyToClipboard(server.link)
        if (!ok) {
            toast.error("Couldn’t copy — long-press the link to copy manually")
            return
        }
        setCopied(true)
        toast.success("Server link copied", { description: cleanServerName(server) })
        setTimeout(() => setCopied(false), 2000)
    }

    const name = cleanServerName(server)

    return (
        <>
            <motion.li
                variants={nodeItemVariants}
                className={cn(
                    "flex items-center gap-3 px-3.5 md:px-4 py-2.5 transition-colors hover:bg-muted/20",
                    !isFirst && "border-t border-border/40"
                )}
            >
                <span className="text-lg leading-none shrink-0" title={server.country_code} aria-hidden>
                    {server.flag}
                </span>

                <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-2">
                        <span className="flex items-center gap-1.5 min-w-0">
                            <span className="text-sm font-semibold tracking-tight truncate">{name}</span>
                            <span className="relative flex h-2 w-2 shrink-0 self-center">
                                {server.is_online && (
                                    <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                                )}
                                <span className={cn(
                                    "relative inline-flex h-2 w-2 rounded-full",
                                    server.is_online ? "bg-emerald-500" : "bg-muted-foreground/50"
                                )} />
                            </span>
                            <span className="sr-only">{server.is_online ? "Online" : "Offline"}</span>
                        </span>
                        <span className="ml-auto shrink-0 text-xs font-medium tabular-nums">
                            {server.data_used_display || "0 B"}
                        </span>
                    </div>
                    <p className="text-xs text-muted-foreground truncate">{metaLine(server)}</p>
                </div>

                {/* Icon actions: 32px of ink, 44px of tap. Repeating a full-width
                    "Copy Link / QR Code" bar under every server tripled the card's
                    height for two secondary actions. */}
                <div className="flex items-center gap-3 shrink-0">
                    <button
                        type="button"
                        onClick={handleCopy}
                        aria-label={`Copy link for ${name}`}
                        title="Copy link"
                        className={cn(
                            "grid place-items-center size-8 rounded-md text-muted-foreground",
                            "hover:text-foreground hover:bg-muted/60 transition-colors",
                            HIT_SLOP_SQUARE, FOCUS_RING
                        )}
                    >
                        <CopyIconMorph copied={copied} />
                    </button>
                    <motion.button
                        type="button"
                        whileTap={{ scale: 0.92 }}
                        onClick={() => setQrOpen(true)}
                        aria-label={`Show QR code for ${name}`}
                        title="QR code"
                        className={cn(
                            "grid place-items-center size-8 rounded-md text-muted-foreground",
                            "hover:text-foreground hover:bg-muted/60 transition-colors",
                            HIT_SLOP_SQUARE, FOCUS_RING
                        )}
                    >
                        <QrCode className="h-4 w-4" />
                    </motion.button>
                </div>
            </motion.li>

            <QRDialog open={qrOpen} onOpenChange={setQrOpen} value={server.link} />
        </>
    )
}

function InfoRow({ text, isFirst }: { text: string; isFirst: boolean }) {
    return (
        <li className={cn("flex items-start gap-2.5 px-3.5 md:px-4 py-3", !isFirst && "border-t border-border/40")}>
            <Info className="h-4 w-4 shrink-0 mt-0.5 text-sky-600 dark:text-sky-400" aria-hidden />
            <p className="text-sm text-muted-foreground">{text}</p>
        </li>
    )
}

interface ServerListProps {
    servers: SubPanelServer[]
    uuid: string
    subscriptionUrl: string
    label: string
}

export function ServerList({ servers, subscriptionUrl, label }: ServerListProps) {
    const [importSheetOpen, setImportSheetOpen] = useState(false)
    const [allCopied, setAllCopied] = useState(false)

    const infoRows = (servers ?? []).filter((s) => s.protocol === "INFO")
    // WireGuard is managed per-device in the WireGuard Devices card, not as a
    // shared server link — exclude it here (its panel link is empty anyway).
    const realServers = (servers ?? []).filter((s) => s.protocol !== "INFO" && s.protocol !== "WIREGUARD")

    const handleCopyAll = async () => {
        const allLinks = realServers.map((s) => s.link).join("\n")
        const ok = await copyToClipboard(allLinks)
        if (!ok) {
            toast.error("Couldn’t copy")
            return
        }
        setAllCopied(true)
        toast.success(`Copied ${realServers.length} server links`)
        setTimeout(() => setAllCopied(false), 2000)
    }

    if (realServers.length === 0 && infoRows.length === 0) {
        return (
            <Card className="border-amber-500/30 bg-amber-500/5 py-0 gap-0">
                <CardContent className="p-5 md:p-6 flex flex-col items-center text-center gap-3">
                    <div className="flex items-center justify-center h-12 w-12 rounded-full bg-amber-500/10 border border-amber-500/20">
                        <ServerOff className="h-5 w-5 text-amber-500" aria-hidden />
                    </div>
                    <div className="space-y-1">
                        <h2 className="text-sm md:text-base font-semibold">No servers linked</h2>
                        <p className="text-xs md:text-sm text-muted-foreground max-w-sm">
                            This subscription isn't linked to any active servers yet, so there are no connection
                            configs to import. Please contact support to have servers assigned.
                        </p>
                    </div>
                </CardContent>
            </Card>
        )
    }

    return (
        <>
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0 overflow-hidden">
                <CardContent className="p-0">
                    {/* Section header inside card */}
                    <div className="flex items-center justify-between gap-2 px-3.5 md:px-4 pt-3 md:pt-3.5 pb-2 md:pb-2.5">
                        <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider whitespace-nowrap">
                            Servers ({realServers.length})
                        </h2>
                        {realServers.length > 0 && (
                        <div className="flex items-center gap-1">
                            <motion.div whileTap={{ scale: 0.95 }}>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className={cn("h-8 px-2 text-xs text-muted-foreground hover:text-foreground gap-1.5", HIT_SLOP)}
                                    onClick={handleCopyAll}
                                >
                                    <CopyIconMorph copied={allCopied} />
                                    <span className={cn(allCopied && "text-emerald-500")}>
                                        {allCopied ? "Copied!" : "Copy All"}
                                    </span>
                                </Button>
                            </motion.div>
                            {/* Getting the config into a VPN app is the job customers
                                come here for — keep it in the header row, but tinted so
                                it reads as the action rather than another ghost link. */}
                            <motion.div whileTap={{ scale: 0.95 }}>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className={cn(
                                        "h-8 px-2.5 text-xs font-semibold gap-1.5 text-emerald-700 dark:text-emerald-400",
                                        "bg-emerald-500/10 hover:bg-emerald-500/20 hover:text-emerald-800 dark:hover:text-emerald-300",
                                        HIT_SLOP
                                    )}
                                    onClick={() => setImportSheetOpen(true)}
                                >
                                    <Smartphone className="h-3.5 w-3.5" />
                                    Import to App
                                </Button>
                            </motion.div>
                        </div>
                        )}
                    </div>

                    {/* One hairline-separated row per server — it is a list, so mark
                        it up as one. */}
                    <motion.ul
                        variants={listContainerVariants}
                        initial="hidden"
                        animate="visible"
                        className="border-t border-border/40"
                    >
                        {infoRows.map((s, i) => (
                            <InfoRow key={`info-${i}`} text={s.name} isFirst={i === 0} />
                        ))}
                        {realServers.map((server, i) => (
                            <ServerNode
                                key={`${server.address}-${server.port}-${i}`}
                                server={server}
                                isFirst={i === 0 && infoRows.length === 0}
                            />
                        ))}
                    </motion.ul>
                </CardContent>
            </Card>

            <ImportToAppSheet
                open={importSheetOpen}
                onOpenChange={setImportSheetOpen}
                subscriptionUrl={subscriptionUrl}
                label={label}
            />
        </>
    )
}
