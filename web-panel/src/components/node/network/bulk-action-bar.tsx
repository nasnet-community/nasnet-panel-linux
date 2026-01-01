import { AnimatePresence, motion } from "framer-motion"
import { useEffect } from "react"
import { cn } from "@/lib/utils"
import { HiOutlineBan, HiOutlineStatusOnline, HiOutlineTrash, HiOutlineX } from "react-icons/hi"
import { Loader2 } from "lucide-react"

interface BulkActionBarProps {
    count: number
    onCancel: () => void
    onDisable: () => void
    onEnable: () => void
    onDelete: () => void
    actionLoading: string | null
}

export function BulkActionBar({ count, onCancel, onDisable, onEnable, onDelete, actionLoading }: BulkActionBarProps) {
    useEffect(() => {
        if (count === 0) return
        const onKey = (e: KeyboardEvent) => {
            const tag = (e.target as HTMLElement | null)?.tagName
            if (tag === "INPUT" || tag === "TEXTAREA") return
            if (e.key.toLowerCase() === "d" && !e.metaKey && !e.ctrlKey) { e.preventDefault(); onDisable() }
            else if (e.key.toLowerCase() === "e" && !e.metaKey && !e.ctrlKey) { e.preventDefault(); onEnable() }
            else if (e.key === "Backspace" && !e.metaKey && !e.ctrlKey) { e.preventDefault(); onDelete() }
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [count, onDisable, onEnable, onDelete])

    return (
        <AnimatePresence>
            {count > 0 && (
                <>
                    {/* Desktop — floating dock */}
                    <motion.div
                        initial={{ y: 40, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 40, opacity: 0 }}
                        transition={{ type: "spring", stiffness: 400, damping: 32 }}
                        role="toolbar"
                        aria-label="Bulk inbound actions"
                        className="hidden md:flex fixed left-1/2 -translate-x-1/2 z-50 items-stretch rounded-2xl border border-white/10 bg-background/85 backdrop-blur-xl shadow-2xl shadow-black/40 overflow-hidden"
                        style={{ bottom: "max(2rem, env(safe-area-inset-bottom))" }}
                    >
                        {/* Info cell */}
                        <div className="flex items-center gap-2.5 px-4 py-3 bg-primary/10">
                            <div className="flex items-center justify-center w-6 h-6 rounded-full bg-primary text-primary-foreground text-xs font-bold font-mono tabular-nums">
                                {count}
                            </div>
                            <span className="text-[11px] font-semibold uppercase tracking-[0.15em] text-foreground/80">Selected</span>
                            <button
                                onClick={onCancel}
                                aria-label="Clear selection (Esc)"
                                className="ml-1 w-6 h-6 rounded-md hover:bg-muted/60 flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                            >
                                <HiOutlineX className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        {/* Primary actions */}
                        <div className="flex items-center gap-1 px-2 py-2 border-l border-white/10">
                            <DockAction
                                label="Disable"
                                kbd="D"
                                icon={HiOutlineBan}
                                onClick={onDisable}
                                loading={actionLoading === "bulk-disable"}
                                color="amber"
                            />
                            <DockAction
                                label="Enable"
                                kbd="E"
                                icon={HiOutlineStatusOnline}
                                onClick={onEnable}
                                loading={actionLoading === "bulk-enable"}
                                color="emerald"
                            />
                        </div>

                        {/* Destructive — separated */}
                        <div className="flex items-center px-2 py-2 border-l border-red-500/20 bg-red-500/[0.04]">
                            <DockAction
                                label="Delete"
                                kbd="⌫"
                                icon={HiOutlineTrash}
                                onClick={onDelete}
                                loading={actionLoading === "bulk-delete"}
                                color="red"
                                destructive
                            />
                        </div>
                    </motion.div>

                    {/* Mobile — full-width bottom sheet */}
                    <motion.div
                        initial={{ y: "100%" }}
                        animate={{ y: 0 }}
                        exit={{ y: "100%" }}
                        transition={{ type: "spring", stiffness: 400, damping: 32 }}
                        role="toolbar"
                        aria-label="Bulk inbound actions"
                        className="md:hidden fixed inset-x-0 z-50 bg-background/95 backdrop-blur-xl border-t border-white/10 shadow-2xl shadow-black/40"
                        style={{ bottom: 0, paddingBottom: "max(1rem, env(safe-area-inset-bottom))" }}
                    >
                        <div className="px-4 pt-3 pb-2 flex items-center justify-between">
                            <div className="flex items-center gap-2">
                                <div className="flex items-center justify-center w-7 h-7 rounded-full bg-primary text-primary-foreground text-sm font-bold font-mono tabular-nums">
                                    {count}
                                </div>
                                <span className="text-xs font-semibold uppercase tracking-[0.15em] text-foreground/80">Selected</span>
                            </div>
                            <button
                                onClick={onCancel}
                                aria-label="Clear selection"
                                className="text-xs text-muted-foreground hover:text-foreground px-2 py-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-md"
                            >
                                Cancel
                            </button>
                        </div>
                        <div className="grid grid-cols-3 gap-2 px-3 pb-3">
                            <SheetAction label="Disable" icon={HiOutlineBan} onClick={onDisable} loading={actionLoading === "bulk-disable"} color="amber" />
                            <SheetAction label="Enable" icon={HiOutlineStatusOnline} onClick={onEnable} loading={actionLoading === "bulk-enable"} color="emerald" />
                            <SheetAction label="Delete" icon={HiOutlineTrash} onClick={onDelete} loading={actionLoading === "bulk-delete"} color="red" destructive />
                        </div>
                    </motion.div>
                </>
            )}
        </AnimatePresence>
    )
}

function DockAction({ label, kbd, icon: Icon, onClick, loading, color, destructive }: {
    label: string
    kbd: string
    icon: React.ComponentType<{ className?: string }>
    onClick: () => void
    loading: boolean
    color: "amber" | "emerald" | "red"
    destructive?: boolean
}) {
    const cm = {
        amber: "hover:bg-amber-500/15 text-amber-400 hover:text-amber-300",
        emerald: "hover:bg-emerald-500/15 text-emerald-400 hover:text-emerald-300",
        red: "hover:bg-red-500/20 text-red-400 hover:text-red-300",
    } as const
    return (
        <button
            onClick={onClick}
            disabled={loading}
            aria-label={label}
            className={cn(
                "h-9 px-3 rounded-lg flex items-center gap-2 text-xs font-bold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60 disabled:cursor-not-allowed",
                cm[color],
                destructive && "font-bold",
            )}
        >
            {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Icon className="w-3.5 h-3.5" />}
            <span>{label}</span>
            <kbd className="ml-1 hidden lg:inline-flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded border border-white/15 bg-muted/30 text-[10px] font-mono text-muted-foreground">
                {kbd}
            </kbd>
        </button>
    )
}

function SheetAction({ label, icon: Icon, onClick, loading, color, destructive }: {
    label: string
    icon: React.ComponentType<{ className?: string }>
    onClick: () => void
    loading: boolean
    color: "amber" | "emerald" | "red"
    destructive?: boolean
}) {
    const cm = {
        amber: "bg-amber-500/10 text-amber-400 border-amber-500/20 active:bg-amber-500/20",
        emerald: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20 active:bg-emerald-500/20",
        red: "bg-red-500/10 text-red-400 border-red-500/20 active:bg-red-500/20",
    } as const
    return (
        <button
            onClick={onClick}
            disabled={loading}
            aria-label={label}
            className={cn(
                "h-11 rounded-xl border flex flex-col items-center justify-center gap-0.5 text-[11px] font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60",
                cm[color],
                destructive && "font-bold",
            )}
        >
            {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Icon className="w-4 h-4" />}
            {label}
        </button>
    )
}
