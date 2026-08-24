import { useState, useEffect, useMemo } from "react"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import {
    DropdownMenu,
    DropdownMenuTrigger,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu"
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { toast } from "sonner"
import { HiOutlinePlus, HiX, HiOutlineExclamation, HiChevronDown } from "react-icons/hi"
import { ArrayInput, InboundTagInput, TagList } from "@/components/ui/tag-input"
import { PRESET_RULE_TAGS } from "@/components/routing/routing-settings-card"
import { BalancingRuleDialog, STRATEGIES } from "@/components/routing/balancing-rule-dialog"
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

// ─── Conditions ───────────────────────────────────────────
// xray rejects a rule with no matcher (BuildCondition: "this rule has no
// effective fields"), so matchers are added one at a time rather than kept
// as thirteen permanently-empty fields.
type ConditionKey =
    | "domain" | "ip" | "port" | "network" | "protocol" | "attr"
    | "inbound" | "user" | "srcip" | "srcport" | "proc" | "localip" | "localport"

interface ConditionSpec {
    key: ConditionKey
    label: string
    scope: "destination" | "source"
    example: string
}

const CONDITIONS: ConditionSpec[] = [
    { key: "domain", label: "Domain / geosite", scope: "destination", example: "example.com" },
    { key: "ip", label: "IP / GeoIP", scope: "destination", example: "geoip:ir" },
    { key: "port", label: "Port", scope: "destination", example: "443" },
    { key: "network", label: "Network", scope: "destination", example: "tcp / udp" },
    { key: "protocol", label: "Protocol", scope: "destination", example: "tls, quic" },
    { key: "attr", label: "HTTP header", scope: "destination", example: "regex" },
    { key: "inbound", label: "Inbound", scope: "source", example: "vless-in" },
    { key: "user", label: "User email", scope: "source", example: "account" },
    { key: "srcip", label: "Source IP", scope: "source", example: "10.0.0.0/8" },
    { key: "srcport", label: "Source port", scope: "source", example: "1024-65535" },
    { key: "proc", label: "Process name", scope: "source", example: "local process" },
    { key: "localip", label: "Local IP", scope: "source", example: "bind address" },
    { key: "localport", label: "Local port", scope: "source", example: "80, 443" },
]

const CONDITION_LABEL: Record<ConditionKey, string> = CONDITIONS.reduce(
    (acc, c) => ({ ...acc, [c.key]: c.label }),
    {} as Record<ConditionKey, string>,
)

// Which rule fields each condition owns — used to detect and to clear it.
const CONDITION_FIELDS: Record<ConditionKey, (keyof RoutingRule)[]> = {
    domain: ["domain_rules"],
    ip: ["geoip_rules", "ipcidr_rules"],
    port: ["port_rules"],
    network: ["network_rules"],
    protocol: ["protocol_rules"],
    attr: ["attributes"],
    inbound: ["inbound_tags"],
    user: ["user_emails"],
    srcip: ["source_ips"],
    srcport: ["source_ports"],
    proc: ["process_names"],
    localip: ["local_ips"],
    localport: ["local_ports"],
}

function conditionHasData(key: ConditionKey, data: Partial<RoutingRule>): boolean {
    return CONDITION_FIELDS[key].some(field => {
        const v = data[field]
        if (Array.isArray(v)) return v.length > 0
        if (v && typeof v === "object") return Object.keys(v).length > 0
        return false
    })
}

// Preset-owned rules are regenerated by the Presets pane; editing one here
// detaches it from the preset that manages it.
function presetOwnerLabel(ruleTag: string | undefined): string | null {
    if (!ruleTag || !PRESET_RULE_TAGS.has(ruleTag)) return null
    if (ruleTag.startsWith("preset:block")) return "Blocking"
    if (ruleTag.startsWith("preset:direct")) return "Direct"
    if (ruleTag.startsWith("preset:ipv4")) return "IPv4 routing"
    if (ruleTag.startsWith("preset:warp")) return "WARP"
    return "Preset"
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
    /** Rules on this node in evaluation order — powers the position hint. */
    siblingRules?: RoutingRule[]
    /** Called after a balancer is created from inside this dialog. */
    onBalancerCreated?: () => void
    /** Opens the Presets pane for a preset-owned rule. */
    onOpenPresets?: () => void
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
    siblingRules = [],
    onBalancerCreated,
    onOpenPresets,
}: RoutingRuleDialogProps) {
    const [formData, setFormData] = useState<Partial<RoutingRule>>(emptyRule)
    const [isSaving, setIsSaving] = useState(false)
    const [activeConditions, setActiveConditions] = useState<ConditionKey[]>([])
    const [targetKind, setTargetKind] = useState<"outbound" | "balancer">("outbound")
    const [showJson, setShowJson] = useState(false)
    const [balancerDialogOpen, setBalancerDialogOpen] = useState(false)

    // Initialize form data
    useEffect(() => {
        if (!open) return
        setShowJson(false)
        if (mode === "edit" && rule) {
            // BE stores GeoIP and IP CIDR separately; the dialog presents
            // them as a single combined list. Merge on load (dedupe).
            const combinedIPs = Array.from(new Set([
                ...(rule.geoip_rules || []),
                ...(rule.ipcidr_rules || []),
            ]))
            const next: Partial<RoutingRule> = {
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
            }
            setFormData(next)
            setActiveConditions(CONDITIONS.filter(c => conditionHasData(c.key, next)).map(c => c.key))
            setTargetKind(rule.balancing_tag ? "balancer" : "outbound")
        } else {
            setFormData({ ...emptyRule, node_id: nodeId })
            setActiveConditions([])
            setTargetKind("outbound")
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

    const addCondition = (key: ConditionKey) => {
        setActiveConditions(prev => (prev.includes(key) ? prev : [...prev, key]))
    }

    const removeCondition = (key: ConditionKey) => {
        setActiveConditions(prev => prev.filter(k => k !== key))
        setFormData(prev => {
            const next = { ...prev }
            for (const field of CONDITION_FIELDS[key]) {
                if (field === "attributes") next.attributes = {}
                else (next[field] as unknown as string[]) = []
            }
            return next
        })
    }

    const matcherCount = activeConditions.filter(k => conditionHasData(k, formData)).length

    const targetTag = targetKind === "balancer" ? formData.balancing_tag : formData.outbound_tag
    const selectedBalancer = balancingRules.find(b => b.tag === formData.balancing_tag)

    // Dangling target: the rule points at something that no longer exists on the node.
    const danglingTarget =
        targetKind === "outbound"
            ? Boolean(formData.outbound_tag) && !outbounds.some(o => o.tag === formData.outbound_tag)
            : Boolean(formData.balancing_tag) && !selectedBalancer

    const invalidReason = (() => {
        const tag = formData.rule_tag?.trim() || ""
        if (!tag) return "Give the rule a tag"
        if (!RULE_TAG_RE.test(tag)) return "Tag: letters, numbers, - _ : . only"
        if (!targetTag) return targetKind === "balancer" ? "Pick a balancer to send to" : "Pick an outbound to send to"
        if (matcherCount === 0) return "Add a condition to save"
        return null
    })()

    const presetOwner = presetOwnerLabel(rule?.rule_tag)

    // Position in evaluation order — rules are matched top-down, so a rule's
    // meaning depends on what sits above it.
    const position = useMemo(() => {
        if (mode !== "edit" || !rule) return null
        const index = siblingRules.findIndex(r => r.id === rule.id)
        if (index < 0) return null
        return { index, total: siblingRules.length, before: siblingRules.slice(0, index).map(r => r.rule_tag) }
    }, [mode, rule, siblingRules])

    const handleSave = async () => {
        if (invalidReason) {
            toast.error(invalidReason)
            return
        }
        const tag = formData.rule_tag!.trim()

        // Split combined IP list into geoip vs cidr fields the BE expects.
        const { geoip, ipcidr } = splitGeoIPCIDR(formData.geoip_rules || [])
        const payload: Partial<RoutingRule> = {
            ...formData,
            rule_tag: tag,
            geoip_rules: geoip,
            ipcidr_rules: ipcidr,
            // Outbound and balancer are mutually exclusive at the BE.
            outbound_tag: targetKind === "outbound" ? formData.outbound_tag || "" : "",
            balancing_tag: targetKind === "balancer" ? formData.balancing_tag || "" : "",
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

    const availableConditions = CONDITIONS.filter(c => !activeConditions.includes(c.key))

    return (
        <>
            <ResponsiveDialog
                open={open}
                onOpenChange={onOpenChange}
                title={mode === "create" ? "Add routing rule" : "Edit routing rule"}
                description="Match traffic, then send it somewhere."
                onSave={handleSave}
                saveLabel={isSaving ? "Saving..." : mode === "create" ? "Add rule" : "Save changes"}
                saveDisabled={isSaving || Boolean(invalidReason)}
                headerExtra={
                    presetOwner ? (
                        <Popover>
                            <PopoverTrigger asChild>
                                <button
                                    type="button"
                                    className="inline-flex items-center gap-1.5 h-6 px-2 rounded-md text-[11.5px] border border-amber-500/35 bg-amber-500/[0.08] text-amber-300 hover:bg-amber-500/[0.14]"
                                >
                                    <HiOutlineExclamation className="w-3.5 h-3.5" />
                                    {presetOwner} preset
                                    <HiChevronDown className="w-3 h-3 opacity-70" />
                                </button>
                            </PopoverTrigger>
                            <PopoverContent align="end" className="w-[300px] border-amber-500/30">
                                <p className="text-[12px] leading-relaxed text-amber-200">
                                    Owned by the <b>{presetOwner}</b> preset. Saving here detaches the rule — the next
                                    preset save stops managing it.
                                </p>
                                {onOpenPresets && (
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        className="mt-3 h-7 text-xs"
                                        onClick={() => {
                                            onOpenChange(false)
                                            onOpenPresets()
                                        }}
                                    >
                                        Open the preset instead
                                    </Button>
                                )}
                            </PopoverContent>
                        </Popover>
                    ) : undefined
                }
                footerNote={
                    invalidReason ? (
                        <span className="text-amber-400">{invalidReason}</span>
                    ) : position ? (
                        <>
                            Rule {position.index + 1} of {position.total}
                            {position.before.length > 0 && (
                                <> · evaluated after <span className="font-mono">{position.before.join(", ")}</span></>
                            )}
                        </>
                    ) : (
                        <>New rule · appended last, evaluated after every rule above it</>
                    )
                }
            >
                {/* ═══ Identity ═══ */}
                <div className="space-y-3">
                    <SectionLabel>Identity</SectionLabel>
                    <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-4 sm:items-start">
                        <div className="space-y-2">
                            <Label>Rule tag <span className="text-red-400">*</span></Label>
                            <Input
                                className="font-mono"
                                placeholder="block-ads"
                                value={formData.rule_tag || ""}
                                onChange={(e) => updateField("rule_tag", e.target.value)}
                            />
                            <p className="text-[11px] text-muted-foreground">
                                Letters, numbers, <span className="font-mono">- _ : .</span> — unique on this node.
                                {position && <> Drag in the table to reorder.</>}
                            </p>
                        </div>
                        <div className="space-y-2">
                            <Label>Rule is active</Label>
                            <div className="flex items-center gap-3 h-9">
                                <Switch
                                    checked={formData.enabled ?? true}
                                    onCheckedChange={(c) => updateField("enabled", c)}
                                />
                                <span className="text-sm">{(formData.enabled ?? true) ? "Enabled" : "Disabled"}</span>
                            </div>
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
                </div>

                {/* ═══ Send to ═══ */}
                <div className="space-y-3">
                    <SectionLabel right="required">Send to</SectionLabel>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <TargetCard
                            active={targetKind === "outbound"}
                            title="An outbound"
                            subtitle="One fixed exit."
                            onSelect={() => {
                                setTargetKind("outbound")
                                setFormData(prev => ({ ...prev, balancing_tag: "" }))
                            }}
                        >
                            <Select
                                value={formData.outbound_tag || "__none"}
                                onValueChange={(v) => updateField("outbound_tag", v === "__none" ? "" : v)}
                            >
                                <SelectTrigger>
                                    <SelectValue placeholder="Select outbound" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="__none">Select outbound</SelectItem>
                                    {outbounds.map((o) => (
                                        // Managed rows have no id of their own, so the tag is the key.
                                        <SelectItem key={o.managed ? `managed:${o.tag}` : o.id} value={o.tag}>
                                            <span className="font-mono">{o.tag}</span>
                                            <span className="text-muted-foreground">
                                                {" "}
                                                ({o.managed && o.remark ? o.remark : o.protocol})
                                            </span>
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </TargetCard>

                        <TargetCard
                            active={targetKind === "balancer"}
                            title="A balancer"
                            subtitle="Several outbounds, picked per connection."
                            onSelect={() => {
                                setTargetKind("balancer")
                                setFormData(prev => ({ ...prev, outbound_tag: "" }))
                            }}
                        >
                            {balancingRules.length === 0 ? (
                                <div className="rounded-lg border border-dashed border-white/10 p-3 text-center">
                                    <p className="text-xs text-muted-foreground mb-2">No balancers on this node yet.</p>
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        className="h-7 text-xs"
                                        onClick={() => setBalancerDialogOpen(true)}
                                    >
                                        <HiOutlinePlus className="w-3.5 h-3.5 mr-1" />
                                        Create balancer
                                    </Button>
                                </div>
                            ) : (
                                <>
                                    <Select
                                        value={formData.balancing_tag || "__none"}
                                        onValueChange={(v) => updateField("balancing_tag", v === "__none" ? "" : v)}
                                    >
                                        <SelectTrigger>
                                            <SelectValue placeholder="Select balancer" />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="__none">Select balancer</SelectItem>
                                            {balancingRules.map((b) => (
                                                <SelectItem key={b.id} value={b.tag}>
                                                    <span className="font-mono">{b.tag}</span>
                                                    <span className="text-muted-foreground"> ({b.strategy})</span>
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                    {selectedBalancer && (
                                        <div className="flex flex-wrap items-center gap-1.5 mt-2">
                                            <Badge variant="outline" className="text-[10.5px] text-emerald-300 border-emerald-500/30 bg-emerald-500/[0.08]">
                                                {STRATEGIES.find(s => s.value === selectedBalancer.strategy)?.label.toLowerCase() ?? selectedBalancer.strategy}
                                            </Badge>
                                            <Badge variant="outline" className="text-[10.5px]">
                                                {selectedBalancer.outbound_selectors.length} outbound{selectedBalancer.outbound_selectors.length !== 1 ? "s" : ""}
                                            </Badge>
                                            {selectedBalancer.fallback_tag && (
                                                <Badge variant="outline" className="text-[10.5px]">
                                                    fallback <span className="font-mono ml-1">{selectedBalancer.fallback_tag}</span>
                                                </Badge>
                                            )}
                                        </div>
                                    )}
                                    <button
                                        type="button"
                                        onClick={() => setBalancerDialogOpen(true)}
                                        className="text-[11px] text-muted-foreground underline underline-offset-2 mt-2 hover:text-foreground"
                                    >
                                        Create another balancer
                                    </button>
                                </>
                            )}
                        </TargetCard>
                    </div>
                    {danglingTarget && (
                        <div className="flex gap-2 items-start rounded-lg border border-red-500/30 bg-red-500/[0.07] px-3 py-2 text-[12px] text-red-200">
                            <HiOutlineExclamation className="w-4 h-4 shrink-0 mt-0.5" />
                            <div>
                                <span className="font-mono">{targetTag}</span> no longer exists on this node. xray rejects
                                a rule pointing at a missing {targetKind}, so pick another target before saving.
                            </div>
                        </div>
                    )}
                </div>

                {/* ═══ When ═══ */}
                <div className="space-y-3">
                    <div className="flex items-center gap-2">
                        <SectionLabel right={matcherCount > 0 ? `${matcherCount} condition${matcherCount !== 1 ? "s" : ""}` : "at least one condition"}>
                            When
                        </SectionLabel>
                        <ConditionMenu options={availableConditions} onPick={addCondition} />
                    </div>

                    {activeConditions.length === 0 ? (
                        <div className="rounded-xl border border-dashed border-white/10 p-5 text-center">
                            <p className="text-[13px] font-semibold mb-1">No conditions yet</p>
                            <p className="text-xs text-muted-foreground mb-3">
                                A rule with no condition is rejected by xray, so this one can&apos;t be saved.
                                <br />Start with what you&apos;re matching on:
                            </p>
                            <div className="flex flex-wrap gap-1.5 justify-center">
                                {(["domain", "ip", "inbound", "port", "protocol"] as ConditionKey[]).map(k => (
                                    <Button key={k} variant="outline" size="sm" className="h-7 text-xs" onClick={() => addCondition(k)}>
                                        {CONDITION_LABEL[k]}
                                    </Button>
                                ))}
                            </div>
                        </div>
                    ) : (
                        <div className="space-y-2.5">
                            {activeConditions.map(key => {
                                const spec = CONDITIONS.find(c => c.key === key)!
                                return (
                                    <ConditionCard key={key} spec={spec} onRemove={() => removeCondition(key)}>
                                        {key === "domain" && (
                                            <div className="space-y-2">
                                                <DomainInput onAdd={addDomainRule} />
                                                <div className="flex flex-wrap gap-1">
                                                    {GEOSITE_PRESETS.map((preset) => (
                                                        <Button
                                                            key={preset.value}
                                                            type="button"
                                                            variant="outline"
                                                            size="sm"
                                                            className="text-xs h-9 md:h-7"
                                                            onClick={() => addDomainRule("plain", preset.value)}
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
                                        )}

                                        {key === "ip" && (
                                            <div className="space-y-2">
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
                                        )}

                                        {key === "port" && (
                                            <div className="space-y-2">
                                                <ArrayInput placeholder="80, 443, 8000-9000" onAdd={(v) => addToArray("port_rules", v)} />
                                                <TagList items={formData.port_rules || []} onRemove={(i) => removeFromArray("port_rules", i)} />
                                            </div>
                                        )}

                                        {key === "network" && (
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
                                        )}

                                        {key === "protocol" && (
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
                                        )}

                                        {key === "attr" && (
                                            <AttributeInput
                                                attributes={formData.attributes || {}}
                                                onChange={(next) => setFormData(prev => ({ ...prev, attributes: next }))}
                                            />
                                        )}

                                        {key === "inbound" && (
                                            <div className="space-y-2">
                                                <InboundTagInput
                                                    suggestions={inbounds.map((i) => ({ tag: i.tag, label: `${i.tag} (${i.protocol}:${i.port})` }))}
                                                    selected={formData.inbound_tags || []}
                                                    onAdd={(v) => addToArray("inbound_tags", v)}
                                                    onRemove={(i) => removeFromArray("inbound_tags", i)}
                                                />
                                                <p className="text-[11px] text-muted-foreground">
                                                    Pick a configured inbound, or type a synthetic tag (e.g. <code>dns-in</code> for DNS queries).
                                                </p>
                                            </div>
                                        )}

                                        {key === "user" && (
                                            <div className="space-y-2">
                                                <ArrayInput placeholder="user@example.com" onAdd={(v) => addToArray("user_emails", v)} />
                                                <TagList items={formData.user_emails || []} onRemove={(i) => removeFromArray("user_emails", i)} />
                                            </div>
                                        )}

                                        {key === "srcip" && (
                                            <div className="space-y-2">
                                                <ArrayInput placeholder="10.0.0.0/8" onAdd={(v) => addToArray("source_ips", v)} />
                                                <TagList items={formData.source_ips || []} onRemove={(i) => removeFromArray("source_ips", i)} />
                                            </div>
                                        )}

                                        {key === "srcport" && (
                                            <div className="space-y-2">
                                                <ArrayInput placeholder="1024-65535" onAdd={(v) => addToArray("source_ports", v)} />
                                                <TagList items={formData.source_ports || []} onRemove={(i) => removeFromArray("source_ports", i)} />
                                            </div>
                                        )}

                                        {key === "proc" && (
                                            <div className="space-y-2">
                                                <ArrayInput placeholder="Process name or path" onAdd={(v) => addToArray("process_names", v)} />
                                                <TagList items={formData.process_names || []} onRemove={(i) => removeFromArray("process_names", i)} />
                                            </div>
                                        )}

                                        {key === "localip" && (
                                            <div className="space-y-2">
                                                <ArrayInput placeholder="geoip:cn or 10.0.0.0/8" onAdd={(v) => addToArray("local_ips", v)} />
                                                <TagList items={formData.local_ips || []} onRemove={(i) => removeFromArray("local_ips", i)} />
                                            </div>
                                        )}

                                        {key === "localport" && (
                                            <div className="space-y-2">
                                                <ArrayInput placeholder="80, 443, 1000-2000" onAdd={(v) => addToArray("local_ports", v)} />
                                                <TagList items={formData.local_ports || []} onRemove={(i) => removeFromArray("local_ports", i)} />
                                            </div>
                                        )}
                                    </ConditionCard>
                                )
                            })}
                        </div>
                    )}
                </div>

                {/* ═══ Result ═══ */}
                {matcherCount > 0 && targetTag && (
                    <div className="rounded-xl border border-white/[0.06] bg-white/[0.02] px-3.5 py-3">
                        <p className="text-[10.5px] uppercase tracking-[0.12em] text-muted-foreground/70 mb-1.5">Result</p>
                        <p className="text-[13px] leading-relaxed">
                            {describeRule(formData, targetKind, selectedBalancer)}
                        </p>
                        <Button
                            variant="outline"
                            size="sm"
                            className="h-7 text-xs mt-2"
                            onClick={() => setShowJson(v => !v)}
                        >
                            {showJson ? "Hide xray JSON" : "Show xray JSON"}
                        </Button>
                        {showJson && (
                            <pre className="mt-2 text-[12px] font-mono text-muted-foreground bg-black/35 border border-white/[0.06] rounded-lg px-3 py-2.5 overflow-x-auto">
                                {buildXrayFragment(formData, targetKind)}
                            </pre>
                        )}
                    </div>
                )}
            </ResponsiveDialog>

            {/* Create a balancer without losing the rule being written. */}
            <BalancingRuleDialog
                open={balancerDialogOpen}
                onOpenChange={setBalancerDialogOpen}
                mode="create"
                rule={null}
                nodeId={nodeId}
                outbounds={outbounds}
                existingBalancers={balancingRules}
                compact
                onSaved={(tag) => {
                    setTargetKind("balancer")
                    setFormData(prev => ({ ...prev, outbound_tag: "", balancing_tag: tag }))
                    onBalancerCreated?.()
                }}
            />
        </>
    )
}

// ─── Presentation helpers ─────────────────────────────────

function SectionLabel({ children, right }: { children: React.ReactNode; right?: string }) {
    return (
        <div className="flex items-center gap-2 flex-1 min-w-0">
            <span className="text-[10.5px] uppercase tracking-[0.12em] text-muted-foreground/70 font-medium">{children}</span>
            {right && <span className="text-[11px] text-muted-foreground">{right}</span>}
            <span className="flex-1 h-px bg-white/[0.06]" />
        </div>
    )
}

function TargetCard({
    active,
    title,
    subtitle,
    onSelect,
    children,
}: {
    active: boolean
    title: string
    subtitle: string
    onSelect: () => void
    children: React.ReactNode
}) {
    return (
        <div
            onClick={onSelect}
            className={`rounded-xl border p-3.5 cursor-pointer transition-colors ${active ? "border-primary/45 bg-white/[0.05]" : "border-white/10 bg-white/[0.012] hover:bg-white/[0.03]"
                }`}
        >
            <div className="flex items-center gap-2">
                <span className={`w-3.5 h-3.5 rounded-full border shrink-0 ${active ? "border-primary bg-primary" : "border-white/25"}`} />
                <span className="text-[13px] font-semibold">{title}</span>
            </div>
            <p className="text-[11px] text-muted-foreground mt-0.5 mb-2.5 ml-[22px]">{subtitle}</p>
            {/* Controls stay live while the card is unselected: acting on one picks the
                card in the same click, instead of making the user select then act. */}
            <div className={`ml-[22px] ${active ? "" : "opacity-50"}`}>{children}</div>
        </div>
    )
}

function ConditionCard({
    spec,
    onRemove,
    children,
}: {
    spec: ConditionSpec
    onRemove: () => void
    children: React.ReactNode
}) {
    return (
        <div className="rounded-xl border border-white/[0.06] bg-white/[0.014] px-3.5 py-3">
            <div className="flex items-center gap-2 mb-2.5">
                <span className="text-[12.5px] font-semibold">{spec.label}</span>
                <Badge variant="outline" className="text-[10px] text-muted-foreground">{spec.scope}</Badge>
                <button
                    type="button"
                    onClick={onRemove}
                    className="ml-auto p-1 -mr-1 rounded text-muted-foreground hover:text-red-400 hover:bg-destructive/15"
                    aria-label={`Remove ${spec.label} condition`}
                >
                    <HiX className="w-3.5 h-3.5" />
                </button>
            </div>
            {children}
        </div>
    )
}

function ConditionMenu({ options, onPick }: { options: ConditionSpec[]; onPick: (key: ConditionKey) => void }) {
    if (options.length === 0) return null
    const destination = options.filter(o => o.scope === "destination")
    const source = options.filter(o => o.scope === "source")
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="h-7 text-xs shrink-0">
                    <HiOutlinePlus className="w-3.5 h-3.5 mr-1" />
                    Add condition
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-[300px]">
                {destination.length > 0 && (
                    <>
                        <DropdownMenuLabel className="text-[10px] uppercase tracking-wider text-muted-foreground">
                            Destination — what is being reached
                        </DropdownMenuLabel>
                        {destination.map(o => (
                            <DropdownMenuItem key={o.key} onSelect={() => onPick(o.key)}>
                                {o.label}
                                <span className="ml-auto text-[11px] text-muted-foreground">{o.example}</span>
                            </DropdownMenuItem>
                        ))}
                    </>
                )}
                {destination.length > 0 && source.length > 0 && <DropdownMenuSeparator />}
                {source.length > 0 && (
                    <>
                        <DropdownMenuLabel className="text-[10px] uppercase tracking-wider text-muted-foreground">
                            Source — who is asking
                        </DropdownMenuLabel>
                        {source.map(o => (
                            <DropdownMenuItem key={o.key} onSelect={() => onPick(o.key)}>
                                {o.label}
                                <span className="ml-auto text-[11px] text-muted-foreground">{o.example}</span>
                            </DropdownMenuItem>
                        ))}
                    </>
                )}
            </DropdownMenuContent>
        </DropdownMenu>
    )
}

function AttributeInput({
    attributes,
    onChange,
}: {
    attributes: Record<string, string>
    onChange: (next: Record<string, string>) => void
}) {
    const [key, setKey] = useState("")
    const [val, setVal] = useState("")

    const add = () => {
        const k = key.trim()
        const v = val.trim()
        if (!k || !v) return
        onChange({ ...attributes, [k]: v })
        setKey("")
        setVal("")
    }

    return (
        <div className="space-y-2">
            <p className="text-[11px] text-muted-foreground">Match HTTP headers — key is the header name, value a regex.</p>
            <div className="flex gap-2">
                <Input placeholder="Header name" value={key} onChange={(e) => setKey(e.target.value)} className="flex-1" />
                <Input
                    placeholder="Regex pattern"
                    value={val}
                    onChange={(e) => setVal(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), add())}
                    className="flex-1"
                />
                <Button type="button" size="icon" variant="outline" onClick={add}>
                    <HiOutlinePlus className="w-4 h-4" />
                </Button>
            </div>
            {Object.keys(attributes).length > 0 && (
                <div className="flex flex-wrap gap-1">
                    {Object.entries(attributes).map(([k, v]) => (
                        <Badge key={k} variant="secondary" className="gap-1">
                            <span className="text-xs opacity-60">{k}:</span>
                            {v}
                            <button
                                onClick={() => {
                                    const next = { ...attributes }
                                    delete next[k]
                                    onChange(next)
                                }}
                                className="ml-1 p-1 -mr-1 rounded hover:bg-destructive/20 hover:text-red-500"
                            >
                                <HiX className="w-3 h-3" />
                            </button>
                        </Badge>
                    ))}
                </div>
            )}
        </div>
    )
}

/** Plain-English read-back of the rule — catches the mistake the form can't. */
function describeRule(
    data: Partial<RoutingRule>,
    targetKind: "outbound" | "balancer",
    balancer?: BalancingRule,
): string {
    const from: string[] = []
    if (data.inbound_tags?.length) from.push(`inbound ${data.inbound_tags.join(", ")}`)
    if (data.source_ips?.length) from.push(`source IP ${data.source_ips.join(", ")}`)
    if (data.source_ports?.length) from.push(`source port ${data.source_ports.join(", ")}`)
    if (data.user_emails?.length) from.push(`user ${data.user_emails.join(", ")}`)
    if (data.process_names?.length) from.push(`process ${data.process_names.join(", ")}`)

    const to: string[] = []
    if (data.domain_rules?.length) to.push(`${data.domain_rules.length} domain matcher${data.domain_rules.length !== 1 ? "s" : ""}`)
    if (data.geoip_rules?.length) to.push(`${data.geoip_rules.length} IP matcher${data.geoip_rules.length !== 1 ? "s" : ""}`)
    if (data.port_rules?.length) to.push(`port ${data.port_rules.join(", ")}`)
    if (data.protocol_rules?.length) to.push(`protocol ${data.protocol_rules.join(", ")}`)
    if (data.attributes && Object.keys(data.attributes).length) to.push(`${Object.keys(data.attributes).length} header matcher(s)`)

    const parts: string[] = ["Traffic"]
    if (from.length) parts.push(`from ${from.join(" and ")}`)
    if (data.network_rules?.length) parts.push(`over ${data.network_rules.join(", ")}`)
    if (to.length) parts.push(`going to ${to.join(" and ")}`)
    if (!from.length && !to.length && !data.network_rules?.length) parts.push("of any kind")

    const target =
        targetKind === "balancer"
            ? `balancer ${data.balancing_tag}${balancer
                ? ` — ${STRATEGIES.find(s => s.value === balancer.strategy)?.label.toLowerCase() ?? balancer.strategy} across ${balancer.outbound_selectors.length} outbound${balancer.outbound_selectors.length !== 1 ? "s" : ""}${balancer.fallback_tag ? `, falling back to ${balancer.fallback_tag}` : ""}`
                : ""
            }`
            : `outbound ${data.outbound_tag}`

    return `${parts.join(" ")} is sent to ${target}.`
}

/** The xray routing-rule fragment this form produces — for operators who read config, not forms. */
function buildXrayFragment(data: Partial<RoutingRule>, targetKind: "outbound" | "balancer"): string {
    const { geoip, ipcidr } = splitGeoIPCIDR(data.geoip_rules || [])
    const fragment: Record<string, unknown> = { type: "field", ruleTag: data.rule_tag || "" }
    if (data.domain_rules?.length) {
        fragment.domain = data.domain_rules.map(d => (d.type === "plain" ? d.value : `${d.type}:${d.value}`))
    }
    const ip = [...geoip, ...ipcidr]
    if (ip.length) fragment.ip = ip
    if (data.port_rules?.length) fragment.port = data.port_rules.join(",")
    if (data.network_rules?.length) fragment.network = data.network_rules.join(",")
    if (data.protocol_rules?.length) fragment.protocol = data.protocol_rules
    if (data.attributes && Object.keys(data.attributes).length) fragment.attrs = data.attributes
    if (data.inbound_tags?.length) fragment.inboundTag = data.inbound_tags
    if (data.user_emails?.length) fragment.user = data.user_emails
    if (data.source_ips?.length) fragment.source = data.source_ips
    if (data.source_ports?.length) fragment.sourcePort = data.source_ports.join(",")
    if (data.process_names?.length) fragment.process = data.process_names
    if (targetKind === "balancer") fragment.balancerTag = data.balancing_tag || ""
    else fragment.outboundTag = data.outbound_tag || ""
    return JSON.stringify(fragment, null, 2)
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
