import React, { useState, useEffect, useCallback, useRef, useMemo } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { toast } from "sonner"
import {
    HiOutlineSave,
    HiOutlineRefresh,
    HiOutlineShieldCheck,
    HiOutlineGlobeAlt,
    HiOutlineBan,
    HiOutlineArrowRight,
} from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { ArrayInput } from "@/components/ui/tag-input"
import {
    getNodeRoutingSettings,
    updateNodeRoutingSettings,
    addNodeRoutingRule,
    updateNodeRoutingRule,
    deleteNodeRoutingRule,
    pushNodeConfig,
} from "@/lib/admin-api"
import type { RoutingSettings, RoutingRule, Outbound } from "@/lib/types"

// ─── Country flag helper ─────────────────────────────────────
const COUNTRY_FLAGS: Record<string, string> = {
    ir: "🇮🇷", cn: "🇨🇳", ru: "🇷🇺", cu: "🇨🇺", sy: "🇸🇾", private: "🏠",
}

function getTagLabel(tag: string): string {
    const match = tag.match(/^geo(?:ip|site):(\w+)$/)
    if (match) {
        const code = match[1].toLowerCase()
        const flag = COUNTRY_FLAGS[code]
        if (flag) return `${flag} ${tag}`
    }
    return tag
}

// ─── Preset Constants ────────────────────────────────────────
const BLOCK_IP_PRESETS = [
    { value: "geoip:private", label: "🏠 Private" },
    { value: "geoip:ir", label: "🇮🇷 Iran" },
    { value: "geoip:cn", label: "🇨🇳 China" },
    { value: "geoip:ru", label: "🇷🇺 Russia" },
    { value: "geoip:cu", label: "🇨🇺 Cuba" },
    { value: "geoip:sy", label: "🇸🇾 Syria" },
] as const

const BLOCK_DOMAIN_PRESETS = [
    { value: "geosite:category-ads-all", label: "🚫 Ads" },
    { value: "geosite:malware", label: "🦠 Malware" },
    { value: "geosite:ir", label: "🇮🇷 Iran" },
    { value: "geosite:cn", label: "🇨🇳 China" },
    { value: "ext:iran.dat:ads", label: "🇮🇷 Iran Ads" },
    { value: "ext:iran.dat:ir", label: "🇮🇷 Iran Sites" },
    { value: "regexp:.*\\.ir$", label: "*.ir" },
] as const

const DIRECT_IP_PRESETS = [
    { value: "geoip:private", label: "🏠 Private" },
    { value: "geoip:ir", label: "🇮🇷 Iran" },
    { value: "geoip:cn", label: "🇨🇳 China" },
] as const

const DIRECT_DOMAIN_PRESETS = [
    { value: "geosite:telegram", label: "✈️ Telegram" },
    { value: "geosite:reddit", label: "📱 Reddit" },
    { value: "geosite:openai", label: "🤖 OpenAI" },
    { value: "geosite:google-play", label: "▶️ Google Play" },
    { value: "geosite:microsoft", label: "🪟 Microsoft" },
    { value: "geosite:apple", label: "🍎 Apple" },
] as const

// ─── Strategy Options ────────────────────────────────────────
const DOMAIN_STRATEGIES = [
    { value: "AsIs", label: "AsIs", description: "Use domain as-is without resolving" },
    { value: "IPIfNonMatch", label: "IPIfNonMatch", description: "Resolve to IP if no domain rule matches" },
    { value: "IPOnDemand", label: "IPOnDemand", description: "Resolve to IP when needed by any rule" },
] as const

// ─── Preset Rule Tags ───────────────────────────────────────
export const PRESET_RULE_TAGS = new Set([
    "preset:block-bittorrent",
    "preset:block-ips",
    "preset:block-domains",
    "preset:direct-ips",
    "preset:direct-domains",
    "preset:ipv4-routing",
    "preset:warp-domains",
    "preset:warp-ips",
])

