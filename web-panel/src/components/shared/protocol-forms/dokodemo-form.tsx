import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import { DokodemoSettings } from "@/lib/types"

interface DokodemoFormProps {
    settings?: DokodemoSettings
    onChange: (settings: DokodemoSettings) => void
}

export function DokodemoForm({ settings, onChange }: DokodemoFormProps) {
    const data = settings || { networks: "tcp,udp" }

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Destination Address</Label>
                    <Input
                        placeholder="127.0.0.1"
                        value={data.address || ""}
                        onChange={(e) => onChange({ ...data, address: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">Forward traffic to this address</p>
                </div>
                <div className="space-y-2">
                    <Label>Destination Port</Label>
                    <Input
                        type="number"
                        placeholder="80"
                        value={data.port ?? ""}
                        onChange={(e) => onChange({ ...data, port: parseInt(e.target.value) || 0 })}
                    />
                    <p className="text-xs text-muted-foreground">Forward traffic to this port</p>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Networks</Label>
                    <Select
                        value={data.networks || "tcp,udp"}
                        onValueChange={(value) => onChange({ ...data, networks: value })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="tcp">TCP</SelectItem>
                            <SelectItem value="udp">UDP</SelectItem>
                            <SelectItem value="tcp,udp">TCP + UDP</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                <div className="space-y-2">
                    <Label>User Level</Label>
                    <Input
                        type="number"
                        placeholder="0"
                        value={data.userLevel ?? ""}
                        onChange={(e) => onChange({ ...data, userLevel: parseInt(e.target.value) || 0 })}
                    />
                </div>
            </div>

            <div className="flex items-center justify-between">
                <div>
                    <Label>Follow Redirect</Label>
                    <p className="text-xs text-muted-foreground">Use system redirect (for tproxy)</p>
                </div>
                <Switch
                    checked={data.followRedirect ?? false}
                    onCheckedChange={(checked) => onChange({ ...data, followRedirect: checked })}
                />
            </div>

            <div className="space-y-3 border rounded-md p-4 bg-muted/20">
                <div className="flex items-center justify-between">
                    <Label className="text-sm font-medium">Port Map</Label>
                    <Button type="button" size="sm" variant="outline" onClick={() => {
                        const pm = { ...(data.portMap || {}), "": "" }
                        onChange({ ...data, portMap: pm })
                    }}>
                        <HiOutlinePlus className="w-4 h-4 mr-1" /> Add
                    </Button>
                </div>
                {Object.entries(data.portMap || {}).map(([key, val], index) => (
                    <div key={index} className="flex gap-2 items-center">
                        <Input
                            placeholder="Source port"
                            value={key}
                            onChange={(e) => {
                                const pm = { ...(data.portMap || {}) }
                                delete pm[key]
                                pm[e.target.value] = val
                                onChange({ ...data, portMap: pm })
                            }}
                            className="flex-1"
                        />
                        <Input
                            placeholder="Dest port"
                            value={String(val || "")}
                            onChange={(e) => {
                                const pm = { ...(data.portMap || {}) }
                                pm[key] = e.target.value
                                onChange({ ...data, portMap: pm })
                            }}
                            className="flex-1"
                        />
                        <Button type="button" size="icon" variant="ghost" className="text-red-500 h-8 w-8" onClick={() => {
                            const pm = { ...(data.portMap || {}) }
                            delete pm[key]
                            onChange({ ...data, portMap: Object.keys(pm).length > 0 ? pm : undefined })
                        }}>
                            <HiOutlineTrash className="w-4 h-4" />
                        </Button>
                    </div>
                ))}
            </div>
        </div>
    )
}
