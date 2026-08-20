import { useMemo, useState } from "react"
import { ArrowRight, Plus, ShieldAlert, Trash2, TriangleAlert } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
    Card,
    CardAction,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
} from "@/components/ui/card"
import { EmptyState } from "@/components/ui/empty-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { useConfirm } from "@/components/ui/confirm-dialog"
import {
    useDeletePortForward,
    usePortForwards,
    useSavePortForward,
} from "@/lib/queries/use-network"
import { collidesLocally, needsConfirm, verdictsFromError } from "@/lib/api/network"
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

    function edit(pf: PortForward) {
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
        } catch (err) {
            const vs = verdictsFromError(err)
            if (needsConfirm(err)) {
                const ok = await confirm({
                    title: "This forward exposes a way in",
                    description:
                        vs.map((v) => v.message).join(" ") ||
                        "Anyone who reaches your uplink can attempt to use it.",
                    confirmLabel: "Add the forward",
                    variant: "warning",
                    typeToConfirm: "CONFIRM",
                    icon: <ShieldAlert className="h-5 w-5" />,
                })
                if (ok) await submit(true)
                return
            }
            setVerdicts(vs.length ? vs : [{ rule: "", level: "reject", message: String(err) }])
        }
    }

    async function del(pf: PortForward) {
        const ok = await confirm({
            title: "Remove this forward?",
            description: `${pf.proto.toUpperCase()}/${pf.dport} stops reaching ${pf.to_addr}:${pf.to_port}. Connections using it now will drop.`,
            confirmLabel: "Remove",
            variant: "destructive",
        })
        if (ok) await remove.mutateAsync(pf.id)
    }

    return (
        <div className="space-y-4">
            <Card>
                <CardHeader>
                    <CardTitle>Port forwards</CardTitle>
                    <CardDescription>
                        Let traffic from the internet reach one device on your network. Replies
                        leave by whichever uplink the request arrived on.
                    </CardDescription>
                    {!draft && (
                        <CardAction>
                            <Button size="sm" onClick={() => setDraft({ ...blank })}>
                                <Plus className="mr-1.5 h-3.5 w-3.5" />
                                Add forward
                            </Button>
                        </CardAction>
                    )}
                </CardHeader>
                <CardContent>
                    {forwards.length === 0 && !draft ? (
                        <EmptyState
                            icon={ArrowRight}
                            title="No port forwards"
                            description="Add one to reach a device on your network — a NAS, a camera, a game server — from outside."
                        />
                    ) : (
                        <ul className="divide-border-subtle divide-y">
                            {forwards.map((pf) => (
                                <li
                                    key={pf.id}
                                    className="flex flex-wrap items-center justify-between gap-3 py-3 first:pt-0"
                                >
                                    <div className="min-w-0">
                                        <div className="flex flex-wrap items-center gap-2">
                                            <span className="font-mono text-sm tabular-nums">
                                                {pf.proto.toUpperCase()}/{pf.dport}
                                            </span>
                                            <ArrowRight className="text-text-tertiary h-3.5 w-3.5" />
                                            <span className="font-mono text-sm tabular-nums">
                                                {pf.to_addr}:{pf.to_port}
                                            </span>
                                            {!pf.enabled && (
                                                <Badge variant="secondary">Turned off</Badge>
                                            )}
                                        </div>
                                        <p className="text-text-secondary mt-0.5 text-xs">
                                            Arriving on{" "}
                                            {pf.uplink_key
                                                ? (labels[pf.uplink_key] ?? pf.uplink_key)
                                                : "any uplink"}
                                            {pf.comment && ` · ${pf.comment}`}
                                        </p>
                                    </div>
                                    <div className="flex items-center gap-1">
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => edit(pf)}
                                            disabled={!!draft}
                                        >
                                            Edit
                                        </Button>
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => del(pf)}
                                            disabled={remove.isPending}
                                            aria-label={`Remove the forward on port ${pf.dport}`}
                                        >
                                            <Trash2 className="text-status-danger h-3.5 w-3.5" />
                                        </Button>
                                    </div>
                                </li>
                            ))}
                        </ul>
                    )}
                </CardContent>
            </Card>

            {draft && (
                <Card>
                    <CardHeader>
                        <CardTitle>{draft.id ? "Edit forward" : "New forward"}</CardTitle>
                        <CardDescription>
                            Takes effect as soon as you save — this changes the firewall, not the
                            addressing, so there is nothing to confirm afterwards.
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <div className="grid gap-4 sm:grid-cols-2">
                            <div className="space-y-1.5">
                                <Label>Accept on</Label>
                                <Select
                                    value={draft.uplink_key || "any"}
                                    onValueChange={(v) =>
                                        setDraft({ ...draft, uplink_key: v === "any" ? "" : v })
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
                                        setDraft({ ...draft, proto: v as "tcp" | "udp" })
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

                        <div className="grid gap-4 sm:grid-cols-3">
                            <div className="space-y-1.5">
                                <Label htmlFor="pf-dport">Outside port</Label>
                                <Input
                                    id="pf-dport"
                                    className="font-mono tabular-nums"
                                    inputMode="numeric"
                                    value={draft.dport}
                                    onChange={(e) => setDraft({ ...draft, dport: e.target.value })}
                                    placeholder="8080"
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor="pf-addr">Device address</Label>
                                <Input
                                    id="pf-addr"
                                    className="font-mono"
                                    value={draft.to_addr}
                                    onChange={(e) => setDraft({ ...draft, to_addr: e.target.value })}
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
                                    onChange={(e) => setDraft({ ...draft, to_port: e.target.value })}
                                    placeholder="80"
                                />
                            </div>
                        </div>

                        <div className="space-y-1.5">
                            <Label htmlFor="pf-comment">Note (optional)</Label>
                            <Input
                                id="pf-comment"
                                value={draft.comment}
                                onChange={(e) => setDraft({ ...draft, comment: e.target.value })}
                                placeholder="NAS web interface"
                            />
                        </div>

                        <div className="flex items-center gap-3">
                            <Switch
                                id="pf-enabled"
                                checked={draft.enabled}
                                onCheckedChange={(v) => setDraft({ ...draft, enabled: v })}
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

                        <div className="flex items-center gap-2">
                            <Button onClick={() => submit()} disabled={save.isPending}>
                                {save.isPending ? "Saving…" : "Save forward"}
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
                        </div>
                    </CardContent>
                </Card>
            )}
        </div>
    )
}
