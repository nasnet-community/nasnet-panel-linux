import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import type { FreedomSettings, FreedomNoise } from "@/lib/types"

interface FreedomFormProps {
    settings?: FreedomSettings
    onChange: (s: FreedomSettings) => void
}

export function FreedomForm({ settings, onChange }: FreedomFormProps) {
    const data = settings || { domainStrategy: "AsIs" }

    const addNoise = () => {
        onChange({ ...data, noise: [...(data.noise || []), { type: "rand", packet: "", delay: "" }] })
    }

    const updateNoise = (index: number, updates: Partial<FreedomNoise>) => {
        const noise = [...(data.noise || [])]
        noise[index] = { ...noise[index], ...updates }
        onChange({ ...data, noise })
    }

    const removeNoise = (index: number) => {
        const noise = [...(data.noise || [])]
        noise.splice(index, 1)
        onChange({ ...data, noise })
    }

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Domain Strategy</Label>
                    <Select
                        value={data.domainStrategy || "AsIs"}
                        onValueChange={(value) => onChange({ ...data, domainStrategy: value })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="AsIs">AsIs</SelectItem>
                            <SelectItem value="UseIP">UseIP</SelectItem>
                            <SelectItem value="UseIPv4">UseIPv4</SelectItem>
                            <SelectItem value="UseIPv6">UseIPv6</SelectItem>
                            <SelectItem value="UseIPv4v6">UseIPv4v6</SelectItem>
                            <SelectItem value="UseIPv6v4">UseIPv6v4</SelectItem>
                            <SelectItem value="ForceIP">ForceIP</SelectItem>
                            <SelectItem value="ForceIPv4">ForceIPv4</SelectItem>
                            <SelectItem value="ForceIPv6">ForceIPv6</SelectItem>
                            <SelectItem value="ForceIPv4v6">ForceIPv4v6</SelectItem>
                            <SelectItem value="ForceIPv6v4">ForceIPv6v4</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                <div className="space-y-2">
                    <Label>Redirect</Label>
                    <Input
                        placeholder="host:port (optional)"
                        value={data.redirect || ""}
                        onChange={(e) => onChange({ ...data, redirect: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">Override destination</p>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>User Level</Label>
                    <Input
                        type="number"
                        placeholder="0"
                        value={data.userLevel ?? ""}
                        onChange={(e) => onChange({ ...data, userLevel: parseInt(e.target.value) || 0 })}
                    />
                </div>
                <div className="space-y-2">
                    <Label>Proxy Protocol</Label>
                    <Select
                        value={String(data.proxyProtocol || 0)}
                        onValueChange={(value) => onChange({ ...data, proxyProtocol: parseInt(value) })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="0">Off</SelectItem>
                            <SelectItem value="1">v1</SelectItem>
                            <SelectItem value="2">v2</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
            </div>

            {/* Fragment Settings */}
            <div className="space-y-3 border rounded-md p-4 bg-muted/20">
                <div className="flex items-center justify-between">
                    <h4 className="text-sm font-medium">Fragment (Anti-Censorship)</h4>
                    <Switch
                        checked={!!data.fragment}
                        onCheckedChange={(checked) =>
                            onChange({ ...data, fragment: checked ? { packets: "tlshello", length: "100-200", interval: "10-20" } : undefined })
                        }
                    />
                </div>
                {data.fragment && (
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                        <div className="space-y-2">
                            <Label>Packets</Label>
                            <Input
                                placeholder="tlshello"
                                value={data.fragment.packets || ""}
                                onChange={(e) => onChange({ ...data, fragment: { ...data.fragment!, packets: e.target.value } })}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Length</Label>
                            <Input
                                placeholder="100-200"
                                value={data.fragment.length || ""}
                                onChange={(e) => onChange({ ...data, fragment: { ...data.fragment!, length: e.target.value } })}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Interval</Label>
                            <Input
                                placeholder="10-20"
                                value={data.fragment.interval || ""}
                                onChange={(e) => onChange({ ...data, fragment: { ...data.fragment!, interval: e.target.value } })}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Max Split</Label>
                            <Input
                                placeholder="5"
                                value={data.fragment.maxSplit || ""}
                                onChange={(e) => onChange({ ...data, fragment: { ...data.fragment!, maxSplit: e.target.value } })}
                            />
                        </div>
                    </div>
                )}
            </div>

            {/* Noise Settings */}
            <div className="space-y-3 border rounded-md p-4 bg-muted/20">
                <div className="flex items-center justify-between">
                    <h4 className="text-sm font-medium">Noise (Anti-Censorship)</h4>
                    <Button type="button" size="sm" variant="outline" onClick={addNoise}>
                        <HiOutlinePlus className="w-4 h-4 mr-1" /> Add Noise
                    </Button>
                </div>
                {(data.noise || []).map((n, index) => (
                    <div key={index} className="flex gap-2 items-end">
                        <div className="flex-1 space-y-1">
                            <Label className="text-xs">Type</Label>
                            <Select value={n.type || "rand"} onValueChange={(v) => updateNoise(index, { type: v })}>
                                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="rand">rand</SelectItem>
                                    <SelectItem value="str">str</SelectItem>
                                    <SelectItem value="base64">base64</SelectItem>
                                    <SelectItem value="hex">hex</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="flex-1 space-y-1">
                            <Label className="text-xs">Packet</Label>
                            <Input value={n.packet || ""} onChange={(e) => updateNoise(index, { packet: e.target.value })} placeholder="content" />
                        </div>
                        <div className="flex-1 space-y-1">
                            <Label className="text-xs">Delay</Label>
                            <Input value={n.delay || ""} onChange={(e) => updateNoise(index, { delay: e.target.value })} placeholder="10-20" />
                        </div>
                        <div className="flex-1 space-y-1">
                            <Label className="text-xs">Apply To</Label>
                            <Select value={n.applyTo || "ip"} onValueChange={(v) => updateNoise(index, { applyTo: v })}>
                                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="ip">IP (all)</SelectItem>
                                    <SelectItem value="ipv4">IPv4</SelectItem>
                                    <SelectItem value="ipv6">IPv6</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <Button type="button" size="icon" variant="ghost" className="text-red-500 h-10 w-10" onClick={() => removeNoise(index)}>
                            <HiOutlineTrash className="w-4 h-4" />
                        </Button>
                    </div>
                ))}
            </div>

            <p className="text-xs text-muted-foreground">
                Freedom outbound sends traffic directly to the destination.
            </p>
        </div>
    )
}
