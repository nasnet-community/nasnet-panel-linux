import { useState } from "react"
import { toast } from "sonner"
import { Plus, RefreshCw, ShieldAlert, Trash2, TriangleAlert } from "lucide-react"
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
import { PortSocket, Wire, WireEmpty, type WireState } from "@/components/network/wire"
import {
    useDeletePortMapRule,
    usePortMapRules,
    usePortMapStatus,
    useProbePortMap,
    useSavePortMapRule,
} from "@/lib/queries/use-network"
import { useUpdateSettings } from "@/lib/queries/use-settings"
import { needsConfirm, verdictsFromError } from "@/lib/api/network"
import { cn } from "@/lib/utils"
import type {
    NetworkInterfaceView,
    NetworkState,
    PortMapRule,
    PortMapVerdict,
    PortMapWANView,
    Verdict,
} from "@/lib/types/network"

interface Props {
    state: NetworkState | undefined
    interfaces: NetworkInterfaceView[]
}

/** Where the path to the internet breaks, which is what each verdict means.
 *  "above" is past the upstream router, "at" is the router itself. */
const VERDICT: Record<
    PortMapVerdict,
    { label: string; tone: "ok" | "warn" | "bad" | "idle"; breaks: "none" | "above" | "at"; hint: string }
> = {
    pending: { label: "Checking", tone: "idle", breaks: "none", hint: "" },
    disabled: { label: "Off", tone: "idle", breaks: "none", hint: "" },
    public_direct: {
        label: "Public address",
        tone: "ok",
        breaks: "none",
        hint: "This uplink already faces the internet. Nothing to ask for.",
    },
    ok: { label: "Mapped", tone: "ok", breaks: "none", hint: "" },
    partial: {
        label: "Partly mapped",
        tone: "warn",
        breaks: "none",
        hint: "Some ports are open, some were refused. The refused ones are listed below.",
    },
    nested_nat: {
        label: "Nested NAT",
        tone: "warn",
        breaks: "above",
        hint: "The upstream router is behind another NAT, so its forward stops there. Only your ISP can open this.",
    },
    no_service: {
        label: "No service",
        tone: "warn",
        breaks: "at",
        hint: "The upstream router answered nothing on UPnP, NAT-PMP or PCP. Turn UPnP on there, or forward the ports by hand.",
    },
    denied: {
        label: "Refused",
        tone: "warn",
        breaks: "at",
        hint: "The upstream router has port mapping turned off. Enable it there, or forward the ports by hand.",
    },
    error: { label: "Error", tone: "bad", breaks: "at", hint: "" },
}

const TONE_BADGE = {
    ok: "success",
    warn: "warning",
    bad: "danger",
    idle: "outline",
} as const

type Draft = {
    uplink_key: string
    proto: "tcp" | "udp"
    port: string
    external_hint: string
    comment: string
}

const blankDraft: Draft = { uplink_key: "", proto: "tcp", port: "", external_hint: "", comment: "" }

