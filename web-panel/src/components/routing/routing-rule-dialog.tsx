import { useState, useEffect } from "react"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import {
    Accordion,
    AccordionContent,
    AccordionItem,
    AccordionTrigger,
} from "@/components/ui/accordion"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { toast } from "sonner"
import { HiOutlinePlus, HiOutlineTrash, HiX } from "react-icons/hi"
import { ArrayInput, InboundTagInput, TagList } from "@/components/ui/tag-input"
import type { RoutingRule, DomainMatcher, Outbound, Inbound, BalancingRule } from "@/lib/types"

const DOMAIN_TYPES = [
    { value: "domain", label: "Domain (include subdomains)" },
    { value: "full", label: "Full (exact match)" },
    { value: "plain", label: "Plain (contains)" },
    { value: "regex", label: "Regex" },
] as const

const GEOIP_PRESETS = [
    { value: "geoip:private", label: "Private IPs" },
    { value: "geoip:cn", label: "China" },
    { value: "geoip:ir", label: "Iran" },
    { value: "geoip:ru", label: "Russia" },
] as const

const GEOSITE_PRESETS = [
    { value: "geosite:google", label: "Google" },
    { value: "geosite:youtube", label: "YouTube" },
    { value: "geosite:facebook", label: "Facebook" },
    { value: "geosite:twitter", label: "Twitter" },
    { value: "geosite:telegram", label: "Telegram" },
    { value: "geosite:category-ads-all", label: "Ads (Block)" },
    { value: "geosite:cn", label: "China Sites" },
    { value: "geosite:ir", label: "Iran Sites" },
] as const

const PROTOCOL_OPTIONS = [
    { value: "http", label: "HTTP" },
    { value: "tls", label: "TLS" },
    { value: "bittorrent", label: "BitTorrent" },
    { value: "quic", label: "QUIC" },
] as const

const NETWORK_OPTIONS = [
    { value: "tcp", label: "TCP" },
    { value: "udp", label: "UDP" },
    { value: "tcp,udp", label: "TCP & UDP" },
] as const

// ─── Validation Helpers ───────────────────────────────────
const RULE_TAG_RE = /^[a-zA-Z0-9_:.-]+$/

function isValidPort(s: string): boolean {
    s = s.trim()
    if (s.includes("-")) {
        const [a, b] = s.split("-", 2)
        const from = Number(a?.trim())
        const to = Number(b?.trim())
        return Number.isInteger(from) && Number.isInteger(to) && from >= 1 && from <= 65535 && to >= 1 && to <= 65535 && from <= to
    }
    const n = Number(s)
    return Number.isInteger(n) && n >= 1 && n <= 65535
}

// Accepts geoip:CC, ext:file:tag, ext-ip:file:tag, or a CIDR / bare IP.
function isValidCIDROrGeoIP(s: string): boolean {
    s = s.trim()
    if (!s) return false
    if (s.startsWith("geoip:")) return s.length > 6
    if (s.startsWith("ext:") || s.startsWith("ext-ip:")) return s.includes(":", 4)

    // Optional /prefix
    let host = s
    let prefix: number | null = null
    const slash = s.lastIndexOf("/")
    if (slash !== -1) {
        host = s.slice(0, slash)
        const p = Number(s.slice(slash + 1))
        if (!Number.isInteger(p) || p < 0) return false
        prefix = p
    }

    // IPv6 (very loose): contains ":" — bound prefix at 128.
    if (host.includes(":")) {
        if (prefix !== null && prefix > 128) return false
        return /^[0-9a-fA-F:]+$/.test(host)
    }
    // IPv4
    const parts = host.split(".")
    if (parts.length !== 4) return false
    for (const p of parts) {
        if (!/^\d{1,3}$/.test(p)) return false
        const n = Number(p)
        if (n < 0 || n > 255) return false
    }
    if (prefix !== null && prefix > 32) return false
    return true
}

const GEOIP_PREFIXES = ["geoip:", "ext:", "ext-ip:"] as const
function splitGeoIPCIDR(items: string[]): { geoip: string[]; ipcidr: string[] } {
    const geoip: string[] = []
    const ipcidr: string[] = []
    for (const raw of items) {
        const v = raw.trim()
        if (!v) continue
        if (GEOIP_PREFIXES.some(p => v.startsWith(p))) geoip.push(v)
        else ipcidr.push(v)
    }
    return { geoip, ipcidr }
}

