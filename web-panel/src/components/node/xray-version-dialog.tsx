import { useState, useEffect } from "react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogTrigger,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Badge } from "@/components/ui/badge"
import { toast } from "sonner"
import { formatDate } from "@/lib/utils"
import { getXrayReleases, updateXrayVersion } from "@/lib/admin-api"
import type { XrayRelease } from "@/lib/admin-api"
import { Loader2, Settings } from "lucide-react"

interface XrayVersionDialogProps {
    nodeId: number
    currentVersion?: string
    isOnline: boolean
    onRefresh?: () => void
}

export function XrayVersionDialog({ nodeId, currentVersion, isOnline, onRefresh }: XrayVersionDialogProps) {
    const [open, setOpen] = useState(false)
    const [releases, setReleases] = useState<XrayRelease[]>([])
    const [loading, setLoading] = useState(false)
    const [updating, setUpdating] = useState(false)
    const [selectedVersion, setSelectedVersion] = useState<string | null>(null)

    // Extract clean version number from "vX.Y.Z" format
    const cleanCurrentVersion = currentVersion?.replace(/^v/, "") || ""

    useEffect(() => {
        if (!open) return
        setLoading(true)
        setSelectedVersion(null)
        getXrayReleases()
            .then((res) => {
                if (res.success && res.data) {
                    setReleases(res.data)
                } else {
                    toast.error("Failed to fetch releases")
                }
            })
            .catch(() => toast.error("Failed to fetch releases"))
            .finally(() => setLoading(false))
    }, [open])

    const handleUpdate = async () => {
        if (!selectedVersion) return
        setUpdating(true)
        try {
            const res = await updateXrayVersion(nodeId, selectedVersion)
            if (res.success) {
                toast.success(res.data?.message || "Xray version updated")
                setOpen(false)
                onRefresh?.()
            } else {
                toast.error(res.error || "Failed to update version")
            }
        } catch {
            toast.error("An unexpected error occurred")
        } finally {
            setUpdating(false)
        }
    }

    return (
        <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
                <button
                    disabled={!isOnline}
                    className="text-muted-foreground/50 group-hover:text-indigo-500 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
                    title={isOnline ? "Manage Xray version" : "Node is offline"}
                >
                    <Settings className="w-4 h-4" />
                </button>
            </DialogTrigger>
            <DialogContent className="max-w-md">
                <DialogHeader>
                    <DialogTitle>Update Xray Version</DialogTitle>
                    <DialogDescription>
                        {cleanCurrentVersion
                            ? `Currently running v${cleanCurrentVersion}`
                            : "Select a version to install"}
                    </DialogDescription>
                </DialogHeader>

                {loading ? (
                    <div className="flex items-center justify-center py-8">
                        <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
                    </div>
                ) : (
                    <>
                        <ScrollArea className="h-[300px] rounded-lg border border-border/50">
                            <div className="flex flex-col">
                                {releases.map((release) => {
                                    const isCurrent = release.version === cleanCurrentVersion
                                    const isSelected = release.version === selectedVersion
                                    return (
                                        <button
                                            key={release.version}
                                            onClick={() => !isCurrent && setSelectedVersion(release.version)}
                                            disabled={isCurrent}
                                            className={`flex items-center justify-between px-3 py-2.5 text-left transition-colors border-b border-border/30 last:border-b-0 ${
                                                isSelected
                                                    ? "bg-indigo-500/10"
                                                    : isCurrent
                                                    ? "bg-muted/30"
                                                    : "hover:bg-muted/50"
                                            }`}
                                        >
                                            <div className="flex flex-col gap-0.5">
                                                <div className="flex items-center gap-2">
                                                    <span className="text-sm font-mono font-medium">
                                                        v{release.version}
                                                    </span>
                                                    {isCurrent && (
                                                        <Badge variant="outline" className="text-[9px] px-1.5 py-0 h-4 uppercase font-bold text-emerald-500 border-emerald-500/30">
                                                            Current
                                                        </Badge>
                                                    )}
                                                    {isSelected && (
                                                        <Badge variant="outline" className="text-[9px] px-1.5 py-0 h-4 uppercase font-bold text-indigo-500 border-indigo-500/30">
                                                            Selected
                                                        </Badge>
                                                    )}
                                                </div>
                                                <span className="text-[11px] text-muted-foreground">
                                                    {formatDate(release.published_at, "long")}
                                                </span>
                                            </div>
                                        </button>
                                    )
                                })}
                                {releases.length === 0 && (
                                    <div className="text-center py-8 text-sm text-muted-foreground">
                                        No releases found
                                    </div>
                                )}
                            </div>
                        </ScrollArea>

                        <Button
                            onClick={handleUpdate}
                            disabled={!selectedVersion || updating}
                            className="w-full"
                        >
                            {updating ? (
                                <>
                                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                                    Updating...
                                </>
                            ) : selectedVersion ? (
                                `Update to v${selectedVersion}`
                            ) : (
                                "Select a version"
                            )}
                        </Button>
                    </>
                )}
            </DialogContent>
        </Dialog>
    )
}
