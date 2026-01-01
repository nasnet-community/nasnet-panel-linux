import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { DNSOutboundSettings } from "@/lib/types"

interface DNSOutboundFormProps {
    settings?: DNSOutboundSettings
    onChange: (s: DNSOutboundSettings) => void
}

export function DNSOutboundForm({ settings, onChange }: DNSOutboundFormProps) {
    const data = settings || {}

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Network</Label>
                    <Select
                        value={data.network || ""}
                        onValueChange={(value) => onChange({ ...data, network: value })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue placeholder="Default" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="tcp">TCP</SelectItem>
                            <SelectItem value="udp">UDP</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                <div className="space-y-2">
                    <Label>Non-IP Query</Label>
                    <Select
                        value={data.nonIPQuery || ""}
                        onValueChange={(value) => onChange({ ...data, nonIPQuery: value })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue placeholder="Default" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="drop">Drop</SelectItem>
                            <SelectItem value="skip">Skip</SelectItem>
                            <SelectItem value="reject">Reject</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <div className="space-y-2">
                    <Label>DNS Address</Label>
                    <Input
                        placeholder="8.8.8.8"
                        value={data.address || ""}
                        onChange={(e) => onChange({ ...data, address: e.target.value })}
                    />
                </div>
                <div className="space-y-2">
                    <Label>DNS Port</Label>
                    <Input
                        type="number"
                        placeholder="53"
                        value={data.port ?? ""}
                        onChange={(e) => onChange({ ...data, port: parseInt(e.target.value) || 0 })}
                    />
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
            <div className="space-y-2">
                <Label>Block Record Types</Label>
                <Input
                    placeholder="28 (AAAA), 65 (HTTPS) — comma-separated type numbers"
                    value={(data.blockTypes || []).join(", ")}
                    onChange={(e) => {
                        const types = e.target.value
                            .split(",")
                            .map((s) => parseInt(s.trim()))
                            .filter((n) => !isNaN(n) && n > 0)
                        onChange({ ...data, blockTypes: types.length > 0 ? types : undefined })
                    }}
                />
                <p className="text-xs text-muted-foreground">
                    Block specific DNS record types. Common: 28 (AAAA/IPv6), 65 (HTTPS/SVCB).
                </p>
            </div>
            <p className="text-xs text-muted-foreground">
                DNS outbound intercepts DNS queries and responds via the configured DNS server.
            </p>
        </div>
    )
}
