import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlineRefresh } from "react-icons/hi"
import type { VMessSettings } from "@/lib/types"
import { generateUUID } from "@/lib/utils"

interface VMessOutboundFormProps {
    settings?: VMessSettings
    onChange: (s: VMessSettings) => void
}

export function VMessOutboundForm({ settings, onChange }: VMessOutboundFormProps) {
    const data = settings || { security: "auto" }

    return (
        <div className="space-y-4">
            <div className="space-y-2">
                <Label>UUID *</Label>
                <div className="flex gap-2">
                    <Input
                        placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
                        value={data.uuid || ""}
                        onChange={(e) => onChange({ ...data, uuid: e.target.value })}
                        className="flex-1 min-w-0 font-mono text-sm"
                    />
                    <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        onClick={() => onChange({ ...data, uuid: generateUUID() })}
                        title="Generate UUID"
                    >
                        <HiOutlineRefresh className="w-4 h-4" />
                    </Button>
                </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Security</Label>
                    <Select
                        value={data.security || "auto"}
                        onValueChange={(value) => onChange({ ...data, security: value })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="auto">Auto</SelectItem>
                            <SelectItem value="aes-128-gcm">AES-128-GCM</SelectItem>
                            <SelectItem value="chacha20-poly1305">ChaCha20-Poly1305</SelectItem>
                            <SelectItem value="none">None</SelectItem>
                            <SelectItem value="zero">Zero</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                <div className="space-y-2">
                    <Label>Alter ID</Label>
                    <Input
                        type="number"
                        placeholder="0"
                        value={data.alterId ?? 0}
                        onChange={(e) => onChange({ ...data, alterId: parseInt(e.target.value) || 0 })}
                    />
                    <p className="text-xs text-muted-foreground">Legacy, keep at 0</p>
                </div>
            </div>
        </div>
    )
}
