import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { BlackholeSettings } from "@/lib/types"

interface BlackholeFormProps {
    settings?: BlackholeSettings
    onChange: (s: BlackholeSettings) => void
}

export function BlackholeForm({ settings, onChange }: BlackholeFormProps) {
    const data = settings || { responseType: "none" }

    return (
        <div className="space-y-4">
            <div className="space-y-2">
                <Label>Response Type</Label>
                <Select
                    value={data.responseType || "none"}
                    onValueChange={(value) => onChange({ ...data, responseType: value })}
                >
                    <SelectTrigger className="w-full">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="none">None (Silent Drop)</SelectItem>
                        <SelectItem value="http">HTTP 403 Response</SelectItem>
                    </SelectContent>
                </Select>
            </div>
            <p className="text-xs text-muted-foreground">
                Blackhole outbound blocks all traffic. Use for ad-blocking or denying specific routes.
            </p>
        </div>
    )
}
