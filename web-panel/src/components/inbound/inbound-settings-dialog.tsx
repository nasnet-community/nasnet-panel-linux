import { useCallback, useState } from "react"
import { toast } from "sonner"
import { AnimatePresence, motion } from "framer-motion"
import { HiArrowLeft } from "react-icons/hi"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useInboundForm, type TabId } from "./use-inbound-form"

import { InboundTabSidebar } from "./inbound-tab-sidebar"
import { InboundTabBarMobile } from "./inbound-tab-bar-mobile"
import { GeneralTab } from "./tabs/general-tab"
import { NetworkTab } from "./tabs/network-tab"
import { TransportTab } from "./tabs/transport-tab"
import { SecurityTab } from "./tabs/security-tab"
import { ProtocolTab } from "./tabs/protocol-tab"
import { AdvancedTab } from "./tabs/advanced-tab"
import { Form } from "@/components/ui/form"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    Sheet,
    SheetContent,
    SheetTitle,
    SheetDescription,
} from "@/components/ui/sheet"
import type { Inbound } from "@/lib/types"

interface InboundSettingsDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    inbound?: Inbound | null
    nodeId: number
    onSave: (data: Partial<Inbound>) => Promise<void>
    mode: "create" | "edit"
}

export function InboundSettingsDialog({
    open,
    onOpenChange,
    inbound,
    nodeId,
    onSave,
    mode,
}: InboundSettingsDialogProps) {
    const [loading, setLoading] = useState(false)
    const isMobile = useIsMobile()
    const {
        form,
        activeTab,
        setActiveTab,
        tabs,
        applyPreset,
        navigateToFirstError,
    } = useInboundForm(mode, inbound, open)

    // confirm before discarding a dirty form on any close path (save bypasses this)
    const handleOpenChange = useCallback((next: boolean) => {
        if (!next && form.formState.isDirty) {
            if (!window.confirm("Discard unsaved changes?")) return
        }
        onOpenChange(next)
    }, [form, onOpenChange])

    const handleSave = async () => {
        const valid = await form.trigger()
        if (!valid) {
            navigateToFirstError()
            toast.error("Please fix validation errors")
            return
        }

        try {
            setLoading(true)
            const values = form.getValues()
            // Only forward fields this form edits; bare "" on
            // address/link_format would wipe server-managed values.
            const payload: Partial<Inbound> = {
                ...values,
                id: inbound?.id,
                node_id: nodeId,
            }
            await onSave(payload)
            onOpenChange(false)
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to save inbound")
        } finally {
            setLoading(false)
        }
    }

    const title = mode === "create" ? "Add Inbound" : "Edit Inbound"
    const saveLabel = loading ? "Saving..." : mode === "create" ? "Create Inbound" : "Save Changes"

    const renderTabContent = () => {
        switch (activeTab) {
            case "general":
                return <GeneralTab form={form} mode={mode} onApplyPreset={applyPreset} />
            case "network":
                return <NetworkTab form={form} />
            case "transport":
                return <TransportTab form={form} />
            case "security":
                return <SecurityTab form={form} />
            case "protocol":
                return <ProtocolTab form={form} />
            case "advanced":
                return <AdvancedTab form={form} />
            default:
                return null
        }
    }

    const renderActiveTab = () => (
        <AnimatePresence mode="wait">
            <motion.div
                key={activeTab}
                initial={{ opacity: 0, x: 8 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: -8 }}
                transition={{ duration: 0.15 }}
            >
                {renderTabContent()}
            </motion.div>
        </AnimatePresence>
    )

    // Mobile layout: full-screen Sheet with bottom tab bar
    if (isMobile) {
        return (
            <Sheet open={open} onOpenChange={handleOpenChange}>
                <SheetContent
                    side="bottom"
                    className="h-[100dvh] rounded-t-xl flex flex-col p-0 [&>button:last-child]:hidden"
                >
                    {/* Sticky header */}
                    <div className="flex items-center justify-between px-4 py-3 border-b bg-background sticky top-0 z-10">
                        <button
                            type="button"
                            onClick={() => handleOpenChange(false)}
                            className="p-2 -ml-2 rounded-lg hover:bg-accent"
                        >
                            <HiArrowLeft className="w-5 h-5" />
                        </button>
                        <SheetTitle className="flex-1 text-center text-base">
                            {title}
                        </SheetTitle>
                        <button
                            type="button"
                            onClick={handleSave}
                            disabled={loading}
                            className="text-primary font-semibold text-sm px-2 py-1 rounded-lg hover:bg-accent disabled:opacity-50 disabled:pointer-events-none"
                        >
                            {saveLabel}
                        </button>
                    </div>
                    <SheetDescription className="sr-only">
                        Configure your inbound connection settings
                    </SheetDescription>

                    <Form {...form}>
                        {/* Scrollable content */}
                        <div className="flex-1 overflow-y-auto px-4 py-4">
                            {renderActiveTab()}
                        </div>

                        {/* Bottom tab bar */}
                        <InboundTabBarMobile
                            tabs={tabs}
                            activeTab={activeTab}
                            onTabChange={setActiveTab}
                        />
                    </Form>
                </SheetContent>
            </Sheet>
        )
    }

    // Desktop layout: Dialog with vertical tab sidebar
    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogContent className="max-w-4xl h-[85vh] max-h-[85vh] flex flex-col p-0 gap-0">
                <DialogHeader className="px-6 pt-6 pb-3">
                    <DialogTitle>{title}</DialogTitle>
                    <DialogDescription>
                        Configure your inbound connection settings
                    </DialogDescription>
                </DialogHeader>

                <Form {...form}>
                    {/* Tab sidebar + content */}
                    <div className="flex flex-1 min-h-0 border-t">
                        <InboundTabSidebar
                            tabs={tabs}
                            activeTab={activeTab}
                            onTabChange={setActiveTab}
                        />
                        <ScrollArea className="flex-1">
                            <div className="p-6">
                                {renderActiveTab()}
                            </div>
                        </ScrollArea>
                    </div>

                    <DialogFooter className="border-t px-6 py-4">
                        <Button variant="outline" onClick={() => handleOpenChange(false)}>
                            Cancel
                        </Button>
                        <Button onClick={handleSave} disabled={loading}>
                            {saveLabel}
                        </Button>
                    </DialogFooter>
                </Form>
            </DialogContent>
        </Dialog>
    )
}
