import { useState } from "react"
import { Plus, Trash2, KeyRound, Users } from "lucide-react"
import { toast } from "@/lib/toast"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState } from "@/components/ui/empty-state"
import { CopyableText } from "@/components/ui/copyable-text"
import { useConfirm } from "@/components/ui/confirm-dialog"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from "@/components/ui/dialog"
import { cn } from "@/lib/utils"
import { useCoreUsers, useToggleCoreUser, useDeleteCoreUser } from "@/lib/queries"
import type { CoreProtocol, CoreUser } from "@/lib/types/cores"
import { CreateCoreUserDialog } from "./create-core-user-dialog"
import { CoreConfigCard } from "./core-config-card"

export function CoreUsersPanel({
    inboundId,
    nodeId,
    protocol,
    host,
    hostOnline,
}: {
    inboundId: number
    nodeId: number
    protocol: CoreProtocol
    host?: string
    hostOnline: boolean
}) {
    const { data: users = [], isLoading } = useCoreUsers(inboundId)
    const toggle = useToggleCoreUser(inboundId)
    const del = useDeleteCoreUser(inboundId, nodeId)
    const confirm = useConfirm()

    const [addOpen, setAddOpen] = useState(false)
    const [configUser, setConfigUser] = useState<CoreUser | null>(null)

    const handleDelete = async (u: CoreUser) => {
        const ok = await confirm({
            title: "Delete user",
            description: `Remove "${u.email}" from this tunnel? Active sessions will be disconnected on the next reconcile.`,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (ok) del.mutate(u.id)
    }

    const openConfig = (u: CoreUser) => {
        if (!hostOnline) {
            toast.warning("Node is offline — reconnect it to fetch client configs.")
            return
        }
        setConfigUser(u)
    }

    return (
        <div className="space-y-3">
            <div className="flex items-center justify-between">
                <h3 className="flex items-center gap-2 text-sm font-semibold">
                    <Users className="size-4 text-muted-foreground" />
                    Users
                    {users.length > 0 && (
                        <span className="text-muted-foreground">({users.length})</span>
                    )}
                </h3>
                <Button size="sm" onClick={() => setAddOpen(true)}>
                    <Plus className="size-4" />
                    Add user
                </Button>
            </div>

            {isLoading ? (
                <div className="space-y-2">
                    {Array.from({ length: 3 }).map((_, i) => (
                        <Skeleton key={i} className="h-12 w-full rounded-lg" />
                    ))}
                </div>
            ) : users.length === 0 ? (
                <div className="rounded-lg border border-dashed">
                    <EmptyState
                        icon={Users}
                        title="No users yet"
                        description="Add a user to generate a client config."
                    />
                </div>
            ) : (
                <div className="overflow-hidden rounded-lg border">
                    <div className="divide-y">
                        {users.map((u) => (
                            <div
                                key={u.id}
                                className={cn(
                                    "flex flex-wrap items-center gap-3 px-3 py-2.5 text-sm",
                                    !u.enabled && "opacity-60",
                                )}
                            >
                                <span
                                    className={cn(
                                        "size-2 shrink-0 rounded-full",
                                        u.enabled ? "bg-emerald-500" : "bg-muted-foreground",
                                    )}
                                    title={u.enabled ? "Enabled" : "Disabled"}
                                />
                                <div className="min-w-0 flex-1">
                                    <CopyableText text={u.email} className="font-medium" />
                                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                        <CopyableText text={u.username} className="font-mono" />
                                        <span>·</span>
                                        <span>{u.k_limit} {u.k_limit === 1 ? "device" : "devices"}</span>
                                        {u.subscription_id ? (
                                            <>
                                                <span>·</span>
                                                <span
                                                    className="rounded bg-primary/10 px-1.5 py-0.5 text-[11px] font-medium text-primary"
                                                    title="Provisioned by a subscription — managed by billing (enable/disable/delete follow the subscription lifecycle)"
                                                >
                                                    Sub #{u.subscription_id}
                                                </span>
                                            </>
                                        ) : null}
                                    </div>
                                </div>
                                <div className="flex items-center gap-1">
                                    <Switch
                                        checked={u.enabled}
                                        disabled={toggle.isPending}
                                        onCheckedChange={() => toggle.mutate(u.id)}
                                        aria-label={u.enabled ? "Disable user" : "Enable user"}
                                    />
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => openConfig(u)}
                                        className="h-8"
                                    >
                                        <KeyRound className="size-4" />
                                        Config
                                    </Button>
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="size-8 text-muted-foreground hover:text-destructive"
                                        onClick={() => handleDelete(u)}
                                        aria-label="Delete user"
                                    >
                                        <Trash2 className="size-4" />
                                    </Button>
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            )}

            <CreateCoreUserDialog
                inboundId={inboundId}
                nodeId={nodeId}
                protocol={protocol}
                host={host}
                hostOnline={hostOnline}
                open={addOpen}
                onOpenChange={setAddOpen}
            />

            <Dialog open={!!configUser} onOpenChange={(o) => !o && setConfigUser(null)}>
                <DialogContent className="max-w-md">
                    <DialogHeader>
                        <DialogTitle>Client config</DialogTitle>
                        <DialogDescription>
                            {configUser?.email} — hand this to the client to connect.
                        </DialogDescription>
                    </DialogHeader>
                    {configUser && (
                        <CoreConfigCard
                            userId={configUser.id}
                            host={host}
                            protocol={protocol}
                            enabled={!!configUser}
                        />
                    )}
                </DialogContent>
            </Dialog>
        </div>
    )
}
