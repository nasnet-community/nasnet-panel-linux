import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import type { TrojanSettings } from "@/lib/types"

interface TrojanOutboundFormProps {
    settings?: TrojanSettings
    onChange: (s: TrojanSettings) => void
}

export function TrojanOutboundForm({ settings, onChange }: TrojanOutboundFormProps) {
    const data = settings || {}

    return (
        <div className="space-y-4">
            <div className="space-y-2">
                <Label>Password *</Label>
                <Input
                    type="password"
                    placeholder="Trojan password"
                    value={data.password || ""}
                    onChange={(e) => onChange({ ...data, password: e.target.value })}
                />
            </div>
        </div>
    )
}
