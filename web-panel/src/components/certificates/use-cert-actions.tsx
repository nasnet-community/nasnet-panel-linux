import { toast } from "@/lib/toast"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { getCA } from "@/lib/api/certificates"
import { usePurgeCerts, useRevokeOrphans, useRotateExpiring, useSetStrictMode } from "@/lib/queries"
import type { CertOverview } from "@/lib/types"
import { blockerLabel, certErrorMessage, downloadText } from "./cert-helpers"

async function withToast<T>(fn: () => Promise<T>): Promise<T> {
    try {
        return await fn()
    } catch (e) {
        toast.error(certErrorMessage(e))
        throw e
    }
}

/** Page-level certificate actions, each behind a confirm. */
export function useCertActions(overview?: CertOverview) {
    const confirm = useConfirm()
    const rotateExpiring = useRotateExpiring()
    const revokeOrphans = useRevokeOrphans()
    const purge = usePurgeCerts()
    const setStrict = useSetStrictMode()
    const days = overview?.auto_rotate.days ?? 30
    const strict = overview?.strict_mode

    return {
        rotateExpiring: () => void confirm({
            title: "Rotate expiring node certificates?",
            description: `Every node whose certificate has ${days} days or fewer left gets a new one. Nodes stay up.`,
            confirmLabel: "Rotate",
            onConfirm: () => withToast(async () => {
                const results = await rotateExpiring.mutateAsync()
                const ok = results.filter(r => r.ok).length
                const failed = results.filter(r => !r.ok)
                if (results.length === 0) toast.info("No node certificates are due for rotation.")
                else if (failed.length === 0) toast.success(`Rotated ${ok} certificate${ok === 1 ? "" : "s"}`)
                else toast.warning(`Rotated ${ok}. Failed: ${failed.map(f => `${f.node_name} (${f.error})`).join(", ")}`)
            }),
        }),
        revokeOrphans: () => void confirm({
            title: "Revoke orphaned certificates?",
            description: `${overview?.counts.orphans ?? 0} valid certificates belong to nodes that no longer exist. Revoking them stops anyone from using them.`,
            variant: "destructive",
            confirmLabel: "Revoke",
            onConfirm: () => withToast(async () => {
                const r = await revokeOrphans.mutateAsync()
                toast.success(`Revoked ${r.revoked} certificate${r.revoked === 1 ? "" : "s"}`)
            }),
        }),
        purge: () => void confirm({
            title: "Purge revoked, expired certificates?",
            description: "Deletes certificates that are both revoked and expired. They can't be used anymore, so nothing changes for your nodes.",
            variant: "destructive",
            confirmLabel: "Purge",
            onConfirm: () => withToast(async () => {
                const r = await purge.mutateAsync()
                toast.success(`Purged ${r.purged} certificate${r.purged === 1 ? "" : "s"}`)
            }),
        }),
        toggleStrict: () => {
            if (!strict) return
            if (strict.enabled) {
                void confirm({
                    title: "Turn off strict mode?",
                    description: "The panel will accept revoked node certificates, and certificates of another node, again.",
                    variant: "warning",
                    confirmLabel: "Turn off",
                    onConfirm: () => withToast(async () => { await setStrict.mutateAsync(false); toast.success("Strict mode is off") }),
                })
                return
            }
            if (!strict.ready) {
                toast.error(`Not ready: ${strict.blockers.map(b => `${b.node_name} ${blockerLabel(b.reason)}`).join(", ")}`)
                return
            }
            void confirm({
                title: "Turn on strict mode?",
                description: "The panel will refuse revoked node certificates and certificates that belong to another node. Every node passes right now.",
                confirmLabel: "Turn on",
                onConfirm: () => withToast(async () => { await setStrict.mutateAsync(true); toast.success("Strict mode is on") }),
            })
        },
        downloadCA: async () => {
            const res = await getCA().catch(() => null)
            if (res?.success && res.data?.certificate) downloadText(res.data.certificate, "nasnet-ca.crt")
            else toast.error("Couldn't load the CA certificate")
        },
    }
}
