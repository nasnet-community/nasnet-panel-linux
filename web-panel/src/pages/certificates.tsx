import { useEffect, useState } from "react"
import { RefreshCw } from "lucide-react"
import { toast } from "@/lib/toast"
import { PageHeader } from "@/components/shared/page-header"
import { Button } from "@/components/ui/button"
import { QueryError } from "@/components/ui/query-error"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { CertActionsMenu } from "@/components/certificates/cert-actions-menu"
import { CertAttentionCard } from "@/components/certificates/cert-attention-card"
import { CertCAStrip } from "@/components/certificates/cert-ca-strip"
import { CertDetailsDrawer } from "@/components/certificates/cert-details-drawer"
import { CertDomainsCard } from "@/components/certificates/cert-domains-card"
import { certErrorMessage } from "@/components/certificates/cert-helpers"
import { CertLedger } from "@/components/certificates/cert-ledger"
import { CertNodeList } from "@/components/certificates/cert-node-list"
import { useCertActions } from "@/components/certificates/use-cert-actions"
import { useCertOverview, useRotateNodeCert } from "@/lib/queries"
import type { CertSummary, NodeCertRow } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useCertificatesStore } from "@/store/certificates-store"

interface PendingRotation {
    nodeId: number
    name: string
    serial: string
    until: number
}

const ROTATION_WAIT_MS = 60_000

export default function CertificatesPage() {
    const confirm = useConfirm()
    const [pending, setPending] = useState<PendingRotation | null>(null)
    const { data, isLoading, isError, isFetching, refetch } = useCertOverview(pending ? 3_000 : 30_000)
    const rotate = useRotateNodeCert()
    const actions = useCertActions(data)
    const { setLedgerOpen, setFilter } = useCertificatesStore()
    const [viewing, setViewing] = useState<{ cert: CertSummary; row?: NodeCertRow } | null>(null)

    // A rotation settles once the node shows its new serial, or after a minute.
    useEffect(() => {
        if (!pending || !data) return
        const row = data.nodes.find(n => n.node_id === pending.nodeId)
        if (row?.presented?.serial === pending.serial) {
            toast.success(`${pending.name} now presents its new certificate`)
            setPending(null)
        }
    }, [data, pending])

    // Own timer: an unchanged refetch keeps the same data object, so the
    // effect above would never run again to notice the minute is up.
    useEffect(() => {
        if (!pending) return
        const id = setTimeout(() => {
            toast.warning(`${pending.name} hasn't shown its new certificate yet. It will be picked up on its next connection.`)
            setPending(null)
        }, Math.max(0, pending.until - Date.now()))
        return () => clearTimeout(id)
    }, [pending])

    const onRotate = (row: NodeCertRow) => void confirm({
        title: `Rotate ${row.name}'s certificate?`,
        description: "The node stays up. Its old certificate is revoked once it shows the new one.",
        confirmLabel: "Rotate",
        onConfirm: async () => {
            try {
                const res = await rotate.mutateAsync(row.node_id)
                setPending({ nodeId: row.node_id, name: row.name, serial: res.serial, until: Date.now() + ROTATION_WAIT_MS })
            } catch (e) {
                toast.error(certErrorMessage(e))
                throw e
            }
        },
    })

    const showLegacy = () => {
        setFilter("legacy")
        setLedgerOpen(true)
    }

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            <PageHeader
                title="Certificates"
                description="Node certificates, the panel's CA and domain certificates"
                actions={
                    <>
                        <Button variant="outline" size="sm" onClick={() => void refetch()} disabled={isFetching}>
                            <RefreshCw className={cn("h-4 w-4 sm:mr-1.5", isFetching && "animate-spin")} />
                            <span className="hidden sm:inline">Refresh</span>
                        </Button>
                        <CertActionsMenu overview={data} />
                    </>
                }
            />

            {isError && (
                <QueryError
                    title={data ? "Couldn't refresh certificates" : "Couldn't load certificates"}
                    description={data
                        ? "Showing the last loaded certificates. These may be out of date."
                        : "Your certificates and CA status are unavailable. Try again to load them."}
                    onRetry={() => void refetch()}
                    isRetrying={isFetching}
                />
            )}

            {isLoading && !data ? (
                <div role="status" className="py-12 text-center text-sm text-muted-foreground">Loading certificates…</div>
            ) : data ? (
                <div className="flex flex-col gap-4">
                    <CertCAStrip ca={data.ca} strict={data.strict_mode} nodeCount={data.nodes.length} overview={data} />
                    <CertNodeList
                        nodes={data.nodes}
                        strictOn={data.strict_mode.enabled}
                        rotatingNodeId={rotate.isPending ? (rotate.variables ?? null) : pending?.nodeId ?? null}
                        onRotate={onRotate}
                        onView={(cert, row) => setViewing({ cert, row })}
                    />
                    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                        <CertDomainsCard domains={data.domains} />
                        <CertAttentionCard
                            items={data.attention}
                            onRevokeOrphans={actions.revokeOrphans}
                            onRotateExpiring={actions.rotateExpiring}
                            onShowLegacy={showLegacy}
                        />
                    </div>
                    <CertLedger certificates={data.certificates} onView={cert => setViewing({ cert })} />
                </div>
            ) : null}

            <CertDetailsDrawer cert={viewing?.cert ?? null} row={viewing?.row} onClose={() => setViewing(null)} />
        </div>
    )
}
