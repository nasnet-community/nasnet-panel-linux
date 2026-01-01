import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { HiOutlinePlus, HiX } from "react-icons/hi"

export function ArrayInput({ placeholder, onAdd }: { placeholder: string; onAdd: (value: string) => void }) {
    const [value, setValue] = useState("")
    const [bulkMode, setBulkMode] = useState(false)
    const [bulkText, setBulkText] = useState("")

    const handleAdd = () => {
        if (value.trim()) {
            onAdd(value)
            setValue("")
        }
    }

    const handleBulkAdd = () => {
        const lines = bulkText.trim().split("\n").filter(Boolean)
        lines.forEach((line) => onAdd(line.trim()))
        setBulkText("")
        setBulkMode(false)
    }

    if (bulkMode) {
        return (
            <div className="space-y-2">
                <div className="flex items-center justify-end">
                    <Button variant="ghost" size="sm" onClick={() => setBulkMode(false)}>Single</Button>
                </div>
                <Textarea
                    placeholder={`One per line...\n${placeholder}`}
                    value={bulkText}
                    onChange={(e) => setBulkText(e.target.value)}
                    rows={4}
                    className="font-mono text-sm"
                />
                <Button size="sm" onClick={handleBulkAdd} disabled={!bulkText.trim()}>
                    Add All ({bulkText.trim().split("\n").filter(Boolean).length})
                </Button>
            </div>
        )
    }

    return (
        <div className="flex gap-2">
            <Input
                placeholder={placeholder}
                value={value}
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), handleAdd())}
                className="flex-1"
            />
            <Button type="button" size="icon" variant="outline" onClick={handleAdd}>
                <HiOutlinePlus className="w-4 h-4" />
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setBulkMode(true)}>Bulk</Button>
        </div>
    )
}

export interface InboundTagSuggestion {
    tag: string
    label: string
}

// Tag input for routing rules: suggests the node's inbounds as chips but
// still takes free-text tags (for synthetic tags like the DNS client's dns.tag).
export function InboundTagInput({
    suggestions,
    selected,
    onAdd,
    onRemove,
}: {
    suggestions: InboundTagSuggestion[]
    selected: string[]
    onAdd: (value: string) => void
    onRemove: (index: number) => void
}) {
    const available = suggestions.filter((s) => !selected.includes(s.tag))
    return (
        <div className="space-y-2">
            <ArrayInput placeholder="Select or type a tag (e.g. dns-in)" onAdd={onAdd} />
            {available.length > 0 && (
                <div className="flex flex-wrap gap-1">
                    {available.map((s) => (
                        <Button
                            key={s.tag}
                            type="button"
                            variant="outline"
                            size="sm"
                            className="text-xs h-9 md:h-7"
                            onClick={() => onAdd(s.tag)}
                        >
                            + {s.label}
                        </Button>
                    ))}
                </div>
            )}
            <TagList items={selected} onRemove={onRemove} />
        </div>
    )
}

export function TagList({ items, onRemove }: { items: string[]; onRemove: (index: number) => void }) {
    if (items.length === 0) return null
    return (
        <div className="flex flex-wrap gap-1 mt-2">
            {items.map((item, i) => (
                <Badge key={i} variant="secondary" className="gap-1">
                    {item}
                    <button onClick={() => onRemove(i)} className="ml-1 p-1 -mr-1 rounded hover:bg-destructive/20 hover:text-red-500">
                        <HiX className="w-3 h-3" />
                    </button>
                </Badge>
            ))}
        </div>
    )
}
