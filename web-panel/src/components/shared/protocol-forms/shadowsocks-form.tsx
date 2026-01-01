import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { SHADOWSOCKS_METHODS } from "@/lib/types"
import type { ShadowsocksSettings } from "@/lib/types"

interface ShadowsocksFormProps {
    settings?: ShadowsocksSettings
    onChange: (settings: ShadowsocksSettings) => void
    isOutbound?: boolean
}

export function ShadowsocksForm({ settings, onChange, isOutbound }: ShadowsocksFormProps) {
    const data = settings || { method: "2022-blake3-aes-128-gcm", network: "tcp,udp" }

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Encryption Method</Label>
                    <Select
                        value={data.method}
                        onValueChange={(value) => onChange({ ...data, method: value })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {SHADOWSOCKS_METHODS.map((m) => (
                                <SelectItem key={m.value} value={m.value}>
                                    {m.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>
                <div className="space-y-2">
                    <Label>Network</Label>
                    <Select
                        value={data.network || "tcp,udp"}
                        onValueChange={(value) => onChange({ ...data, network: value })}
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
            </div>
            <div className="space-y-2">
                <Label>Password / Key</Label>
                <Input
                    type="password"
                    placeholder={
                        (data.method || "").startsWith("2022-blake3-")
                            ? "Required — base64 key (16 bytes for aes-128, 32 for aes-256/chacha)"
                            : "Server password (required)"
                    }
                    value={data.password || ""}
                    onChange={(e) => onChange({ ...data, password: e.target.value })}
                />
                {(data.method || "").startsWith("2022-blake3-") && (
                    <p className="text-xs text-muted-foreground">
                        2022 methods need a base64-encoded PSK of exactly{" "}
                        {(data.method || "").includes("aes-128") ? "16" : "32"} bytes.
                    </p>
                )}
            </div>
            {isOutbound && (
                <div className="flex items-center justify-between">
                    <div>
                        <Label>UDP over TCP</Label>
                        <p className="text-xs text-muted-foreground">Encapsulate UDP in TCP</p>
                    </div>
                    <Switch
                        checked={data.uot ?? false}
                        onCheckedChange={(checked) => onChange({ ...data, uot: checked })}
                    />
                </div>
            )}
            {isOutbound && data.uot && (
                <div className="space-y-2">
                    <Label>UoT Version</Label>
                    <Input
                        type="number"
                        placeholder="2"
                        value={data.uotVersion ?? ""}
                        onChange={(e) => onChange({ ...data, uotVersion: parseInt(e.target.value) || 0 })}
                    />
                </div>
            )}
        </div>
    )
}
