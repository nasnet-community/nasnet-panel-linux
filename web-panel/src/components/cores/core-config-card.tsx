import { useState } from "react"
import { Copy, QrCode, Check, TriangleAlert, Info } from "lucide-react"
import { toast } from "@/lib/toast"
import { copyToClipboard, cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useCoreUserConfig } from "@/lib/queries"
import type { ClientArtifact, CoreProtocol } from "@/lib/types/cores"
import { CoreConfigQrDialog } from "./core-config-qr-dialog"

/** Per-protocol "how to use" hint shown under the artifact. */
function importHint(protocol: CoreProtocol): string | null {
    switch (protocol) {
        case "ssh":
            return "Import into an SSH-tunnel client, or connect from a terminal with the host, port, username and password."
        case "mtproto":
            return "Open the link on a device with Telegram installed to add the proxy."
        default:
            return null
    }
}

/** Human label for an MTProto secret's camouflage mode, read off its prefix. */
function linkLabel(uri: string): string | null {
    const secret = /[?&]secret=([^&]+)/.exec(uri)?.[1]
    if (!secret) return null
    if (secret.startsWith("ee")) {
        // ee<32 hex><hex(sni)> — decode the domain so the operator can tell the
        // per-domain links apart.
        const domainHex = secret.slice(34)
        let domain = ""
        for (let i = 0; i + 1 < domainHex.length; i += 2) {
            domain += String.fromCharCode(parseInt(domainHex.slice(i, i + 2), 16))
        }
        return domain ? `FakeTLS · ${domain}` : "FakeTLS"
    }
    if (secret.startsWith("dd")) return "Secure (dd)"
    return "Classic"
}

/** One link row with its own copy/QR — a multi-mode artifact must not be
 *  copied or QR-encoded as one blob. */
function LinkRow({ uri, label }: { uri: string; label: string | null }) {
    const [qrOpen, setQrOpen] = useState(false)
    const [copied, setCopied] = useState(false)
    const copy = async () => {
        await copyToClipboard(uri)
        setCopied(true)
        toast.success("Copied to clipboard")
        setTimeout(() => setCopied(false), 1500)
    }
    return (
        <div className="rounded-lg border bg-muted/40 p-3">
            {label && (
                <p className="mb-1.5 text-[12px] font-medium uppercase tracking-wide text-muted-foreground">
                    {label}
                </p>
            )}
            <pre className="max-h-32 overflow-auto whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-foreground">
                {uri}
            </pre>
            <div className="mt-2 flex flex-wrap gap-2">
                <Button variant="outline" size="sm" onClick={copy}>
                    {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
                    Copy link
                </Button>
                <Button variant="outline" size="sm" onClick={() => setQrOpen(true)}>
                    <QrCode className="size-4" />
                    QR code
                </Button>
            </div>
            <CoreConfigQrDialog open={qrOpen} onOpenChange={setQrOpen} value={uri} />
        </div>
    )
}

function ConfigView({ art, protocol }: { art: ClientArtifact; protocol: CoreProtocol }) {
    const [qrOpen, setQrOpen] = useState(false)
    const [copied, setCopied] = useState(false)

    // An artifact may carry several links (e.g. one MTProto link per
    // camouflage mode / SNI domain), so each gets its own row.
    const uris = art.content.split("\n").map((l) => l.trim()).filter(Boolean)

    const copy = async () => {
        await copyToClipboard(art.content)
        setCopied(true)
        toast.success("Copied to clipboard")
        setTimeout(() => setCopied(false), 1500)
    }

    const hint = importHint(protocol)
    const addressNote = art.address_missing && (
        <p className="flex items-start gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/5 p-2.5 text-xs text-muted-foreground">
            <Info className="mt-0.5 size-3.5 shrink-0 text-amber-500" />
            <span>
                This node has no public address, so the link uses the one you opened the panel on. Set the
                panel's public address (<span className="font-mono">APP_BASE_URL</span>) so links work from outside.
            </span>
        </p>
    )

    if (uris.length > 1) {
        return (
            <div className="space-y-3">
                {addressNote}
                {uris.map((uri) => (
                    <LinkRow key={uri} uri={uri} label={linkLabel(uri)} />
                ))}
                {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
            </div>
        )
    }

    return (
        <div className="space-y-3">
            {addressNote}
            <div className="rounded-lg border bg-muted/40 p-3">
                <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-foreground">
                    {art.content}
                </pre>
            </div>

            <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" onClick={copy}>
                    {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
                    Copy link
                </Button>
                <Button variant="outline" size="sm" onClick={() => setQrOpen(true)}>
                    <QrCode className="size-4" />
                    QR code
                </Button>
            </div>

            {hint && <p className="text-xs text-muted-foreground">{hint}</p>}

            <CoreConfigQrDialog open={qrOpen} onOpenChange={setQrOpen} value={art.content} />
        </div>
    )
}

export function CoreConfigCard({
    userId,
    host,
    protocol,
    enabled = true,
}: {
    userId: number
    host?: string
    protocol: CoreProtocol
    enabled?: boolean
}) {
    const { data: art, isLoading, isError, error } = useCoreUserConfig(userId, host, enabled)

    if (isLoading) {
        return (
            <div className="space-y-3">
                <Skeleton className="h-24 w-full rounded-lg" />
                <div className="flex gap-2">
                    <Skeleton className="h-8 w-24" />
                    <Skeleton className="h-8 w-24" />
                </div>
            </div>
        )
    }

    if (isError || !art) {
        return (
            <div className={cn("flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-500/5 p-3 text-sm")}>
                <TriangleAlert className="mt-0.5 size-4 shrink-0 text-red-500" />
                <div>
                    <p className="font-medium text-red-600 dark:text-red-400">Couldn't build client config</p>
                    <p className="text-muted-foreground">
                        {error instanceof Error ? error.message : "The node may be offline or the protocol config is incomplete."}
                    </p>
                </div>
            </div>
        )
    }

    return <ConfigView art={art} protocol={protocol} />
}
