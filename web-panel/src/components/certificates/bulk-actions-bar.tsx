import { motion, AnimatePresence } from "framer-motion"
import { Button } from "@/components/ui/button"
import { XCircle, Trash2, X, RefreshCw } from "lucide-react"
import { useCertificatesStore } from "@/store/certificates-store"
import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

interface BulkActionsBarProps {
    onBulkRevoke: (ids: number[]) => void
    onBulkDelete: (ids: number[]) => void
    isRevoking?: boolean
    isDeleting?: boolean
}

export function BulkActionsBar({ onBulkRevoke, onBulkDelete, isRevoking, isDeleting }: BulkActionsBarProps) {
    const { selectedIds, clearSelection } = useCertificatesStore()
    const [confirmAction, setConfirmAction] = useState<"revoke" | "delete" | null>(null)

    const count = selectedIds.size
    if (count === 0) return null

    const ids = Array.from(selectedIds)

    const handleConfirm = () => {
        if (confirmAction === "revoke") {
            onBulkRevoke(ids)
        } else if (confirmAction === "delete") {
            onBulkDelete(ids)
        }
        setConfirmAction(null)
    }

    return (
        <>
            <AnimatePresence>
                {count > 0 && (
                    <motion.div
                        initial={{ y: 100, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 100, opacity: 0 }}
                        transition={{ type: "spring", stiffness: 300, damping: 30 }}
                        className="fixed bottom-0 left-0 right-0 z-50 border-t border-border bg-background/95 backdrop-blur-sm shadow-lg"
                    >
                        <div className="flex items-center justify-between px-4 py-3 max-w-screen-xl mx-auto">
                            <div className="flex items-center gap-3">
                                <span className="text-sm font-medium">
                                    {count} selected
                                </span>
                                <button
                                    onClick={clearSelection}
                                    className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                                >
                                    Deselect all
                                </button>
                            </div>
                            <div className="flex gap-2">
                                <Button
                                    size="sm"
                                    variant="outline"
                                    className="text-amber-600 border-amber-200 hover:bg-amber-50 dark:border-amber-900 dark:hover:bg-amber-950/30"
                                    onClick={() => setConfirmAction("revoke")}
                                    disabled={isRevoking || isDeleting}
                                >
                                    <XCircle className="h-3.5 w-3.5 mr-1.5" />
                                    Revoke
                                </Button>
                                <Button
                                    size="sm"
                                    variant="destructive"
                                    onClick={() => setConfirmAction("delete")}
                                    disabled={isRevoking || isDeleting}
                                >
                                    <Trash2 className="h-3.5 w-3.5 mr-1.5" />
                                    Delete
                                </Button>
                            </div>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>

            {/* Confirmation Dialog */}
            <Dialog open={!!confirmAction} onOpenChange={(open) => !open && setConfirmAction(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>
                            {confirmAction === "revoke" ? "Revoke" : "Delete"} {count} Certificate{count > 1 ? "s" : ""}?
                        </DialogTitle>
                        <DialogDescription>
                            {confirmAction === "revoke"
                                ? "This will immediately revoke all selected certificates. They will no longer be valid for authentication."
                                : "This will permanently delete all selected certificate records from the database."
                            }
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setConfirmAction(null)}>Cancel</Button>
                        <Button
                            variant="destructive"
                            onClick={handleConfirm}
                            disabled={isRevoking || isDeleting}
                        >
                            {(isRevoking || isDeleting) && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                            {confirmAction === "revoke" ? "Revoke All" : "Delete All"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    )
}
