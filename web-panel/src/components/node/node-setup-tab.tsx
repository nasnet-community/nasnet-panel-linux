import { useState } from "react"
import { AlertCircle, Check, RefreshCw } from "lucide-react"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Button } from "@/components/ui/button"
import { cn, formatRelativeTime } from "@/lib/utils"
import type { Node } from "@/lib/types"
import { useNodeDeps } from "@/lib/queries"
import { coreProtocolMeta, type CoreProtocol } from "@/lib/types/cores"
import { Stat } from "./network/section-stat"
import { PlainHead } from "./network/sortable-head"
import { DepInstallDialog } from "./dep-install-dialog"
import { CoreDepRow } from "./setup/core-dep-row"
import { PACKAGES_CELL, SETUP_GRID, STATUS_CELL } from "./setup/setup-grid"

/**
 * The binaries the tunnels need on this node, under the tunnel list. SSH is
 * built in; MTProto needs telemt, installed on the node itself from a pinned,
 * checksum-verified release.
 */
export function TunnelDependencies({ nodeId, node }: { nodeId: number; node: Node }) {
    const confirm = useConfirm()
    const [expanded, setExpanded] = useState<Set<string>>(new Set())
    const [job, setJob] = useState<{ cores: string[]; uninstall: boolean } | null>(null)
    const { data: reports = [], isLoading, isFetching, refetch, error, dataUpdatedAt } = useNodeDeps(nodeId, node.is_online)
    const installable = reports.filter(report => report.deps.length > 0)
    const missing = installable.filter(report => !report.ready)
    const builtIn = reports.filter(report => !report.deps.length)
    const busy = !!job || isFetching
    const nodeName = node.name || "this node"

    const openUninstall = async (core: string) => {
        if (await confirm({
            title: `Remove ${safeMeta(core)?.label ?? core}'s binary?`,
            description: "Its tunnels stop until the binary is installed again.",
            confirmLabel: "Uninstall",
            variant: "destructive",
        })) setJob({ cores: [core], uninstall: true })
    }

    return <section className="space-y-3">
        {/* Same shape as the Network section headers: the numbers under the title, actions on the right. */}
        <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0 space-y-1">
                <h3 className="text-base font-semibold leading-tight">Dependencies</h3>
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 text-sm">
                    {!node.is_online ? <span className="text-muted-foreground">Checked when {nodeName} is online.</span>
                        : isLoading ? <span className="text-muted-foreground">Checking what {nodeName} can run…</span> : <>
                            <Stat value={String(installable.length - missing.length)} label="ready" tone={missing.length === 0 ? "good" : undefined} />
                            {missing.length > 0 && <Stat value={String(missing.length)} label="need setup" tone="warn" />}
                            {dataUpdatedAt > 0 && <span className="whitespace-nowrap text-muted-foreground" title={new Date(dataUpdatedAt).toLocaleString()}>checked {formatRelativeTime(new Date(dataUpdatedAt).toISOString())}</span>}
                        </>}
                </div>
            </div>
            {node.is_online && (
                <Button variant="outline" size="sm" disabled={busy} onClick={() => void refetch()}>
                    <RefreshCw className={cn("mr-2 size-4", isFetching && "animate-spin")} />Refresh
                </Button>
            )}
        </div>

        {error && (
            <div role="alert" className="flex items-start gap-3 rounded-xl border border-amber-500/25 bg-amber-500/5 p-4">
                <AlertCircle className="mt-0.5 size-4 shrink-0 text-amber-500" />
                <div>
                    <p className="text-sm font-medium">Could not check {nodeName}</p>
                    <p className="mt-1 text-xs text-muted-foreground">{error.message}</p>
                </div>
            </div>
        )}

        {node.is_online && (isLoading || installable.length > 0) && (
            <div className="@container">
                <div className="overflow-x-auto rounded-xl border bg-card">
                    <div className={cn(SETUP_GRID, "h-9 rounded-t-xl bg-muted/50 pl-2 pr-3")}>
                        <span />
                        <span />
                        <PlainHead>Protocol</PlainHead>
                        <div className={PACKAGES_CELL}><PlainHead>Binaries</PlainHead></div>
                        <div className={STATUS_CELL}><PlainHead>Status</PlainHead></div>
                        <span />
                    </div>
                    {isLoading ? (
                        <div aria-label="Checking dependencies" aria-busy="true" className="flex h-14 animate-pulse items-center gap-3 border-t px-4">
                            <div className="size-8 rounded-lg bg-muted" />
                            <div className="space-y-2"><div className="h-3 w-28 rounded bg-muted" /><div className="h-2.5 w-20 rounded bg-muted" /></div>
                        </div>
                    ) : installable.map(report => {
                        const meta = safeMeta(report.core_type)
                        return (
                            <CoreDepRow
                                key={report.core_type}
                                report={report}
                                label={meta?.label ?? report.core_type}
                                family={meta?.family}
                                expanded={expanded.has(report.core_type)}
                                disabled={busy}
                                onToggleExpand={() => setExpanded(previous => {
                                    const next = new Set(previous)
                                    if (next.has(report.core_type)) next.delete(report.core_type)
                                    else next.add(report.core_type)
                                    return next
                                })}
                                onInstall={() => setJob({ cores: [report.core_type], uninstall: false })}
                                onUninstall={() => void openUninstall(report.core_type)}
                            />
                        )
                    })}
                </div>
            </div>
        )}

        {builtIn.length > 0 && (
            <p className="flex items-center gap-2 text-xs text-muted-foreground">
                <Check className="size-3.5 text-emerald-500" />
                {builtIn.map(report => safeMeta(report.core_type)?.label ?? report.core_type).join(", ")} {builtIn.length === 1 ? "is" : "are"} built in and {builtIn.length === 1 ? "needs" : "need"} nothing installed.
            </p>
        )}

        {job && (
            <DepInstallDialog
                nodeId={nodeId}
                cores={job.cores}
                uninstall={job.uninstall}
                open
                onOpenChange={open => { if (!open) { setJob(null); void refetch() } }}
                onComplete={() => void refetch()}
            />
        )}
    </section>
}

function safeMeta(coreType: string) {
    try { return coreProtocolMeta(coreType as CoreProtocol) } catch { return undefined }
}
