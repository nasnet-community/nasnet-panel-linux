import { motion, AnimatePresence } from "framer-motion"
import { Button } from "@/components/ui/button"
import { Loader2 } from "lucide-react"
import { HiOutlineExclamation } from "react-icons/hi"

interface SettingsSaveBarProps {
    dirtyCount: number
    onSaveAll: () => void
    onDiscardAll: () => void
    isSaving: boolean
}

export function SettingsSaveBar({ dirtyCount, onSaveAll, onDiscardAll, isSaving }: SettingsSaveBarProps) {
    return (
        <AnimatePresence>
            {dirtyCount > 0 && (
                <motion.div
                    initial={{ y: 100, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    exit={{ y: 100, opacity: 0 }}
                    transition={{ type: "spring", stiffness: 300, damping: 30 }}
                    className="fixed left-0 right-0 z-50 px-4 pointer-events-none bottom-[calc(56px+env(safe-area-inset-bottom))] pb-2 md:bottom-0 md:pb-6"
                >
                    <div className="pointer-events-auto max-w-4xl mx-auto flex items-center justify-between gap-3 rounded-lg border border-amber-500/20 bg-card/95 backdrop-blur-md px-4 py-3 shadow-2xl">
                        <div className="flex items-center gap-2 text-sm text-amber-500">
                            <HiOutlineExclamation className="w-4 h-4 shrink-0" />
                            <span>
                                Unsaved changes in {dirtyCount} {dirtyCount === 1 ? "category" : "categories"}
                            </span>
                        </div>
                        <div className="flex items-center gap-2">
                            <Button
                                variant="ghost"
                                size="sm"
                                onClick={onDiscardAll}
                                disabled={isSaving}
                            >
                                Discard All
                            </Button>
                            <Button
                                size="sm"
                                onClick={onSaveAll}
                                disabled={isSaving}
                            >
                                {isSaving && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                                Save All
                            </Button>
                        </div>
                    </div>
                </motion.div>
            )}
        </AnimatePresence>
    )
}
