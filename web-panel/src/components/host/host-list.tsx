import { useState, useEffect, useCallback } from "react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { toast } from "sonner"
import { useQueryClient } from "@tanstack/react-query"
import {
    HiOutlinePlus,
    HiOutlinePencil,
    HiOutlineTrash,
} from "react-icons/hi"
import type { Host, Inbound } from "@/lib/types"
import { listInboundHosts, updateHost, deleteHost } from "@/lib/admin-api"
import { queryKeys } from "@/lib/queries/keys"
import { HostSettingsDialog } from "./host-settings-dialog"

interface HostListProps {
    inboundId: number
    initialHosts?: Host[]
    /** The inbound these hosts belong to — lets the settings dialog show only
     *  the override fields this protocol/transport actually uses. */
    inbound?: Inbound
}

export function HostList({ inboundId, initialHosts, inbound }: HostListProps) {
    const queryClient = useQueryClient()
    const [hosts, setHosts] = useState<Host[]>(initialHosts || [])
    const [loading, setLoading] = useState(!initialHosts)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editingHost, setEditingHost] = useState<Host | null>(null)
    const [deleteConfirm, setDeleteConfirm] = useState<number | null>(null)

    const fetchHosts = useCallback(async () => {
        setLoading(true)
        try {
            const result = await listInboundHosts(inboundId)
            if (result.success && result.data) {
                setHosts(result.data)
            } else {
                toast.error(result.error || "Failed to fetch hosts")
            }
        } catch (err: any) {
            toast.error(err.message || "Failed to fetch hosts")
        } finally {
            setLoading(false)
        }
    }, [inboundId])

    useEffect(() => {
        if (!initialHosts) {
            fetchHosts()
        }
    }, [initialHosts, fetchHosts])

    async function handleToggleDisabled(host: Host) {
        try {
            const result = await updateHost(host.id, { is_disabled: !host.is_disabled })
            if (!result.success) throw new Error(result.error)
            setHosts((prev) =>
                prev.map((h) => (h.id === host.id ? { ...h, is_disabled: !h.is_disabled } : h))
            )
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
            toast.success(host.is_disabled ? "Host enabled" : "Host disabled")
        } catch (err: any) {
            toast.error(err.message || "Failed to toggle host")
        }
    }

    async function handleDelete(id: number) {
        try {
            const result = await deleteHost(id)
            if (!result.success) throw new Error(result.error)
            setHosts((prev) => prev.filter((h) => h.id !== id))
            setDeleteConfirm(null)
            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
            toast.success("Host deleted")
        } catch (err: any) {
            toast.error(err.message || "Failed to delete host")
        }
    }

    function openCreate() {
        setEditingHost(null)
        setDialogOpen(true)
    }

    function openEdit(host: Host) {
        setEditingHost(host)
        setDialogOpen(true)
    }

    const sorted = [...hosts].sort((a, b) => a.priority - b.priority || a.id - b.id)

    return (
        <div className="space-y-2">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                        Hosts
                    </span>
                    <Badge variant="secondary" className="text-[10px] h-4 px-1.5">
                        {hosts.length}
                    </Badge>
                </div>
                <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 text-xs gap-1"
                    onClick={openCreate}
                >
                    <HiOutlinePlus className="h-3.5 w-3.5" />
                    Add Host
                </Button>
            </div>

            {loading && hosts.length === 0 && (
                <div className="text-xs text-muted-foreground py-2 text-center">Loading...</div>
            )}

            {!loading && hosts.length === 0 && (
                <div className="text-xs text-muted-foreground py-3 text-center border border-dashed rounded-md">
                    No hosts configured. The inbound will produce a single default config link.
                </div>
            )}

            {sorted.map((host) => (
                <div
                    key={host.id}
                    className={`flex items-center gap-3 px-3 py-2 rounded-md border text-sm transition-colors ${
                        host.is_disabled
                            ? "opacity-50 bg-muted/30 border-muted"
                            : "bg-card border-border hover:border-primary/20"
                    }`}
                >
                    {/* Enable/Disable Toggle */}
                    <Switch
                        checked={!host.is_disabled}
                        onCheckedChange={() => handleToggleDisabled(host)}
                        className="scale-75"
                    />

                    {/* Remark / Address info */}
                    <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                            <span className="font-medium text-xs truncate">
                                {host.remark || (host.address || "Default")}
                            </span>
                            <Badge variant="outline" className="text-[10px] h-4 px-1 shrink-0">
                                P:{host.priority}
                            </Badge>
                        </div>
                        <div className="flex items-center gap-2 text-[11px] text-muted-foreground mt-0.5">
                            {host.address && <span>{host.address}</span>}
                            {host.port != null && <span>:{host.port}</span>}
                            {host.sni && (
                                <>
                                    <span className="text-muted-foreground/40">|</span>
                                    <span>SNI: {host.sni}</span>
                                </>
                            )}
                            {host.security && (
                                <>
                                    <span className="text-muted-foreground/40">|</span>
                                    <span>{host.security.toUpperCase()}</span>
                                </>
                            )}
                        </div>
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1 shrink-0">
                        <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7"
                            onClick={() => openEdit(host)}
                        >
                            <HiOutlinePencil className="h-3.5 w-3.5" />
                        </Button>
                        {deleteConfirm === host.id ? (
                            <div className="flex items-center gap-1">
                                <Button
                                    variant="destructive"
                                    size="sm"
                                    className="h-7 text-[10px] px-2"
                                    onClick={() => handleDelete(host.id)}
                                >
                                    Confirm
                                </Button>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-7 text-[10px] px-2"
                                    onClick={() => setDeleteConfirm(null)}
                                >
                                    Cancel
                                </Button>
                            </div>
                        ) : (
                            <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7 text-destructive hover:text-destructive"
                                onClick={() => setDeleteConfirm(host.id)}
                            >
                                <HiOutlineTrash className="h-3.5 w-3.5" />
                            </Button>
                        )}
                    </div>
                </div>
            ))}

            <HostSettingsDialog
                open={dialogOpen}
                onOpenChange={setDialogOpen}
                host={editingHost}
                inboundId={inboundId}
                inbound={inbound}
                onSuccess={fetchHosts}
            />
        </div>
    )
}
