import { useState } from "react"
import { Copy, Check, QrCode, Link as LinkIcon, Server, Gauge, Send } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { toast } from "@/lib/toast"
import { QRDialog } from "./qr-dialog"

interface CoreConfigCardProps {
    /** "ssh" | "mtproto" */
    protocol: string
    /** Link URIs, one per line. */
    content: string
    /** Node label (one card per tunnel). */
    nodeName?: string
    /** Per-tunnel consumed-traffic display (e.g. "4.2 GB"). */
    dataUsedDisplay?: string
}

const TITLES: Record<string, string> = { ssh: "SSH tunnel", mtproto: "Telegram proxy" }

interface SshFields {
    host: string
    port: string
    username: string
    password: string
}

/** Reads ssh://base64url(user:password)@host:port#label into its fields. */
function parseSshLink(link: string): SshFields | null {
    const m = /^ssh:\/\/([^@]+)@(\[[^\]]+\]|[^:/#]+):(\d+)/.exec(link)
    if (!m) return null
    try {
        const b64 = m[1].replace(/-/g, "+").replace(/_/g, "/")
        const creds = atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4))
        const i = creds.indexOf(":")
        if (i < 0) return null
        return { host: m[2].replace(/^\[|\]$/g, ""), port: m[3], username: creds.slice(0, i), password: creds.slice(i + 1) }
    } catch {
        return null
    }
}

/**
 * One SSH or MTProto tunnel on the public sub panel. SSH also lists the
 * connection fields, since many SSH-tunnel apps take them one by one; MTProto
 * links open straight in Telegram.
 */
export function CoreConfigCard({ protocol, content, nodeName, dataUsedDisplay }: CoreConfigCardProps) {
    const [copied, setCopied] = useState<string | null>(null)
    const [qrValue, setQrValue] = useState<string | null>(null)

    const copy = async (text: string, key: string, msg = "Copied") => {
        try {
            await navigator.clipboard.writeText(text)
        } catch {
            toast.error("Couldn’t copy — select and copy manually")
            return
        }
        setCopied(key)
        setTimeout(() => setCopied((c) => (c === key ? null : c)), 1500)
        toast.success(msg)
    }

    const links = content.split("\n").map((s) => s.trim()).filter(Boolean)
    const ssh = protocol === "ssh" && links[0] ? parseSshLink(links[0]) : null
    const copyIcon = (key: string) =>
        copied === key ? <Check className="w-3.5 h-3.5 text-emerald-500" /> : <Copy className="w-3.5 h-3.5" />

    return (
        <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
            <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-3">
                <div className="flex items-center gap-2">
                    <LinkIcon className="w-4 h-4 text-primary" />
                    <h2 className="text-sm font-medium text-muted-foreground">
                        {TITLES[protocol] ?? protocol}
                    </h2>
                    {nodeName && (
                        <span className="flex items-center gap-1 text-xs text-muted-foreground">
                            <Server className="w-3 h-3" />
                            {nodeName}
                        </span>
                    )}
                    <Badge variant="outline" className="ml-auto text-xs">
                        {links.length > 1 ? `${links.length} links` : "link"}
                    </Badge>
                </div>

                {dataUsedDisplay && (
                    <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Gauge className="w-3.5 h-3.5" />
                        <span>Used</span>
                        <span className="ml-auto font-medium tabular-nums text-foreground">{dataUsedDisplay}</span>
                    </div>
                )}

                {links.length === 0 ? (
                    <p className="text-sm text-muted-foreground">
                        No config available yet. It appears once the subscription is active.
                    </p>
                ) : (
                    <>
                        {ssh && (
                            <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 rounded-md border bg-muted/40 px-3 py-1.5 text-xs">
                                {(
                                    [
                                        ["Host", ssh.host],
                                        ["Port", ssh.port],
                                        ["Username", ssh.username],
                                        ["Password", ssh.password],
                                    ] as const
                                ).map(([label, value]) => (
                                    <div key={label} className="contents">
                                        <dt className="text-muted-foreground">{label}</dt>
                                        <dd className="truncate font-mono">{value}</dd>
                                        <Button
                                            size="icon"
                                            variant="ghost"
                                            className="size-9 shrink-0"
                                            onClick={() => copy(value, label, `${label} copied`)}
                                            aria-label={`Copy ${label.toLowerCase()}`}
                                        >
                                            {copyIcon(label)}
                                        </Button>
                                    </div>
                                ))}
                            </dl>
                        )}
                        <div className="space-y-2">
                            {links.map((link, i) => (
                                <div key={i} className="flex items-center gap-2 rounded-md border bg-muted/40 p-2">
                                    <span className="flex-1 truncate font-mono text-xs">{link}</span>
                                    {link.startsWith("tg://") && (
                                        <Button asChild size="icon" variant="ghost" className="size-11 shrink-0">
                                            <a href={link} aria-label="Open in Telegram">
                                                <Send className="w-3.5 h-3.5" />
                                            </a>
                                        </Button>
                                    )}
                                    <Button
                                        size="icon"
                                        variant="ghost"
                                        className="size-11 shrink-0"
                                        onClick={() => copy(link, `l${i}`, "Link copied")}
                                        aria-label="Copy link"
                                    >
                                        {copyIcon(`l${i}`)}
                                    </Button>
                                    <Button
                                        size="icon"
                                        variant="ghost"
                                        className="size-11 shrink-0"
                                        onClick={() => setQrValue(link)}
                                        aria-label="Show QR code"
                                    >
                                        <QrCode className="w-3.5 h-3.5" />
                                    </Button>
                                </div>
                            ))}
                        </div>
                    </>
                )}
            </CardContent>
            <QRDialog
                open={qrValue !== null}
                onOpenChange={(o) => !o && setQrValue(null)}
                value={qrValue || ""}
                title={protocol === "mtproto" ? "Telegram proxy" : "SSH tunnel"}
                description={
                    protocol === "mtproto"
                        ? "Scan with your phone's camera to open this proxy in Telegram."
                        : "Scan with an SSH-tunnel app that imports ssh:// links."
                }
            />
        </Card>
    )
}
