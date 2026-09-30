import { useCallback, useEffect, useState } from "react"
import { CheckCircle2, Copy, Download, Loader2, RefreshCw, Terminal } from "lucide-react"
import { Link } from "react-router"
import { getInstallCommand, getPasteScript, type DeployInstructions } from "@/lib/api/nodes"
import { useNodes } from "@/lib/queries"
import { Button } from "@/components/ui/button"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { copyToClipboard, cn } from "@/lib/utils"
import { toast } from "@/lib/toast"

interface DeployAgentDialogProps {
    nodeId: number | null
    open: boolean
    onOpenChange: (open: boolean) => void
}

// btoa only takes Latin-1; the script carries UTF-8 (log glyphs).
function base64UTF8(text: string): string {
    const bytes = new TextEncoder().encode(text)
    let binary = ""
    for (const b of bytes) binary += String.fromCharCode(b)
    return btoa(binary)
}

function CommandBox({ text }: { text: string }) {
    return (
        <div className="flex gap-2 rounded-md bg-muted px-3 py-2.5 font-mono text-xs text-muted-foreground">
            <Terminal className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
            <span className="min-w-0 truncate" title={text}>{text}</span>
        </div>
    )
}

export function DeployAgentDialog({ nodeId, open, onOpenChange }: DeployAgentDialogProps) {
    const [info, setInfo] = useState<DeployInstructions | null>(null)
    const [loading, setLoading] = useState(false)
    const [busy, setBusy] = useState<"copy" | "download" | null>(null)
    const [copied, setCopied] = useState<"pull" | "paste" | null>(null)
    const { data: nodes } = useNodes(open)
    const node = nodes?.find(n => n.id === nodeId)

    const load = useCallback(() => {
        if (!nodeId) return
        setLoading(true)
        getInstallCommand(nodeId)
            .then(res => {
                if (res.success && res.data) setInfo(res.data)
                else toast.error(res.error || "Failed to load the install steps")
            })
            .catch(() => toast.error("Failed to load the install steps"))
            .finally(() => setLoading(false))
    }, [nodeId])

    useEffect(() => {
        if (open) load()
    }, [open, load])

    const flash = (which: "pull" | "paste") => {
        setCopied(which)
        setTimeout(() => setCopied(null), 2000)
    }

    const copyPull = async () => {
        if (!info?.command) return
        await copyToClipboard(info.command)
        flash("pull")
    }

    const copyPaste = async () => {
        if (!nodeId) return
        setBusy("copy")
        try {
            const script = await getPasteScript(nodeId)
            await copyToClipboard(`echo ${base64UTF8(script)} | base64 -d | sudo bash`)
            flash("paste")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to build the installer")
        } finally {
            setBusy(null)
        }
    }

    const downloadPaste = async () => {
        if (!nodeId) return
        setBusy("download")
        try {
            const script = await getPasteScript(nodeId)
            const url = URL.createObjectURL(new Blob([script], { type: "text/x-shellscript" }))
            const a = document.createElement("a")
            a.href = url
            a.download = `install-agent-${nodeId}.sh`
            a.click()
            URL.revokeObjectURL(url)
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to build the installer")
        } finally {
            setBusy(null)
        }
    }

    const pull = info?.mode === "pull"
    const name = info?.node_name ?? node?.name ?? "node"

    return (
        <Dialog open={open} onOpenChange={(next) => { if (!next) setInfo(null); onOpenChange(next) }}>
            <DialogContent className="sm:max-w-xl">
                <DialogHeader>
                    <DialogTitle>Deploy {name}</DialogTitle>
                    <DialogDescription>
                        Run {pull ? "one of these" : "this"} on the node as root. It installs Xray and the agent.
                    </DialogDescription>
                </DialogHeader>

                {loading && !info ? (
                    <div className="space-y-3 py-2">
                        <Skeleton className="h-4 w-1/2" />
                        <Skeleton className="h-10 w-full" />
                        <Skeleton className="h-4 w-2/3" />
                    </div>
                ) : info && (
                    <div className="space-y-3 py-2">
                        {pull && (
                            <section className="space-y-2.5 rounded-lg border p-3.5">
                                <div className="flex items-center justify-between gap-3">
                                    <h3 className="text-sm font-semibold">One-line install</h3>
                                    <span className="font-mono text-xs text-muted-foreground">one use · 1 hour</span>
                                </div>
                                <CommandBox text={info.command} />
                                <div className="flex flex-wrap gap-2">
                                    <Button size="sm" onClick={copyPull}>
                                        {copied === "pull" ? <CheckCircle2 className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                                        {copied === "pull" ? "Copied" : "Copy command"}
                                    </Button>
                                    <Button size="sm" variant="outline" onClick={load} disabled={loading}>
                                        <RefreshCw className={cn("h-3.5 w-3.5", loading && "animate-spin")} />
                                        New command
                                    </Button>
                                </div>
                            </section>
                        )}

                        <section className="space-y-2.5 rounded-lg border p-3.5">
                            <h3 className="text-sm font-semibold">Paste script</h3>
                            <p className="text-xs text-muted-foreground">
                                {pull
                                    ? "For a server that can't reach this panel. It carries the node's certificate and key: keep it private."
                                    : "Needs nothing from this router. It carries the node's certificate and key: keep it private."}
                            </p>
                            <div className="flex flex-wrap gap-2">
                                <Button size="sm" variant={pull ? "outline" : "default"} onClick={copyPaste} disabled={busy !== null}>
                                    {busy === "copy" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : copied === "paste" ? <CheckCircle2 className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                                    {copied === "paste" ? "Copied" : "Copy one-liner"}
                                </Button>
                                <Button size="sm" variant="outline" onClick={downloadPaste} disabled={busy !== null}>
                                    {busy === "download" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
                                    Download install.sh
                                </Button>
                            </div>
                        </section>

                        {!pull && info.reason && (
                            <section className="space-y-1 rounded-lg border border-dashed bg-muted/30 p-3.5">
                                <h3 className="text-sm font-semibold text-muted-foreground">One-line install</h3>
                                <p className="text-xs text-muted-foreground">
                                    {info.reason}{" "}
                                    <Link to="/settings" className="underline underline-offset-2 hover:text-foreground">Open settings</Link>
                                </p>
                            </section>
                        )}

                        <p className="flex items-center gap-2 text-xs text-muted-foreground">
                            <span className={cn("h-2 w-2 rounded-full", node?.is_online ? "bg-emerald-500" : "bg-amber-500")} aria-hidden />
                            {node?.is_online ? "Connected" : "Waiting for the agent to connect…"}
                        </p>
                    </div>
                )}
            </DialogContent>
        </Dialog>
    )
}