// inferDomainMatcher: prefixed forms (geosite:/ext:/etc) → "plain" (BE
// emits as-is, xray parses the prefix). Bare hostnames → "domain" (suffix
// match); "plain" would substring-match, e.g. google.com ⊂ google.computer.
const KNOWN_DOMAIN_PREFIXES = /^(geosite:|ext:|ext-domain:|domain:|full:|regexp:|keyword:|dotless:)/
function inferDomainMatcher(value: string): { type: "plain" | "domain"; value: string } {
    const v = value.trim()
    if (!v) return { type: "plain", value: v }
    if (KNOWN_DOMAIN_PREFIXES.test(v)) return { type: "plain", value: v }
    return { type: "domain", value: v }
}

// Split combined GeoIP/CIDR list into the two domain fields the BE expects.
// xray-core's JSON `ip` array accepts both prefixed (geoip:cn, ext:...) and
// plain CIDR; BE merges them on emit. Splitting at FE keeps the data model
// honest and avoids feeding plain CIDRs into the grpc rule path.
function splitGeoIPCIDR(items: string[]): { geoip: string[]; ipcidr: string[] } {
    const geoip: string[] = []
    const ipcidr: string[] = []
    for (const raw of items) {
        const v = raw.trim()
        if (!v) continue
        if (v.startsWith("geoip:") || v.startsWith("ext:") || v.startsWith("ext-ip:")) {
            geoip.push(v)
        } else {
            ipcidr.push(v)
        }
    }
    return { geoip, ipcidr }
}

// ─── Generate Rules from Settings ───────────────────────────
export function generatePresetRules(settings: RoutingSettings, nodeId: number): Partial<RoutingRule>[] {
    const rules: Partial<RoutingRule>[] = []

    if (settings.block_bittorrent) {
        rules.push({
            node_id: nodeId,
            rule_tag: "preset:block-bittorrent",
            remark: "Block BitTorrent protocol",
            priority: -100,
            enabled: true,
            outbound_tag: "blocked",
            protocol_rules: ["bittorrent"],
        })
    }

    if (settings.block_ips?.length > 0) {
        const { geoip, ipcidr } = splitGeoIPCIDR(settings.block_ips)
        rules.push({
            node_id: nodeId,
            rule_tag: "preset:block-ips",
            remark: "Block IPs",
            priority: -90,
            enabled: true,
            outbound_tag: "blocked",
            geoip_rules: geoip,
            ipcidr_rules: ipcidr,
        })
    }

    if (settings.block_domains?.length > 0) {
        rules.push({
            node_id: nodeId,
            rule_tag: "preset:block-domains",
            remark: "Block Domains",
            priority: -80,
            enabled: true,
            outbound_tag: "blocked",
            domain_rules: settings.block_domains.map(inferDomainMatcher),
        })
    }

    if (settings.direct_ips?.length > 0) {
        const { geoip, ipcidr } = splitGeoIPCIDR(settings.direct_ips)
        rules.push({
            node_id: nodeId,
            rule_tag: "preset:direct-ips",
            remark: "Direct IPs",
            priority: -70,
            enabled: true,
            outbound_tag: "direct",
            geoip_rules: geoip,
            ipcidr_rules: ipcidr,
        })
    }

    if (settings.direct_domains?.length > 0) {
        rules.push({
            node_id: nodeId,
            rule_tag: "preset:direct-domains",
            remark: "Direct Domains",
            priority: -60,
            enabled: true,
            outbound_tag: "direct",
            domain_rules: settings.direct_domains.map(inferDomainMatcher),
        })
    }

    if (settings.ipv4_routing?.length > 0) {
        rules.push({
            node_id: nodeId,
            rule_tag: "preset:ipv4-routing",
            remark: "IPv4 Routing",
            priority: -50,
            enabled: true,
            outbound_tag: "IPv4",
            domain_rules: settings.ipv4_routing.map(inferDomainMatcher),
        })
    }

    if (settings.warp_enabled) {
        if (settings.warp_domains?.length > 0) {
            rules.push({
                node_id: nodeId,
                rule_tag: "preset:warp-domains",
                remark: "WARP Domains",
                priority: -40,
                enabled: true,
                outbound_tag: "warp",
                domain_rules: settings.warp_domains.map(inferDomainMatcher),
            })
        }

        if (settings.warp_ips?.length > 0) {
            const { geoip, ipcidr } = splitGeoIPCIDR(settings.warp_ips)
            rules.push({
                node_id: nodeId,
                rule_tag: "preset:warp-ips",
                remark: "WARP IPs",
                priority: -30,
                enabled: true,
                outbound_tag: "warp",
                geoip_rules: geoip,
                ipcidr_rules: ipcidr,
            })
        }
    }

    return rules
}

