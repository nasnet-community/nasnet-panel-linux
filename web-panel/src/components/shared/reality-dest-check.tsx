import { useState } from "react"
import { AlertTriangle, CheckCircle2, Loader2, XCircle } from "lucide-react"
import { toast } from "@/lib/toast"
import { Button } from "@/components/ui/button"
import { checkRealityDest, type RealityDestCheck as Check, type RealityDestReport } from "@/lib/api/reality-dest"
import type { RealitySettings } from "@/lib/types"
import { cn, formatRelativeTime } from "@/lib/utils"

interface RealityDestCheckProps {
    nodeId: number
    settings?: RealitySettings
    /** Last report for the current dest/server names, or null. */
    report: RealityDestReport | null
    onReport: (report: RealityDestReport | null) => void
    /** True when a save was refused because of this report. */
    blocking?: boolean
}

// Sits under the Reality fields. Runs the dest handshake from the node and
// shows, per server name, whether REALITY could hijack it. The same view
// renders the report a blocked save comes back with.
export function RealityDestCheck({ nodeId, settings, report, onReport, blocking = false }: RealityDestCheckProps) {
    const [checking, setChecking] = useState(false)
    const dest = settings?.dest?.trim() ?? ""
    const serverNames = (settings?.serverNames ?? []).map((s) => s.trim()).filter(Boolean)
    const canRun = dest !== "" && serverNames.length > 0

    const run = async () => {
        setChecking(true)
        try {
            const res = await checkRealityDest(nodeId, { dest, xver: settings?.xver, serverNames })
            if (res.success && res.data) {
                onReport(res.data)
            } else {
                toast.error(res.error || "Could not check the destination")
            }
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Could not check the destination")
        } finally {
            setChecking(false)
        }
    }

    return (
        <div className={cn(
            "rounded-md border bg-card",
            blocking ? "border-status-danger/40" : "border-border-subtle",
        )}>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2.5">
                <div className="min-w-0 flex-1 text-[14px] leading-[18px]">
                    <p className="font-medium text-foreground">Destination check</p>
                    <p className="text-xs text-muted-foreground">
                        Handshakes with the site from the node and confirms REALITY can mirror it:
                        TLS 1.3, an X25519 key exchange and records under 8 KB.
                    </p>
                </div>
                <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => { void run() }}
                    disabled={!canRun || checking}
                    title={canRun ? undefined : "Enter a destination and at least one server name first"}
                >
                    {checking ? <><Loader2 className="animate-spin" /> Checking…</> : report ? "Check again" : "Check destination"}
                </Button>
            </div>
            {report && <ReportBody report={report} />}
        </div>
    )
}

function ReportBody({ report }: { report: RealityDestReport }) {
    const warnings = [
        ...(report.warnings ?? []),
        ...report.checks.flatMap((c) => c.warnings ?? []),
    ]
    const vantage = report.checked_from === "hub" ? "hub" : "node"
    return (
        <div className="space-y-2.5 border-t border-border-subtle px-3 py-2.5 text-[14px] leading-[18px]" role="status">
            <div className="flex items-start gap-2">
                {report.ok
                    ? <CheckCircle2 className="mt-px h-4 w-4 shrink-0 text-status-success" />
                    : <XCircle className="mt-px h-4 w-4 shrink-0 text-status-danger" />}
                <div className="min-w-0 flex-1">
                    <p className={cn("font-medium", report.ok ? "text-foreground" : "text-status-danger")}>
                        {report.ok ? "Ready for REALITY" : "Not usable as a REALITY destination"}
                    </p>
                    <p className="text-xs text-muted-foreground">
                        <span className="font-mono">{report.dest}</span>
                        {" · checked from the "}{vantage}{" "}{formatRelativeTime(report.checked_at)}
                    </p>
                </div>
            </div>

            <ul className="space-y-1.5 pl-6">
                {report.checks.map((c) => <CheckRow key={c.server_name || "(no name)"} check={c} />)}
            </ul>

            {warnings.length > 0 && (
                <ul className="space-y-1 pl-6">
                    {warnings.map((w) => (
                        <li key={w} className="flex items-start gap-1.5 text-xs text-status-warning">
                            <AlertTriangle className="mt-px h-3.5 w-3.5 shrink-0" />
                            <span className="min-w-0 flex-1 text-muted-foreground">{w}</span>
                        </li>
                    ))}
                </ul>
            )}
        </div>
    )
}

function CheckRow({ check }: { check: Check }) {
    const facts = check.ok
        ? [check.tls_version, check.key_share, check.alpn || "no ALPN", `${check.handshake_ms} ms`].filter(Boolean)
        : []
    return (
        <li className="flex items-start gap-2">
            <span className={cn(
                "mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full",
                check.ok ? "bg-status-success" : "bg-status-danger",
            )} />
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-baseline gap-x-2">
                    <span className="font-mono text-xs">{check.server_name || "(no SNI)"}</span>
                    {facts.length > 0 && (
                        <span className="text-xs tabular-nums text-muted-foreground">{facts.join(" · ")}</span>
                    )}
                </div>
                {!check.ok && check.message && (
                    <p className="text-xs text-status-danger">{check.message}</p>
                )}
            </div>
        </li>
    )
}
