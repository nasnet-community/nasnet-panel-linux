import { useState, useRef, useEffect } from "react"
import { motion, AnimatePresence } from "framer-motion"
import { Button } from "@/components/ui/button"
import {
    HiOutlineTerminal,
    HiOutlineRefresh,
    HiOutlinePlay,
    HiOutlineStop,
} from "react-icons/hi"
import { Wrench, Loader2 } from "lucide-react"
import { toast } from "sonner"
import {
    restartXrayProcess,
    startXrayProcess,
    stopXrayProcess,
    clearNodeSSHLogs,
    restartNodeSSH,
} from "@/lib/admin-api"
import { useIsMobile } from "@/hooks/use-is-mobile"
import type { Node } from "@/lib/types"

interface NodeSettingsFabProps {
    node: Node
    onRefresh: () => void
}

interface FabAction {
    id: string
    label: string
    icon: React.ReactNode
    onClick: () => void | Promise<void>
    show: boolean
    variant?: "default" | "destructive"
}

export function NodeSettingsFab({ node, onRefresh }: NodeSettingsFabProps) {
    const [open, setOpen] = useState(false)
    const [loadingAction, setLoadingAction] = useState<string | null>(null)
    const isMobile = useIsMobile()
    const containerRef = useRef<HTMLDivElement>(null)

    // Close on outside click
    useEffect(() => {
        if (!open) return
        const handler = (e: MouseEvent) => {
            if (containerRef.current && !containerRef.current.contains(e.target as HTMLElement)) {
                setOpen(false)
            }
        }
        document.addEventListener("mousedown", handler)
        return () => document.removeEventListener("mousedown", handler)
    }, [open])

    // Close on Escape
    useEffect(() => {
        if (!open) return
        const handler = (e: KeyboardEvent) => {
            if (e.key === "Escape") setOpen(false)
        }
        document.addEventListener("keydown", handler)
        return () => document.removeEventListener("keydown", handler)
    }, [open])

    const runAction = async (id: string, fn: () => void | Promise<void>) => {
        setLoadingAction(id)
        try {
            await fn()
        } finally {
            setLoadingAction(null)
            setOpen(false)
        }
    }

    const handleRestartXray = async () => {
        const res = await restartXrayProcess(node.id)
        if (res.success) {
            toast.success("Xray restarted")
            onRefresh()
        } else {
            toast.error(res.error || "Failed to restart Xray")
        }
    }

    const handleStartXray = async () => {
        const res = await startXrayProcess(node.id)
        if (res.success) {
            toast.success("Xray started")
            onRefresh()
        } else {
            toast.error(res.error || "Failed to start Xray")
        }
    }

    const handleStopXray = async () => {
        const res = await stopXrayProcess(node.id)
        if (res.success) {
            toast.success("Xray stopped")
            onRefresh()
        } else {
            toast.error(res.error || "Failed to stop Xray")
        }
    }

    const handleClearSSHLogs = async () => {
        const res = await clearNodeSSHLogs(node.id)
        if (res.success) {
            toast.success("SSH logs cleared")
        } else {
            toast.error(res.error || "Failed to clear SSH logs")
        }
    }

    const handleRestartSSH = async () => {
        if (!confirm("Restart SSH service? Active connections may drop.")) return
        const res = await restartNodeSSH(node.id)
        if (res.success) {
            toast.success("SSH service restarted")
        } else {
            toast.error(res.error || "Failed to restart SSH")
        }
    }

    const actions: FabAction[] = [
        {
            id: "restart-xray",
            label: "Restart Xray",
            icon: <HiOutlineRefresh className="w-4 h-4" />,
            onClick: handleRestartXray,
            show: node.is_online,
        },
        {
            id: "start-xray",
            label: "Start Xray",
            icon: <HiOutlinePlay className="w-4 h-4" />,
            onClick: handleStartXray,
            show: node.is_online,
        },
        {
            id: "stop-xray",
            label: "Stop Xray",
            icon: <HiOutlineStop className="w-4 h-4" />,
            onClick: handleStopXray,
            show: node.is_online,
            variant: "destructive",
        },
        {
            id: "clear-ssh-logs",
            label: "Clear SSH Logs",
            icon: <HiOutlineTerminal className="w-4 h-4" />,
            onClick: handleClearSSHLogs,
            show: node.is_online,
        },
        {
            id: "restart-ssh",
            label: "Restart SSH",
            icon: <HiOutlineRefresh className="w-4 h-4" />,
            onClick: handleRestartSSH,
            show: node.is_online,
            variant: "destructive",
        },
    ]

    const visibleActions = actions.filter((a) => a.show)
    if (visibleActions.length === 0) return null

    return (
        <div
            ref={containerRef}
            className={`fixed z-40 ${isMobile ? "bottom-[100px] right-6" : "bottom-6 right-6"}`}
        >
            <AnimatePresence>
                {open && (
                    <>
                        {/* Backdrop on mobile */}
                        {isMobile && (
                            <motion.div
                                className="fixed inset-0 bg-black/40 z-[-1]"
                                initial={{ opacity: 0 }}
                                animate={{ opacity: 1 }}
                                exit={{ opacity: 0 }}
                                onClick={() => setOpen(false)}
                            />
                        )}

                        {/* Action items */}
                        <motion.div
                            className={
                                isMobile
                                    ? "fixed bottom-[170px] left-4 right-4 flex flex-col gap-2"
                                    : "absolute bottom-16 right-0 flex flex-col gap-2 min-w-[180px]"
                            }
                            initial={{ opacity: 0, y: 20 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: 20 }}
                            transition={{ type: "spring", stiffness: 300, damping: 25 }}
                        >
                            {visibleActions.map((action, i) => (
                                <motion.div
                                    key={action.id}
                                    initial={{ opacity: 0, y: 10 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    transition={{ delay: i * 0.03 }}
                                >
                                    <Button
                                        variant={action.variant === "destructive" ? "destructive" : "secondary"}
                                        size={isMobile ? "default" : "sm"}
                                        className={`w-full justify-start gap-2 ${isMobile ? "h-12" : ""}`}
                                        onClick={() => runAction(action.id, action.onClick)}
                                        disabled={loadingAction !== null}
                                    >
                                        {loadingAction === action.id ? (
                                            <Loader2 className="w-4 h-4 animate-spin" />
                                        ) : (
                                            action.icon
                                        )}
                                        {action.label}
                                    </Button>
                                </motion.div>
                            ))}
                        </motion.div>
                    </>
                )}
            </AnimatePresence>

            {/* FAB trigger button */}
            <motion.button
                className="w-14 h-14 rounded-full bg-primary text-primary-foreground shadow-lg flex items-center justify-center active:scale-95"
                onClick={() => setOpen(!open)}
                whileTap={{ scale: 0.9 }}
                animate={{ rotate: open ? 45 : 0 }}
                transition={{ type: "spring", stiffness: 300, damping: 25 }}
            >
                <Wrench className="w-5 h-5" />
            </motion.button>
        </div>
    )
}
