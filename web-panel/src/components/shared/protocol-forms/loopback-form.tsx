import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import type { LoopbackSettings } from "@/lib/types"

interface LoopbackFormProps {
    settings?: LoopbackSettings
    onChange: (s: LoopbackSettings) => void
}

export function LoopbackForm({ settings, onChange }: LoopbackFormProps) {
    const data = settings || {}

    return (
        <div className="space-y-4">
            <div className="space-y-2">
                <Label>Inbound Tag *</Label>
                <Input
                    placeholder="inbound-tag"
                    value={data.inboundTag || ""}
                    onChange={(e) => onChange({ ...data, inboundTag: e.target.value })}
                />
                <p className="text-xs text-muted-foreground">
                    Route traffic back to the specified inbound for re-processing.
                </p>
            </div>
        </div>
    )
}
