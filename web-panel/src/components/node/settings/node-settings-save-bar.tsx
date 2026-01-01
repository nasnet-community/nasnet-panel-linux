import { motion, AnimatePresence } from "framer-motion"
import { Button } from "@/components/ui/button"
import { Loader2 } from "lucide-react"
import { HiOutlineExclamation } from "react-icons/hi"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

interface NodeSettingsSaveBarProps {
    settingsForm: NodeSettingsForm
}

export function NodeSettingsSaveBar({ settingsForm }: NodeSettingsSaveBarProps) {
    const { isDirty, isSaving, save, reset } = settingsForm

    return (
        <AnimatePresence>
            {isDirty && (
                <motion.div
                    initial={{ y: 100, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    exit={{ y: 100, opacity: 0 }}
                    transition={{ type: "spring", stiffness: 300, damping: 30 }}
                    className="fixed bottom-0 left-0 right-0 z-50 px-4 pb-4 md:pb-6 pointer-events-none"
                >
                    <div className="pointer-events-auto max-w-4xl mx-auto flex items-center justify-between gap-3 rounded-lg border border-yellow-500/20 bg-card/95 backdrop-blur-md px-4 py-3 shadow-2xl">
                        <div className="flex items-center gap-2 text-sm text-yellow-500">
                            <HiOutlineExclamation className="w-4 h-4 shrink-0" />
                            <span>You have unsaved changes</span>
                        </div>
                        <div className="flex items-center gap-2">
                            <Button
                                variant="ghost"
                                size="sm"
                                onClick={reset}
                                disabled={isSaving}
                            >
                                Discard
                            </Button>
                            <Button
                                size="sm"
                                onClick={save}
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
