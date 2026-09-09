import { useState, type ClipboardEvent, type KeyboardEvent } from "react"
import { type UseFormReturn } from "react-hook-form"
import { ClipboardPaste, Link2 } from "lucide-react"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import { OUTBOUND_PRESETS, OUTBOUND_PRESET_GROUPS } from "@/lib/presets/outbound-presets"
import { OUTBOUND_PROTOCOLS } from "@/lib/types"
import type { Outbound } from "@/lib/types"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
    FormControl,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
    FormDescription,
} from "@/components/ui/form"
import { TemplatePicker, type TemplateItem } from "@/components/connection-dialog/template-picker"
import { Callout, FormSection } from "@/components/connection-dialog/section"
import { isConfigLink } from "@/lib/link-parser"
import { formatRelativeTime } from "@/lib/utils"
import { toast } from "sonner"
import { NEEDS_ADDRESS_PROTOCOLS } from "../use-outbound-form"

const TEMPLATE_ITEMS: TemplateItem[] = OUTBOUND_PRESETS.map((p) => ({
    id: p.id,
    name: p.name,
    tokens: p.tokens,
    description: p.description,
    group: p.group,
}))

const SCHEMES = "vless://  vmess://  trojan://  ss://  socks://  wg://  hy2://"

interface GeneralTabProps {
    form: UseFormReturn<OutboundFormData>
    mode: "create" | "edit"
    outbound?: Outbound | null
    appliedPresetId: string | null
    onApplyPreset: (presetId: string) => void
    onImportConfig: (link: string) => { success: boolean; error?: string }
}

// Paste-first import: a share link is the most common way an outbound is
// created, so it is the first thing on the tab and fills the form on paste.
function ImportLinkField({ onImport }: { onImport: (link: string) => { success: boolean; error?: string } }) {
    const [value, setValue] = useState("")
    const [error, setError] = useState<string | null>(null)

    const attempt = (text: string) => {
        const trimmed = text.trim()
        if (!trimmed) return
        if (!isConfigLink(trimmed)) {
            setError("Not a recognised share link. Supported: vless://, vmess://, trojan://, ss://, socks://, wg://, hy2://")
            return
        }
        const result = onImport(trimmed)
        if (result.success) {
            setValue("")
            setError(null)
        } else {
            setError(result.error || "Failed to parse link")
        }
    }

    const handlePaste = (e: ClipboardEvent<HTMLInputElement>) => {
        const text = e.clipboardData.getData("text")
        if (!text) return
        e.preventDefault()
        setValue(text)
        attempt(text)
    }

    const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
        if (e.key === "Enter") {
            e.preventDefault()
            attempt(value)
        }
    }

    const handlePasteButton = async () => {
        try {
            if (!window.isSecureContext || !navigator.clipboard?.readText) {
                toast.error("Clipboard access needs HTTPS. Paste into the field with Ctrl+V / Cmd+V instead.")
                return
            }
            const text = await navigator.clipboard.readText()
            setValue(text)
            attempt(text)
        } catch {
            toast.error("Could not read the clipboard. Paste into the field instead.")
        }
    }

    return (
        <FormSection title="Import a share link">
            <div className="relative">
                <Link2 className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                    value={value}
                    onChange={(e) => { setValue(e.target.value); setError(null) }}
                    onPaste={handlePaste}
                    onKeyDown={handleKeyDown}
                    placeholder={SCHEMES}
                    aria-label="Share link"
                    aria-invalid={!!error}
                    className="h-11 pl-9 pr-24 font-mono text-xs"
                />
                <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => { void handlePasteButton() }}
                    className="absolute right-1.5 top-1/2 -translate-y-1/2"
                >
                    <ClipboardPaste className="h-4 w-4" />
                    Paste
                </Button>
            </div>
            {error
                ? <p className="text-xs text-status-danger">{error}</p>
                : <p className="text-xs text-muted-foreground">Paste and every tab fills itself. You can edit anything afterwards.</p>}
        </FormSection>
    )
}

function LastTestCallout({ outbound }: { outbound: Outbound }) {
    const result = outbound.last_test_result
    if (!result) return null
    const when = outbound.last_tested_at ? formatRelativeTime(outbound.last_tested_at) : "earlier"
    if (result.success) {
        return (
            <Callout tone="success">
                Last test {when}: reachable
                {typeof result.latency_ms === "number" && <> · <span className="font-mono">{result.latency_ms} ms</span></>}
                {result.ip && <> · <span className="font-mono">{result.ip}</span>{result.country ? ` (${result.country})` : ""}</>}
            </Callout>
        )
    }
    return (
        <Callout tone="warning">
            Last test {when}: {result.error || result.message || "failed"}
        </Callout>
    )
}

export function GeneralTab({ form, mode, outbound, appliedPresetId, onApplyPreset, onImportConfig }: GeneralTabProps) {
    const protocol = form.watch("protocol")
    const needsAddress = NEEDS_ADDRESS_PROTOCOLS.includes(protocol)

    return (
        <div className="space-y-6">
            {mode === "create" && <ImportLinkField onImport={onImportConfig} />}

            {mode === "create" && (
                <TemplatePicker
                    items={TEMPLATE_ITEMS}
                    appliedId={appliedPresetId}
                    onApply={onApplyPreset}
                    groups={OUTBOUND_PRESET_GROUPS}
                />
            )}

            {mode === "edit" && outbound && <LastTestCallout outbound={outbound} />}

            <FormSection title="Identity">
                <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
                    <FormField
                        control={form.control}
                        name="tag"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Tag <span className="text-text-tertiary">*</span></FormLabel>
                                <FormControl>
                                    <Input placeholder="proxy-out" {...field} />
                                </FormControl>
                                <FormMessage />
                                <FormDescription className="text-xs">Referenced by routing rules.</FormDescription>
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="remark"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Remark</FormLabel>
                                <FormControl>
                                    <Input placeholder="US Server" {...field} value={field.value || ""} />
                                </FormControl>
                                <FormDescription className="text-xs">Shown to you in lists.</FormDescription>
                            </FormItem>
                        )}
                    />
                </div>
            </FormSection>

            <FormSection title="Protocol">
                <FormField
                    control={form.control}
                    name="protocol"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Protocol</FormLabel>
                            <Select value={field.value} onValueChange={field.onChange}>
                                <FormControl>
                                    <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                </FormControl>
                                <SelectContent>
                                    {OUTBOUND_PROTOCOLS.map((p) => (
                                        <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <FormDescription className="text-xs">
                                {needsAddress
                                    ? "Changes which settings appear under Protocol and Transport."
                                    : "Address and port appear here for protocols that dial a server."}
                            </FormDescription>
                        </FormItem>
                    )}
                />
            </FormSection>

            {needsAddress && (
                <FormSection title="Server">
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1fr)_148px]">
                        <FormField
                            control={form.control}
                            name="address"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Address <span className="text-text-tertiary">*</span></FormLabel>
                                    <FormControl>
                                        <Input placeholder="example.com" {...field} value={field.value || ""} />
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                        <FormField
                            control={form.control}
                            name="port"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Port <span className="text-text-tertiary">*</span></FormLabel>
                                    <FormControl>
                                        <Input
                                            type="number"
                                            placeholder="443"
                                            {...field}
                                            value={field.value || ""}
                                            onChange={(e) => field.onChange(parseInt(e.target.value) || 0)}
                                        />
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                    </div>
                </FormSection>
            )}
        </div>
    )
}
