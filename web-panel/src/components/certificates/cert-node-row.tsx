import { useState } from "react"
import { Link, useNavigate } from "react-router"
import { ChevronRight, Download, ExternalLink, Eye, MoreHorizontal, RefreshCw, ShieldAlert, ShieldCheck, ShieldQuestion, XCircle } from "lucide-react"
import { toast } from "@/lib/toast"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { getCertificateDetails } from "@/lib/api/certificates"
import { useRevokeCert } from "@/lib/queries"
import type { CertSummary, NodeCertRow } from "@/lib/types"
import { cn, formatDate } from "@/lib/utils"
import { certErrorMessage, daysLabel, downloadText, expiryTone, lifeLeftPct, presentedState, toneText, type Tone } from "./cert-helpers"
import { ExpiryBar, T } from "./cert-ui"

interface CertNodeRowProps {
    row: NodeCertRow
    strictOn: boolean
    rotating: boolean
    onRotate: (row: NodeCertRow) => void
    onView: (cert: CertSummary, row: NodeCertRow) => void
}

const presentedIcon: Record<Tone, typeof ShieldCheck> = {
    ok: ShieldCheck,
    warn: ShieldAlert,
    danger: ShieldAlert,
    muted: ShieldQuestion,
}

function rotateBlocker(row: NodeCertRow): string | null {
    if (row.is_stealth) return "Stealth nodes get a new certificate every time they restart."
    if (row.rotate_supported === false) return "Update the node agent to rotate its certificate."
    if (!row.online) return "The node is offline."
    return null
}

export async function downloadCert(cert: CertSummary) {
    try {
        const res = await getCertificateDetails(cert.id)
        if (!res.success || !res.data) throw new Error(res.error || "Couldn't load the certificate")
        downloadText(res.data.certificate, `${cert.common_name}.crt`)
    } catch (e) {
        toast.error(certErrorMessage(e))
    }
}

