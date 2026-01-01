import { useState } from "react"
import { type UseFormReturn } from "react-hook-form"
import { type OutboundFormData } from "@/lib/validations/outbound-schema"
import { OUTBOUND_PRESETS, type OutboundPreset } from "@/lib/presets/outbound-presets"
import { OUTBOUND_PROTOCOLS } from "@/lib/types"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
    FormControl,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
    FormDescription,
} from "@/components/ui/form"
import { cn } from "@/lib/utils"
import { motion } from "framer-motion"
import { HiOutlineLink, HiOutlineClipboardCopy, HiOutlineX, HiOutlineCheck } from "react-icons/hi"
import { isConfigLink } from "@/lib/link-parser"
import { toast } from "sonner"

// Protocols that need address + port input on the General tab.
// Hysteria2 is included even though it has no transport tab — without
// these inputs the schema validation would fail and the user couldn't save.
const NEEDS_ADDRESS_PROTOCOLS = ["vless", "vmess", "trojan", "shadowsocks", "socks", "http", "hysteria2"]

interface GeneralTabProps {
    form: UseFormReturn<OutboundFormData>
    mode: "create" | "edit"
    onApplyPreset: (presetId: string) => void
    onImportConfig: (link: string) => { success: boolean; error?: string }
}

export function GeneralTab({ form, mode, onApplyPreset, onImportConfig }: GeneralTabProps) {
    const [selectedPreset, setSelectedPreset] = useState<string | null>(null)
    const [showImport, setShowImport] = useState(false)
    const [importLink, setImportLink] = useState("")
    const [importError, setImportError] = useState<string | null>(null)
    const protocol = form.watch("protocol")

    const handlePresetClick = (preset: OutboundPreset) => {
        setSelectedPreset(preset.id)
        onApplyPreset(preset.id)
    }

    const handleImport = () => {
        if (!importLink.trim()) {
            setImportError("Please enter a config link")
            return
        }

        const result = onImportConfig(importLink)
        if (!result.success) {
            setImportError(result.error || "Failed to parse link")
            return
        }

        setShowImport(false)
        setImportLink("")
        setImportError(null)
    }

    const handlePaste = async () => {
        try {
            if (!window.isSecureContext || !navigator.clipboard?.readText) {
                toast.error("Paste not available over HTTP. Use Ctrl+V / Cmd+V instead.")
                return
            }
            const text = await navigator.clipboard.readText()
            setImportLink(text)
            setImportError(null)
        } catch {
            toast.error("Failed to read clipboard. Use Ctrl+V / Cmd+V instead.")
        }
    }

    return (
        <div className="space-y-6">
            {/* Config link import - create mode only */}
            {mode === "create" && (
                <div className="space-y-3">
                    <button
                        type="button"
                        onClick={() => setShowImport(!showImport)}
                        className="flex items-center gap-2 text-sm font-medium text-primary hover:text-primary/80 transition-colors"
                    >
                        <HiOutlineLink className="w-4 h-4" />
                        {showImport ? "Hide import panel" : "Import from config link"}
                        <Badge variant="secondary" className="text-xs hidden md:inline-flex">
                            vless:// vmess:// trojan:// ss:// socks://
                        </Badge>
                    </button>

                    {showImport && (
                        <div className="border rounded-lg p-4 bg-muted/30 space-y-3">
                            <div className="flex items-center justify-between">
                                <Label className="text-sm font-medium">
                                    Paste config link
                                </Label>
                                <Button
                                    type="button"
                                    variant="ghost"
                                    size="sm"
                                    onClick={handlePaste}
                                    className="text-xs"
                                >
                                    <HiOutlineClipboardCopy className="w-4 h-4 mr-1" />
                                    Paste
                                </Button>
                            </div>
                            <Textarea
                                placeholder="vless://uuid@host:port?type=ws&security=tls&sni=example.com#Remark"
                                value={importLink}
                                onChange={(e) => {
                                    setImportLink(e.target.value)
                                    setImportError(null)
                                }}
                                className="font-mono text-xs min-h-[80px] resize-none"
                            />
                            {importError && (
                                <div className="flex items-center gap-2 text-sm text-red-500">
                                    <HiOutlineX className="w-4 h-4" />
                                    {importError}
                                </div>
                            )}
                            {importLink && isConfigLink(importLink) && !importError && (
                                <div className="flex items-center gap-2 text-sm text-green-500">
                                    <HiOutlineCheck className="w-4 h-4" />
                                    Valid config link detected
                                </div>
                            )}
                            <div className="flex justify-end gap-2">
                                <Button
                                    type="button"
                                    variant="outline"
                                    size="sm"
                                    onClick={() => {
                                        setShowImport(false)
                                        setImportLink("")
                                        setImportError(null)
                                    }}
                                >
                                    Cancel
                                </Button>
                                <Button
                                    type="button"
                                    size="sm"
                                    onClick={handleImport}
                                    disabled={!importLink.trim()}
                                >
                                    Import & Fill Form
                                </Button>
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* Presets grid - create mode only */}
            {mode === "create" && (
                <div className="space-y-3">
                    <h3 className="text-sm font-medium text-muted-foreground">Quick Start Templates</h3>
                    <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
                        {OUTBOUND_PRESETS.map((preset) => {
                            const Icon = preset.icon
                            const isSelected = selectedPreset === preset.id
                            return (
                                <motion.button
                                    key={preset.id}
                                    type="button"
                                    whileTap={{ scale: 0.97 }}
                                    animate={isSelected ? { scale: [1, 1.02, 1] } : {}}
                                    transition={{ duration: 0.15 }}
                                    onClick={() => handlePresetClick(preset)}
                                    className={cn(
                                        "flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors",
                                        isSelected
                                            ? "border-primary bg-primary/5 ring-1 ring-primary/20"
                                            : "hover:bg-accent/50"
                                    )}
                                >
                                    <div className="flex items-center gap-2">
                                        <Icon className="h-4 w-4 text-muted-foreground" />
                                        <span className="text-sm font-medium">{preset.name}</span>
                                    </div>
                                    <p className="text-[11px] text-muted-foreground leading-tight">
                                        {preset.description}
                                    </p>
                                </motion.button>
                            )
                        })}
                    </div>
                </div>
            )}

            {/* Tag & Remark */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
                <FormField
                    control={form.control}
                    name="tag"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Tag *</FormLabel>
                            <FormControl>
                                <Input
                                    placeholder="proxy-out"
                                    {...field}
                                />
                            </FormControl>
                            <FormDescription className="text-xs">
                                Unique identifier for routing
                            </FormDescription>
                            <FormMessage />
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
                                <Input
                                    placeholder="US Server"
                                    {...field}
                                    value={field.value || ""}
                                />
                            </FormControl>
                        </FormItem>
                    )}
                />
            </div>

            {/* Protocol selector */}
            <FormField
                control={form.control}
                name="protocol"
                render={({ field }) => (
                    <FormItem>
                        <FormLabel>Protocol</FormLabel>
                        <Select
                            value={field.value}
                            onValueChange={field.onChange}
                        >
                            <FormControl>
                                <SelectTrigger className="w-full">
                                    <SelectValue />
                                </SelectTrigger>
                            </FormControl>
                            <SelectContent>
                                {OUTBOUND_PROTOCOLS.map((p) => (
                                    <SelectItem key={p.value} value={p.value}>
                                        {p.label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </FormItem>
                )}
            />

            {/* Address & Port - conditionally visible */}
            {NEEDS_ADDRESS_PROTOCOLS.includes(protocol) && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <FormField
                        control={form.control}
                        name="address"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Address *</FormLabel>
                                <FormControl>
                                    <Input
                                        placeholder="example.com"
                                        {...field}
                                        value={field.value || ""}
                                    />
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
                                <FormLabel>Port *</FormLabel>
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
            )}

            {/* Send Through */}
            <div className="space-y-2">
                <FormLabel>Send Through</FormLabel>
                <Input
                    placeholder="0.0.0.0 (local address)"
                    value={form.watch("send_through") || ""}
                    onChange={(e) => form.setValue("send_through", e.target.value, { shouldDirty: true })}
                />
                <p className="text-xs text-muted-foreground">Local address to send traffic through</p>
            </div>
        </div>
    )
}
