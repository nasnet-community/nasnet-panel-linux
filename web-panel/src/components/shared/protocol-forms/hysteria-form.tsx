import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import type { HysteriaSettings } from "@/lib/types"

interface HysteriaFormProps {
    settings?: HysteriaSettings
    onChange: (s: HysteriaSettings) => void
    isOutbound?: boolean
}

export function HysteriaForm({ settings, onChange, isOutbound = false }: HysteriaFormProps) {
    const data = settings || {}

    return (
        <div className="space-y-4">
            {isOutbound && (
                <div className="space-y-2">
                    <Label>Auth</Label>
                    <Input
                        placeholder="Authentication string"
                        value={data.auth || ""}
                        onChange={(e) => onChange({ ...data, auth: e.target.value })}
                    />
                </div>
            )}
            {!isOutbound && (
                <p className="text-sm text-muted-foreground">
                    Hysteria2 client authentication is managed through the user/subscription system.
                </p>
            )}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {isOutbound && (
                    <div className="space-y-2">
                        <Label>Congestion</Label>
                        <Input
                            placeholder="bbr (optional)"
                            value={data.congestion || ""}
                            onChange={(e) => onChange({ ...data, congestion: e.target.value })}
                        />
                    </div>
                )}
                <div className="space-y-2">
                    <Label>UDP Idle Timeout</Label>
                    <Input
                        type="number"
                        placeholder="60"
                        value={data.udpIdleTimeout ?? ""}
                        onChange={(e) => onChange({ ...data, udpIdleTimeout: parseInt(e.target.value) || 0 })}
                    />
                    <p className="text-xs text-muted-foreground">2-600 seconds</p>
                </div>
            </div>
            {isOutbound && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div className="space-y-2">
                        <Label>Upload Bandwidth</Label>
                        <Input
                            placeholder="100 mbps"
                            value={data.up || ""}
                            onChange={(e) => onChange({ ...data, up: e.target.value })}
                        />
                    </div>
                    <div className="space-y-2">
                        <Label>Download Bandwidth</Label>
                        <Input
                            placeholder="200 mbps"
                            value={data.down || ""}
                            onChange={(e) => onChange({ ...data, down: e.target.value })}
                        />
                    </div>
                </div>
            )}
            {!isOutbound && (data.congestion || data.up || data.down) && (
                <p className="text-xs text-muted-foreground">
                    Congestion / Up / Down are outbound-only — use Advanced &rarr; FinalMask for inbound.
                </p>
            )}
        </div>
    )
}
