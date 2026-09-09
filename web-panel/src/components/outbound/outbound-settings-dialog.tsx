import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Form } from "@/components/ui/form"
import { ConnectionDialogShell } from "@/components/connection-dialog/connection-dialog-shell"
import { ErrorSummary, UnsavedIndicator } from "@/components/connection-dialog/error-summary"
import { useOutboundForm, type TabId } from "./use-outbound-form"
import { GeneralTab } from "./tabs/general-tab"
import { ProtocolTab } from "./tabs/protocol-tab"
import { TransportTab } from "./tabs/transport-tab"
import { AdvancedTab } from "./tabs/advanced-tab"
import type { Outbound } from "@/lib/types"

import { canTestOutbound } from "@/components/node/network/outbound-details"

interface OutboundSettingsDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    outbound?: Outbound | null
    nodeId: number
    onSave: (data: Partial<Outbound>) => Promise<void>
    /** Optional: run the connectivity test for a saved outbound (edit mode "Save & test"). */
    onTest?: (outbound: Outbound) => Promise<unknown>
    mode: "create" | "edit"
    allOutbounds?: Outbound[]
}

export function OutboundSettingsDialog({
    open,
    onOpenChange,
    outbound,
    nodeId,
    onSave,
    onTest,
    mode,
    allOutbounds = [],
}: OutboundSettingsDialogProps) {
    const [loading, setLoading] = useState(false)
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
        importConfigLink,
        navigateToFirstError,
        protocol,
    } = useOutboundForm(mode, outbound, open)

    useEffect(() => {
        if (open) setSubmitted(false)
    }, [open])

    const isDirty = form.formState.isDirty
    const hasChanges = isDirty || (mode === "create" && appliedPresetId !== null)

    const handleOpenChange = useCallback(async (next: boolean) => {
        if (!next && hasChanges) {
            const ok = await confirm({
                title: "Discard changes?",
                description: mode === "create"
                    ? "This outbound has not been created yet. Closing now loses what you entered."
                    : "Your edits to this outbound have not been saved.",
                confirmLabel: "Discard",
                cancelLabel: "Keep editing",
                variant: "warning",
            })
            if (!ok) return
        }
        onOpenChange(next)
    }, [hasChanges, confirm, mode, onOpenChange])

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

    // Validate + persist. Returns the saved values, or null when validation
    // failed or the save threw (both already surfaced to the user).
    const save = async (): Promise<Partial<Outbound> | null> => {
        const valid = await form.trigger()
        if (!valid) {
            setSubmitted(true)
            navigateToFirstError()
            return null
        }
        try {
            setLoading(true)
            const values = form.getValues()
            const payload = {
                ...values,
                id: outbound?.id,
                node_id: nodeId,
            } as Partial<Outbound>
            await onSave(payload)
            onOpenChange(false)
            return payload
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to save outbound")
            return null
        } finally {
            setLoading(false)
        }
    }

    const handleSaveAndTest = async () => {
        const saved = await save()
        if (saved && outbound && onTest) {
            void onTest({ ...outbound, ...saved } as Outbound)
        }
    }

    const canTest = mode === "edit" && !!onTest && !!outbound && canTestOutbound({ ...outbound, protocol })

    const title = mode === "create"
        ? "New outbound"
        : <>Edit outbound{outbound?.tag && <span className="font-normal text-text-tertiary"> · {outbound.tag}</span>}</>
    const primaryLabel = mode === "create" ? "Create outbound" : "Save changes"

    const statusSlot = submitted && errorList.length > 0
        ? <ErrorSummary errors={errorList} onJump={(tab) => setActiveTab(tab as TabId)} />
        : hasChanges ? <UnsavedIndicator /> : null

    const renderTab = () => {
        switch (activeTab) {
            case "general":
                return (
                    <GeneralTab
                        form={form}
                        mode={mode}
                        outbound={outbound}
                        appliedPresetId={appliedPresetId}
                        onApplyPreset={handleApplyPreset}
                        onImportConfig={importConfigLink}
                    />
                )
            case "protocol":
                return <ProtocolTab form={form} />
            case "transport":
                return <TransportTab form={form} sectionVisibility={sectionVisibility} />
            case "advanced":
                return <AdvancedTab form={form} allOutbounds={allOutbounds} currentTag={form.watch("tag")} />
            default:
                return null
        }
    }

    return (
        <ConnectionDialogShell
            open={open}
            onOpenChange={(next) => { void handleOpenChange(next) }}
            title={title}
            description="Configure the outbound protocol, server, transport and advanced options"
            stack={stack}
            rail={rail}
            activeTab={activeTab}
            onTabChange={(id) => setActiveTab(id as TabId)}
            statusSlot={statusSlot}
            primaryLabel={primaryLabel}
            onPrimary={() => { void save() }}
            loading={loading}
            secondaryAction={canTest ? { label: "Save & test", onClick: () => { void handleSaveAndTest() } } : undefined}
        >
            <Form {...form}>{renderTab()}</Form>
        </ConnectionDialogShell>
    )
}
