import { useEffect, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"
import {
    Select,
    SelectContent,
    SelectGroup,
    SelectItem,
    SelectLabel,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { Callout } from "@/components/connection-dialog/section"
import { CountryFlag } from "@/components/ui/country-flag"
import { listAllHosts } from "@/lib/api/hosts"
import { isLiteNode } from "@/lib/node-kind"
import { useBalancingRules, useNodeInbounds, useNodeOutbounds } from "@/lib/queries/use-nodes"
import { useCreateReverseTunnel, useUpdateReverseTunnel } from "@/lib/queries/use-reverse-tunnels"
import type { Inbound, Node, ReverseTunnel, ReverseTunnelInput } from "@/lib/types"

// "nodes": the panel runs both ends. "external": one end runs elsewhere.
export type ReverseTunnelMode = "nodes" | "external"

type Role = "portal" | "bridge"

// Radix Select can't hold an empty value, so the defaults get names.
const MANAGED_EXIT = "managed"
const OWN_ADDRESS = "inbound"

interface ReverseTunnelDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    mode: ReverseTunnelMode
    tunnel: ReverseTunnel | null
    nodeId: number
    nodes: Node[]
    inbounds: Inbound[]
    onCreated?: (tunnel: ReverseTunnel) => void
}

export function ReverseTunnelDialog({ open, onOpenChange, mode, tunnel, nodeId, nodes, inbounds, onCreated }: ReverseTunnelDialogProps) {
    const editing = tunnel != null
    const create = useCreateReverseTunnel()
    const update = useUpdateReverseTunnel()

    const [role, setRole] = useState<Role>("portal")
    const [otherId, setOtherId] = useState(0)
    const [portalInboundId, setPortalInboundId] = useState(0)
    const [host, setHost] = useState(OWN_ADDRESS)
    const [routed, setRouted] = useState<string[]>([])
    const [exit, setExit] = useState(MANAGED_EXIT)
    const [link, setLink] = useState("")
    const [name, setName] = useState("")
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        if (!open) return
        setError(null)
        if (tunnel) {
            const isPortal = tunnel.portal_node_id === nodeId
            setRole(isPortal ? "portal" : "bridge")
            setOtherId((isPortal ? tunnel.bridge_node_id : tunnel.portal_node_id) ?? 0)
            setPortalInboundId(tunnel.portal_inbound_id ?? 0)
            setHost(tunnel.portal_host_id ? String(tunnel.portal_host_id) : OWN_ADDRESS)
            setRouted(tunnel.inbound_tags ?? [])
            setExit(tunnel.exit_balancer_tag ? `b:${tunnel.exit_balancer_tag}` : tunnel.exit_outbound_tag ? `o:${tunnel.exit_outbound_tag}` : MANAGED_EXIT)
            setLink(tunnel.portal_link ?? "")
            setName(tunnel.name)
        } else {
            setRole("portal")
            setOtherId(0)
            setPortalInboundId(0)
            setHost(OWN_ADDRESS)
            setRouted([])
            setExit(MANAGED_EXIT)
            setLink("")
            setName("")
        }
    }, [open, tunnel, nodeId])

    const external = mode === "external"
    const portalNodeId = role === "portal" ? nodeId : external ? 0 : otherId
    const bridgeNodeId = role === "bridge" ? nodeId : external ? 0 : otherId
    // Which ends this panel configures, and so which fields apply.
    const showPortal = portalNodeId > 0 || (!external && role === "bridge")
    const showBridge = bridgeNodeId > 0 || (!external && role === "portal")
    const showLink = external && role === "bridge"

    const { data: portalInbounds = [] } = useNodeInbounds(portalNodeId)
    const { data: bridgeOutbounds = [] } = useNodeOutbounds(bridgeNodeId)
    const { data: bridgeBalancers = [] } = useBalancingRules(bridgeNodeId)
    const { data: hosts = [] } = useQuery({
        queryKey: ["hosts", "reverse-tunnel", portalInboundId],
        queryFn: async () => {
            const res = await listAllHosts({ inbound_id: portalInboundId, disabled: false, limit: 100, offset: 0 })
            return res.success ? res.data || [] : []
        },
        enabled: open && portalInboundId > 0,
    })

    // MTProto nodes run no Xray, so they can be neither end
    const otherNodes = useMemo(() => nodes.filter((n) => n.id !== nodeId && !isLiteNode(n)), [nodes, nodeId])
    const vlessInbounds = (portalNodeId === nodeId ? inbounds : portalInbounds).filter(
        (i) => i.protocol === "vless" && !i.is_disabled,
    )
    const routable = portalNodeId === nodeId ? inbounds : portalInbounds
    const exitOutbounds = bridgeOutbounds.filter((o) => !o.is_disabled && !o.managed_pool_id)
    const exitBalancers = bridgeBalancers.filter((b) => b.enabled)

    const toggleRouted = (tag: string) =>
        setRouted((cur) => (cur.includes(tag) ? cur.filter((t) => t !== tag) : [...cur, tag]))

    const save = async () => {
        setError(null)
        if (!external && !otherId) return setError(role === "portal" ? "Choose the bridge node" : "Choose the portal node")
        if (showPortal && !portalInboundId) return setError("Choose the portal inbound the bridge connects to")
        if (showLink && !link.trim().startsWith("vless://")) return setError("Paste the external portal's vless:// link")
        const data: ReverseTunnelInput = {
            name: name.trim(),
            portal_node_id: portalNodeId || null,
            portal_inbound_id: showPortal ? portalInboundId : null,
            portal_host_id: showPortal && host !== OWN_ADDRESS ? Number(host) : null,
            inbound_tags: showPortal ? routed : [],
            bridge_node_id: bridgeNodeId || null,
            exit_outbound_tag: showBridge && exit.startsWith("o:") ? exit.slice(2) : "",
            exit_balancer_tag: showBridge && exit.startsWith("b:") ? exit.slice(2) : "",
            portal_link: showLink ? link.trim() : "",
        }
        try {
            if (tunnel) {
                await update.mutateAsync({ id: tunnel.id, data })
            } else {
                const created = await create.mutateAsync(data)
                onCreated?.(created)
            }
            onOpenChange(false)
        } catch (e) {
            setError(e instanceof Error ? e.message : "Save failed")
        }
    }

    const busy = create.isPending || update.isPending
    const title = editing ? "Edit reverse tunnel" : external ? "Tunnel to an external server" : "Tunnel to another node"

    return (
        <ResponsiveDialog
            open={open}
            onOpenChange={onOpenChange}
            title={title}
            description="The bridge dials the portal; traffic the portal routes into the tunnel leaves through the bridge's internet."
            onSave={save}
            saveLabel={busy ? "Saving..." : editing ? "Save Changes" : "Create Tunnel"}
            saveDisabled={busy}
            footerNote={error ? <span className="text-destructive">{error}</span> : undefined}
        >
            <div className="space-y-5">
                <div className="space-y-2">
                    <Label>This node is the</Label>
                    <Select value={role} onValueChange={(v) => setRole(v as Role)} disabled={editing}>
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="portal">Portal: users connect here, traffic leaves through the bridge</SelectItem>
                            <SelectItem value="bridge">Bridge: dials the portal, traffic leaves through this node</SelectItem>
                        </SelectContent>
                    </Select>
                </div>

                {!external && (
                    <div className="space-y-2">
                        <Label>{role === "portal" ? "Bridge node" : "Portal node"}</Label>
                        <Select value={otherId ? String(otherId) : ""} onValueChange={(v) => { setOtherId(Number(v)); setPortalInboundId(0); setRouted([]); setExit(MANAGED_EXIT) }} disabled={editing}>
                            <SelectTrigger className="w-full">
                                <SelectValue placeholder="Choose a node..." />
                            </SelectTrigger>
                            <SelectContent>
                                {otherNodes.map((n) => (
                                    <SelectItem key={n.id} value={String(n.id)}>
                                        <span className="inline-flex items-center gap-2">
                                            {n.country_code && <CountryFlag code={n.country_code} className="h-3 w-4 rounded-[2px]" />}
                                            {n.name}
                                            {!n.is_online && <span className="text-xs text-muted-foreground">offline</span>}
                                        </span>
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                )}

                {showLink && (
                    <div className="space-y-2">
                        <Label>Portal link</Label>
                        <Textarea
                            value={link}
                            onChange={(e) => setLink(e.target.value)}
                            placeholder="vless://id@portal.example.com:443?security=reality&..."
                            className="font-mono text-xs min-h-24"
                        />
                        <p className="text-xs text-muted-foreground">
                            A VLESS link for a client of the external portal. After saving, the panel shows the client entry to add on the portal.
                        </p>
                    </div>
                )}

                {showPortal && (portalNodeId > 0) && (
                    <>
                        <div className="space-y-2">
                            <Label>Portal inbound</Label>
                            {vlessInbounds.length === 0 ? (
                                <p className="text-sm text-muted-foreground border rounded-md px-3 py-2">The portal node has no enabled VLESS inbound</p>
                            ) : (
                                <Select value={portalInboundId ? String(portalInboundId) : ""} onValueChange={(v) => { setPortalInboundId(Number(v)); setHost(OWN_ADDRESS) }}>
                                    <SelectTrigger className="w-full">
                                        <SelectValue placeholder="Choose the inbound the bridge connects to..." />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {vlessInbounds.map((i) => (
                                            <SelectItem key={i.id} value={String(i.id)}>
                                                <span className="font-mono">{i.tag}</span>
                                                <span className="text-muted-foreground"> · {i.network || "raw"}{i.security && i.security !== "none" ? `+${i.security}` : ""} :{i.port}</span>
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            )}
                            <p className="text-xs text-muted-foreground">
                                The tunnel gets its own hidden user here. Customers on this inbound keep working, and can't take the tunnel over.
                            </p>
                        </div>

                        {hosts.length > 0 && (
                            <div className="space-y-2">
                                <Label>Bridge dials</Label>
                                <Select value={host} onValueChange={setHost}>
                                    <SelectTrigger className="w-full">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value={OWN_ADDRESS}>The inbound's own address</SelectItem>
                                        {hosts.map((h) => (
                                            <SelectItem key={h.id} value={String(h.id)}>
                                                {h.remark || "Host"} <span className="font-mono text-muted-foreground">{h.address}{h.port ? `:${h.port}` : ""}</span>
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                <p className="text-xs text-muted-foreground">The bridge connects the way a customer's link for this host does.</p>
                            </div>
                        )}

                        {routable.length > 0 && (
                            <div className="space-y-2">
                                <Label>Send these portal inbounds through the tunnel</Label>
                                <div className="border rounded-md p-3 max-h-40 overflow-y-auto space-y-2">
                                    {routable.map((i) => (
                                        <div key={i.id} className="flex items-center gap-2">
                                            <Checkbox id={`rvs-in-${i.id}`} checked={routed.includes(i.tag)} onCheckedChange={() => toggleRouted(i.tag)} />
                                            <label htmlFor={`rvs-in-${i.id}`} className="text-sm font-mono cursor-pointer select-none">{i.tag}</label>
                                        </div>
                                    ))}
                                </div>
                                <p className="text-xs text-muted-foreground">
                                    Optional. Routing rules on the portal can also send any traffic to the tunnel, and pools can use it.
                                </p>
                            </div>
                        )}
                    </>
                )}

                {showBridge && bridgeNodeId > 0 && (
                    <div className="space-y-2">
                        <Label>Traffic leaves the bridge through</Label>
                        <Select value={exit} onValueChange={setExit}>
                            <SelectTrigger className="w-full">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value={MANAGED_EXIT}>Direct, LAN blocked</SelectItem>
                                {exitOutbounds.length > 0 && (
                                    <SelectGroup>
                                        <SelectLabel>Outbounds</SelectLabel>
                                        {exitOutbounds.map((o) => (
                                            <SelectItem key={o.id} value={`o:${o.tag}`}>
                                                <span className="font-mono">{o.tag}</span> <span className="text-muted-foreground">({o.protocol})</span>
                                            </SelectItem>
                                        ))}
                                    </SelectGroup>
                                )}
                                {exitBalancers.length > 0 && (
                                    <SelectGroup>
                                        <SelectLabel>Balancers</SelectLabel>
                                        {exitBalancers.map((b) => (
                                            <SelectItem key={b.id} value={`b:${b.tag}`}>
                                                <span className="font-mono">{b.tag}</span>
                                            </SelectItem>
                                        ))}
                                    </SelectGroup>
                                )}
                            </SelectContent>
                        </Select>
                        <p className="text-xs text-muted-foreground">
                            A Freedom outbound is used through a copy that keeps the bridge's private addresses blocked.
                        </p>
                    </div>
                )}

                <div className="space-y-2">
                    <Label>Name</Label>
                    <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Portal → Bridge" maxLength={100} />
                </div>

                <Callout tone="info">
                    Customers sent through a reverse tunnel aren't limited by their bandwidth tier. Saving restarts Xray on the nodes at both ends.
                </Callout>
            </div>
        </ResponsiveDialog>
    )
}
