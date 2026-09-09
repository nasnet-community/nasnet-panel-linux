import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Form } from "@/components/ui/form"
import { ConnectionDialogShell } from "@/components/connection-dialog/connection-dialog-shell"
import { ErrorSummary, UnsavedIndicator } from "@/components/connection-dialog/error-summary"
import { useInboundForm, type TabId } from "./use-inbound-form"
import { GeneralTab } from "./tabs/general-tab"
import { ProtocolTab } from "./tabs/protocol-tab"
import { TransportTab } from "./tabs/transport-tab"
import { AdvancedTab } from "./tabs/advanced-tab"
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
    // True once the user pressed Save with a failing form; keeps the error
    // summary in the footer until the errors are gone.
    const [submitted, setSubmitted] = useState(false)
    const confirm = useConfirm()
    const {
        form,
        activeTab,
        setActiveTab,
        rail,
        stack,
        errorList,
        sectionVisibility,
        applyPreset,
        appliedPresetId,
        navigateToFirstError,
    } = useInboundForm(mode, inbound, open)

    useEffect(() => {
        if (open) setSubmitted(false)
    }, [open])

    const isDirty = form.formState.isDirty
    const hasChanges = isDirty || (mode === "create" && appliedPresetId !== null)

    // Guard every close path (overlay/Escape, close button, Cancel, mobile
    // back arrow) so a dirty form isn't discarded silently. A successful
    // save calls onOpenChange directly, bypassing this guard.
    const handleOpenChange = useCallback(async (next: boolean) => {
        if (!next && hasChanges) {
            const ok = await confirm({
                title: "Discard changes?",
                description: mode === "create"
                    ? "This inbound has not been created yet. Closing now loses what you entered."
                    : "Your edits to this inbound have not been saved.",
                confirmLabel: "Discard",
                cancelLabel: "Keep editing",
                variant: "warning",
            })
            if (!ok) return
        }
        onOpenChange(next)
    }, [hasChanges, confirm, mode, onOpenChange])

    // Applying a template resets every tab. Ask first when the user has
    // already changed something beyond tag/remark, which the reset keeps.
    const handleApplyPreset = useCallback(async (presetId: string) => {
        const dirtyKeys = Object.keys(form.formState.dirtyFields).filter((k) => k !== "tag" && k !== "remark")
        if (dirtyKeys.length > 0) {
            const ok = await confirm({
                title: "Replace current settings?",
                description: "The template fills every tab and replaces what you changed. Tag and remark are kept.",
                confirmLabel: "Apply template",
                cancelLabel: "Keep my settings",
                variant: "warning",
            })
            if (!ok) return
        }
        applyPreset(presetId)
    }, [form, confirm, applyPreset])

    const handleSave = async () => {
        const valid = await form.trigger()
        if (!valid) {
            setSubmitted(true)
            navigateToFirstError()
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

    const title = mode === "create"
        ? "New inbound"
        : <>Edit inbound{inbound?.tag && <span className="font-normal text-text-tertiary"> · {inbound.tag}</span>}</>
    const primaryLabel = mode === "create" ? "Create inbound" : "Save changes"

    const statusSlot = submitted && errorList.length > 0
        ? <ErrorSummary errors={errorList} onJump={(tab) => setActiveTab(tab as TabId)} />
        : hasChanges ? <UnsavedIndicator /> : null

    const renderTab = () => {
        switch (activeTab) {
            case "general":
                return <GeneralTab form={form} mode={mode} appliedPresetId={appliedPresetId} onApplyPreset={handleApplyPreset} />
            case "protocol":
                return <ProtocolTab form={form} />
            case "transport":
                return <TransportTab form={form} sectionVisibility={sectionVisibility} />
            case "advanced":
                return <AdvancedTab form={form} />
            default:
                return null
        }
    }

    return (
        <ConnectionDialogShell
            open={open}
            onOpenChange={(next) => { void handleOpenChange(next) }}
            title={title}
            description="Configure the inbound listener, protocol, transport and advanced options"
            stack={stack}
            rail={rail}
            activeTab={activeTab}
            onTabChange={(id) => setActiveTab(id as TabId)}
            statusSlot={statusSlot}
            primaryLabel={primaryLabel}
            onPrimary={() => { void handleSave() }}
            loading={loading}
        >
            <Form {...form}>{renderTab()}</Form>
        </ConnectionDialogShell>
    )
}
