import { useState } from "react"
import { ArrowDown, ArrowUp, Check, Pencil, Plus, ShieldCheck, ShieldOff, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/ui/empty-state"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { VpnAddDialog } from "@/components/network/vpn-add-dialog"
import { useDeleteVPNProfile, useSetVPNRole } from "@/lib/queries/use-network"
import { formatBytes, handshakeShort } from "@/lib/vpn-labels"
import { cn } from "@/lib/utils"
import type { TunnelStatus, VPNProfile } from "@/lib/types/network"
import type { TunnelHealth, TunnelVerdict } from "@/lib/types/health"
import { toast } from "sonner"

interface Props {
    profiles: VPNProfile[] | undefined
    loading: boolean
    tunnels: TunnelStatus[]
    health: TunnelHealth[]
    /** A change is already waiting to be kept, so a second one must wait. */
    armed: boolean
    busy: boolean
    onEnable: (id: number) => void
    onDisable: (id: number) => void
}

const CHIP_TONE: Record<TunnelVerdict, string> = {
    "up": "bg-status-success-soft text-status-success",
    "degraded": "bg-status-warning-soft text-status-warning",
    "no-internet": "bg-status-danger-soft text-status-danger",
    "": "bg-surface-3 text-text-tertiary",
}

const CHIP_LABEL: Record<TunnelVerdict, string> = {
    "up": "online",
    "degraded": "degraded",
    "no-internet": "no internet",
    "": "waiting",
}

function HealthChip({ h }: { h: TunnelHealth | undefined }) {
    if (!h) {
        return <span className="text-text-tertiary text-sm">—</span>
    }
    return (
        <div className="space-y-0.5">
            <span
                data-verdict={h.verdict}
                className={cn(
                    "rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap",
                    CHIP_TONE[h.verdict] ?? CHIP_TONE[""],
                )}
            >
                {CHIP_LABEL[h.verdict] ?? h.verdict}
                {!h.in_pool && h.verdict === "up" && " · standby"}
            </span>
            <p className="text-text-tertiary text-xs tabular-nums">
                {h.loss_pct}% · {h.median_rtt_ms}ms
            </p>
        </div>
    )
}

/** Pencil → number → check, the lan-devices pattern. Commits on Enter too. */
function RoleCell({
    value,
    min,
    max,
    label,
    disabled,
    onCommit,
}: {
    value: number
    min: number
    max: number
    label: string
    disabled: boolean
    onCommit: (n: number) => void
}) {
    const [editing, setEditing] = useState(false)
    const [draft, setDraft] = useState(String(value))

    if (!editing) {
        return (
            <div className="flex w-36 items-center gap-1">
                <span className="tabular-nums">{value}</span>
                <Button
                    size="icon"
                    variant="ghost"
                    className="h-6 w-6"
                    aria-label={`Change ${label}`}
                    disabled={disabled}
                    onClick={() => {
                        setDraft(String(value))
                        setEditing(true)
                    }}
                >
                    <Pencil className="h-3 w-3" />
                </Button>
            </div>
        )
    }

    function commit() {
        const n = Number(draft)
        setEditing(false)
        if (!Number.isInteger(n) || n < min || n > max) {
            toast.error(`${label} must be between ${min} and ${max}`)
            return
        }
        if (n !== value) onCommit(n)
    }

    return (
        <div className="flex w-36 items-center gap-1">
            <Input
                autoFocus
                type="number"
                min={min}
                max={max}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === "Enter") commit()
                    if (e.key === "Escape") setEditing(false)
                }}
                // On a number field this sizes the wrapper, stepper included.
                className="h-7 w-20 text-sm"
                aria-label={label}
            />
            <Button size="icon" variant="ghost" className="h-6 w-6" aria-label={`Save ${label}`} onClick={commit}>
                <Check className="h-3 w-3" />
            </Button>
            <Button
                size="icon"
                variant="ghost"
                className="h-6 w-6"
                aria-label="Cancel"
                onClick={() => setEditing(false)}
            >
                <X className="h-3 w-3" />
            </Button>
        </div>
    )
}