// ─── Default Settings ────────────────────────────────────────
const DEFAULT_SETTINGS: RoutingSettings = {
    domain_strategy: "IPIfNonMatch",
    block_bittorrent: false,
    block_ips: [],
    block_domains: [],
    direct_ips: [],
    direct_domains: [],
    ipv4_routing: [],
    warp_enabled: false,
    warp_domains: [],
    warp_ips: [],
    outbound_test_url: "",
}

// ─── Tag Chip ────────────────────────────────────────────────
function TagChip({ label, onRemove }: { label: string; onRemove: () => void }) {
    return (
        <Badge
            variant="secondary"
            className="gap-1 font-mono text-[11px] h-6 px-2 group/tag transition-colors"
        >
            {getTagLabel(label)}
            <button
                onClick={onRemove}
                className="ml-0.5 p-0.5 -mr-1 rounded opacity-40 group-hover/tag:opacity-100 hover:bg-destructive/20 hover:text-red-400 transition-all"
                aria-label={`Remove ${label}`}
            >
                <svg className="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
            </button>
        </Badge>
    )
}

// ─── Preset Row ──────────────────────────────────────────────
function PresetRow({
    presets,
    items,
    onAdd,
}: {
    presets: readonly { value: string; label: string }[]
    items: string[]
    onAdd: (value: string) => void
}) {
    const available = presets.filter((p) => !items.includes(p.value))
    if (available.length === 0) return null

    return (
        <div className="flex flex-wrap gap-1">
            {available.map((preset) => (
                <button
                    key={preset.value}
                    type="button"
                    className="text-[11px] px-2 py-0.5 rounded-md border border-white/10 text-muted-foreground hover:text-foreground hover:border-white/20 hover:bg-white/5 transition-all"
                    onClick={() => onAdd(preset.value)}
                >
                    + {preset.label}
                </button>
            ))}
        </div>
    )
}

// ─── Settings Section ────────────────────────────────────────
function SettingsSection({
    label,
    items,
    presets,
    onAdd,
    onRemove,
    placeholder,
}: {
    label: string
    items: string[]
    presets?: readonly { value: string; label: string }[]
    onAdd: (value: string) => void
    onRemove: (index: number) => void
    placeholder: string
}) {
    return (
        <div className="space-y-2">
            <div className="flex items-center justify-between">
                <Label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{label}</Label>
                {items.length > 0 && (
                    <span className="text-[10px] text-muted-foreground/60">{items.length}</span>
                )}
            </div>
            <ArrayInput placeholder={placeholder} onAdd={onAdd} />
            {presets && <PresetRow presets={presets} items={items} onAdd={onAdd} />}
            {items.length > 0 && (
                <div className="flex flex-wrap gap-1">
                    {items.map((item, i) => (
                        <TagChip key={`${item}-${i}`} label={item} onRemove={() => onRemove(i)} />
                    ))}
                </div>
            )}
        </div>
    )
}

// ─── Category Card ───────────────────────────────────────────
type CategoryColor = "red" | "emerald" | "blue" | "orange"

