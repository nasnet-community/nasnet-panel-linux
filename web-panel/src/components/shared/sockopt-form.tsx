import { useState } from "react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import { SockoptSettings } from "@/lib/types"

interface SockoptFormProps {
    data?: SockoptSettings
    onChange: (data: SockoptSettings | null) => void
}

export function SockoptForm({ data, onChange }: SockoptFormProps) {
    // If data is null/undefined, treat as disabled
    const enabled = !!data

    const update = (key: keyof SockoptSettings, value: SockoptSettings[keyof SockoptSettings]) => {
        onChange({ ...data, [key]: value })
    }

    const toggleEnabled = (checked: boolean) => {
        if (checked) {
            onChange({ mark: 0 })
        } else {
            // null (not undefined) so the merge-bind update path nils the
            // backend pointer instead of silently keeping the old sockopt.
            onChange(null)
        }
    }

    if (!enabled) {
        return (
            <div className="flex items-center space-x-2">
                <Switch
                    id="sockopt-enabled"
                    checked={false}
                    onCheckedChange={toggleEnabled}
                />
                <Label htmlFor="sockopt-enabled">Enable Socket Options</Label>
            </div>
        )
    }

    return (
        <div className="space-y-4 border rounded-md p-4 bg-muted/20">
            <div className="flex items-center space-x-2 pb-2 border-b mb-2">
                <Switch
                    id="sockopt-enabled"
                    checked={true}
                    onCheckedChange={toggleEnabled}
                />
                <Label htmlFor="sockopt-enabled">Enable Socket Options</Label>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Mark</Label>
                    <Input
                        type="number"
                        value={data.mark !== undefined ? data.mark : 0}
                        onChange={(e) => update("mark", parseInt(e.target.value) || 0)}
                        placeholder="0"
                    />
                </div>
                <div className="space-y-2">
                    <Label>Interface</Label>
                    <Input
                        value={data.interface || ""}
                        onChange={(e) => update("interface", e.target.value)}
                        placeholder="eth0"
                    />
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>TProxy</Label>
                    <select
                        className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                        value={data.tproxy || "off"}
                        onChange={(e) => update("tproxy", e.target.value)}
                    >
                        <option value="off">Off</option>
                        <option value="redirect">Redirect</option>
                        <option value="tproxy">TProxy</option>
                    </select>
                </div>
                <div className="space-y-2">
                    <Label>Domain Strategy</Label>
                    <select
                        className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                        value={data.domainStrategy || "AsIs"}
                        onChange={(e) => update("domainStrategy", e.target.value)}
                    >
                        <option value="AsIs">AsIs</option>
                        <option value="UseIP">UseIP</option>
                        <option value="UseIPv4">UseIPv4</option>
                        <option value="UseIPv6">UseIPv6</option>
                    </select>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>TCP Keep Alive (s)</Label>
                    <Input
                        type="number"
                        value={data.tcpKeepAliveInterval || ""}
                        onChange={(e) => update("tcpKeepAliveInterval", parseInt(e.target.value) || 0)}
                        placeholder="0"
                    />
                </div>
                <div className="space-y-2">
                    <Label>Dialer Proxy</Label>
                    <Input
                        value={data.dialerProxy || ""}
                        onChange={(e) => update("dialerProxy", e.target.value)}
                        placeholder="tag"
                    />
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2">
                <div className="flex items-center space-x-2">
                    <Switch
                        id="tcpFastOpen"
                        checked={data.tcpFastOpen || false}
                        onCheckedChange={(c) => update("tcpFastOpen", c)}
                    />
                    <Label htmlFor="tcpFastOpen">TCP Fast Open</Label>
                </div>
                <div className="flex items-center space-x-2">
                    <Switch
                        id="tcpMptcp"
                        checked={data.tcpMptcp || false}
                        onCheckedChange={(c) => update("tcpMptcp", c)}
                    />
                    <Label htmlFor="tcpMptcp">TCP MPTCP</Label>
                </div>
                <div className="flex items-center space-x-2">
                    <Switch
                        id="v6Only"
                        checked={data.v6Only || false}
                        onCheckedChange={(c) => update("v6Only", c)}
                    />
                    <Label htmlFor="v6Only">V6 Only</Label>
                </div>
                <div className="flex items-center space-x-2">
                    <Switch
                        id="acceptProxyProtocol"
                        checked={data.acceptProxyProtocol || false}
                        onCheckedChange={(c) => update("acceptProxyProtocol", c)}
                    />
                    <Label htmlFor="acceptProxyProtocol">Accept Proxy Protocol</Label>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>TCP Keep Alive Idle (s)</Label>
                    <Input
                        type="number"
                        value={data.tcpKeepAliveIdle || ""}
                        onChange={(e) => update("tcpKeepAliveIdle", parseInt(e.target.value) || 0)}
                        placeholder="0"
                    />
                </div>
                <div className="space-y-2">
                    <Label>TCP Congestion</Label>
                    <Input
                        value={data.tcpCongestion || ""}
                        onChange={(e) => update("tcpCongestion", e.target.value)}
                        placeholder="bbr"
                    />
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <div className="space-y-2">
                    <Label>TCP Window Clamp</Label>
                    <Input
                        type="number"
                        value={data.tcpWindowClamp || ""}
                        onChange={(e) => update("tcpWindowClamp", parseInt(e.target.value) || 0)}
                        placeholder="0"
                    />
                </div>
                <div className="space-y-2">
                    <Label>TCP User Timeout</Label>
                    <Input
                        type="number"
                        value={data.tcpUserTimeout || ""}
                        onChange={(e) => update("tcpUserTimeout", parseInt(e.target.value) || 0)}
                        placeholder="0"
                    />
                </div>
                <div className="space-y-2">
                    <Label>TCP Max Seg</Label>
                    <Input
                        type="number"
                        value={data.tcpMaxSeg || ""}
                        onChange={(e) => update("tcpMaxSeg", parseInt(e.target.value) || 0)}
                        placeholder="0"
                    />
                </div>
            </div>

            <ExpertSockoptSection data={data} onChange={onChange} />
        </div>
    )
}

