import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import type { WireGuardSettings, WireGuardPeer } from "@/lib/types"
import { useState, useRef, useEffect } from "react"

interface WireGuardFormProps {
    settings?: WireGuardSettings
    onChange: (settings: WireGuardSettings) => void
    outbound?: boolean
}

export function WireGuardForm({ settings, onChange, outbound = false }: WireGuardFormProps) {
    const data = settings || { secretKey: "", endpoint: [], mtu: 1420, peers: [] }

    // Local text state for Remote DNS, like Reserved below
    const [remoteDnsText, setRemoteDnsText] = useState(() => (data.remoteDNS || []).join(", "))
    const remoteDnsFocused = useRef(false)
    const remoteDnsKey = JSON.stringify(data.remoteDNS || [])
    useEffect(() => {
        if (!remoteDnsFocused.current) {
            setRemoteDnsText((data.remoteDNS || []).join(", "))
        }
    }, [remoteDnsKey])

    // Local text state for Reserved to prevent comma from being eaten during typing
    const [reservedText, setReservedText] = useState(() => (data.reserved || []).join(", "))
    const reservedFocused = useRef(false)
    const reservedKey = JSON.stringify(data.reserved || [])
    useEffect(() => {
        if (!reservedFocused.current) {
            setReservedText((data.reserved || []).join(", "))
        }
    }, [reservedKey])

    // Local text state per peer for Allowed IPs
    const [peerIpsText, setPeerIpsText] = useState<Record<number, string>>(() => {
        const init: Record<number, string> = {}
        ;(data.peers || []).forEach((peer, i) => {
            init[i] = (peer.allowedIps || []).join(", ")
        })
        return init
    })
    const peerIpsFocused = useRef<Record<number, boolean>>({})
    const peersIpsKey = JSON.stringify((data.peers || []).map((p) => p.allowedIps || []))
    useEffect(() => {
        setPeerIpsText((prev) => {
            const next: Record<number, string> = {}
            ;(data.peers || []).forEach((peer, i) => {
                if (peerIpsFocused.current[i]) {
                    next[i] = prev[i] ?? (peer.allowedIps || []).join(", ")
                } else {
                    next[i] = (peer.allowedIps || []).join(", ")
                }
            })
            return next
        })
    }, [peersIpsKey])

    const addPeer = () => {
        onChange({
            ...data,
            peers: [...(data.peers || []), { publicKey: "", allowedIps: ["0.0.0.0/0", "::/0"] }],
        })
    }

    const updatePeer = (index: number, peer: WireGuardPeer) => {
        const peers = [...(data.peers || [])]
        peers[index] = peer
        onChange({ ...data, peers })
    }

    const removePeer = (index: number) => {
        const peers = [...(data.peers || [])]
        peers.splice(index, 1)
        onChange({ ...data, peers })
    }

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Secret Key (Private Key)</Label>
                    <Input
                        placeholder="Server private key"
                        value={data.secretKey}
                        onChange={(e) => onChange({ ...data, secretKey: e.target.value })}
                    />
                </div>
                <div className="space-y-2">
                    <Label>MTU</Label>
                    <Input
                        type="number"
                        placeholder="1420"
                        value={data.mtu || 1420}
                        onChange={(e) => onChange({ ...data, mtu: parseInt(e.target.value) || 1420 })}
                    />
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Reserved</Label>
                    <Input
                        placeholder="0, 0, 0"
                        value={reservedText}
                        onFocus={() => { reservedFocused.current = true }}
                        onChange={(e) => {
                            setReservedText(e.target.value)
                            onChange({
                                ...data,
                                reserved: e.target.value
                                    .split(",")
                                    .map((s) => parseInt(s.trim()))
                                    .filter((n) => !isNaN(n)),
                            })
                        }}
                        onBlur={() => {
                            reservedFocused.current = false
                            const parsed = reservedText
                                .split(",")
                                .map((s) => parseInt(s.trim()))
                                .filter((n) => !isNaN(n))
                            setReservedText(parsed.join(", "))
                        }}
                    />
                    <p className="text-xs text-muted-foreground">Comma-separated bytes (e.g. 0, 0, 0)</p>
                </div>
            </div>

            <div className="space-y-2">
                <Label>Local Endpoints (CIDR)</Label>
                <Textarea
                    placeholder={"10.0.0.1/32\nfd00::1/128"}
                    value={(data.endpoint || []).join("\n")}
                    onChange={(e) =>
                        onChange({
                            ...data,
                            endpoint: e.target.value.split("\n").filter(Boolean),
                        })
                    }
                    rows={2}
                />
                <p className="text-xs text-muted-foreground">One address per line in CIDR format</p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Peer Pool CIDR</Label>
                    <Input
                        placeholder="10.8.0.0/16"
                        value={data.peerPoolCidr || ""}
                        onChange={(e) => onChange({ ...data, peerPoolCidr: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">Per-device selling: device IPs allocated from this pool</p>
                </div>
                <div className="space-y-2">
                    <Label>Client DNS</Label>
                    <Input
                        placeholder="1.1.1.1"
                        value={data.clientDns || ""}
                        onChange={(e) => onChange({ ...data, clientDns: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">DNS written into generated client .conf</p>
                </div>
            </div>

            {outbound && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div className="space-y-2">
                        <Label>Resolve domains</Label>
                        <Select
                            value={data.domainStrategy || "tunnel"}
                            onValueChange={(value) =>
                                onChange({
                                    ...data,
                                    domainStrategy: value === "tunnel" ? undefined : value as WireGuardSettings["domainStrategy"],
                                })
                            }
                        >
                            <SelectTrigger className="w-full">
                                <SelectValue />
                            </SelectTrigger>
                            {/* Anything but "tunnel" goes out as the outbound's
                                targetStrategy; xray-core 26.9.30 removed the
                                WireGuard domainStrategy. */}
                            <SelectContent>
                                <SelectItem value="tunnel">Inside the tunnel</SelectItem>
                                <SelectItem value="forceip">Xray DNS, any IP</SelectItem>
                                <SelectItem value="forceipv4">Xray DNS, IPv4</SelectItem>
                                <SelectItem value="forceipv6">Xray DNS, IPv6</SelectItem>
                                <SelectItem value="forceipv4v6">Xray DNS, IPv4 then IPv6</SelectItem>
                                <SelectItem value="forceipv6v4">Xray DNS, IPv6 then IPv4</SelectItem>
                            </SelectContent>
                        </Select>
                        <p className="text-xs text-muted-foreground">How destinations are looked up. An Xray DNS choice also picks the address family for the peer endpoint.</p>
                    </div>
                    {!data.domainStrategy && (
                        <div className="space-y-2">
                            <Label>Remote DNS</Label>
                            <Input
                                placeholder="1.1.1.1, 1.0.0.1"
                                value={remoteDnsText}
                                onFocus={() => { remoteDnsFocused.current = true }}
                                onChange={(e) => {
                                    setRemoteDnsText(e.target.value)
                                    const ips = e.target.value.split(",").map((s) => s.trim()).filter(Boolean)
                                    onChange({ ...data, remoteDNS: ips.length > 0 ? ips : undefined })
                                }}
                                onBlur={() => {
                                    remoteDnsFocused.current = false
                                    setRemoteDnsText((data.remoteDNS || []).join(", "))
                                }}
                            />
                            <p className="text-xs text-muted-foreground">DNS servers asked through the tunnel. Empty uses Cloudflare.</p>
                        </div>
                    )}
                </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="flex items-center justify-between">
                    <Label>No Kernel TUN</Label>
                    <Switch
                        checked={data.noKernelTun ?? false}
                        onCheckedChange={(checked) => onChange({ ...data, noKernelTun: checked })}
                    />
                </div>
            </div>

            {/* Peers */}
            <div className="space-y-3 border-t pt-4">
                <div className="flex items-center justify-between">
                    <h4 className="font-medium">Peers</h4>
                    <Button type="button" size="sm" variant="outline" onClick={addPeer}>
                        <HiOutlinePlus className="w-4 h-4 mr-1" /> Add Peer
                    </Button>
                </div>
                {(data.peers || []).map((peer, index) => (
                    <div key={index} className="border rounded-lg p-4 space-y-3">
                        <div className="flex justify-between items-center">
                            <span className="text-sm font-medium">Peer #{index + 1}</span>
                            <Button
                                type="button"
                                size="icon"
                                variant="ghost"
                                className="text-red-500 h-10 w-10 md:h-8 md:w-8"
                                onClick={() => removePeer(index)}
                            >
                                <HiOutlineTrash className="w-4 h-4" />
                            </Button>
                        </div>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                            <div className="space-y-1">
                                <Label className="text-xs">Public Key</Label>
                                <Input
                                    placeholder="Peer public key"
                                    value={peer.publicKey}
                                    onChange={(e) =>
                                        updatePeer(index, { ...peer, publicKey: e.target.value })
                                    }
                                />
                            </div>
                            <div className="space-y-1">
                                <Label className="text-xs">Endpoint</Label>
                                <Input
                                    placeholder="host:port"
                                    value={peer.endpoint || ""}
                                    onChange={(e) =>
                                        updatePeer(index, { ...peer, endpoint: e.target.value })
                                    }
                                />
                            </div>
                        </div>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                            <div className="space-y-1">
                                <Label className="text-xs">Pre-Shared Key</Label>
                                <Input
                                    placeholder="Optional pre-shared key"
                                    value={peer.preSharedKey || ""}
                                    onChange={(e) =>
                                        updatePeer(index, { ...peer, preSharedKey: e.target.value })
                                    }
                                />
                            </div>
                            <div className="space-y-1">
                                <Label className="text-xs">Keep Alive (seconds)</Label>
                                <Input
                                    type="number"
                                    placeholder="0 (disabled)"
                                    value={peer.keepAlive || 0}
                                    onChange={(e) =>
                                        updatePeer(index, {
                                            ...peer,
                                            keepAlive: parseInt(e.target.value) || 0,
                                        })
                                    }
                                />
                            </div>
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs">Allowed IPs</Label>
                            <Input
                                placeholder="0.0.0.0/0, ::/0"
                                value={peerIpsText[index] ?? (peer.allowedIps || []).join(", ")}
                                onFocus={() => { peerIpsFocused.current[index] = true }}
                                onChange={(e) => {
                                    setPeerIpsText((prev) => ({ ...prev, [index]: e.target.value }))
                                    updatePeer(index, {
                                        ...peer,
                                        allowedIps: e.target.value.split(",").map((s) => s.trim()).filter(Boolean),
                                    })
                                }}
                                onBlur={() => {
                                    peerIpsFocused.current[index] = false
                                    const text = peerIpsText[index] ?? ""
                                    const parsed = text.split(",").map((s) => s.trim()).filter(Boolean)
                                    setPeerIpsText((prev) => ({ ...prev, [index]: parsed.join(", ") }))
                                }}
                            />
                        </div>
                    </div>
                ))}
            </div>
        </div>
    )
}
