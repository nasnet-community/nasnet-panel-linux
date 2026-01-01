import { useState } from "react"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import { HiOutlineInformationCircle, HiOutlineGlobeAlt, HiOutlineClipboardCopy, HiOutlineCheck, HiOutlineStatusOnline, HiOutlineExclamationCircle } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { fieldOverrides } from "./settings-constants"
import { useSettingValidation } from "./use-setting-validation"
import type { Setting } from "@/lib/domain/setting"
import type { RetentionStat } from "@/lib/api/settings"
import { cn } from "@/lib/utils"

interface SettingFieldProps {
    setting: Setting
    isModified: boolean
    isHighlighted: boolean
    onChange: (key: string, value: string | boolean) => void
    /** When set, appended as a muted caption under the field — "12.3K rows ·
     *  oldest Apr 10". Only passed for retention_* keys by the data-category
     *  parent; the field itself is agnostic. */
    retentionStat?: RetentionStat | null
}

// formatRowCount: "1.2K" / "3.4M" / plain — keeps the retention caption
// inline on narrow panels.
function formatRowCount(n: number): string {
    if (n < 1000) return n.toLocaleString()
    if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0).replace(/\.0$/, "")}K`
    return `${(n / 1_000_000).toFixed(n < 10_000_000 ? 1 : 0).replace(/\.0$/, "")}M`
}

function formatOldestDate(iso: string | null | undefined): string {
    if (!iso) return ""
    try {
        const d = new Date(iso)
        if (isNaN(d.getTime())) return ""
        return d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })
    } catch {
        return ""
    }
}

function URLTestButton({ url }: { url: string }) {
    const [status, setStatus] = useState<"idle" | "loading" | "ok" | "error">("idle")
    const handleTest = async () => {
        if (!url) return
        setStatus("loading")
        try {
            const resp = await fetch(url, { method: "HEAD", mode: "no-cors" })
            // no-cors returns opaque response, so we consider any response as ok
            setStatus(resp.type === "opaque" || resp.ok ? "ok" : "error")
        } catch {
            setStatus("error")
        }
        setTimeout(() => setStatus("idle"), 3000)
    }
    return (
        <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" onClick={handleTest} disabled={!url || status === "loading"}>
            {status === "loading" && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            {status === "ok" && <HiOutlineStatusOnline className="w-3.5 h-3.5 text-green-500" />}
            {status === "error" && <HiOutlineExclamationCircle className="w-3.5 h-3.5 text-red-500" />}
            {status === "idle" && <HiOutlineStatusOnline className="w-3.5 h-3.5" />}
        </Button>
    )
}

function CopyButton({ text }: { text: string }) {
    const [copied, setCopied] = useState(false)
    const handleCopy = async () => {
        await navigator.clipboard.writeText(text)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
    }
    return (
        <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" onClick={handleCopy}>
            {copied ? (
                <HiOutlineCheck className="w-3.5 h-3.5 text-green-500" />
            ) : (
                <HiOutlineClipboardCopy className="w-3.5 h-3.5" />
            )}
        </Button>
    )
}

export function SettingField({ setting, isModified, isHighlighted, onChange, retentionStat }: SettingFieldProps) {
    const override = fieldOverrides[setting.key]
    const { error, onBlur, onChange: onValidationChange } = useSettingValidation({
        type: setting.type,
        key: setting.key,
        label: setting.label,
    })
    const handleChange = (value: string | boolean) => {
        onChange(setting.key, value)
        if (typeof value === "string") onValidationChange(value)
    }

    const handleBlur = () => {
        if (setting.type !== "bool") onBlur(setting.value)
    }

    const isTextarea = override?.type === "textarea"
    const isUrl = override?.type === "url"
    const isPort = override?.type === "port"
    const isDuration = override?.type === "duration"
    const isPercentage = override?.type === "percentage"
    const isCryptoAddress = override?.type === "crypto_address"

    const labelContent = (
        <div className="flex items-center gap-1.5 min-w-0 flex-wrap">
            <Label htmlFor={setting.key} className="text-sm font-medium whitespace-nowrap">
                {setting.label || setting.key}
            </Label>
            {isModified && <span className="w-1.5 h-1.5 rounded-full bg-amber-500 shrink-0" />}
            {setting.requires_restart && (
                <Badge variant="outline" className="text-[10px] px-1.5 py-0 border-orange-500/50 text-orange-500 shrink-0">
                    Restart required
                </Badge>
            )}
            {setting.description && (
                <TooltipProvider delayDuration={200}>
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <button type="button" className="text-muted-foreground hover:text-foreground shrink-0">
                                <HiOutlineInformationCircle className="w-3.5 h-3.5" />
                            </button>
                        </TooltipTrigger>
                        <TooltipContent side="top" className="max-w-xs">
                            <p className="text-xs">{setting.description}</p>
                        </TooltipContent>
                    </Tooltip>
                </TooltipProvider>
            )}
        </div>
    )

    const fieldClasses = cn(
        "p-3 rounded-lg border transition-all min-h-[48px]",
        isModified ? "bg-amber-500/5 border-amber-500/20" : "bg-card/50 hover:bg-muted/20",
        isHighlighted && "ring-2 ring-primary/40",
        error && "border-destructive/50"
    )

    // Bool: label left, switch right
    if (setting.type === "bool") {
        return (
            <div className={fieldClasses}>
                <div className="flex items-center justify-between">
                    {labelContent}
                    <Switch
                        id={setting.key}
                        checked={setting.value === "true"}
                        onCheckedChange={(checked: boolean) => handleChange(checked)}
                    />
                </div>
            </div>
        )
    }

    // Textarea: label on top, full-width textarea below
    if (isTextarea) {
        return (
            <div className={fieldClasses}>
                <div className="space-y-2">
                    {labelContent}
                    <Textarea
                        id={setting.key}
                        value={setting.value}
                        onChange={(e) => handleChange(e.target.value)}
                        onBlur={handleBlur}
                        className="min-h-[80px]"
                        rows={3}
                    />
                    {error && <p className="text-xs text-destructive">{error}</p>}
                </div>
            </div>
        )
    }

    // Select
    if (override?.type === "select") {
        return (
            <div className={fieldClasses}>
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 sm:gap-4">
                    {labelContent}
                    <Select
                        value={setting.value}
                        onValueChange={(val) => handleChange(val)}
                    >
                        <SelectTrigger className="w-full sm:w-52">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {override.options?.map(opt => (
                                <SelectItem key={opt} value={opt}>
                                    {opt}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>
                {error && <p className="text-xs text-destructive mt-1">{error}</p>}
            </div>
        )
    }

    // URL field: globe icon prefix + optional test button
    if (isUrl) {
        const testableKeys = ["app_base_url", "sub_panel_url"]
        const showTest = testableKeys.includes(setting.key)
        return (
            <div className={fieldClasses}>
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 sm:gap-4">
                    {labelContent}
                    <div className="flex items-center gap-1.5 w-full sm:w-auto">
                        <div className="relative flex-1 sm:w-60">
                            <HiOutlineGlobeAlt className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                            <Input
                                id={setting.key}
                                type="url"
                                value={setting.value}
                                onChange={(e) => handleChange(e.target.value)}
                                onBlur={handleBlur}
                                className="pl-8"
                                placeholder={override?.placeholder || "https://"}
                            />
                        </div>
                        {showTest && <URLTestButton url={setting.value} />}
                    </div>
                </div>
                {error && <p className="text-xs text-destructive mt-1">{error}</p>}
            </div>
        )
    }

    // Port field: clamped 1-65535
    if (isPort) {
        return (
            <div className={fieldClasses}>
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 sm:gap-4">
                    {labelContent}
                    <Input
                        id={setting.key}
                        type="number"
                        min={1}
                        max={65535}
                        value={setting.value}
                        onChange={(e) => handleChange(e.target.value)}
                        onBlur={handleBlur}
                        className="w-full sm:w-32"
                    />
                </div>
                {error && <p className="text-xs text-destructive mt-1">{error}</p>}
            </div>
        )
    }

    // Duration field. Retention keys treat 0 as "keep forever"; badge
    // swaps to a primary pill so 0 doesn't look unconfigured.
    if (isDuration) {
        const isRetentionKey = setting.key.startsWith("retention_")
        const isForever = isRetentionKey && setting.value === "0"
        // Render the per-table stats caption only when the parent supplied
        // stats AND the key is retention_* (the parent already filters, but
        // we double-check so the field stays safe to reuse elsewhere).
        const showStat = isRetentionKey && retentionStat && retentionStat.rows > 0
        return (
            <div className={fieldClasses}>
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 sm:gap-4">
                    {labelContent}
                    <div className="flex items-center gap-2 w-full sm:w-auto">
                        <Input
                            id={setting.key}
                            type="number"
                            min={0}
                            value={setting.value}
                            onChange={(e) => handleChange(e.target.value)}
                            onBlur={handleBlur}
                            className={cn(
                                "w-full sm:w-24",
                                isForever && "text-muted-foreground italic",
                            )}
                        />
                        {isForever ? (
                            <Badge
                                variant="outline"
                                className="text-[10px] font-medium whitespace-nowrap shrink-0 border-primary/40 bg-primary/10 text-primary uppercase tracking-wider"
                                title="Setting is 0 — rows will never be auto-cleaned. Enter a positive number of days to enable cleanup."
                            >
                                keep forever
                            </Badge>
                        ) : (
                            <Badge variant="secondary" className="text-xs whitespace-nowrap shrink-0">
                                {override?.unit || ""}
                            </Badge>
                        )}
                    </div>
                </div>
                {showStat && (
                    <p className="text-[11px] text-muted-foreground/80 mt-1.5 font-mono tabular-nums">
                        <span className="text-foreground/70">{formatRowCount(retentionStat!.rows)}</span>
                        <span className="opacity-60"> rows</span>
                        {retentionStat!.oldest_at && (
                            <>
                                <span className="opacity-40 mx-1.5">·</span>
                                <span className="opacity-60">oldest </span>
                                <span className="text-foreground/70">{formatOldestDate(retentionStat!.oldest_at)}</span>
                            </>
                        )}
                        <span className="opacity-40 mx-1.5">·</span>
                        <span className="font-sans text-muted-foreground/60">{retentionStat!.table}</span>
                    </p>
                )}
                {error && <p className="text-xs text-destructive mt-1">{error}</p>}
            </div>
        )
    }

    // Percentage field: number + % suffix
    if (isPercentage) {
        return (
            <div className={fieldClasses}>
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 sm:gap-4">
                    {labelContent}
                    <div className="flex items-center gap-1.5 w-full sm:w-auto">
                        <Input
                            id={setting.key}
                            type="number"
                            min={0}
                            max={100}
                            value={setting.value}
                            onChange={(e) => handleChange(e.target.value)}
                            onBlur={handleBlur}
                            className="w-full sm:w-24"
                        />
                        <span className="text-sm font-medium text-muted-foreground">%</span>
                    </div>
                </div>
                {error && <p className="text-xs text-destructive mt-1">{error}</p>}
            </div>
        )
    }

    // Crypto address: full-width with copy button
    if (isCryptoAddress) {
        return (
            <div className={fieldClasses}>
                <div className="space-y-2">
                    {labelContent}
                    <div className="flex items-center gap-1.5">
                        <Input
                            id={setting.key}
                            type="text"
                            value={setting.value}
                            onChange={(e) => handleChange(e.target.value)}
                            onBlur={handleBlur}
                            className="flex-1 font-mono text-xs"
                        />
                        {setting.value && <CopyButton text={setting.value} />}
                    </div>
                </div>
                {error && <p className="text-xs text-destructive mt-1">{error}</p>}
            </div>
        )
    }

    // Sensitive field: plain text input (value is already masked server-side)
    if (setting.sensitive) {
        return (
            <div className={fieldClasses}>
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 sm:gap-4">
                    {labelContent}
                    <Input
                        id={setting.key}
                        type="text"
                        value={setting.value}
                        onChange={(e) => handleChange(e.target.value)}
                        onBlur={handleBlur}
                        className="w-full sm:w-52"
                    />
                </div>
                {error && <p className="text-xs text-destructive mt-1">{error}</p>}
            </div>
        )
    }

    // Text / Number: label left, input right (responsive)
    return (
        <div className={fieldClasses}>
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 sm:gap-4">
                {labelContent}
                <Input
                    id={setting.key}
                    type={setting.type === "int" ? "number" : "text"}
                    value={setting.value}
                    onChange={(e) => handleChange(e.target.value)}
                    onBlur={handleBlur}
                    className="w-full sm:w-52"
                />
            </div>
            {error && <p className="text-xs text-destructive mt-1">{error}</p>}
        </div>
    )
}