// ============ Expert Socket Options ============
function ExpertSockoptSection({ data, onChange }: { data: SockoptSettings; onChange: (d: SockoptSettings | null) => void }) {
    const [showExpert, setShowExpert] = useState(false)
    const [showHappyEyeballs, setShowHappyEyeballs] = useState(!!data.happyEyeballs)

    const update = (key: keyof SockoptSettings, value: SockoptSettings[keyof SockoptSettings]) => {
        onChange({ ...data, [key]: value })
    }

    const addCustomSockopt = () => {
        const current = data.customSockopt || []
        onChange({ ...data, customSockopt: [...current, { level: 0, optName: 0, optValue: "" }] })
    }

    const updateCustomSockopt = (index: number, updates: Partial<{ level: number; optName: number; optValue: unknown }>) => {
        const current = [...(data.customSockopt || [])]
        current[index] = { ...current[index], ...updates }
        onChange({ ...data, customSockopt: current })
    }

    const removeCustomSockopt = (index: number) => {
        const current = [...(data.customSockopt || [])]
        current.splice(index, 1)
        onChange({ ...data, customSockopt: current.length > 0 ? current : undefined })
    }

    return (
        <>
            <button
                type="button"
                className="text-xs text-primary hover:underline"
                onClick={() => setShowExpert(!showExpert)}
            >
                {showExpert ? "Hide" : "Show"} Expert Socket Options
            </button>
            {showExpert && (
                <div className="space-y-4 border rounded-md p-4 bg-background">
                    <div className="flex items-center space-x-2">
                        <Switch
                            id="penetrate"
                            checked={data.penetrate || false}
                            onCheckedChange={(c) => update("penetrate", c)}
                        />
                        <Label htmlFor="penetrate">Penetrate</Label>
                    </div>

                    <div className="space-y-2">
                        <Label>Address Port Strategy</Label>
                        <Select
                            value={data.addressPortStrategy || "none"}
                            onValueChange={(v) => update("addressPortStrategy", v === "none" ? "" : v)}
                        >
                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="none">None</SelectItem>
                                <SelectItem value="srvportonly">srvportonly</SelectItem>
                                <SelectItem value="srvaddressonly">srvaddressonly</SelectItem>
                                <SelectItem value="srvportandaddress">srvportandaddress</SelectItem>
                                <SelectItem value="txtportonly">txtportonly</SelectItem>
                                <SelectItem value="txtaddressonly">txtaddressonly</SelectItem>
                                <SelectItem value="txtportandaddress">txtportandaddress</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="space-y-2">
                        <Label>Trusted X-Forwarded-For</Label>
                        <Textarea
                            placeholder={"10.0.0.0/8\n172.16.0.0/12"}
                            value={(data.trustedXForwardedFor || []).join("\n")}
                            onChange={(e) => update("trustedXForwardedFor", e.target.value.split("\n").filter(Boolean))}
                            rows={3}
                        />
                        <p className="text-xs text-muted-foreground">One CIDR/IP per line</p>
                    </div>

                    {/* Happy Eyeballs */}
                    <div className="space-y-3 border rounded-md p-3 bg-muted/20">
                        <div className="flex items-center justify-between">
                            <Label className="text-sm font-medium">Happy Eyeballs</Label>
                            <Switch
                                checked={showHappyEyeballs}
                                onCheckedChange={(checked) => {
                                    setShowHappyEyeballs(checked)
                                    // null clears the nested pointer through merge-bind; undefined would no-op.
                                    if (!checked) update("happyEyeballs", null)
                                }}
                            />
                        </div>
                        {showHappyEyeballs && (
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                <div className="space-y-2">
                                    <Label>Try Delay (ms)</Label>
                                    <Input
                                        type="number"
                                        placeholder="250"
                                        value={data.happyEyeballs?.tryDelay ?? ""}
                                        onChange={(e) => update("happyEyeballs", { ...(data.happyEyeballs || {}), tryDelay: parseInt(e.target.value) || 0 })}
                                    />
                                </div>
                                <div className="space-y-2">
                                    <Label>Max Concurrency</Label>
                                    <Input
                                        type="number"
                                        placeholder="2"
                                        value={data.happyEyeballs?.maxConcurrency ?? ""}
                                        onChange={(e) => update("happyEyeballs", { ...(data.happyEyeballs || {}), maxConcurrency: parseInt(e.target.value) || 0 })}
                                    />
                                </div>
                            </div>
                        )}
                    </div>

                    {/* Custom Sockopt */}
                    <div className="space-y-3 border rounded-md p-3 bg-muted/20">
                        <div className="flex items-center justify-between">
                            <Label className="text-sm font-medium">Custom Sockopt</Label>
                            <Button type="button" size="sm" variant="outline" onClick={addCustomSockopt}>
                                <HiOutlinePlus className="w-4 h-4 mr-1" /> Add
                            </Button>
                        </div>
                        {(data.customSockopt || []).map((opt, index) => (
                            <div key={index} className="flex gap-2 items-end">
                                <div className="flex-1 space-y-1">
                                    <Label className="text-xs">Level</Label>
                                    <Input
                                        type="number"
                                        value={opt.level ?? ""}
                                        onChange={(e) => updateCustomSockopt(index, { level: parseInt(e.target.value) || 0 })}
                                        placeholder="6"
                                    />
                                </div>
                                <div className="flex-1 space-y-1">
                                    <Label className="text-xs">OptName</Label>
                                    <Input
                                        type="number"
                                        value={opt.optName ?? ""}
                                        onChange={(e) => updateCustomSockopt(index, { optName: parseInt(e.target.value) || 0 })}
                                        placeholder="1"
                                    />
                                </div>
                                <div className="flex-1 space-y-1">
                                    <Label className="text-xs">OptValue</Label>
                                    <Input
                                        value={String(opt.optValue ?? "")}
                                        onChange={(e) => updateCustomSockopt(index, { optValue: e.target.value })}
                                        placeholder="value"
                                    />
                                </div>
                                <Button type="button" size="icon" variant="ghost" className="text-red-500 h-10 w-10" onClick={() => removeCustomSockopt(index)}>
                                    <HiOutlineTrash className="w-4 h-4" />
                                </Button>
                            </div>
                        ))}
                    </div>
                </div>
            )}
        </>
    )
}
