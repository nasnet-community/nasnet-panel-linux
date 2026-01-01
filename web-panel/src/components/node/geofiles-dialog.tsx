import { useState } from "react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogTrigger,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { toast } from "sonner"
import { updateGeoFiles } from "@/lib/admin-api"
import { Globe, Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"

interface GeofilesDialogProps {
    nodeId: number
    isOnline: boolean
    onRefresh?: () => void
    /** Optional trigger element. If omitted, renders a default icon button. */
    trigger?: React.ReactNode
    /** Controlled open state (for use without trigger, e.g. from dropdown menus). */
    open?: boolean
    /** Controlled open state callback. */
    onOpenChange?: (open: boolean) => void
}

const regions = [
    {
        id: "iran",
        label: "Iran",
        flag: "\u{1F1EE}\u{1F1F7}",
        description: "chocolate4u/Iran-v2ray-rules",
    },
    {
        id: "china",
        label: "China",
        flag: "\u{1F1E8}\u{1F1F3}",
        description: "Loyalsoldier/v2ray-rules-dat",
    },
    {
        id: "russia",
        label: "Russia",
        flag: "\u{1F1F7}\u{1F1FA}",
        description: "v2fly/geoip + domain-list-community",
    },
    {
        id: "custom",
        label: "Custom",
        flag: "",
        description: "Provide your own URLs",
    },
] as const

export function GeofilesDialog({
    nodeId,
    isOnline,
    onRefresh,
    trigger,
    open: controlledOpen,
    onOpenChange: controlledOnOpenChange,
}: GeofilesDialogProps) {
    const [internalOpen, setInternalOpen] = useState(false)
    const isControlled = controlledOpen !== undefined
    const open = isControlled ? controlledOpen : internalOpen
    const setOpen = isControlled ? controlledOnOpenChange! : setInternalOpen

    const [selectedRegion, setSelectedRegion] = useState<string>("iran")
    const [customGeoIPURL, setCustomGeoIPURL] = useState("")
    const [customGeoSiteURL, setCustomGeoSiteURL] = useState("")
    const [updating, setUpdating] = useState(false)

    const handleUpdate = async () => {
        if (!selectedRegion) return

        if (selectedRegion === "custom") {
            if (!customGeoIPURL || !customGeoSiteURL) {
                toast.error("Both GeoIP and GeoSite URLs are required for custom region")
                return
            }
        }

        setUpdating(true)
        try {
            const res = await updateGeoFiles(nodeId, {
                region: selectedRegion,
                ...(selectedRegion === "custom" && {
                    custom_geoip_url: customGeoIPURL,
                    custom_geosite_url: customGeoSiteURL,
                }),
            })
            if (res.success) {
                toast.success(res.data?.message || "Geofiles updated successfully")
                setOpen(false)
                onRefresh?.()
            } else {
                toast.error(res.error || "Failed to update geofiles")
            }
        } catch {
            toast.error("An unexpected error occurred")
        } finally {
            setUpdating(false)
        }
    }

    const dialogContent = (
        <DialogContent className="max-w-md">
            <DialogHeader>
                <DialogTitle>Update Geofiles</DialogTitle>
                <DialogDescription>
                    Update geoip.dat and geosite.dat on the node for geographic routing rules
                </DialogDescription>
            </DialogHeader>

            <div className="space-y-3">
                {regions.map((region) => (
                    <button
                        key={region.id}
                        onClick={() => setSelectedRegion(region.id)}
                        className={cn(
                            "w-full flex items-center gap-3 px-4 py-3 rounded-lg border text-left transition-all",
                            selectedRegion === region.id
                                ? "border-primary bg-primary/5 ring-1 ring-primary/20"
                                : "border-border/50 hover:border-border hover:bg-muted/30"
                        )}
                    >
                        <span className="text-xl w-7 text-center shrink-0">
                            {region.flag || <Globe className="w-5 h-5 text-muted-foreground inline" />}
                        </span>
                        <div className="min-w-0">
                            <div className="font-medium text-sm">{region.label}</div>
                            <div className="text-[11px] text-muted-foreground truncate">
                                {region.description}
                            </div>
                        </div>
                    </button>
                ))}
            </div>

            {selectedRegion === "custom" && (
                <div className="space-y-3 animate-in fade-in slide-in-from-top-2 duration-200">
                    <div className="space-y-1.5">
                        <Label htmlFor="geoip-url" className="text-xs">GeoIP URL</Label>
                        <Input
                            id="geoip-url"
                            placeholder="https://example.com/geoip.dat"
                            value={customGeoIPURL}
                            onChange={(e) => setCustomGeoIPURL(e.target.value)}
                            className="text-sm"
                        />
                    </div>
                    <div className="space-y-1.5">
                        <Label htmlFor="geosite-url" className="text-xs">GeoSite URL</Label>
                        <Input
                            id="geosite-url"
                            placeholder="https://example.com/geosite.dat"
                            value={customGeoSiteURL}
                            onChange={(e) => setCustomGeoSiteURL(e.target.value)}
                            className="text-sm"
                        />
                    </div>
                </div>
            )}

            <Button
                onClick={handleUpdate}
                disabled={updating || !selectedRegion}
                className="w-full"
            >
                {updating ? (
                    <>
                        <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        Updating Geofiles...
                    </>
                ) : (
                    <>
                        <Globe className="w-4 h-4 mr-2" />
                        Update Geofiles
                    </>
                )}
            </Button>
        </DialogContent>
    )

    // Controlled mode (no trigger needed)
    if (isControlled) {
        return (
            <Dialog open={open} onOpenChange={setOpen}>
                {dialogContent}
            </Dialog>
        )
    }

    // Trigger mode
    return (
        <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
                {trigger || (
                    <button
                        disabled={!isOnline}
                        className="text-muted-foreground/50 group-hover:text-indigo-500 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
                        title={isOnline ? "Update Geofiles" : "Node is offline"}
                    >
                        <Globe className="w-4 h-4" />
                    </button>
                )}
            </DialogTrigger>
            {dialogContent}
        </Dialog>
    )
}
