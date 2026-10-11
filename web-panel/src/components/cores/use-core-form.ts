import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { coreFormSchema, type CoreFormData, SETTINGS_SLOT } from "@/lib/validations/core-schema"
import { presetFor, CORE_DEFAULT_PORTS } from "@/lib/presets/core-presets"
import { useCreateCoreInbound, useUpdateCoreInbound } from "@/lib/queries"
import type { CoreInbound, CoreProtocol, CoreSettings } from "@/lib/types/cores"

/**
 * The create/edit form brain. Protocol is fixed for the lifetime of the hook
 * (chosen in the picker's step 1, or the edited inbound's protocol), so — unlike
 * the Xray inbound form — there is no mid-form protocol switch to guard against.
 * Remount via a React key to switch protocols.
 */
export function useCoreForm({
    nodeId,
    protocol,
    editInbound,
    lite = false,
}: {
    nodeId: number
    protocol: CoreProtocol
    /** the tunnel lives on an MTProto node: its egress is the panel's */
    lite?: boolean
    editInbound?: CoreInbound | null
}) {
    const slot = SETTINGS_SLOT[protocol]
    const isEdit = !!editInbound
    const create = useCreateCoreInbound(nodeId)
    const update = useUpdateCoreInbound(nodeId)

    const initialSettings: CoreSettings =
        editInbound?.settings && Object.keys(editInbound.settings).length > 0
            ? editInbound.settings
            : presetFor(protocol)

    const form = useForm<CoreFormData>({
        resolver: zodResolver(coreFormSchema),
        mode: "onBlur",
        defaultValues: {
            node_id: nodeId,
            protocol,
            listen: editInbound?.listen || "0.0.0.0",
            port: editInbound?.port ?? CORE_DEFAULT_PORTS[protocol],
            egress_mode: editInbound?.egress_mode ?? (lite ? "hub" : "direct"),
            remark: editInbound?.remark ?? "",
            is_disabled: editInbound?.is_disabled ?? false,
            [slot]: initialSettings,
        } as CoreFormData,
    })

    /** Read the active protocol's settings slot. */
    const getSettings = (): CoreSettings =>
        ((form.watch(slot as keyof CoreFormData) as CoreSettings) ?? {}) as CoreSettings

    /** Replace the active protocol's settings slot. */
    const setSettings = (next: CoreSettings) =>
        form.setValue(slot as keyof CoreFormData, next as never, {
            shouldDirty: true,
            shouldValidate: form.formState.isSubmitted,
        })

    const submit = (onDone: () => void) =>
        form.handleSubmit(async (data) => {
            const settings = ((data as Record<string, unknown>)[slot] ?? {}) as CoreSettings
            const body = {
                node_id: nodeId,
                protocol,
                listen: data.listen || "0.0.0.0",
                port: data.port,
                egress_mode: data.egress_mode ?? (lite ? "hub" : "direct"),
                remark: data.remark || "",
                settings,
                is_disabled: data.is_disabled,
            }
            try {
                if (isEdit && editInbound) {
                    await update.mutateAsync({ id: editInbound.id, body })
                } else {
                    await create.mutateAsync(body)
                }
                onDone()
            } catch (e) {
                // Toast is surfaced by the mutation hook; also mirror a port
                // collision inline on the field so the fix is obvious.
                const msg = e instanceof Error ? e.message : ""
                if (/port/i.test(msg) && /(in use|already)/i.test(msg)) {
                    form.setError("port", {
                        message: "This port is already in use on the node.",
                    })
                }
            }
        })

    return {
        form,
        slot,
        isEdit,
        submit,
        getSettings,
        setSettings,
        isSubmitting: create.isPending || update.isPending,
    }
}
