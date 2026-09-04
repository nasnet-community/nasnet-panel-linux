import { useMemo, useState } from "react"
import { toast } from "sonner"
import { Pencil, Plus, ShieldAlert, Trash2, TriangleAlert } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
    Card,
    CardAction,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetFooter,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { UpstreamMappingCard } from "@/components/network/upstream-mapping-card"
import { PortSocket, Wire, WireEmpty } from "@/components/network/wire"
import {
    useDeletePortForward,
    usePortForwards,
    useSavePortForward,
} from "@/lib/queries/use-network"
import { collidesLocally, needsConfirm, verdictsFromError } from "@/lib/api/network"
import { cn } from "@/lib/utils"
import type {
    NetworkInterfaceView,
    NetworkState,
    PortForward,
    Verdict,
} from "@/lib/types/network"

interface Props {
    state: NetworkState | undefined
    interfaces: NetworkInterfaceView[]
}

type Draft = {
    id?: number
    uplink_key: string
    proto: "tcp" | "udp"
    dport: string
    to_addr: string
    to_port: string
    comment: string
    enabled: boolean
}

const blank: Draft = {
    uplink_key: "",
    proto: "tcp",
    dport: "",
    to_addr: "",
    to_port: "",
    comment: "",
    enabled: true,
}

const CONFIRM_EXPOSURE = {
    title: "This forward exposes a way in",
    confirmLabel: "Open it",
    variant: "warning" as const,
    typeToConfirm: "CONFIRM",
}

function forwardName(pf: Pick<PortForward, "proto" | "dport">): string {
    return `${pf.proto.toUpperCase()}/${pf.dport}`
}

/** A forward drawn as the path it opens: internet, uplink, socket, device. */
function ForwardPath({
    proto,
    dport,
    toAddr,
    toPort,
    uplink,
    comment,
    enabled,
}: {
    proto: string
    dport: string
    toAddr: string
    toPort: string
    uplink: string
    comment?: string
    enabled: boolean
}) {
    const state = enabled ? ("live" as const) : ("off" as const)
    return (
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2.5 gap-y-1.5">
            <span className="text-text-tertiary shrink-0 font-mono text-xs">internet</span>
            <Wire state={state} />
            <span className="text-text-secondary shrink-0 text-xs">{uplink}</span>
            <Wire state={state} />
            <PortSocket className={cn(!enabled && "text-text-tertiary")}>
                {proto}/{dport}
            </PortSocket>
            <Wire state={state} arrow />
            <span className={cn("min-w-0 shrink-0 text-right", !enabled && "opacity-60")}>
                <span className="block truncate font-mono text-sm tabular-nums">
                    {toAddr}:{toPort}
                </span>
                {comment && (
                    <span className="text-text-tertiary block truncate text-xs">{comment}</span>
                )}
            </span>
        </div>
    )
}