const COLOR_MAP: Record<CategoryColor, { border: string; bg: string; icon: string }> = {
    red: { border: "border-l-red-500/50", bg: "bg-red-500/[0.03]", icon: "text-red-400" },
    emerald: { border: "border-l-emerald-500/50", bg: "bg-emerald-500/[0.03]", icon: "text-emerald-400" },
    blue: { border: "border-l-blue-500/50", bg: "bg-blue-500/[0.03]", icon: "text-blue-400" },
    orange: { border: "border-l-orange-500/50", bg: "bg-orange-500/[0.03]", icon: "text-orange-400" },
}

function CategoryCard({
    title,
    icon,
    color,
    count,
    children,
}: {
    title: string
    icon: React.ReactNode
    color: CategoryColor
    count?: number
    children: React.ReactNode
}) {
    const colors = COLOR_MAP[color]

    return (
        <div className={`rounded-lg border border-white/[0.06] ${colors.bg} ${colors.border} border-l-[3px] p-4 space-y-4`}>
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <span className={colors.icon}>{icon}</span>
                    <span className="text-sm font-semibold">{title}</span>
                </div>
                {count !== undefined && count > 0 && (
                    <Badge variant="secondary" className="text-[10px] h-5 px-1.5 font-mono">
                        {count}
                    </Badge>
                )}
            </div>
            {children}
        </div>
    )
}

// ─── Main Component ──────────────────────────────────────────
interface RoutingSettingsCardProps {
    nodeId: number
    existingRules?: RoutingRule[]
    outbounds?: Outbound[]
    onSettingsSaved?: () => void
    onPresetRulesChanged?: (rules: Partial<RoutingRule>[]) => void
}

