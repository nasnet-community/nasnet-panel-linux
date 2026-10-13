// Reusable controlled field components for the per-protocol settings forms.
// Each is uncoupled from react-hook-form — the dialog bridges the whole settings
// slot to RHF, and these edit individual keys within it.
import { useState } from "react"
import { X, Plus } from "lucide-react"
import { cn } from "@/lib/utils"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"

export function SettingRow({
    label,
    htmlFor,
    description,
    error,
    children,
    className,
}: {
    label: string
    htmlFor?: string
    description?: string
    error?: string
    children: React.ReactNode
    className?: string
}) {
    return (
        <div className={cn("space-y-1.5", className)}>
            <Label htmlFor={htmlFor} className={cn(error && "text-destructive")}>
                {label}
            </Label>
            {children}
            {error ? (
                <p className="text-xs text-destructive">{error}</p>
            ) : description ? (
                <p className="text-xs text-muted-foreground">{description}</p>
            ) : null}
        </div>
    )
}

export function TextSetting({
    id,
    label,
    value,
    onChange,
    placeholder,
    description,
    error,
    mono,
    readOnly,
}: {
    id?: string
    label: string
    value: string
    onChange: (v: string) => void
    placeholder?: string
    description?: string
    error?: string
    mono?: boolean
    readOnly?: boolean
}) {
    return (
        <SettingRow label={label} htmlFor={id} description={description} error={error}>
            <Input
                id={id}
                value={value}
                onChange={(e) => onChange(e.target.value)}
                placeholder={placeholder}
                readOnly={readOnly}
                className={cn(mono && "font-mono", readOnly && "bg-muted/50 text-muted-foreground")}
            />
        </SettingRow>
    )
}

export function NumberSetting({
    id,
    label,
    value,
    onChange,
    min,
    max,
    placeholder,
    zeroLabel,
    description,
    error,
}: {
    id?: string
    label: string
    value: number
    onChange: (v: number) => void
    min?: number
    max?: number
    placeholder?: string
    /** 0 shows as an empty box with this placeholder, and an empty box saves 0 */
    zeroLabel?: string
    description?: string
    error?: string
}) {
    const blank = !Number.isFinite(value) || (zeroLabel !== undefined && value === 0)
    return (
        <SettingRow label={label} htmlFor={id} description={description} error={error}>
            <Input
                id={id}
                type="number"
                min={min}
                max={max}
                value={blank ? "" : value}
                placeholder={zeroLabel ?? placeholder}
                onChange={(e) => onChange(e.target.value === "" ? 0 : e.target.valueAsNumber)}
            />
        </SettingRow>
    )
}

export function SwitchSetting({
    label,
    checked,
    onChange,
    description,
    disabled,
}: {
    label: string
    checked: boolean
    onChange: (v: boolean) => void
    description?: string
    disabled?: boolean
}) {
    return (
        <div className="flex items-center justify-between gap-4 rounded-lg border p-3">
            <div className="space-y-0.5">
                <Label className={cn(disabled && "text-muted-foreground")}>{label}</Label>
                {description && <p className="text-xs text-muted-foreground">{description}</p>}
            </div>
            <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} />
        </div>
    )
}

export function SelectSetting({
    label,
    value,
    onChange,
    options,
    description,
    error,
}: {
    label: string
    value: string
    onChange: (v: string) => void
    options: { value: string; label: string }[]
    description?: string
    error?: string
}) {
    return (
        <SettingRow label={label} description={description} error={error}>
            <Select value={value} onValueChange={onChange}>
                <SelectTrigger>
                    <SelectValue />
                </SelectTrigger>
                <SelectContent>
                    {options.map((o) => (
                        <SelectItem key={o.value} value={o.value}>
                            {o.label}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>
        </SettingRow>
    )
}

/** Editable list of short string values (DNS servers, ciphers, routes …). */
export function ListSetting({
    label,
    values,
    onChange,
    placeholder,
    description,
    mono = true,
}: {
    label: string
    values: string[]
    onChange: (v: string[]) => void
    placeholder?: string
    description?: string
    mono?: boolean
}) {
    const [draft, setDraft] = useState("")
    const add = () => {
        const v = draft.trim()
        if (!v) return
        onChange([...values, v])
        setDraft("")
    }
    return (
        <SettingRow label={label} description={description}>
            <div className="flex gap-2">
                <Input
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") {
                            e.preventDefault()
                            add()
                        }
                    }}
                    placeholder={placeholder}
                    className={cn(mono && "font-mono")}
                />
                <button
                    type="button"
                    onClick={add}
                    className="flex size-9 shrink-0 items-center justify-center rounded-md border hover:bg-accent"
                    aria-label={`Add ${label}`}
                >
                    <Plus className="size-4" />
                </button>
            </div>
            {values.length > 0 && (
                <div className="flex flex-wrap gap-1.5 pt-1">
                    {values.map((v, i) => (
                        <span
                            key={`${v}-${i}`}
                            className={cn(
                                "inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs",
                                mono && "font-mono",
                            )}
                        >
                            {v}
                            <button
                                type="button"
                                onClick={() => onChange(values.filter((_, idx) => idx !== i))}
                                className="text-muted-foreground hover:text-destructive"
                                aria-label={`Remove ${v}`}
                            >
                                <X className="size-3" />
                            </button>
                        </span>
                    ))}
                </div>
            )}
        </SettingRow>
    )
}
