import { HiOutlineClipboardCopy, HiOutlineDownload } from "react-icons/hi"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { copyToClipboard } from "@/lib/utils"
import { toast } from "@/lib/toast"
import { useReverseTunnelPeerConfig } from "@/lib/queries/use-reverse-tunnels"

interface ReverseTunnelPeerDialogProps {
    tunnelId: number | null
    onClose: () => void
}

// Shows what to set up on the end of a tunnel the panel doesn't run.
export function ReverseTunnelPeerDialog({ tunnelId, onClose }: ReverseTunnelPeerDialogProps) {
    const { data, isLoading, error } = useReverseTunnelPeerConfig(tunnelId)
    const bridge = data?.kind === "external_bridge"

    return (
        <ResponsiveDialog
            open={tunnelId != null}
            onOpenChange={(open) => !open && onClose()}
            title={bridge ? "External bridge config" : "External portal client"}
            description={bridge
                ? "Run this config with Xray 26.5 or newer on the bridge. It dials this node and sends the tunnel's traffic out to the internet, with private addresses blocked."
                : "Add this client to the VLESS inbound your link points at on the external portal, then route traffic there to its reverse tag."}
            onSave={onClose}
            saveLabel="Done"
        >
            {isLoading ? (
                <p className="text-sm text-muted-foreground">Loading...</p>
            ) : error ? (
                <p className="text-sm text-destructive">{error instanceof Error ? error.message : "Failed to load"}</p>
            ) : data ? (
                <div className="space-y-5">
                    {data.config && <CodeBlock label="Xray config" text={data.config} file="bridge-config.json" />}
                    {data.link && <CodeBlock label="Link the bridge dials" text={data.link} />}
                    {data.client && <CodeBlock label="Client entry" text={data.client} />}
                </div>
            ) : null}
        </ResponsiveDialog>
    )
}

function CodeBlock({ label, text, file }: { label: string; text: string; file?: string }) {
    const download = () => {
        const url = URL.createObjectURL(new Blob([text], { type: "application/json" }))
        const a = document.createElement("a")
        a.href = url
        a.download = file!
        a.click()
        URL.revokeObjectURL(url)
    }
    return (
        <div className="space-y-2">
            <div className="flex items-center justify-between gap-2">
                <Label>{label}</Label>
                <div className="flex gap-1">
                    <Button
                        variant="ghost"
                        size="sm"
                        onClick={async () => {
                            if (await copyToClipboard(text)) toast.success("Copied")
                        }}
                    >
                        <HiOutlineClipboardCopy className="w-4 h-4 mr-1.5" />
                        Copy
                    </Button>
                    {file && (
                        <Button variant="ghost" size="sm" onClick={download}>
                            <HiOutlineDownload className="w-4 h-4 mr-1.5" />
                            Download
                        </Button>
                    )}
                </div>
            </div>
            <pre className="max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 text-xs font-mono whitespace-pre-wrap break-all">{text}</pre>
        </div>
    )
}