export function CertNodeRow({ row, strictOn, rotating, onRotate, onView }: CertNodeRowProps) {
    const [historyOpen, setHistoryOpen] = useState(false)
    const navigate = useNavigate()
    const confirm = useConfirm()
    const revoke = useRevokeCert()
    const state = presentedState(row)
    const Icon = presentedIcon[state.tone]
    const cur = row.current
    const tone = expiryTone(cur?.days_left)
    const blocker = rotateBlocker(row)

    const revokeCurrent = () => {
        if (!cur) return
        void confirm({
            title: `Revoke ${row.name}'s current certificate?`,
            description: strictOn
                ? "Strict mode is on: the panel stops talking to this node right away, until you redeploy it."
                : "Nothing changes until strict mode is on; from then this node is cut off until you redeploy it. Rotating is usually what you want.",
            variant: "destructive",
            typeToConfirm: row.name,
            confirmLabel: "Revoke",
            onConfirm: async () => {
                try {
                    await revoke.mutateAsync({ id: cur.id, confirm: row.name })
                    toast.success("Certificate revoked")
                } catch (e) {
                    toast.error(certErrorMessage(e))
                    throw e
                }
            },
        })
    }

    const rotateButton = (
        <Button size="sm" variant="outline" className="h-11 w-full sm:h-8 sm:w-auto" disabled={!!blocker || rotating} onClick={() => onRotate(row)}>
            <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", rotating && "animate-spin")} />
            {rotating ? "Rotating…" : "Rotate"}
        </Button>
    )

    return (
        <div className="py-3 first:pt-0 last:pb-0">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                <div className="flex min-w-0 items-center gap-2 sm:w-52">
                    <span className={cn("h-2 w-2 shrink-0 rounded-full", row.online ? "bg-emerald-500" : "bg-muted-foreground/40")} aria-label={row.online ? "online" : "offline"} />
                    <Link to={`/nodes/${row.node_id}`} className="truncate text-sm font-medium hover:underline">{row.name}</Link>
                    <Badge variant="outline" className="px-1.5 py-0 text-[12px] font-normal text-muted-foreground">{row.is_stealth ? "stealth" : row.connect_mode}</Badge>
                </div>
                <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                    <div className="flex min-w-0 items-center gap-2">
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <span className={cn("inline-flex shrink-0 items-center gap-1 text-xs font-medium", toneText[state.tone])} tabIndex={0}>
                                    <Icon className="h-3.5 w-3.5" />{state.label}
                                </span>
                            </TooltipTrigger>
                            <TooltipContent>{state.hint}</TooltipContent>
                        </Tooltip>
                        {cur && <span className={cn(T.meta, "truncate font-mono")}>{cur.common_name}</span>}
                    </div>
                    {cur ? (
                        <div className="flex items-center gap-2">
                            <ExpiryBar pct={lifeLeftPct(cur.not_before, cur.not_after)} tone={tone} />
                            <span className={cn("w-14 shrink-0 text-right text-xs tabular-nums", toneText[tone])} title={`Expires ${formatDate(cur.not_after, "long")}`}>
                                {daysLabel(cur.days_left)}
                            </span>
                        </div>
                    ) : (
                        <span className={T.meta}>No valid certificate on record</span>
                    )}
                </div>
                <div className="flex items-center gap-2">
                    {blocker ? (
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <span className="flex-1 sm:flex-none" tabIndex={0}>{rotateButton}</span>
                            </TooltipTrigger>
                            <TooltipContent align="end">{blocker}</TooltipContent>
                        </Tooltip>
                    ) : (
                        <span className="flex-1 sm:flex-none">{rotateButton}</span>
                    )}
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-11 w-11 sm:h-8 sm:w-8" aria-label={`More actions for ${row.name}`}>
                                <MoreHorizontal className="h-4 w-4" />
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                            {cur && <DropdownMenuItem onClick={() => onView(cur, row)}><Eye className="mr-2 h-4 w-4" />View certificate</DropdownMenuItem>}
                            {cur && <DropdownMenuItem onClick={() => void downloadCert(cur)}><Download className="mr-2 h-4 w-4" />Download certificate</DropdownMenuItem>}
                            <DropdownMenuItem onClick={() => navigate(`/nodes/${row.node_id}`)}><ExternalLink className="mr-2 h-4 w-4" />Open node</DropdownMenuItem>
                            {cur && (
                                <>
                                    <DropdownMenuSeparator />
                                    <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={revokeCurrent}>
                                        <XCircle className="mr-2 h-4 w-4" />Revoke current certificate
                                    </DropdownMenuItem>
                                </>
                            )}
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>
            </div>
            {row.history.length > 0 && (
                <div className="mt-2 sm:pl-52">
                    <button type="button" className={cn(T.meta, "inline-flex min-h-8 items-center gap-1 hover:text-foreground")} onClick={() => setHistoryOpen(o => !o)} aria-expanded={historyOpen}>
                        <ChevronRight className={cn("h-3 w-3 transition-transform", historyOpen && "rotate-90")} />
                        {row.history.length} older certificate{row.history.length === 1 ? "" : "s"}
                    </button>
                    {historyOpen && (
                        <ul className="mt-1 flex flex-col gap-0.5">
                            {row.history.map(h => (
                                <li key={h.id}>
                                    <button type="button" onClick={() => onView(h, row)} className="flex min-h-9 w-full items-center gap-2 rounded-md px-2 text-left text-xs hover:bg-muted/50">
                                        <span className="truncate font-mono text-muted-foreground">{h.common_name}</span>
                                        <span className="hidden font-mono text-[12px] text-muted-foreground/70 sm:inline">{h.serial.slice(0, 12)}…</span>
                                        <span className="ml-auto shrink-0 text-muted-foreground">
                                            {h.is_revoked ? `revoked${h.revoked_at ? ` ${formatDate(h.revoked_at)}` : ""}` : daysLabel(h.days_left)}
                                        </span>
                                    </button>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
            )}
        </div>
    )
}