export function RoutingSettingsCard({ nodeId, existingRules, outbounds, onSettingsSaved, onPresetRulesChanged }: RoutingSettingsCardProps) {
    const [settings, setSettings] = useState<RoutingSettings>(DEFAULT_SETTINGS)
    const [originalSettings, setOriginalSettings] = useState<RoutingSettings>(DEFAULT_SETTINGS)
    const [isLoading, setIsLoading] = useState(true)
    const [isSaving, setIsSaving] = useState(false)
    const hasLoadedRef = useRef(false)

    const isDirty = JSON.stringify(settings) !== JSON.stringify(originalSettings)

    // Load settings
    const loadSettings = useCallback(async () => {
        try {
            setIsLoading(true)
            const res = await getNodeRoutingSettings(nodeId)
            if (res.success && res.data) {
                const normalized: RoutingSettings = {
                    ...DEFAULT_SETTINGS,
                    ...res.data,
                    block_ips: res.data.block_ips || [],
                    block_domains: res.data.block_domains || [],
                    direct_ips: res.data.direct_ips || [],
                    direct_domains: res.data.direct_domains || [],
                    ipv4_routing: res.data.ipv4_routing || [],
                    warp_domains: res.data.warp_domains || [],
                    warp_ips: res.data.warp_ips || [],
                }
                setSettings(normalized)
                setOriginalSettings(normalized)
            }
        } catch {
            toast.error("Failed to load routing settings")
        } finally {
            setIsLoading(false)
        }
    }, [nodeId])

    useEffect(() => {
        if (!hasLoadedRef.current) {
            hasLoadedRef.current = true
            loadSettings()
        }
    }, [loadSettings])

    // Emit generated rules to parent whenever settings change
    const generatedRules = useMemo(
        () => generatePresetRules(settings, nodeId),
        [settings, nodeId]
    )

    useEffect(() => {
        if (isDirty) {
            onPresetRulesChanged?.(generatedRules)
        } else {
            onPresetRulesChanged?.([])
        }
    }, [isDirty, generatedRules, onPresetRulesChanged])

    // Save settings + create/update/delete rules via CRUD API
    const handleSave = async () => {
        try {
            setIsSaving(true)

            // 1. Save general settings to JSONB
            const res = await updateNodeRoutingSettings(nodeId, settings)
            if (!res.success) {
                toast.error("Failed to save routing settings")
                return
            }

            // 2. Compute desired rules
            const desiredRules = generatePresetRules(settings, nodeId)
            const desiredTags = new Set(desiredRules.map(r => r.rule_tag))

            // 3. Find existing preset rules in DB (by tag)
            const existingPreset = (existingRules || []).filter(r => PRESET_RULE_TAGS.has(r.rule_tag))

            // 4. Create or update rules (skip individual config pushes — we push once at the end)
            for (const desired of desiredRules) {
                const existing = existingPreset.find(r => r.rule_tag === desired.rule_tag)
                if (existing) {
                    await updateNodeRoutingRule(nodeId, existing.id, { ...desired, id: existing.id }, true)
                } else {
                    await addNodeRoutingRule(nodeId, desired, true)
                }
            }

            // 5. Delete rules no longer needed
            for (const existing of existingPreset) {
                if (!desiredTags.has(existing.rule_tag)) {
                    await deleteNodeRoutingRule(nodeId, existing.id, true)
                }
            }

            // 6. Push config to agent now that all rules are synced
            await pushNodeConfig(nodeId)

            // 7. Update local state
            const normalized: RoutingSettings = {
                ...DEFAULT_SETTINGS,
                ...res.data,
                block_ips: res.data?.block_ips || [],
                block_domains: res.data?.block_domains || [],
                direct_ips: res.data?.direct_ips || [],
                direct_domains: res.data?.direct_domains || [],
                ipv4_routing: res.data?.ipv4_routing || [],
                warp_domains: res.data?.warp_domains || [],
                warp_ips: res.data?.warp_ips || [],
            }
            setSettings(normalized)
            setOriginalSettings(normalized)

            toast.success("Routing settings saved", {
                description: "Config will be synced to the node automatically",
            })
            onSettingsSaved?.()
        } catch {
            toast.error("Failed to save routing settings")
        } finally {
            setIsSaving(false)
        }
    }

    const handleReset = () => {
        setSettings(originalSettings)
    }

    // Field helpers
    const updateField = <K extends keyof RoutingSettings>(key: K, value: RoutingSettings[K]) => {
        setSettings((prev) => ({ ...prev, [key]: value }))
    }

    const addToArray = (field: keyof RoutingSettings, value: string) => {
        const current = (settings[field] as string[]) || []
        if (!current.includes(value.trim())) {
            updateField(field, [...current, value.trim()] as never)
        }
    }

    const removeFromArray = (field: keyof RoutingSettings, index: number) => {
        const current = [...((settings[field] as string[]) || [])]
        current.splice(index, 1)
        updateField(field, current as never)
    }

    // Count helpers
    const blockCount = (settings.block_bittorrent ? 1 : 0) + settings.block_ips.length + settings.block_domains.length
    const directCount = settings.direct_ips.length + settings.direct_domains.length
    const ipv4Count = settings.ipv4_routing.length
    const warpCount = settings.warp_enabled ? settings.warp_domains.length + settings.warp_ips.length : 0

    if (isLoading) {
        return (
            <Card className="border border-white/5 bg-card/50 backdrop-blur-sm mb-6">
                <CardHeader className="pb-4">
                    <div className="flex items-center justify-between">
                        <Skeleton className="h-6 w-48" />
                        <Skeleton className="h-8 w-20" />
                    </div>
                </CardHeader>
                <CardContent className="space-y-4">
                    <div className="grid grid-cols-3 gap-4">
                        <Skeleton className="h-10 w-full" />
                        <Skeleton className="h-10 w-full" />
                        <Skeleton className="h-10 w-full" />
                    </div>
                    <div className="grid grid-cols-2 gap-4">
                        <Skeleton className="h-40 w-full" />
                        <Skeleton className="h-40 w-full" />
                    </div>
                </CardContent>
            </Card>
        )
    }

    return (
        <Card className="border border-white/5 bg-card/50 backdrop-blur-sm mb-6">
            {/* ═══ Header ═══ */}
            <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                        <HiOutlineShieldCheck className="w-5 h-5 text-primary" />
                        <CardTitle className="text-base font-semibold">Routing Configuration</CardTitle>
                    </div>
                    {isDirty && (
                        <div className="flex items-center gap-2 animate-in fade-in slide-in-from-right-2 duration-200">
                            <Button
                                variant="ghost"
                                size="sm"
                                onClick={handleReset}
                                disabled={isSaving}
                                className="h-8 text-xs"
                            >
                                <HiOutlineRefresh className="w-3.5 h-3.5 mr-1.5" />
                                Reset
                            </Button>
                            <Button
                                size="sm"
                                onClick={handleSave}
                                disabled={isSaving}
                                className="h-8 text-xs"
                            >
                                {isSaving ? (
                                    <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                                ) : (
                                    <HiOutlineSave className="w-3.5 h-3.5 mr-1.5" />
                                )}
                                {isSaving ? "Saving..." : "Save Changes"}
                            </Button>
                        </div>
                    )}
                </div>
            </CardHeader>

            <CardContent className="space-y-5">
                {/* ═══ General Settings ═══ */}
                <div className="flex flex-col sm:flex-row gap-4">
                    <div className="space-y-1.5 max-w-xs">
                        <Label className="text-xs text-muted-foreground">Domain Strategy</Label>
                        <Select
                            value={settings.domain_strategy || "IPIfNonMatch"}
                            onValueChange={(v) => updateField("domain_strategy", v)}
                        >
                            <SelectTrigger className="h-9">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {DOMAIN_STRATEGIES.map((s) => (
                                    <SelectItem key={s.value} value={s.value}>
                                        <div className="flex flex-col">
                                            <span className="font-medium">{s.label}</span>
                                            <span className="text-xs text-muted-foreground">{s.description}</span>
                                        </div>
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                    <div className="space-y-1.5 flex-1 max-w-sm">
                        <Label className="text-xs text-muted-foreground">Outbound Test URL</Label>
                        <Input
                            className="h-9 text-sm"
                            placeholder="https://www.google.com/generate_204"
                            value={settings.outbound_test_url || ""}
                            onChange={(e) => updateField("outbound_test_url", e.target.value)}
                        />
                        <p className="text-[10px] text-muted-foreground">Default URL for outbound connectivity tests</p>
                    </div>
                </div>

                {/* ═══ Divider ═══ */}
                <div className="border-t border-white/[0.06]" />

                {/* ═══ Basic Routing — 2×2 Category Grid ═══ */}
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {/* ── Blocking Rules (Red) ── */}
                    <CategoryCard
                        title="Blocking Rules"
                        icon={<HiOutlineBan className="w-4 h-4" />}
                        color="red"
                        count={blockCount}
                    >
                        {/* Block BitTorrent */}
                        <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                            <div className="flex items-center gap-2">
                                <HiOutlineBan className="w-3.5 h-3.5 text-red-400/70" />
                                <div>
                                    <Label className="text-xs font-medium cursor-pointer">Block BitTorrent</Label>
                                    <p className="text-[10px] text-muted-foreground leading-tight">Block P2P protocol</p>
                                </div>
                            </div>
                            <Switch
                                checked={settings.block_bittorrent}
                                onCheckedChange={(c) => updateField("block_bittorrent", c)}
                            />
                        </div>

                        <SettingsSection
                            label="Block IPs"
                            items={settings.block_ips}
                            presets={BLOCK_IP_PRESETS}
                            onAdd={(v) => addToArray("block_ips", v)}
                            onRemove={(i) => removeFromArray("block_ips", i)}
                            placeholder="geoip:private or 192.168.0.0/16"
                        />

                        <SettingsSection
                            label="Block Domains"
                            items={settings.block_domains}
                            presets={BLOCK_DOMAIN_PRESETS}
                            onAdd={(v) => addToArray("block_domains", v)}
                            onRemove={(i) => removeFromArray("block_domains", i)}
                            placeholder="geosite:category-ads-all or domain.com"
                        />
                    </CategoryCard>

                    {/* ── Direct Rules (Green) ── */}
                    <CategoryCard
                        title="Direct Rules"
                        icon={<HiOutlineArrowRight className="w-4 h-4" />}
                        color="emerald"
                        count={directCount}
                    >
                        <SettingsSection
                            label="Direct IPs"
                            items={settings.direct_ips}
                            presets={DIRECT_IP_PRESETS}
                            onAdd={(v) => addToArray("direct_ips", v)}
                            onRemove={(i) => removeFromArray("direct_ips", i)}
                            placeholder="geoip:private or 10.0.0.0/8"
                        />

                        <SettingsSection
                            label="Direct Domains"
                            items={settings.direct_domains}
                            presets={DIRECT_DOMAIN_PRESETS}
                            onAdd={(v) => addToArray("direct_domains", v)}
                            onRemove={(i) => removeFromArray("direct_domains", i)}
                            placeholder="geosite:telegram or direct-domain.com"
                        />
                    </CategoryCard>

                    {/* ── IPv4 Routing (Blue) ── */}
                    <CategoryCard
                        title="IPv4 Routing"
                        icon={<HiOutlineGlobeAlt className="w-4 h-4" />}
                        color="blue"
                        count={ipv4Count}
                    >
                        <p className="text-[11px] text-muted-foreground -mt-2">
                            Force traffic to specific domains via IPv4
                        </p>
                        <SettingsSection
                            label="Domains"
                            items={settings.ipv4_routing}
                            onAdd={(v) => addToArray("ipv4_routing", v)}
                            onRemove={(i) => removeFromArray("ipv4_routing", i)}
                            placeholder="geosite:google or domain.com"
                        />
                    </CategoryCard>

                    {/* ── WARP Routing (Orange) ── */}
                    <CategoryCard
                        title="WARP Routing"
                        icon={
                            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
                                <path d="M16.5 7.5h-6l-1.5 3h4.5l-1.5 6 6-7.5h-4.5l3-1.5z" />
                            </svg>
                        }
                        color="orange"
                        count={warpCount}
                    >
                        {/* WARP Enable Toggle */}
                        <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04] -mt-1">
                            <div>
                                <Label className="text-xs font-medium cursor-pointer">Enable WARP</Label>
                                <p className="text-[10px] text-muted-foreground leading-tight">Route via Cloudflare WARP</p>
                            </div>
                            <Switch
                                checked={settings.warp_enabled}
                                onCheckedChange={(c) => updateField("warp_enabled", c)}
                            />
                        </div>

                        {settings.warp_enabled && (
                            <div className="space-y-4 animate-in slide-in-from-top-2 duration-200">
                                {outbounds && !outbounds.some(o => o.tag === "warp") && (
                                    <div className="text-[11px] rounded-md border border-amber-500/30 bg-amber-500/[0.06] text-amber-300 px-3 py-2 leading-snug">
                                        WARP rules target an outbound tagged <span className="font-mono">warp</span>, which is not configured on this node. Add a WireGuard outbound with tag <span className="font-mono">warp</span> in the Outbounds tab — xray will refuse to load otherwise.
                                    </div>
                                )}
                                <SettingsSection
                                    label="WARP Domains"
                                    items={settings.warp_domains}
                                    onAdd={(v) => addToArray("warp_domains", v)}
                                    onRemove={(i) => removeFromArray("warp_domains", i)}
                                    placeholder="geosite:openai or domain.com"
                                />

                                <SettingsSection
                                    label="WARP IPs"
                                    items={settings.warp_ips}
                                    onAdd={(v) => addToArray("warp_ips", v)}
                                    onRemove={(i) => removeFromArray("warp_ips", i)}
                                    placeholder="geoip:ir or 1.1.1.1/32"
                                />
                            </div>
                        )}
                    </CategoryCard>
                </div>
            </CardContent>
        </Card>
    )
}
