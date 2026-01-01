import { useState } from "react"
import { Copy, Check, QrCode, ArrowUpDown, Smartphone, ServerOff, Info } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { toast } from "sonner"
import { motion, AnimatePresence } from "framer-motion"
import { cn, copyToClipboard, formatRelativeTime } from "@/lib/utils"
import { QRDialog } from "./qr-dialog"
import type { SubPanelServer } from "@/lib/types/sub-panel"
import { ImportToAppSheet } from "./import-to-app-sheet"

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
        toast.success("Server link copied", { description: server.name })
        setTimeout(() => setCopied(false), 2000)
    }

    const hasTransport = server.network && server.network !== "none"
    const hasSecurity = server.security && server.security !== "none"

    return (
        <>
            <motion.div
                variants={nodeItemVariants}
                className={cn(
                    "transition-colors",
                    !isFirst && "border-t border-border/50"
                )}
            >
                <div className="p-3.5 md:p-4 space-y-2.5">
                    {/* Header: flag + name (with inline online-dot) | status badge */}
                    <div className="flex items-start justify-between gap-2">
                        <div className="flex items-start gap-2.5 min-w-0 flex-1">
                            <span
                                className="text-xl md:text-2xl leading-none shrink-0 mt-0.5"
                                title={server.country_code}
                            >
                                {server.flag}
                            </span>
                            <div className="min-w-0 flex-1">
                                <div className="flex items-center gap-2 min-w-0">
                                    <span className="text-sm md:text-base font-semibold tracking-tight truncate">
                                        {server.name || server.address}
                                    </span>
                                    {server.is_online && (
                                        <span
                                            className="relative inline-flex h-2 w-2 shrink-0"
                                            title="Online"
                                        >
                                            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                                            <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500 shadow-[0_0_6px_rgba(16,185,129,0.7)]" />
                                        </span>
                                    )}
                                </div>
                                <div className="flex flex-wrap items-center gap-1 mt-1.5">
                                    <Badge
                                        variant="secondary"
                                        className="text-[10px] md:text-xs px-1.5 md:px-2 py-0 font-medium"
                                    >
                                        {server.protocol}
                                    </Badge>
                                    {hasTransport && (
                                        <Badge
                                            variant="outline"
                                            className="text-[10px] md:text-xs px-1.5 md:px-2 py-0"
                                        >
                                            {server.network}
                                        </Badge>
                                    )}
                                    {hasSecurity && (
                                        <Badge
                                            variant="outline"
                                            className="text-[10px] md:text-xs px-1.5 md:px-2 py-0"
                                        >
                                            {server.security}
                                        </Badge>
                                    )}
                                </div>
                            </div>
                        </div>
                        <Badge
                            variant={server.is_online ? "success" : "outline"}
                            className={cn(
                                "text-[10px] md:text-xs font-semibold mt-0.5 shrink-0",
                                !server.is_online && "text-muted-foreground border-zinc-500/20"
                            )}
                        >
                            {server.is_online ? "Online" : "Offline"}
                        </Badge>
                    </div>

                    {/* Data usage row */}
                    <div className="flex items-center gap-3 bg-muted/40 rounded-lg px-2.5 py-2">
                        <ArrowUpDown className="h-4 w-4 text-muted-foreground shrink-0" />
                        <div className="flex items-baseline gap-1.5 min-w-0">
                            <span className="text-base md:text-lg font-semibold tracking-tight truncate">
                                {server.data_used_display || "0 B"}
                            </span>
                            <span className="text-xs text-muted-foreground/80 shrink-0">used</span>
                        </div>
                        {server.last_activity_at && (
                            <span className="text-[10px] md:text-xs text-muted-foreground/70 ml-auto shrink-0">
                                {formatRelativeTime(server.last_activity_at)}
                            </span>
                        )}
                    </div>
                </div>

                {/* Action buttons — inset within card */}
                <div className="flex border-t border-border/50">
                    <button
                        onClick={handleCopy}
                        className="flex-1 flex items-center justify-center gap-1.5 md:gap-2 py-2 md:py-2.5 text-xs md:text-sm font-medium text-foreground/60 hover:text-foreground bg-muted/10 hover:bg-muted/40 transition-colors"
                    >
                        <CopyIconMorph copied={copied} />
                        <span className={cn(copied && "text-emerald-500")}>
                            {copied ? "Copied!" : "Copy Link"}
                        </span>
                    </button>
                    <div className="w-px bg-border/50" />
                    <motion.button
                        whileTap={{ scale: 0.92 }}
                        onClick={() => setQrOpen(true)}
                        className="flex-1 flex items-center justify-center gap-1.5 md:gap-2 py-2 md:py-2.5 text-xs md:text-sm font-medium text-foreground/60 hover:text-foreground bg-muted/10 hover:bg-muted/40 transition-colors"
                    >
                        <QrCode className="h-3.5 w-3.5 md:h-4 md:w-4" />
                        <span>QR Code</span>
                    </motion.button>
                </div>
            </motion.div>

            <QRDialog open={qrOpen} onOpenChange={setQrOpen} value={server.link} />
        </>
    )
}

function InfoRow({ text, isFirst }: { text: string; isFirst: boolean }) {
    return (
        <div className={cn("flex items-start gap-2.5 p-3.5 md:p-4", !isFirst && "border-t border-border/50")}>
            <Info className="h-4 w-4 shrink-0 mt-0.5 text-sky-400" aria-hidden />
            <p className="text-sm text-muted-foreground">{text}</p>
        </div>
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
                        <h3 className="text-sm md:text-base font-semibold">No servers linked</h3>
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
                        <h3 className="text-xs md:text-sm font-medium text-muted-foreground uppercase tracking-wider">
                            Servers ({realServers.length})
                        </h3>
                        {realServers.length > 0 && (
                        <div className="flex items-center gap-1">
                            <motion.div whileTap={{ scale: 0.95 }}>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-7 md:h-8 px-2.5 md:px-3 text-xs md:text-sm text-muted-foreground hover:text-foreground gap-1.5"
                                    onClick={handleCopyAll}
                                >
                                    <CopyIconMorph copied={allCopied} />
                                    <span className={cn(allCopied && "text-emerald-500")}>
                                        {allCopied ? "Copied!" : "Copy All"}
                                    </span>
                                </Button>
                            </motion.div>
                            <motion.div whileTap={{ scale: 0.95 }}>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-7 md:h-8 px-2.5 md:px-3 text-xs md:text-sm text-emerald-500 hover:text-emerald-400 hover:bg-emerald-500/10 gap-1.5"
                                    onClick={() => setImportSheetOpen(true)}
                                >
                                    <Smartphone className="h-3.5 w-3.5 md:h-4 md:w-4" />
                                    Import to App
                                </Button>
                            </motion.div>
                        </div>
                        )}
                    </div>

                    {/* Stacked nodes (each separated by its own divider) */}
                    <motion.div
                        variants={listContainerVariants}
                        initial="hidden"
                        animate="visible"
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
                    </motion.div>
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
