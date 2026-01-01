import { useEffect, useState } from "react"
import { QRCodeSVG } from "qrcode.react"
import { motion } from "framer-motion"
import { Check, Copy, Download, Plus, RotateCw, ShieldCheck, Smartphone, Trash2 } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { ConfirmDialogProvider, useConfirm } from "@/components/ui/confirm-dialog"
import { toast } from "sonner"
import { cn, copyToClipboard } from "@/lib/utils"
import {
    ApiError,
    useAddWgDevice,
    useRemoveWgDevice,
    useRotateWgDevice,
    useWgDevices,
    useWgServers,
} from "@/lib/queries/use-wg-devices"
import type { WgDevice } from "@/lib/types/sub-panel"

function fmtBytes(n: number): string {
    if (!n || n < 0) return "0 B"
    if (n < 1024) return `${n} B`
    const u = ["KB", "MB", "GB", "TB"]
    let v = n / 1024
    let i = 0
    while (v >= 1024 && i < u.length - 1) {
        v /= 1024
        i++
    }
    return `${v.toFixed(1)} ${u[i]}`
}

function flagEmoji(cc: string): string {
    if (!cc || cc.length !== 2) return "🌍"
    const up = cc.toUpperCase()
    return String.fromCodePoint(0x1f1e6 + (up.charCodeAt(0) - 65)) + String.fromCodePoint(0x1f1e6 + (up.charCodeAt(1) - 65))
}

function downloadConf(filename: string, text: string) {
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }))
    const a = document.createElement("a")
    a.href = url
    a.download = filename
    a.click()
    URL.revokeObjectURL(url)
}

// ConfigReveal shows a freshly created/rotated .conf ONCE — the private key is
// never stored server-side, so re-showing means regenerating.
function ConfigReveal({ label, conf, onClose }: { label: string; conf: string; onClose: () => void }) {
    const [copied, setCopied] = useState(false)
    const copy = async () => {
        const ok = await copyToClipboard(conf)
        if (!ok) {
            toast.error("Couldn’t copy — long-press to copy manually")
            return
        }
        setCopied(true)
        toast.success("Config copied")
        setTimeout(() => setCopied(false), 2000)
    }
    const safeName = (label || "wireguard").replace(/[^a-zA-Z0-9_-]+/g, "_")
    return (
        <Card className="border-emerald-500/30 bg-emerald-500/[0.04] py-0 gap-0">
            <CardContent className="flex flex-col items-center gap-3 p-4">
                <p className="text-center text-xs text-amber-500">
                    Save this now — it’s shown only once. Re-showing regenerates and invalidates this config.
                </p>
                <div className="rounded-lg bg-white p-3">
                    <QRCodeSVG value={conf} size={200} level="M" bgColor="#ffffff" fgColor="#000000" />
                </div>
                <div className="grid w-full grid-cols-2 gap-2">
                    <Button variant="outline" className="w-full" onClick={copy}>
                        {copied ? <Check className="size-4 text-emerald-500" /> : <Copy className="size-4" />}
                        {copied ? "Copied!" : "Copy config"}
                    </Button>
                    <Button variant="outline" className="w-full" onClick={() => downloadConf(`${safeName}.conf`, conf)}>
                        <Download className="size-4" /> Download .conf
                    </Button>
                </div>
                <Button className="w-full" onClick={onClose}>
                    Done
                </Button>
            </CardContent>
        </Card>
    )
}