export function VpnPoolTable({
    profiles,
    loading,
    tunnels,
    health,
    armed,
    busy,
    onEnable,
    onDisable,
}: Props) {
    const del = useDeleteVPNProfile()
    const role = useSetVPNRole()
    const confirm = useConfirm()
    const [adding, setAdding] = useState(false)
    const [editing, setEditing] = useState<VPNProfile | null>(null)

    if (loading) return <Skeleton className="h-48 w-full" />

    const rows = profiles ?? []
    const enabledCount = rows.filter((p) => p.enabled).length
    const statusById = new Map(tunnels.map((t) => [t.profile_id, t]))
    const healthById = new Map(health.map((h) => [h.profile_id, h]))

    async function turnOn(p: VPNProfile) {
        const ok = await confirm({
            title: `Add ${p.name} to the pool`,
            description:
                "Traffic starts riding this tunnel as soon as it joins. " +
                "You get 90 seconds to keep the change before it reverts itself.",
            confirmLabel: "Turn it on",
            icon: <ShieldCheck className="h-5 w-5" />,
        })
        if (ok) onEnable(p.id)
    }

    async function turnOff(p: VPNProfile) {
        const last = enabledCount === 1
        const ok = await confirm({
            title: `Remove ${p.name} from the pool`,
            description: last
                ? "This is the last tunnel. Traffic bound for the secondary uplink will be " +
                  "dropped rather than sent in the open. Nothing falls back to the other uplink. " +
                  "You get 90 seconds to keep the change before it reverts itself."
                : "Its flows move to the remaining tunnels. " +
                  "You get 90 seconds to keep the change before it reverts itself.",
            confirmLabel: "Turn it off",
            variant: "warning",
            icon: <ShieldOff className="h-5 w-5" />,
        })
        if (ok) onDisable(p.id)
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

    function commitRole(p: VPNProfile, priority: number, weight: number) {
        role.mutate(
            { id: p.id, priority, weight },
            {
                onSuccess: () =>
                    toast.success(`${p.name} set to tier ${priority}, weight ${weight}`),
                onError: (e) =>
                    toast.error(e instanceof Error ? e.message : "Failed to change the role"),
            },
        )
    }

    return (
        <Card>
            <CardHeader className="pb-4">
                <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
                    <div>
                        <CardTitle>VPN pool</CardTitle>
                        <CardDescription>
                            Enabled tunnels share the load by weight. Lower tiers step in only
                            when every tunnel above them is down.
                        </CardDescription>
                    </div>
                    <Button size="sm" onClick={() => setAdding(true)}>
                        <Plus className="mr-1.5 h-3.5 w-3.5" />
                        Add a VPN
                    </Button>
                </div>
            </CardHeader>

            <CardContent>
                {rows.length === 0 ? (
                    <EmptyState
                        icon={ShieldCheck}
                        title="No VPN yet"
                        description="Until one is added and enabled, nothing goes out over the secondary uplink."
                    />
                ) : (
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Tunnel</TableHead>
                                <TableHead>On</TableHead>
                                <TableHead className="w-36">Tier</TableHead>
                                <TableHead className="w-36">Weight</TableHead>
                                <TableHead>Health</TableHead>
                                <TableHead>Handshake</TableHead>
                                <TableHead>Traffic</TableHead>
                                <TableHead className="w-0" />
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {rows.map((p) => {
                                const st = statusById.get(p.id)
                                const h = healthById.get(p.id)
                                return (
                                    <TableRow
                                        key={p.id}
                                        data-profile={p.id}
                                        className={cn(!p.enabled && "text-text-tertiary")}
                                    >
                                        <TableCell>
                                            <p className="text-text-primary font-medium">{p.name}</p>
                                            {p.unreadable ? (
                                                // Listed only so it can be deleted.
                                                <p className="text-status-warning max-w-56 truncate text-xs">
                                                    Stored config cannot be read — {p.unreadable}
                                                </p>
                                            ) : (
                                                <p className="text-text-tertiary max-w-56 truncate font-mono text-xs">
                                                    {p.config.peer.endpoint}
                                                </p>
                                            )}
                                        </TableCell>
                                        <TableCell>
                                            <Switch
                                                checked={p.enabled}
                                                aria-label={
                                                    p.enabled ? `Turn ${p.name} off` : `Turn ${p.name} on`
                                                }
                                                disabled={armed || busy || !!p.unreadable}
                                                onCheckedChange={(on) =>
                                                    void (on ? turnOn(p) : turnOff(p))
                                                }
                                            />
                                        </TableCell>
                                        <TableCell>
                                            <RoleCell
                                                value={p.priority}
                                                min={0}
                                                max={7}
                                                label="tier"
                                                disabled={!p.enabled || role.isPending}
                                                onCommit={(n) => commitRole(p, n, p.weight)}
                                            />
                                        </TableCell>
                                        <TableCell>
                                            <RoleCell
                                                value={p.weight}
                                                min={1}
                                                max={100}
                                                label="weight"
                                                disabled={!p.enabled || role.isPending}
                                                onCommit={(n) => commitRole(p, p.priority, n)}
                                            />
                                        </TableCell>
                                        <TableCell>
                                            {p.enabled ? (
                                                <HealthChip h={h} />
                                            ) : (
                                                <span className="text-sm">—</span>
                                            )}
                                        </TableCell>
                                        <TableCell className="text-sm whitespace-nowrap">
                                            {p.enabled && st
                                                ? handshakeShort(st.handshake_age_seconds)
                                                : "—"}
                                        </TableCell>
                                        <TableCell className="text-sm whitespace-nowrap tabular-nums">
                                            {p.enabled && st ? (
                                                <span className="flex items-center gap-2">
                                                    <span className="flex items-center gap-0.5">
                                                        <ArrowDown className="h-3 w-3" aria-hidden />
                                                        {formatBytes(st.rx_bytes)}
                                                    </span>
                                                    <span className="flex items-center gap-0.5">
                                                        <ArrowUp className="h-3 w-3" aria-hidden />
                                                        {formatBytes(st.tx_bytes)}
                                                    </span>
                                                </span>
                                            ) : (
                                                "—"
                                            )}
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-1">
                                                <Button
                                                    size="icon"
                                                    variant="ghost"
                                                    className="h-8 w-8"
                                                    aria-label={`Edit ${p.name}`}
                                                    onClick={() => setEditing(p)}
                                                    // Editing a pool member is a routing change,
                                                    // so it goes through turning it off first.
                                                    disabled={p.enabled || !!p.unreadable}
                                                >
                                                    <Pencil className="h-3.5 w-3.5" />
                                                </Button>
                                                <Button
                                                    size="icon"
                                                    variant="ghost"
                                                    className="h-8 w-8"
                                                    aria-label={`Delete ${p.name}`}
                                                    onClick={() => void remove(p)}
                                                    // Deleting a live member would leave
                                                    // nothing to turn it off with.
                                                    disabled={p.enabled}
                                                >
                                                    <Trash2 className="h-3.5 w-3.5" />
                                                </Button>
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                )
                            })}
                        </TableBody>
                    </Table>
                )}
            </CardContent>

            <VpnAddDialog open={adding} onOpenChange={setAdding} />
            {/* Keyed: the dialog seeds its fields at mount, so it needs a remount. */}
            <VpnAddDialog
                key={editing?.id ?? "none"}
                open={editing !== null}
                onOpenChange={(o) => !o && setEditing(null)}
                profile={editing}
            />
        </Card>
    )
}