export function UpstreamMappingCard({ state, interfaces }: Props) {
    const status = usePortMapStatus()
    const rules = usePortMapRules()
    const save = useSavePortMapRule()
    const remove = useDeletePortMapRule()
    const probe = useProbePortMap()
    const settings = useUpdateSettings()
    const confirm = useConfirm()

    const [draft, setDraft] = useState<Draft | null>(null)
    const [verdicts, setVerdicts] = useState<Verdict[]>([])

    const uplinks = interfaces
        .filter((i) => i.role === "wan" && i.present)
        .map((i) => ({
            key: i.key,
            label:
                i.label || state?.uplinks?.find((u) => u.if_name === i.if_name)?.label || i.if_name,
        }))

    const enabled = status.data?.enabled ?? false

    async function toggleFeature(on: boolean) {
        try {
            await settings.mutateAsync([
                {
                    key: "router_portmap_enabled",
                    value: on ? "true" : "false",
                    type: "bool",
                    category: "router",
                    label: "Upstream port mapping",
                    description:
                        "Ask the router in front of this box (UPnP, NAT-PMP or PCP) to forward the VPN ports here.",
                },
            ])
            void status.refetch()
        } catch {
            // useUpdateSettings already says the save failed.
        }
    }

    async function submit(confirmed = false) {
        if (!draft) return
        const payload = {
            uplink_key: draft.uplink_key,
            proto: draft.proto,
            port: Number(draft.port),
            external_hint: draft.external_hint ? Number(draft.external_hint) : 0,
            comment: draft.comment,
            enabled: true,
            confirmed,
        }
        setVerdicts([])
        try {
            await save.mutateAsync(payload)
            setDraft(null)
            toast.success("Rule added — asking upstream now")
        } catch (err) {
            const vs = verdictsFromError(err)
            if (needsConfirm(err)) {
                const ok = await confirm({
                    title: "This opens a way in",
                    confirmLabel: "Map it",
                    variant: "warning" as const,
                    typeToConfirm: "CONFIRM",
                    description:
                        vs.map((v) => v.message).join(" ") ||
                        "Anyone who reaches the upstream router can attempt to use it.",
                    icon: <ShieldAlert className="h-5 w-5" />,
                })
                if (ok) await submit(true)
                return
            }
            setVerdicts(vs.length ? vs : [{ rule: "", level: "reject", message: String(err) }])
        }
    }

    async function toggleRule(rule: PortMapRule, on: boolean, confirmed = false) {
        try {
            await save.mutateAsync({ ...rule, enabled: on, confirmed })
            toast.success(
                on ? `${rule.proto}/${rule.port} turned on` : `${rule.proto}/${rule.port} turned off`,
            )
        } catch (err) {
            if (!confirmed && needsConfirm(err)) {
                const ok = await confirm({
                    title: "This opens a way in",
                    confirmLabel: "Map it",
                    variant: "warning" as const,
                    typeToConfirm: "CONFIRM",
                    description:
                        verdictsFromError(err)
                            .map((v) => v.message)
                            .join(" ") || "Anyone who reaches the upstream router can attempt to use it.",
                    icon: <ShieldAlert className="h-5 w-5" />,
                })
                if (ok) await toggleRule(rule, on, true)
                return
            }
            toast.error(err instanceof Error ? err.message : "The rule did not save")
        }
    }

    async function del(rule: PortMapRule) {
        const ok = await confirm({
            title: "Remove this rule?",
            description: `The mapping for ${rule.proto}/${rule.port} is released. Connections using it will drop.`,
            confirmLabel: "Remove",
            variant: "destructive",
        })
        if (!ok) return
        try {
            await remove.mutateAsync(rule.id)
            toast.success("Rule removed — mapping released")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to remove the rule")
        }
    }

    if (status.isLoading) return <Skeleton className="h-40 w-full" />

    const wans = status.data?.wans ?? []
    const ruleRows = rules.data ?? []

    return (
        <Card>
            <CardHeader>
                <CardTitle>Upstream port mapping</CardTitle>
                <CardDescription>
                    Ask the router in front of this box to forward ports here — the opposite
                    direction from the forwards above. VPN inbounds are mapped for you.
                </CardDescription>
                <CardAction>
                    <div className="flex items-center gap-2">
                        {enabled && (
                            <Button
                                size="sm"
                                variant="ghost"
                                disabled={probe.isPending}
                                onClick={() =>
                                    void probe.mutateAsync().then(
                                        () => toast.success("Asking the upstream routers again"),
                                        (e: unknown) =>
                                            toast.error(e instanceof Error ? e.message : "Probe failed"),
                                    )
                                }
                            >
                                <RefreshCw
                                    className={cn("mr-1.5 h-3.5 w-3.5", probe.isPending && "animate-spin")}
                                />
                                Check again
                            </Button>
                        )}
                        <Switch
                            checked={enabled}
                            aria-label={enabled ? "Turn upstream mapping off" : "Turn upstream mapping on"}
                            disabled={settings.isPending}
                            onCheckedChange={(v) => void toggleFeature(v)}
                        />
                    </div>
                </CardAction>
            </CardHeader>

            <CardContent className="space-y-6">
                {status.isError && (
                    <Alert variant="warning">
                        <TriangleAlert className="h-4 w-4" />
                        <AlertDescription>
                            The mapping status could not be read. {status.error?.message}
                        </AlertDescription>
                    </Alert>
                )}

                {!enabled ? (
                    <WireEmpty
                        from="internet"
                        to="this box"
                        title="Upstream mapping is off"
                        description="Turn it on and this box asks the router in front of it — over UPnP, NAT-PMP or PCP — to forward the VPN ports here."
                    />
                ) : wans.length === 0 ? (
                    <WireEmpty
                        from="internet"
                        to="this box"
                        title="No uplinks to map"
                        description="Assign a WAN role on the Ports tab first."
                    />
                ) : (
                    <div className="space-y-3">
                        {wans.map((wan) => (
                            <WanBlock key={wan.key} wan={wan} />
                        ))}
                    </div>
                )}

                {enabled && (
                    <div className="space-y-3">
                        <div className="flex items-center justify-between">
                            <div>
                                <p className="text-sm font-medium">Extra ports</p>
                                <p className="text-text-secondary text-xs">
                                    Anything beyond the inbounds. The panel and SSH are never mapped for you.
                                </p>
                            </div>
                            {!draft && (
                                <Button size="sm" variant="outline" onClick={() => setDraft(blankDraft)}>
                                    <Plus className="mr-1.5 h-3.5 w-3.5" />
                                    Add port
                                </Button>
                            )}
                        </div>

                        {ruleRows.length > 0 && (
                            <ul className="divide-border-subtle divide-y">
                                {ruleRows.map((r) => (
                                    <li key={r.id} className="flex items-center justify-between gap-3 py-2">
                                        <div className="flex min-w-0 items-center gap-3">
                                            <Switch
                                                checked={r.enabled}
                                                aria-label={`Toggle ${r.proto}/${r.port}`}
                                                disabled={save.isPending}
                                                onCheckedChange={(v) => void toggleRule(r, v)}
                                            />
                                            <PortSocket className={cn(!r.enabled && "text-text-tertiary")}>
                                                {r.proto}/{r.port}
                                                {r.external_hint > 0 && ` as :${r.external_hint}`}
                                            </PortSocket>
                                            <span className="text-text-secondary truncate text-xs">
                                                {r.uplink_key
                                                    ? (uplinks.find((u) => u.key === r.uplink_key)?.label ??
                                                      r.uplink_key)
                                                    : "every WAN"}
                                                {r.comment && ` · ${r.comment}`}
                                            </span>
                                        </div>
                                        <Button
                                            size="icon-sm"
                                            variant="ghost"
                                            aria-label={`Remove ${r.proto}/${r.port}`}
                                            onClick={() => void del(r)}
                                        >
                                            <Trash2 className="h-3.5 w-3.5" />
                                        </Button>
                                    </li>
                                ))}
                            </ul>
                        )}

                        {draft && (
                            <div className="border-border-subtle space-y-3 rounded-lg border p-3">
                                <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Protocol</Label>
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
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Port here</Label>
                                        <Input
                                            inputMode="numeric"
                                            value={draft.port}
                                            onChange={(e) => setDraft({ ...draft, port: e.target.value })}
                                        />
                                    </div>
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Port outside</Label>
                                        <Input
                                            inputMode="numeric"
                                            placeholder="same"
                                            value={draft.external_hint}
                                            onChange={(e) =>
                                                setDraft({ ...draft, external_hint: e.target.value })
                                            }
                                        />
                                    </div>
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Uplink</Label>
                                        <Select
                                            value={draft.uplink_key || "all"}
                                            onValueChange={(v) =>
                                                setDraft({ ...draft, uplink_key: v === "all" ? "" : v })
                                            }
                                        >
                                            <SelectTrigger>
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="all">Every WAN</SelectItem>
                                                {uplinks.map((u) => (
                                                    <SelectItem key={u.key} value={u.key}>
                                                        {u.label}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Note</Label>
                                        <Input
                                            value={draft.comment}
                                            onChange={(e) => setDraft({ ...draft, comment: e.target.value })}
                                        />
                                    </div>
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

                                <div className="flex justify-end gap-2">
                                    <Button
                                        size="sm"
                                        variant="ghost"
                                        onClick={() => {
                                            setDraft(null)
                                            setVerdicts([])
                                        }}
                                    >
                                        Cancel
                                    </Button>
                                    <Button
                                        size="sm"
                                        disabled={!draft.port || save.isPending}
                                        onClick={() => void submit()}
                                    >
                                        Add port
                                    </Button>
                                </div>
                            </div>
                        )}
                    </div>
                )}
            </CardContent>
        </Card>
    )
}

/** One uplink: the path out, then what is riding it. */
function WanBlock({ wan }: { wan: PortMapWANView }) {
    const v = VERDICT[wan.verdict] ?? VERDICT.error
    const protocols = [
        wan.probe.pmp && "NAT-PMP",
        wan.probe.pcp && "PCP",
        wan.probe.upnp && "UPnP",
    ].filter(Boolean) as string[]

    return (
        <div className="border-border-subtle rounded-lg border p-3">
            <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium">{wan.label || wan.if_name}</span>
                {wan.label && (
                    <span className="text-text-tertiary font-mono text-xs">{wan.if_name}</span>
                )}
                <Badge variant={TONE_BADGE[v.tone]}>{v.label}</Badge>
                {wan.suspended && <Badge variant="outline">Uplink down</Badge>}
                {protocols.length > 0 && (
                    <span className="text-text-tertiary ml-auto text-xs">{protocols.join(" · ")}</span>
                )}
            </div>

            <PathStrip wan={wan} breaks={v.breaks} />

            {(v.hint || wan.error) && (
                <p className="text-text-secondary mt-2 text-xs">{v.hint || wan.error}</p>
            )}

            {(wan.unmapped_ranges?.length ?? 0) > 0 && (
                <p className="text-text-secondary mt-2 text-xs">
                    Port-hopping ranges stay unmapped:{" "}
                    <span className="font-mono">{wan.unmapped_ranges!.join(", ")}</span>. Forward those
                    by hand if you need them.
                </p>
            )}

            {wan.failures?.length > 0 && (
                <ul className="border-border-subtle mt-3 space-y-1.5 border-t pt-3">
                    {wan.failures.map((f) => (
                        <li
                            key={`fail:${f.proto}:${f.internal_port}`}
                            className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs"
                        >
                            <PortSocket className="text-text-tertiary">
                                {f.proto}/{f.internal_port}
                            </PortSocket>
                            <Wire state="cut" className="max-w-16" />
                            <span className="text-status-warning">not mapped</span>
                            <span className="text-text-tertiary ml-auto truncate">
                                {f.source} · {f.error}
                            </span>
                        </li>
                    ))}
                </ul>
            )}

            {wan.leases.length > 0 && (
                <ul className="border-border-subtle mt-3 space-y-1.5 border-t pt-3">
                    {wan.leases.map((l) => (
                        <li
                            key={`${l.proto}:${l.internal_port}`}
                            className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs"
                        >
                            <PortSocket>
                                {l.proto}/{l.internal_port}
                            </PortSocket>
                            <span className="text-text-tertiary">reachable at</span>
                            <span className="font-mono tabular-nums">
                                {l.external_ip}:{l.external_port}
                            </span>
                            <span className="text-text-tertiary ml-auto">
                                {l.source} · renews {new Date(l.renews_at).toLocaleTimeString()}
                            </span>
                            {l.warning && (
                                <span className="text-status-warning basis-full">
                                    Outside port is {l.external_port}, but clients are still handed{" "}
                                    {l.internal_port}. Hand out {l.external_ip}:{l.external_port}, or free
                                    the port on the upstream router and check again.
                                </span>
                            )}
                        </li>
                    ))}
                </ul>
            )}
        </div>
    )
}

/** The chain from the internet to this box. Where it is cut is the diagnosis:
 *  above the router means only the ISP can fix it, at the router means the
 *  operator can. Whole chain flows when the mapping works. */
function PathStrip({ wan, breaks }: { wan: PortMapWANView; breaks: "none" | "above" | "at" }) {
    const direct = wan.verdict === "public_direct"
    const upstream = wan.external_ip || wan.gateway || "upstream router"
    const live = direct || wan.verdict === "ok"
    const seg = (cut: boolean): WireState => (cut ? "cut" : live ? "live" : "off")

    return (
        <div className="mt-3 flex items-center gap-2.5 overflow-x-auto font-mono text-xs whitespace-nowrap">
            <span className="text-text-tertiary">internet</span>
            <Wire state={seg(breaks === "above")} className="min-w-8" />
            {!direct && (
                <>
                    <span className="text-text-primary tabular-nums">{upstream}</span>
                    <Wire state={seg(breaks === "at")} className="min-w-8" />
                </>
            )}
            <span className="text-text-tertiary">this box</span>
        </div>
    )
}
