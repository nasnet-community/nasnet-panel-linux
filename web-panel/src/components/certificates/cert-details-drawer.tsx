import { useState } from "react"
import { Copy, Download, KeyRound } from "lucide-react"
import { toast } from "@/lib/toast"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { revealPrivateKey } from "@/lib/api/certificates"
import { useCertificateDetails } from "@/lib/queries"
import type { CertSummary, NodeCertRow } from "@/lib/types"
import { cn, copyToClipboard, formatDateTime } from "@/lib/utils"
import { certErrorMessage, certTypeLabel, daysLabel, downloadText, expiryTone, toneBadge, toneText } from "./cert-helpers"
import { T } from "./cert-ui"

interface CertDetailsDrawerProps {
    cert: CertSummary | null
    row?: NodeCertRow
    onClose: () => void
}

async function copy(text: string, label: string) {
    await copyToClipboard(text)
    toast.success(`${label} copied`)
}

export function CertDetailsDrawer({ cert, row, onClose }: CertDetailsDrawerProps) {
    const { data, isLoading } = useCertificateDetails(cert?.id ?? null)
    const confirm = useConfirm()
    // Keyed by cert id so switching certs never shows another cert's key.
    const [revealed, setRevealed] = useState<{ id: number; key: string } | null>(null)
    const key = cert && revealed?.id === cert.id ? revealed.key : null

    const reveal = () => {
        if (!cert) return
        const isCA = cert.type === "ca"
        void confirm({
            title: "Reveal the private key?",
            description: isCA
                ? "Anyone with the CA key can issue certificates every node trusts. The reveal is written to the audit log."
                : "The reveal is written to the audit log.",
            variant: "warning",
            typeToConfirm: isCA ? cert.common_name : undefined,
            confirmLabel: "Reveal",
            onConfirm: async () => {
                try {
                    const res = await revealPrivateKey(cert.id, isCA ? cert.common_name : undefined)
                    if (!res.success || !res.data) throw new Error(res.error || "Couldn't load the key")
                    setRevealed({ id: cert.id, key: res.data.private_key })
                } catch (e) {
                    toast.error(certErrorMessage(e))
                    throw e
                }
            },
        })
    }

    const tone = expiryTone(cert?.days_left)
    const presentedHere = row?.presented && row.current?.id === cert?.id ? row.presented : null
    const names = [...(data?.dns_names ?? []), ...(data?.ip_addresses ?? [])]

    return (
        <Sheet open={!!cert} onOpenChange={o => { if (!o) onClose() }}>
            <SheetContent className="w-full overflow-y-auto sm:max-w-lg">
                <SheetHeader>
                    <SheetTitle className="break-all font-mono text-base">{cert?.common_name}</SheetTitle>
                    <SheetDescription>
                        {cert ? certTypeLabel(cert.type) : ""} certificate{cert?.node_name ? ` · ${cert.node_name}` : ""}
                    </SheetDescription>
                </SheetHeader>
                {cert && (
                    <div className="flex flex-col gap-4 px-4 pb-6">
                        <div className="flex items-center gap-2">
                            <Badge variant="outline" className={cn("px-2 py-0 text-[12px]", cert.is_revoked ? toneBadge.muted : toneBadge[tone])}>
                                {cert.is_revoked ? "Revoked" : cert.days_left < 0 ? "Expired" : "Valid"}
                            </Badge>
                            {!cert.is_revoked && <span className={cn("text-xs", toneText[tone])}>{daysLabel(cert.days_left)} left</span>}
                        </div>

                        {presentedHere && (
                            <div className="grid grid-cols-2 gap-3 rounded-lg border border-border/60 p-3 text-xs">
                                <div>
                                    <div className={T.eyebrow}>On record</div>
                                    <div className="mt-1 truncate font-mono">{cert.serial.slice(0, 16)}…</div>
                                </div>
                                <div>
                                    <div className={T.eyebrow}>Node presents</div>
                                    <div className={cn("mt-1 truncate font-mono", presentedHere.serial !== cert.serial && "text-amber-500")}>
                                        {presentedHere.serial.slice(0, 16)}…
                                    </div>
                                    <div className={T.meta}>seen {formatDateTime(presentedHere.seen_at)}</div>
                                </div>
                            </div>
                        )}

                        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-xs">
                            <dt className="text-muted-foreground">Serial</dt>
                            <dd className="flex min-w-0 items-center gap-1">
                                <span className="truncate font-mono">{cert.serial}</span>
                                <button type="button" onClick={() => void copy(cert.serial, "Serial")} aria-label="Copy serial"><Copy className="h-3 w-3" /></button>
                            </dd>
                            <dt className="text-muted-foreground">Valid from</dt>
                            <dd>{formatDateTime(cert.not_before)}</dd>
                            <dt className="text-muted-foreground">Valid until</dt>
                            <dd>{formatDateTime(cert.not_after)}</dd>
                            {cert.revoked_at && (<><dt className="text-muted-foreground">Revoked</dt><dd>{formatDateTime(cert.revoked_at)}</dd></>)}
                            {data?.issuer && (<><dt className="text-muted-foreground">Issuer</dt><dd>{data.issuer}</dd></>)}
                            {names.length > 0 && (<><dt className="text-muted-foreground">Names</dt><dd className="break-all font-mono">{names.join(", ")}</dd></>)}
                            {data?.fingerprint_sha256 && (
                                <>
                                    <dt className="text-muted-foreground">SHA-256</dt>
                                    <dd className="flex min-w-0 items-center gap-1">
                                        <span className="truncate font-mono">{data.fingerprint_sha256}</span>
                                        <button type="button" onClick={() => void copy(data.fingerprint_sha256!, "Fingerprint")} aria-label="Copy fingerprint"><Copy className="h-3 w-3" /></button>
                                    </dd>
                                </>
                            )}
                        </dl>

                        <div className="flex flex-col gap-2">
                            <div className="flex items-center justify-between">
                                <span className={T.eyebrow}>Certificate (PEM)</span>
                                {data?.certificate && (
                                    <div className="flex gap-1">
                                        <Button variant="ghost" size="sm" className="h-8" onClick={() => void copy(data.certificate, "Certificate")}><Copy className="mr-1 h-3.5 w-3.5" />Copy</Button>
                                        <Button variant="ghost" size="sm" className="h-8" onClick={() => downloadText(data.certificate, `${cert.common_name}.crt`)}><Download className="mr-1 h-3.5 w-3.5" />Download</Button>
                                    </div>
                                )}
                            </div>
                            <pre className="max-h-48 overflow-auto rounded-lg bg-muted/40 p-3 font-mono text-[12px] leading-4">
                                {isLoading ? "Loading…" : data?.certificate ?? "Unavailable"}
                            </pre>
                        </div>

                        {data?.has_private_key && (
                            key ? (
                                <div className="flex flex-col gap-2">
                                    <div className="flex items-center justify-between">
                                        <span className={T.eyebrow}>Private key</span>
                                        <Button variant="ghost" size="sm" className="h-8" onClick={() => void copy(key, "Private key")}><Copy className="mr-1 h-3.5 w-3.5" />Copy</Button>
                                    </div>
                                    <pre className="max-h-48 overflow-auto rounded-lg bg-muted/40 p-3 font-mono text-[12px] leading-4">{key}</pre>
                                </div>
                            ) : (
                                <Button variant="outline" size="sm" className="h-11 self-start sm:h-8" onClick={reveal}>
                                    <KeyRound className="mr-1.5 h-3.5 w-3.5" />Reveal private key
                                </Button>
                            )
                        )}
                        {cert.type === "agent" && data && !data.has_private_key && (
                            <p className={T.meta}>Node keys never leave the node.</p>
                        )}
                    </div>
                )}
            </SheetContent>
        </Sheet>
    )
}
