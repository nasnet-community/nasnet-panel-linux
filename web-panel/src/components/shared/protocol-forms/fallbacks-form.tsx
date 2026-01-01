import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import { Fallback } from "@/lib/types"

interface FallbacksFormProps {
    fallbacks?: Fallback[]
    onChange: (fallbacks: Fallback[]) => void
}

export function FallbacksForm({ fallbacks = [], onChange }: FallbacksFormProps) {
    const addFallback = () => {
        onChange([...fallbacks, { name: "", dest: "", xver: 0 }])
    }

    const updateFallback = (index: number, updates: Partial<Fallback>) => {
        const updated = [...fallbacks]
        updated[index] = { ...updated[index], ...updates }
        onChange(updated)
    }

    const removeFallback = (index: number) => {
        const updated = [...fallbacks]
        updated.splice(index, 1)
        onChange(updated)
    }

    return (
        <div className="space-y-3 border rounded-md p-4 bg-muted/20">
            <div className="flex items-center justify-between">
                <h4 className="text-sm font-medium">Fallbacks</h4>
                <Button type="button" size="sm" variant="outline" onClick={addFallback}>
                    <HiOutlinePlus className="w-4 h-4 mr-1" /> Add Fallback
                </Button>
            </div>
            {fallbacks.length === 0 && (
                <p className="text-xs text-muted-foreground">No fallbacks configured. Add one to forward unmatched connections.</p>
            )}
            {fallbacks.map((fb, index) => (
                <div key={index} className="space-y-3 border rounded-md p-3 bg-background">
                    <div className="flex items-center justify-between">
                        <span className="text-xs font-medium text-muted-foreground">Fallback #{index + 1}</span>
                        <Button
                            type="button"
                            size="icon"
                            variant="ghost"
                            className="text-red-500 h-8 w-8"
                            onClick={() => removeFallback(index)}
                        >
                            <HiOutlineTrash className="w-4 h-4" />
                        </Button>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                        <div className="space-y-1">
                            <Label className="text-xs">Name</Label>
                            <Input
                                placeholder="Fallback name"
                                value={fb.name || ""}
                                onChange={(e) => updateFallback(index, { name: e.target.value })}
                                className="text-sm"
                            />
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs">Type</Label>
                            <Input
                                value={fb.type || ""}
                                onChange={(e) => updateFallback(index, { type: e.target.value })}
                                placeholder="tcp/unix"
                                className="text-sm"
                            />
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs">Dest *</Label>
                            <Input
                                placeholder="127.0.0.1:8080 or 8080"
                                value={String(fb.dest ?? "")}
                                onChange={(e) => {
                                    const val = e.target.value
                                    const num = parseInt(val)
                                    updateFallback(index, { dest: !isNaN(num) && String(num) === val ? num : val })
                                }}
                                className="text-sm"
                            />
                        </div>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                        <div className="space-y-1">
                            <Label className="text-xs">ALPN</Label>
                            <Input
                                placeholder="h2"
                                value={fb.alpn || ""}
                                onChange={(e) => updateFallback(index, { alpn: e.target.value })}
                                className="text-sm"
                            />
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs">Path</Label>
                            <Input
                                placeholder="/path"
                                value={fb.path || ""}
                                onChange={(e) => updateFallback(index, { path: e.target.value })}
                                className="text-sm"
                            />
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs">Xver</Label>
                            <Select
                                value={String(fb.xver || 0)}
                                onValueChange={(v) => updateFallback(index, { xver: parseInt(v) })}
                            >
                                <SelectTrigger className="w-full text-sm">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="0">0 (None)</SelectItem>
                                    <SelectItem value="1">1 (v1)</SelectItem>
                                    <SelectItem value="2">2 (v2)</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                </div>
            ))}
        </div>
    )
}
