import { useState } from "react"
import { Pencil, Plus, ShieldOff, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/ui/empty-state"
import { Skeleton } from "@/components/ui/skeleton"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { VpnAddDialog } from "@/components/network/vpn-add-dialog"
import { useDeleteVPNProfile } from "@/lib/queries/use-network"
import { ShieldCheck } from "lucide-react"
import type { VPNProfile } from "@/lib/types/network"
import { toast } from "sonner"

interface Props {
    profiles: VPNProfile[] | undefined
    loading: boolean
    /** A change is already waiting to be kept, so a second one must wait. */
    armed: boolean
    busy: boolean
    onActivate: (id: number) => void
    onDeactivate: () => void
}

export function VpnProfiles({ profiles, loading, armed, busy, onActivate, onDeactivate }: Props) {
    const del = useDeleteVPNProfile()
    const confirm = useConfirm()
    const [adding, setAdding] = useState(false)
    const [editing, setEditing] = useState<VPNProfile | null>(null)

    if (loading) return <Skeleton className="h-48 w-full" />

    async function turnOff() {
        const ok = await confirm({
            title: "Turn this VPN off",
            description:
                "Traffic bound for the secondary uplink will be dropped rather than sent in the open. " +
                "Nothing falls back to the other uplink. You get 90 seconds to keep the change before it reverts itself.",
            confirmLabel: "Turn it off",
            variant: "warning",
            icon: <ShieldOff className="h-5 w-5" />,
        })
        if (ok) onDeactivate()
    }

    async function remove(p: VPNProfile) {
        const ok = await confirm({
            title: `Delete ${p.name}`,
            description: "The stored config and its keys are removed. Nothing else changes.",
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await del.mutateAsync(p.id)
            toast.success("VPN deleted")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to delete the VPN")
        }
    }

    return (
        <Card>
            <CardHeader className="pb-4">
                <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
                    <div>
                        <CardTitle>Saved VPNs</CardTitle>
                        <CardDescription>
                            One is in use at a time. The rest sit here ready to switch to.
                        </CardDescription>
                    </div>
                    <Button size="sm" onClick={() => setAdding(true)}>
                        <Plus className="mr-1.5 h-3.5 w-3.5" />
                        Add a VPN
                    </Button>
                </div>
            </CardHeader>

            <CardContent>
                {!profiles || profiles.length === 0 ? (
                    <EmptyState
                        icon={ShieldCheck}
                        title="No VPN yet"
                        description="Until one is added and turned on, nothing goes out over the secondary uplink."
                    />
                ) : (
                    <ul className="divide-border-subtle divide-y">
                        {profiles.map((p) => (
                            <li
                                key={p.id}
                                className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0"
                            >
                                <div className="min-w-0">
                                    <div className="flex items-center gap-2">
                                        <span className="truncate font-medium">{p.name}</span>
                                        {p.active && <Badge variant="secondary">In use</Badge>}
                                    </div>
                                    {p.unreadable ? (
                                        // Listed only so it can be deleted.
                                        <p className="text-status-warning truncate text-xs">
                                            Stored config cannot be read — {p.unreadable}
                                        </p>
                                    ) : (
                                        <p className="text-text-tertiary truncate font-mono text-xs">
                                            {p.config.peer.endpoint}
                                        </p>
                                    )}
                                </div>

                                <div className="flex items-center gap-1.5">
                                    {p.active ? (
                                        <Button
                                            size="sm"
                                            variant="outline"
                                            onClick={() => void turnOff()}
                                            disabled={armed || busy}
                                        >
                                            Turn off
                                        </Button>
                                    ) : (
                                        <Button
                                            size="sm"
                                            onClick={() => onActivate(p.id)}
                                            disabled={armed || busy || !!p.unreadable}
                                        >
                                            Use this one
                                        </Button>
                                    )}
                                    <Button
                                        size="icon"
                                        variant="ghost"
                                        className="h-8 w-8"
                                        aria-label={`Edit ${p.name}`}
                                        onClick={() => setEditing(p)}
                                        // Editing the running tunnel is a routing
                                        // change, so it goes through turning it off.
                                        // An unreadable row has nothing to edit.
                                        disabled={p.active || !!p.unreadable}
                                    >
                                        <Pencil className="h-3.5 w-3.5" />
                                    </Button>
                                    <Button
                                        size="icon"
                                        variant="ghost"
                                        className="h-8 w-8"
                                        aria-label={`Delete ${p.name}`}
                                        onClick={() => void remove(p)}
                                        // Deleting the row under a live tunnel would
                                        // leave nothing to turn it off with.
                                        disabled={p.active}
                                    >
                                        <Trash2 className="h-3.5 w-3.5" />
                                    </Button>
                                </div>
                            </li>
                        ))}
                    </ul>
                )}
            </CardContent>

            <VpnAddDialog open={adding} onOpenChange={setAdding} />
            <VpnAddDialog
                open={editing !== null}
                onOpenChange={(o) => !o && setEditing(null)}
                profile={editing}
            />
        </Card>
    )
}