export function PortForwardsTab({ state, interfaces }: Props) {
    const rows = usePortForwards()
    const save = useSavePortForward()
    const remove = useDeletePortForward()
    const confirm = useConfirm()

    const [draft, setDraft] = useState<Draft | null>(null)
    const [verdicts, setVerdicts] = useState<Verdict[]>([])

    // Uplinks carry a stable key; a forward names the key, never the address.
    const uplinks = useMemo(
        () =>
            interfaces
                .filter((i) => i.role === "wan" && i.present)
                .map((i) => ({
                    key: i.key,
                    label:
                        i.label ||
                        state?.uplinks?.find((u) => u.if_name === i.if_name)?.label ||
                        i.if_name,
                })),
        [interfaces, state],
    )
    const labels = useMemo(
        () => Object.fromEntries(uplinks.map((u) => [u.key, u.label])),
        [uplinks],
    )

    if (rows.isLoading) return <Skeleton className="h-64 w-full" />
    if (rows.isError) {
        return (
            <Alert variant="warning">
                <TriangleAlert className="h-4 w-4" />
                <AlertDescription>
                    The port forwards could not be read. {rows.error?.message}
                </AlertDescription>
            </Alert>
        )
    }

    const forwards = rows.data ?? []

    function openNew() {
        setVerdicts([])
        setDraft({ ...blank })
    }

    function openEdit(pf: PortForward) {
        setVerdicts([])
        setDraft({
            id: pf.id,
            uplink_key: pf.uplink_key,
            proto: pf.proto,
            dport: String(pf.dport),
            to_addr: pf.to_addr,
            to_port: String(pf.to_port),
            comment: pf.comment,
            enabled: pf.enabled,
        })
    }

    async function submit(confirmed = false) {
        if (!draft) return
        setVerdicts([])

        const payload = {
            id: draft.id,
            uplink_key: draft.uplink_key,
            proto: draft.proto,
            dport: Number(draft.dport),
            to_addr: draft.to_addr.trim(),
            to_port: Number(draft.to_port),
            comment: draft.comment.trim(),
            enabled: draft.enabled,
            confirmed,
        }

        if (!confirmed && collidesLocally(forwards, payload)) {
            setVerdicts([
                {
                    rule: "V27",
                    level: "reject",
                    message: `${payload.proto.toUpperCase()}/${payload.dport} is already forwarded on that uplink.`,
                },
            ])
            return
        }

        try {
            await save.mutateAsync(payload)
            setDraft(null)
            toast.success(draft.id ? "Forward updated" : "Forward added — live now")
        } catch (err) {
            const vs = verdictsFromError(err)
            if (needsConfirm(err)) {
                const ok = await confirm({
                    ...CONFIRM_EXPOSURE,
                    description:
                        vs.map((v) => v.message).join(" ") ||
                        "Anyone who reaches your uplink can attempt to use it.",
                    icon: <ShieldAlert className="h-5 w-5" />,
                })
                if (ok) await submit(true)
                return
            }
            setVerdicts(vs.length ? vs : [{ rule: "", level: "reject", message: String(err) }])
        }
    }

    // The row switch: firewall-only, so it lands at once — same as saving.
    async function toggle(pf: PortForward, on: boolean, confirmed = false) {
        const payload = { ...pf, enabled: on, confirmed }
        try {
            await save.mutateAsync(payload)
            toast.success(on ? `${forwardName(pf)} turned on` : `${forwardName(pf)} turned off`)
        } catch (err) {
            const vs = verdictsFromError(err)
            if (needsConfirm(err)) {
                const ok = await confirm({
                    ...CONFIRM_EXPOSURE,
                    description:
                        vs.map((v) => v.message).join(" ") ||
                        "Anyone who reaches your uplink can attempt to use it.",
                    icon: <ShieldAlert className="h-5 w-5" />,
                })
                if (ok) await toggle(pf, on, true)
                return
            }
            toast.error(
                vs.map((v) => v.message).join(" ") ||
                    (err instanceof Error ? err.message : "Failed to change the forward"),
            )
        }
    }

    async function del(pf: PortForward) {
        const ok = await confirm({
            title: "Remove this forward?",
            description: `${forwardName(pf)} stops reaching ${pf.to_addr}:${pf.to_port}. Connections using it now will drop.`,
            confirmLabel: "Remove",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await remove.mutateAsync(pf.id)
            toast.success("Forward removed")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to remove the forward")
        }
    }

    return (
        <>
            <Card>
                <CardHeader>
                    <CardTitle>Port forwards</CardTitle>
                    <CardDescription>
                        Let traffic from the internet reach one device on your network. Replies
                        leave by whichever uplink the request arrived on.
                    </CardDescription>
                    <CardAction>
                        <Button size="sm" onClick={openNew}>
                            <Plus className="mr-1.5 h-3.5 w-3.5" />
                            Add forward
                        </Button>
                    </CardAction>
                </CardHeader>
                <CardContent>
                    {forwards.length === 0 ? (
                        <WireEmpty
                            from="internet"
                            to="your network"
                            title="No ways in are open"
                            description="Forward a port to reach a device on your network — a NAS, a camera, a game server — from outside."
                        />
                    ) : (
                        <ul className="divide-border-subtle divide-y">
                            {forwards.map((pf) => (
                                <li
                                    key={pf.id}
                                    className="flex flex-col gap-2.5 py-3.5 first:pt-0 last:pb-0 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-5 sm:gap-y-2"
                                >
                                    <ForwardPath
                                        proto={pf.proto}
                                        dport={String(pf.dport)}
                                        toAddr={pf.to_addr}
                                        toPort={String(pf.to_port)}
                                        uplink={
                                            pf.uplink_key
                                                ? (labels[pf.uplink_key] ?? pf.uplink_key)
                                                : "any uplink"
                                        }
                                        comment={pf.comment}
                                        enabled={pf.enabled}
                                    />
                                    <div className="ml-auto flex shrink-0 items-center gap-1.5">
                                        <Switch
                                            checked={pf.enabled}
                                            aria-label={
                                                pf.enabled
                                                    ? `Turn ${forwardName(pf)} off`
                                                    : `Turn ${forwardName(pf)} on`
                                            }
                                            disabled={save.isPending}
                                            onCheckedChange={(v) => void toggle(pf, v)}
                                        />
                                        <Button
                                            size="icon"
                                            variant="ghost"
                                            className="text-text-tertiary hover:text-text-primary h-8 w-8"
                                            aria-label={`Edit ${forwardName(pf)}`}
                                            onClick={() => openEdit(pf)}
                                        >
                                            <Pencil className="h-3.5 w-3.5" />
                                        </Button>
                                        <Button
                                            size="icon"
                                            variant="ghost"
                                            className="text-text-tertiary hover:text-status-danger h-8 w-8"
                                            onClick={() => void del(pf)}
                                            disabled={remove.isPending}
                                            aria-label={`Remove the forward on port ${pf.dport}`}
                                        >
                                            <Trash2 className="h-3.5 w-3.5" />
                                        </Button>
                                    </div>
                                </li>
                            ))}
                        </ul>
                    )}
                </CardContent>
            </Card>

            <UpstreamMappingCard state={state} interfaces={interfaces} />

            <Sheet
                open={draft !== null}
                onOpenChange={(o) => {
                    if (!o) {
                        setDraft(null)
                        setVerdicts([])
                    }
                }}
            >
                <SheetContent className="overflow-y-auto sm:max-w-lg">
                    {draft && (
                        <>
                            <SheetHeader className="pb-0">
                                <SheetTitle>
                                    {draft.id ? "Edit forward" : "New forward"}
                                </SheetTitle>
                                <SheetDescription>
                                    Takes effect as soon as you save — this changes the firewall,
                                    not the addressing, so there is nothing to confirm afterwards.
                                </SheetDescription>
                            </SheetHeader>

                            <div className="space-y-5 px-4">
                                <div className="border-border-subtle bg-surface-1 rounded-lg border px-3.5 py-3">
                                    <ForwardPath
                                        proto={draft.proto}
                                        dport={draft.dport || "…"}
                                        toAddr={draft.to_addr || "device"}
                                        toPort={draft.to_port || "…"}
                                        uplink={
                                            draft.uplink_key
                                                ? (labels[draft.uplink_key] ?? draft.uplink_key)
                                                : "any uplink"
                                        }
                                        enabled={draft.enabled}
                                    />
                                </div>

                                <div className="space-y-4">
                                    <p className="text-text-tertiary font-mono text-xs">
                                        from the internet
                                    </p>
                                    <div className="grid gap-4 sm:grid-cols-2">
                                        <div className="space-y-1.5">
                                            <Label>Accept on</Label>
                                            <Select
                                                value={draft.uplink_key || "any"}
                                                onValueChange={(v) =>
                                                    setDraft({
                                                        ...draft,
                                                        uplink_key: v === "any" ? "" : v,
                                                    })
                                                }
                                            >
                                                <SelectTrigger>
                                                    <SelectValue />
                                                </SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="any">Any uplink</SelectItem>
                                                    {uplinks.map((u) => (
                                                        <SelectItem key={u.key} value={u.key}>
                                                            {u.label}
                                                        </SelectItem>
                                                    ))}
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="space-y-1.5">
                                            <Label>Protocol</Label>
                                            <Select
                                                value={draft.proto}
                                                onValueChange={(v) =>
                                                    setDraft({
                                                        ...draft,
                                                        proto: v as "tcp" | "udp",
                                                    })
                                                }
                                            >
                                                <SelectTrigger>
                                                    <SelectValue />
                                                </SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="tcp">TCP</SelectItem>
                                                    <SelectItem value="udp">UDP</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
                                    </div>
                                    <div className="space-y-1.5">
                                        <Label htmlFor="pf-dport">Outside port</Label>
                                        <Input
                                            id="pf-dport"
                                            className="font-mono tabular-nums"
                                            inputMode="numeric"
                                            value={draft.dport}
                                            onChange={(e) =>
                                                setDraft({ ...draft, dport: e.target.value })
                                            }
                                            placeholder="8080"
                                        />
                                    </div>
                                </div>

                                <div className="space-y-4">
                                    <p className="text-text-tertiary font-mono text-xs">
                                        to the device
                                    </p>
                                    <div className="grid gap-4 sm:grid-cols-2">
                                        <div className="space-y-1.5">
                                            <Label htmlFor="pf-addr">Device address</Label>
                                            <Input
                                                id="pf-addr"
                                                className="font-mono"
                                                value={draft.to_addr}
                                                onChange={(e) =>
                                                    setDraft({ ...draft, to_addr: e.target.value })
                                                }
                                                placeholder="10.77.0.5"
                                            />
                                        </div>
                                        <div className="space-y-1.5">
                                            <Label htmlFor="pf-toport">Port on the device</Label>
                                            <Input
                                                id="pf-toport"
                                                className="font-mono tabular-nums"
                                                inputMode="numeric"
                                                value={draft.to_port}
                                                onChange={(e) =>
                                                    setDraft({ ...draft, to_port: e.target.value })
                                                }
                                                placeholder="80"
                                            />
                                        </div>
                                    </div>
                                </div>

                                <div className="space-y-1.5">
                                    <Label htmlFor="pf-comment">Note (optional)</Label>
                                    <Input
                                        id="pf-comment"
                                        value={draft.comment}
                                        onChange={(e) =>
                                            setDraft({ ...draft, comment: e.target.value })
                                        }
                                        placeholder="NAS web interface"
                                    />
                                </div>

                                <div className="flex items-center gap-3">
                                    <Switch
                                        id="pf-enabled"
                                        checked={draft.enabled}
                                        onCheckedChange={(v) =>
                                            setDraft({ ...draft, enabled: v })
                                        }
                                    />
                                    <Label htmlFor="pf-enabled" className="text-sm font-normal">
                                        Active
                                    </Label>
                                </div>

                                {verdicts.map((v) => (
                                    <Alert key={v.rule + v.message} variant="warning">
                                        <TriangleAlert className="h-4 w-4" />
                                        <AlertDescription>
                                            {v.rule && (
                                                <span className="text-text-tertiary font-mono text-xs">
                                                    {v.rule}{" "}
                                                </span>
                                            )}
                                            {v.message}
                                        </AlertDescription>
                                    </Alert>
                                ))}
                            </div>

                            <SheetFooter className="flex-row items-center gap-2">
                                <Button onClick={() => void submit()} disabled={save.isPending}>
                                    {save.isPending
                                        ? "Saving…"
                                        : draft.id
                                          ? "Save forward"
                                          : "Add forward"}
                                </Button>
                                <Button
                                    variant="ghost"
                                    onClick={() => {
                                        setDraft(null)
                                        setVerdicts([])
                                    }}
                                >
                                    Cancel
                                </Button>
                            </SheetFooter>
                        </>
                    )}
                </SheetContent>
            </Sheet>
        </>
    )
}