function AddDeviceSheet({
    open,
    onOpenChange,
    servers,
    pending,
    onCreate,
}: {
    open: boolean
    onOpenChange: (v: boolean) => void
    servers: { inbound_id: number; node_name: string; country_code: string }[]
    pending: boolean
    onCreate: (label: string, inboundId?: number) => void
}) {
    const multi = servers.length > 1
    const [name, setName] = useState("")
    const [inboundId, setInboundId] = useState<number | undefined>(undefined)

    // Reset fields each time the sheet opens; default to the first server.
    useEffect(() => {
        if (open) {
            setName("")
            setInboundId(servers[0]?.inbound_id)
        }
    }, [open, servers])

    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent side="bottom" className="rounded-t-2xl">
                <SheetHeader className="pb-1">
                    <SheetTitle className="text-[15px]">Add WireGuard device</SheetTitle>
                    <SheetDescription className="text-xs">
                        A new config is generated and shown once. Name it so you can tell your devices apart.
                    </SheetDescription>
                </SheetHeader>

                <div className="space-y-4 px-4 pb-6">
                    <div className="space-y-1.5">
                        <Label htmlFor="wg-device-name" className="text-xs text-muted-foreground">
                            Name (optional)
                        </Label>
                        <Input
                            id="wg-device-name"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            placeholder="e.g. My Phone"
                            maxLength={48}
                            autoComplete="off"
                        />
                    </div>

                    {multi && (
                        <div className="space-y-1.5">
                            <Label className="text-xs text-muted-foreground">Server</Label>
                            <Select
                                value={inboundId != null ? String(inboundId) : undefined}
                                onValueChange={(v) => setInboundId(Number(v))}
                            >
                                <SelectTrigger className="w-full">
                                    <SelectValue placeholder="Choose a server" />
                                </SelectTrigger>
                                <SelectContent>
                                    {servers.map((s) => (
                                        <SelectItem key={s.inbound_id} value={String(s.inbound_id)}>
                                            {flagEmoji(s.country_code)} {s.node_name}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                    )}

                    <Button
                        className="w-full"
                        disabled={pending || servers.length === 0}
                        onClick={() => onCreate(name.trim(), inboundId)}
                    >
                        {pending ? "Creating…" : "Create config"}
                    </Button>
                </div>
            </SheetContent>
        </Sheet>
    )
}

function StatusBadge({ status }: { status: string }) {
    const active = status === "active"
    return (
        <Badge
            variant={active ? "success" : "outline"}
            className={cn("h-4 px-1.5 text-[10px] font-semibold", !active && "text-muted-foreground border-zinc-500/20")}
        >
            {active ? "active" : status || "disabled"}
        </Badge>
    )
}

function WgDevicesInner({ uuid }: { uuid: string }) {
    const confirm = useConfirm()
    const devicesQ = useWgDevices(uuid, true)
    const serversQ = useWgServers(uuid, true)
    const add = useAddWgDevice(uuid)
    const rotate = useRotateWgDevice(uuid)
    const remove = useRemoveWgDevice(uuid)

    const [reveal, setReveal] = useState<{ label: string; conf: string } | null>(null)
    const [sheetOpen, setSheetOpen] = useState(false)

    const list = devicesQ.data?.devices ?? []
    const max = devicesQ.data?.max_devices ?? 0
    const used = devicesQ.data?.used ?? list.length
    const servers = serversQ.data ?? []
    const multi = servers.length > 1
    const srvName = new Map(servers.map((s) => [s.inbound_id, s.node_name]))
    const full = max > 0 && used >= max

    const onCreate = async (label: string, inboundId?: number) => {
        try {
            const res = await add.mutateAsync(inboundId ? { inbound_id: inboundId, label } : { label })
            setReveal({ label: res.device.label || label || "wireguard", conf: res.config })
            setSheetOpen(false)
            toast.success("Device added")
        } catch (e) {
            if (e instanceof ApiError && e.status === 409) {
                toast.error("Device limit reached")
            } else {
                toast.error("Could not add device")
            }
        }
    }

    const onRotate = async (d: WgDevice) => {
        const ok = await confirm({
            title: "Regenerate config?",
            description: `“${d.label}” will get a new config. The current one stops working immediately.`,
            confirmLabel: "Regenerate",
            variant: "warning",
        })
        if (!ok) return
        try {
            const res = await rotate.mutateAsync(d.id)
            setReveal({ label: d.label, conf: res.config })
            toast.success("Config regenerated")
        } catch {
            toast.error("Could not regenerate")
        }
    }

    const onRemove = async (d: WgDevice) => {
        const ok = await confirm({
            title: "Remove device?",
            description: `“${d.label}” will stop working immediately.`,
            confirmLabel: "Remove",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await remove.mutateAsync(d.id)
            toast.success("Device removed")
        } catch {
            toast.error("Could not remove")
        }
    }

    const loading = devicesQ.isLoading || serversQ.isLoading

    return (
        <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0 overflow-hidden">
            <CardContent className="p-0">
                {/* Header */}
                <div className="flex items-center justify-between gap-2 px-3.5 md:px-4 pt-3 md:pt-3.5 pb-2 md:pb-2.5">
                    <div className="flex items-center gap-2">
                        <ShieldCheck className="h-4 w-4 text-emerald-400" />
                        <h3 className="text-xs md:text-sm font-medium text-muted-foreground uppercase tracking-wider">
                            WireGuard Devices
                        </h3>
                        {!loading && (
                            <Badge variant="secondary" className="h-4.5 px-1.5 text-[10px] font-semibold">
                                {max > 0 ? `${used} / ${max}` : used}
                            </Badge>
                        )}
                    </div>
                </div>

                <div className="px-3.5 md:px-4 pb-3.5 md:pb-4 space-y-2.5">
                    {reveal && (
                        <ConfigReveal label={reveal.label} conf={reveal.conf} onClose={() => setReveal(null)} />
                    )}

                    {loading ? (
                        <div className="space-y-2">
                            {[0, 1].map((i) => (
                                <div key={i} className="h-16 rounded-lg bg-muted/40 animate-pulse" />
                            ))}
                        </div>
                    ) : (
                        <>
                            {list.length === 0 && !reveal && (
                                <p className="text-sm text-muted-foreground py-1">
                                    No devices yet. Add one to get a WireGuard config.
                                </p>
                            )}

                            <div className="space-y-2">
                                {list.map((d) => (
                                    <motion.div
                                        key={d.id}
                                        initial={{ opacity: 0, y: 6 }}
                                        animate={{ opacity: 1, y: 0 }}
                                        transition={{ type: "spring", stiffness: 320, damping: 26 }}
                                        className="flex items-center justify-between gap-2 rounded-lg border border-border/50 bg-muted/20 p-3"
                                    >
                                        <div className="min-w-0 space-y-0.5">
                                            <div className="flex items-center gap-2 min-w-0">
                                                <Smartphone className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                                                <span className="text-sm font-medium truncate">{d.label}</span>
                                                <StatusBadge status={d.status} />
                                            </div>
                                            <div className="text-[11px] text-muted-foreground font-mono">{d.assigned_ip}</div>
                                            {multi && srvName.get(d.inbound_id) && (
                                                <div className="text-[11px] text-muted-foreground">🌍 {srvName.get(d.inbound_id)}</div>
                                            )}
                                            <div className="text-[11px] text-muted-foreground">
                                                ↑↓ {fmtBytes((d.up_bytes || 0) + (d.down_bytes || 0))}
                                            </div>
                                        </div>
                                        <div className="flex gap-1.5 shrink-0">
                                            <Button
                                                variant="outline"
                                                size="icon-sm"
                                                aria-label="Regenerate config"
                                                disabled={rotate.isPending}
                                                onClick={() => void onRotate(d)}
                                            >
                                                <RotateCw className="size-4" />
                                            </Button>
                                            <Button
                                                variant="outline"
                                                size="icon-sm"
                                                aria-label="Remove device"
                                                disabled={remove.isPending}
                                                className="text-destructive hover:text-destructive"
                                                onClick={() => void onRemove(d)}
                                            >
                                                <Trash2 className="size-4" />
                                            </Button>
                                        </div>
                                    </motion.div>
                                ))}
                            </div>

                            <Button
                                className="w-full"
                                disabled={full || servers.length === 0}
                                onClick={() => setSheetOpen(true)}
                            >
                                <Plus className="size-4" />
                                {full ? "Device limit reached" : "Add device"}
                            </Button>
                            {full && (
                                <p className="text-center text-[11px] text-muted-foreground">
                                    Remove a device or upgrade your plan to add more.
                                </p>
                            )}
                        </>
                    )}
                </div>
            </CardContent>

            {/* Add always opens the sheet so the user can name the device; the
                server picker only appears when there's more than one WG server. */}
            <AddDeviceSheet
                open={sheetOpen}
                onOpenChange={setSheetOpen}
                servers={servers}
                pending={add.isPending}
                onCreate={onCreate}
            />
        </Card>
    )
}

// WgDevices wraps the section in its own ConfirmDialogProvider because the public
// /sub/:uuid route (unlike the admin dashboard) doesn't mount one globally.
export function WgDevices({ uuid }: { uuid: string }) {
    return (
        <ConfirmDialogProvider>
            <WgDevicesInner uuid={uuid} />
        </ConfirmDialogProvider>
    )
}