interface RoutingRuleDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    rule: RoutingRule | null
    nodeId: number
    outbounds: Outbound[]
    inbounds: Inbound[]
    balancingRules: BalancingRule[]
    onSave: (data: Partial<RoutingRule>) => Promise<void>
    mode: "create" | "edit"
}

const emptyRule: Partial<RoutingRule> = {
    rule_tag: "",
    remark: "",
    priority: 0,
    enabled: true,
    outbound_tag: "",
    balancing_tag: "",
    domain_rules: [],
    geoip_rules: [],
    ipcidr_rules: [],
    port_rules: [],
    network_rules: [],
    protocol_rules: [],
    inbound_tags: [],
    user_emails: [],
    source_ips: [],
    source_ports: [],
    attributes: {},
    process_names: [],
    local_ips: [],
    local_ports: [],
}

export function RoutingRuleDialog({
    open,
    onOpenChange,
    rule,
    nodeId,
    outbounds,
    inbounds,
    balancingRules,
    onSave,
    mode,
}: RoutingRuleDialogProps) {
    const [formData, setFormData] = useState<Partial<RoutingRule>>(emptyRule)
    const [isSaving, setIsSaving] = useState(false)
    const [expandedSections, setExpandedSections] = useState<string[]>([])

    // Initialize form data
    useEffect(() => {
        if (open) {
            if (mode === "edit" && rule) {
                // BE stores GeoIP and IP CIDR separately; the dialog presents
                // them as a single combined list. Merge on load (dedupe).
                const combinedIPs = Array.from(new Set([
                    ...(rule.geoip_rules || []),
                    ...(rule.ipcidr_rules || []),
                ]))
                setFormData({
                    ...rule,
                    domain_rules: rule.domain_rules || [],
                    geoip_rules: combinedIPs,
                    ipcidr_rules: [], // owned by combined display until save
                    port_rules: rule.port_rules || [],
                    network_rules: rule.network_rules || [],
                    protocol_rules: rule.protocol_rules || [],
                    inbound_tags: rule.inbound_tags || [],
                    user_emails: rule.user_emails || [],
                    source_ips: rule.source_ips || [],
                    source_ports: rule.source_ports || [],
                    attributes: rule.attributes || {},
                    process_names: rule.process_names || [],
                    local_ips: rule.local_ips || [],
                    local_ports: rule.local_ports || [],
                })
                // Auto-expand sections with data
                const sections: string[] = []
                if (rule.domain_rules?.length || rule.geoip_rules?.length || rule.ipcidr_rules?.length || rule.port_rules?.length || Object.keys(rule.attributes || {}).length) {
                    sections.push("destination")
                }
                if (rule.source_ips?.length || rule.source_ports?.length || rule.inbound_tags?.length || rule.process_names?.length || rule.local_ips?.length || rule.local_ports?.length) {
                    sections.push("source")
                }
                setExpandedSections(sections)
            } else {
                setFormData({ ...emptyRule, node_id: nodeId })
                setExpandedSections([])
            }
        }
    }, [open, mode, rule, nodeId])

    const updateField = <K extends keyof RoutingRule>(key: K, value: RoutingRule[K]) => {
        setFormData((prev) => ({ ...prev, [key]: value }))
    }

    // Domain rules helpers
    const addDomainRule = (type: DomainMatcher["type"], value: string) => {
        if (!value.trim()) return
        const newRule: DomainMatcher = { type, value: value.trim() }
        updateField("domain_rules", [...(formData.domain_rules || []), newRule])
    }

    const removeDomainRule = (index: number) => {
        const updated = [...(formData.domain_rules || [])]
        updated.splice(index, 1)
        updateField("domain_rules", updated)
    }

    // Array field helpers with validation
    const addToArray = (field: keyof RoutingRule, value: string) => {
        const v = value.trim()
        if (!v) return

        // Field-specific validation
        if (field === "port_rules" || field === "source_ports" || field === "local_ports") {
            if (!isValidPort(v)) {
                toast.error(`Invalid port: "${v}" (use 1-65535 or range like 1000-2000)`)
                return
            }
        }
        if (field === "geoip_rules" || field === "source_ips" || field === "local_ips") {
            if (!isValidCIDROrGeoIP(v)) {
                toast.error(`Invalid IP/GeoIP: "${v}" (use geoip:XX or CIDR like 10.0.0.0/8)`)
                return
            }
        }
        if (field === "user_emails") {
            // Light email check — BE accepts arbitrary user identifiers but reject empty.
            if (!v.includes("@")) {
                toast.error(`Invalid user email: "${v}"`)
                return
            }
        }

        const current = (formData[field] as string[]) || []
        if (!current.includes(v)) {
            updateField(field, [...current, v] as never)
        }
    }

    const removeFromArray = (field: keyof RoutingRule, index: number) => {
        const current = [...((formData[field] as string[]) || [])]
        current.splice(index, 1)
        updateField(field, current as never)
    }

    const hasAnyMatcher = (data: Partial<RoutingRule>) =>
        Boolean(
            data.domain_rules?.length ||
            data.geoip_rules?.length ||
            data.ipcidr_rules?.length ||
            data.port_rules?.length ||
            data.network_rules?.length ||
            data.protocol_rules?.length ||
            data.inbound_tags?.length ||
            data.user_emails?.length ||
            data.source_ips?.length ||
            data.source_ports?.length ||
            (data.attributes && Object.keys(data.attributes).length) ||
            data.process_names?.length ||
            data.local_ips?.length ||
            data.local_ports?.length
        )

    const handleSave = async () => {
        const tag = formData.rule_tag?.trim() || ""
        if (!tag) {
            toast.error("Rule tag is required")
            return
        }
        if (!RULE_TAG_RE.test(tag)) {
            toast.error("Rule tag can only contain letters, numbers, underscores, colons, dots, and hyphens")
            return
        }
        if (!formData.outbound_tag && !formData.balancing_tag) {
            toast.error("Target outbound is required")
            return
        }
        if (formData.outbound_tag && formData.balancing_tag) {
            toast.error("Choose either an outbound or a balancer, not both")
            return
        }

        // Split combined IP list into geoip vs cidr fields the BE expects.
        const { geoip, ipcidr } = splitGeoIPCIDR(formData.geoip_rules || [])
        const payload: Partial<RoutingRule> = {
            ...formData,
            rule_tag: tag,
            geoip_rules: geoip,
            ipcidr_rules: ipcidr,
        }

        if (!hasAnyMatcher(payload)) {
            toast.error("Add at least one matcher (domain, IP, port, network, protocol, inbound, user, source, process, or local).")
            return
        }

        // Strip server-managed identity fields so a stale dialog state cannot
        // race with the URL id and so the BE sees a clean payload.
        if (mode === "create") {
            delete payload.id
            delete payload.node_id
        }
        delete payload.created_at
        delete payload.updated_at

        try {
            setIsSaving(true)
            await onSave(payload)
            onOpenChange(false)
        } catch {
            toast.error("Failed to save routing rule")
        } finally {
            setIsSaving(false)
        }
    }

    // Count active matchers for badges
    const destMatcherCount =
        (formData.domain_rules?.length || 0) +
        (formData.geoip_rules?.length || 0) +
        (formData.ipcidr_rules?.length || 0) +
        (formData.port_rules?.length || 0) +
        (formData.network_rules?.length || 0) +
        (formData.protocol_rules?.length || 0) +
        Object.keys(formData.attributes || {}).length

    const sourceMatcherCount =
        (formData.source_ips?.length || 0) +
        (formData.source_ports?.length || 0) +
        (formData.inbound_tags?.length || 0) +
        (formData.user_emails?.length || 0) +
        (formData.process_names?.length || 0) +
        (formData.local_ips?.length || 0) +
        (formData.local_ports?.length || 0)

    return (
        <ResponsiveDialog
            open={open}
            onOpenChange={onOpenChange}
            title={mode === "create" ? "Add Routing Rule" : "Edit Routing Rule"}
            description="Configure traffic matching rules and target outbound"
            onSave={handleSave}
            saveLabel={isSaving ? "Saving..." : mode === "create" ? "Add Rule" : "Save Changes"}
            saveDisabled={isSaving}
        >
                    {/* ============ BASIC SETTINGS ============ */}
                    <div className="space-y-4">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="space-y-2">
                                <Label>Rule Tag *</Label>
                                <Input
                                    placeholder="block-ads"
                                    value={formData.rule_tag || ""}
                                    onChange={(e) => updateField("rule_tag", e.target.value)}
                                />
                                <p className="text-xs text-muted-foreground">Unique identifier</p>
                            </div>
                            <div className="space-y-2">
                                <Label>Priority</Label>
                                <Input
                                    type="number"
                                    placeholder="0"
                                    value={formData.priority ?? 0}
                                    onChange={(e) => updateField("priority", parseInt(e.target.value) || 0)}
                                />
                                <p className="text-xs text-muted-foreground">Lower = higher priority</p>
                            </div>
                        </div>

                        <div className="space-y-2">
                            <Label>Remark</Label>
                            <Input
                                placeholder="Block advertisement domains"
                                value={formData.remark || ""}
                                onChange={(e) => updateField("remark", e.target.value)}
                            />
                        </div>

                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="space-y-2">
                                <Label>Target *</Label>
                                <div className="flex gap-1 mb-2">
                                    <Button
                                        type="button"
                                        size="sm"
                                        variant={!formData.balancing_tag ? "default" : "outline"}
                                        className="text-xs"
                                        onClick={() => {
                                            setFormData(prev => ({ ...prev, balancing_tag: "", outbound_tag: prev.outbound_tag || "" }))
                                        }}
                                    >
                                        Outbound
                                    </Button>
                                    <Button
                                        type="button"
                                        size="sm"
                                        variant={formData.balancing_tag ? "default" : "outline"}
                                        className="text-xs"
                                        onClick={() => {
                                            setFormData(prev => ({ ...prev, outbound_tag: "", balancing_tag: prev.balancing_tag || "" }))
                                        }}
                                    >
                                        Balancer
                                    </Button>
                                </div>
                                {!formData.balancing_tag ? (
                                    <Select
                                        value={formData.outbound_tag || "none"}
                                        onValueChange={(v) => updateField("outbound_tag", v === "none" ? "" : v)}
                                    >
                                        <SelectTrigger>
                                            <SelectValue placeholder="Select outbound..." />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="none">None</SelectItem>
                                            {outbounds.map((o) => (
                                                <SelectItem key={o.id} value={o.tag}>
                                                    {o.tag} ({o.protocol})
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                ) : (
                                    <Select
                                        value={formData.balancing_tag || "none"}
                                        onValueChange={(v) => updateField("balancing_tag", v === "none" ? "" : v)}
                                    >
                                        <SelectTrigger>
                                            <SelectValue placeholder="Select balancer..." />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="none">None</SelectItem>
                                            {balancingRules.map((b) => (
                                                <SelectItem key={b.id} value={b.tag}>
                                                    {b.tag} ({b.strategy})
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                )}
                            </div>
                            <div className="flex items-center justify-between pt-6">
                                <div>
                                    <Label>Enabled</Label>
                                    <p className="text-xs text-muted-foreground">Rule is active</p>
                                </div>
                                <Switch
                                    checked={formData.enabled ?? true}
                                    onCheckedChange={(c) => updateField("enabled", c)}
                                />
                            </div>
                        </div>
                    </div>

                    {/* ============ DESTINATION MATCHERS ============ */}
                    <Accordion
                        type="multiple"
                        value={expandedSections}
                        onValueChange={setExpandedSections}
                    >
                        <AccordionItem value="destination" className="border rounded-lg px-4">
                            <AccordionTrigger className="hover:no-underline py-3">
                                <div className="flex items-center gap-2">
                                    <span className="font-medium">Destination Matchers</span>
                                    {destMatcherCount > 0 && (
                                        <Badge variant="secondary" className="text-xs">
                                            {destMatcherCount} rules
                                        </Badge>
                                    )}
                                </div>
                            </AccordionTrigger>
                            <AccordionContent className="pt-2 pb-4 space-y-4">
                                {/* Domain Rules */}
                                <div className="space-y-2">
                                    <Label>Domains & Geosite</Label>
                                    <div className="flex gap-2">
                                        <DomainInput onAdd={addDomainRule} />
                                    </div>
                                    <div className="flex flex-wrap gap-1">
                                        {GEOSITE_PRESETS.map((preset) => (
                                            <Button
                                                key={preset.value}
                                                type="button"
                                                variant="outline"
                                                size="sm"
                                                className="text-xs h-9 md:h-7"
                                                onClick={() => addDomainRule("domain", preset.value)}
                                            >
                                                + {preset.label}
                                            </Button>
                                        ))}
                                    </div>
                                    {(formData.domain_rules?.length || 0) > 0 && (
                                        <div className="flex flex-wrap gap-1 mt-2">
                                            {formData.domain_rules?.map((d, i) => (
                                                <Badge key={i} variant="secondary" className="gap-1">
                                                    <span className="text-xs opacity-60">{d.type}:</span>
                                                    {d.value}
                                                    <button onClick={() => removeDomainRule(i)} className="ml-1 p-1 -mr-1 rounded hover:bg-destructive/20 hover:text-red-500">
                                                        <HiX className="w-3 h-3" />
                                                    </button>
                                                </Badge>
                                            ))}
                                        </div>
                                    )}
                                </div>

                                {/* GeoIP Rules */}
                                <div className="space-y-2">
                                    <Label>GeoIP & IP CIDR</Label>
                                    <ArrayInput
                                        placeholder="192.168.0.0/16 or geoip:cn"
                                        onAdd={(v) => addToArray("geoip_rules", v)}
                                    />
                                    <div className="flex flex-wrap gap-1">
                                        {GEOIP_PRESETS.map((preset) => (
                                            <Button
                                                key={preset.value}
                                                type="button"
                                                variant="outline"
                                                size="sm"
                                                className="text-xs h-9 md:h-7"
                                                onClick={() => addToArray("geoip_rules", preset.value)}
                                            >
                                                + {preset.label}
                                            </Button>
                                        ))}
                                    </div>
                                    <TagList items={formData.geoip_rules || []} onRemove={(i) => removeFromArray("geoip_rules", i)} />
                                </div>

                                {/* Port Rules */}
                                <div className="space-y-2">
                                    <Label>Ports</Label>
                                    <ArrayInput
                                        placeholder="80, 443, 8000-9000"
                                        onAdd={(v) => addToArray("port_rules", v)}
                                    />
                                    <TagList items={formData.port_rules || []} onRemove={(i) => removeFromArray("port_rules", i)} />
                                </div>

                                {/* Protocol & Network */}
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <div className="space-y-2">
                                        <Label>Protocol</Label>
                                        <div className="flex flex-wrap gap-1">
                                            {PROTOCOL_OPTIONS.map((p) => (
                                                <Button
                                                    key={p.value}
                                                    type="button"
                                                    variant={formData.protocol_rules?.includes(p.value) ? "default" : "outline"}
                                                    size="sm"
                                                    className="text-xs h-9 md:h-7"
                                                    onClick={() => {
                                                        const current = formData.protocol_rules || []
                                                        if (current.includes(p.value)) {
                                                            updateField("protocol_rules", current.filter((x) => x !== p.value))
                                                        } else {
                                                            updateField("protocol_rules", [...current, p.value])
                                                        }
                                                    }}
                                                >
                                                    {p.label}
                                                </Button>
                                            ))}
                                        </div>
                                    </div>
                                    <div className="space-y-2">
                                        <Label>Network</Label>
                                        <div className="flex flex-wrap gap-1">
                                            {NETWORK_OPTIONS.map((n) => (
                                                <Button
                                                    key={n.value}
                                                    type="button"
                                                    variant={formData.network_rules?.includes(n.value) ? "default" : "outline"}
                                                    size="sm"
                                                    className="text-xs h-9 md:h-7"
                                                    onClick={() => {
                                                        const current = formData.network_rules || []
                                                        if (current.includes(n.value)) {
                                                            // Deselect
                                                            updateField("network_rules", current.filter((x) => x !== n.value))
                                                        } else if (n.value === "tcp,udp") {
                                                            // "TCP & UDP" replaces individual selections
                                                            updateField("network_rules", ["tcp,udp"])
                                                        } else {
                                                            // Individual selection clears "tcp,udp" composite
                                                            const filtered = current.filter((x) => x !== "tcp,udp")
                                                            updateField("network_rules", [...filtered, n.value])
                                                        }
                                                    }}
                                                >
                                                    {n.label}
                                                </Button>
                                            ))}
                                        </div>
                                    </div>
                                </div>

                                {/* Attributes (HTTP header matching) */}
                                <div className="space-y-2">
                                    <Label>Attributes</Label>
                                    <p className="text-xs text-muted-foreground">Match HTTP headers (key: header name, value: regex pattern)</p>
                                    <div className="flex gap-2">
                                        <Input
                                            placeholder="Header name"
                                            id="attr-key"
                                            className="flex-1"
                                            onKeyDown={(e) => {
                                                if (e.key === "Enter") {
                                                    e.preventDefault()
                                                    document.getElementById("attr-val")?.focus()
                                                }
                                            }}
                                        />
                                        <Input
                                            placeholder="Regex pattern"
                                            id="attr-val"
                                            className="flex-1"
                                            onKeyDown={(e) => {
                                                if (e.key === "Enter") {
                                                    e.preventDefault()
                                                    const keyEl = document.getElementById("attr-key") as HTMLInputElement
                                                    const valEl = e.target as HTMLInputElement
                                                    const k = keyEl?.value.trim()
                                                    const v = valEl?.value.trim()
                                                    if (k && v) {
                                                        setFormData(prev => ({
                                                            ...prev,
                                                            attributes: { ...(prev.attributes || {}), [k]: v }
                                                        }))
                                                        keyEl.value = ""
                                                        valEl.value = ""
                                                        keyEl.focus()
                                                    }
                                                }
                                            }}
                                        />
                                        <Button type="button" size="icon" variant="outline" onClick={() => {
                                            const keyEl = document.getElementById("attr-key") as HTMLInputElement
                                            const valEl = document.getElementById("attr-val") as HTMLInputElement
                                            const k = keyEl?.value.trim()
                                            const v = valEl?.value.trim()
                                            if (k && v) {
                                                setFormData(prev => ({
                                                    ...prev,
                                                    attributes: { ...(prev.attributes || {}), [k]: v }
                                                }))
                                                keyEl.value = ""
                                                valEl.value = ""
                                                keyEl.focus()
                                            }
                                        }}>
                                            <HiOutlinePlus className="w-4 h-4" />
                                        </Button>
                                    </div>
                                    {Object.keys(formData.attributes || {}).length > 0 && (
                                        <div className="flex flex-wrap gap-1 mt-2">
                                            {Object.entries(formData.attributes || {}).map(([key, val]) => (
                                                <Badge key={key} variant="secondary" className="gap-1">
                                                    <span className="text-xs opacity-60">{key}:</span>
                                                    {val}
                                                    <button onClick={() => {
                                                        const next = { ...(formData.attributes || {}) }
                                                        delete next[key]
                                                        setFormData(prev => ({ ...prev, attributes: next }))
                                                    }} className="ml-1 p-1 -mr-1 rounded hover:bg-destructive/20 hover:text-red-500">
                                                        <HiX className="w-3 h-3" />
                                                    </button>
                                                </Badge>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            </AccordionContent>
                        </AccordionItem>

                        {/* ============ SOURCE MATCHERS ============ */}
                        <AccordionItem value="source" className="border rounded-lg px-4 mt-2">
                            <AccordionTrigger className="hover:no-underline py-3">
                                <div className="flex items-center gap-2">
                                    <span className="font-medium">Source Matchers</span>
                                    {sourceMatcherCount > 0 && (
                                        <Badge variant="secondary" className="text-xs">
                                            {sourceMatcherCount} rules
                                        </Badge>
                                    )}
                                </div>
                            </AccordionTrigger>
                            <AccordionContent className="pt-2 pb-4 space-y-4">
                                {/* Inbound Tags */}
                                <div className="space-y-2">
                                    <Label>Inbound Tags</Label>
                                    <InboundTagInput
                                        suggestions={inbounds.map((i) => ({ tag: i.tag, label: `${i.tag} (${i.protocol}:${i.port})` }))}
                                        selected={formData.inbound_tags || []}
                                        onAdd={(v) => addToArray("inbound_tags", v)}
                                        onRemove={(i) => removeFromArray("inbound_tags", i)}
                                    />
                                    <p className="text-xs text-muted-foreground">Pick a configured inbound, or type a synthetic tag (e.g. <code>dns-in</code> for DNS queries).</p>
                                </div>

                                {/* Source IPs */}
                                <div className="space-y-2">
                                    <Label>Source IPs</Label>
                                    <ArrayInput
                                        placeholder="10.0.0.0/8"
                                        onAdd={(v) => addToArray("source_ips", v)}
                                    />
                                    <TagList items={formData.source_ips || []} onRemove={(i) => removeFromArray("source_ips", i)} />
                                </div>

                                {/* Source Ports */}
                                <div className="space-y-2">
                                    <Label>Source Ports</Label>
                                    <ArrayInput
                                        placeholder="1024-65535"
                                        onAdd={(v) => addToArray("source_ports", v)}
                                    />
                                    <TagList items={formData.source_ports || []} onRemove={(i) => removeFromArray("source_ports", i)} />
                                </div>

                                {/* User Emails */}
                                <div className="space-y-2">
                                    <Label>User Emails</Label>
                                    <ArrayInput
                                        placeholder="user@example.com"
                                        onAdd={(v) => addToArray("user_emails", v)}
                                    />
                                    <p className="text-xs text-muted-foreground">Match specific user accounts</p>
                                    <TagList items={formData.user_emails || []} onRemove={(i) => removeFromArray("user_emails", i)} />
                                </div>

                                {/* Process Names */}
                                <div className="space-y-2">
                                    <Label>Process Names</Label>
                                    <ArrayInput
                                        placeholder="Process name or path"
                                        onAdd={(v) => addToArray("process_names", v)}
                                    />
                                    <p className="text-xs text-muted-foreground">Match by local process name or path</p>
                                    <TagList items={formData.process_names || []} onRemove={(i) => removeFromArray("process_names", i)} />
                                </div>

                                {/* Local IPs */}
                                <div className="space-y-2">
                                    <Label>Local IPs</Label>
                                    <ArrayInput
                                        placeholder="geoip:cn or 10.0.0.0/8"
                                        onAdd={(v) => addToArray("local_ips", v)}
                                    />
                                    <p className="text-xs text-muted-foreground">Match local/bind address</p>
                                    <TagList items={formData.local_ips || []} onRemove={(i) => removeFromArray("local_ips", i)} />
                                </div>

                                {/* Local Ports */}
                                <div className="space-y-2">
                                    <Label>Local Ports</Label>
                                    <ArrayInput
                                        placeholder="80, 443, 1000-2000"
                                        onAdd={(v) => addToArray("local_ports", v)}
                                    />
                                    <TagList items={formData.local_ports || []} onRemove={(i) => removeFromArray("local_ports", i)} />
                                </div>
                            </AccordionContent>
                        </AccordionItem>
                    </Accordion>

        </ResponsiveDialog>
    )
}

// Helper Components
function DomainInput({ onAdd }: { onAdd: (type: DomainMatcher["type"], value: string) => void }) {
    const [type, setType] = useState<DomainMatcher["type"]>("domain")
    const [value, setValue] = useState("")
    const [bulkMode, setBulkMode] = useState(false)
    const [bulkText, setBulkText] = useState("")
    const [bulkType, setBulkType] = useState<DomainMatcher["type"]>("domain")

    const handleAdd = () => {
        if (value.trim()) {
            onAdd(type, value)
            setValue("")
        }
    }

    const handleBulkAdd = () => {
        const lines = bulkText.trim().split("\n").filter(Boolean)
        lines.forEach((line) => onAdd(bulkType, line.trim()))
        setBulkText("")
        setBulkMode(false)
    }

    if (bulkMode) {
        return (
            <div className="space-y-2">
                <div className="flex items-center justify-between">
                    <Select value={bulkType} onValueChange={(v) => setBulkType(v as DomainMatcher["type"])}>
                        <SelectTrigger className="w-[180px]">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {DOMAIN_TYPES.map((t) => (
                                <SelectItem key={t.value} value={t.value}>
                                    {t.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <Button variant="ghost" size="sm" onClick={() => setBulkMode(false)}>Single</Button>
                </div>
                <Textarea
                    placeholder="One domain per line..."
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
        <div className="flex flex-col md:flex-row gap-2 flex-1">
            <Select value={type} onValueChange={(v) => setType(v as DomainMatcher["type"])}>
                <SelectTrigger className="w-full md:w-[180px]">
                    <SelectValue />
                </SelectTrigger>
                <SelectContent>
                    {DOMAIN_TYPES.map((t) => (
                        <SelectItem key={t.value} value={t.value}>
                            {t.label}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>
            <div className="flex gap-2 flex-1">
                <Input
                    placeholder="example.com or geosite:google"
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
        </div>
    )
}

// ArrayInput and TagList are imported from shared component
