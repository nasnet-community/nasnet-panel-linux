import React, { useState, useEffect, useCallback, useRef } from "react"
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
    HiOutlinePlus,
    HiOutlineTrash,
    HiOutlineGlobeAlt,
    HiOutlinePencil,
    HiOutlineX,
    HiOutlineChevronDown,
    HiOutlineChevronRight,
} from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { getNodeDNSSettings, updateNodeDNSSettings, deleteNodeDNSSettings, getNodeFakeDNSSettings, updateNodeFakeDNSSettings } from "@/lib/admin-api"
import type { DNSSettings, DNSServer, FakeDNSPool } from "@/lib/types"

// ─── Constants ─────────────────────────────────────────────
const QUERY_STRATEGIES = [
    { value: "UseIP", label: "UseIP", description: "Query both A and AAAA" },
    { value: "UseIPv4", label: "UseIPv4", description: "Only query A records" },
    { value: "UseIPv6", label: "UseIPv6", description: "Only query AAAA records" },
    { value: "UseSystem", label: "UseSystem", description: "Auto-detect from system routes" },
]

// ─── Validation helpers ─────────────────────────────────────
const IPV4_RE = /^(\d{1,3}\.){3}\d{1,3}$/
const IPV6_LOOSE_RE = /^[0-9a-fA-F:]+$/

function isValidIP(s: string): boolean {
    s = s.trim()
    if (!s) return false
    if (IPV4_RE.test(s)) {
        return s.split(".").every(p => {
            const n = Number(p)
            return Number.isInteger(n) && n >= 0 && n <= 255
        })
    }
    return s.includes(":") && IPV6_LOOSE_RE.test(s)
}

function isValidCIDR(s: string): boolean {
    s = s.trim()
    if (!s) return false
    const slash = s.lastIndexOf("/")
    if (slash === -1) return isValidIP(s)
    const host = s.slice(0, slash)
    const prefix = Number(s.slice(slash + 1))
    if (!Number.isInteger(prefix) || prefix < 0) return false
    if (host.includes(":")) return prefix <= 128 && IPV6_LOOSE_RE.test(host)
    return prefix <= 32 && isValidIP(host)
}

// Mirror BE/xray-core: accept !-prefix reverse, geoip:, ext:, ext-ip:, IP, CIDR.
function isValidIPRule(s: string): boolean {
    s = s.trim()
    while (s.startsWith("!")) s = s.slice(1)
    if (!s) return false
    if (s.startsWith("geoip:")) return s.length > "geoip:".length
    if (s.startsWith("ext:") || s.startsWith("ext-ip:")) {
        const body = s.slice(s.indexOf(":") + 1)
        return body.includes(":") && body.length > 1
    }
    return isValidCIDR(s)
}

// xray-core HostsWrapper accepts: a literal IP, a domain (with prefix forms),
// or "#<rcode>" shorthand for a DNS RCode reply.
function isValidHostsValue(v: string): boolean {
    v = v.trim()
    if (!v) return false
    if (v.startsWith("#")) {
        const rest = v.slice(1)
        return rest.length > 0 && /^\d+$/.test(rest)
    }
    if (isValidIP(v)) return true
    // Domain forms — strip known prefixes.
    let body = v
    for (const p of ["domain:", "full:", "regexp:", "keyword:", "geosite:", "ext:", "ext-domain:", "dotless:"] as const) {
        if (body.startsWith(p)) {
            body = body.slice(p.length)
            break
        }
    }
    if (!body) return false
    return v.startsWith("regexp:") || body === "localhost" || body.includes(".")
}

// LRU vs subnet guard mirroring xray-core's runtime check.
function fakeDNSPoolError(pool: FakeDNSPool): string | null {
    if (!pool.ip_pool || !pool.ip_pool.trim()) return "IP pool is required"
    if (!isValidCIDR(pool.ip_pool)) return "Invalid CIDR"
    const slash = pool.ip_pool.lastIndexOf("/")
    if (slash === -1) return "CIDR prefix is required"
    const prefix = Number(pool.ip_pool.slice(slash + 1))
    const isV6 = pool.ip_pool.includes(":")
    const rooms = (isV6 ? 128 : 32) - prefix
    if (rooms <= 0) return "CIDR has no host bits"
    const lru = pool.lru_size ?? 0
    if (lru <= 0) return "LRU size must be > 0"
    // xray fails when math.Log2(lruSize) >= rooms.
    if (rooms < 53 && lru >= 2 ** rooms) {
        return `LRU ${lru} >= subnet size 2^${rooms}; use a larger CIDR or smaller LRU`
    }
    return null
}

const DNS_PRESETS: { label: string; server: DNSServer }[] = [
    { label: "Google", server: { address: "8.8.8.8" } },
    { label: "Google (2)", server: { address: "8.8.4.4" } },
    { label: "Cloudflare", server: { address: "1.1.1.1" } },
    { label: "Google DoH", server: { address: "https://dns.google/dns-query" } },
    { label: "Cloudflare DoH", server: { address: "https://cloudflare-dns.com/dns-query" } },
    { label: "Google TCP", server: { address: "tcp://8.8.8.8" } },
    { label: "AdGuard DoQ", server: { address: "quic+local://dns.adguard.com" } },
    { label: "localhost", server: { address: "localhost" } },
    { label: "FakeDNS", server: { address: "fakedns" } },
]

const EMPTY_SETTINGS: DNSSettings = {
    servers: [],
    hosts: {},
    client_ip: "",
    query_strategy: "",
    disable_cache: false,
    disable_fallback: false,
    disable_fallback_if_match: false,
    tag: "",
    serve_stale: false,
    serve_expired_ttl: null,
    enable_parallel_query: false,
    use_system_hosts: false,
}

const EMPTY_SERVER: DNSServer = {
    address: "",
    port: undefined,
    domains: [],
    expected_ips: [],
    unexpected_ips: [],
    skip_fallback: false,
    query_strategy: "",
    tag: "",
    client_ip: "",
    timeout_ms: undefined,
    disable_cache: undefined,
    serve_stale: undefined,
    serve_expired_ttl: undefined,
    final_query: false,
}

// ─── Helper Components ─────────────────────────────────────

function TagList({ items, onRemove }: { items: string[]; onRemove: (i: number) => void }) {
    if (!items.length) return null
    return (
        <div className="flex flex-wrap gap-1.5 mt-1.5">
            {items.map((item, i) => (
                <Badge key={i} variant="secondary" className="pl-2 pr-1 py-0.5 text-xs font-mono gap-1 bg-white/[0.04] border border-white/[0.06]">
                    {item}
                    <button onClick={() => onRemove(i)} className="ml-0.5 hover:text-red-400 transition-colors">
                        <HiOutlineX className="w-3 h-3" />
                    </button>
                </Badge>
            ))}
        </div>
    )
}

function ArrayField({
    label,
    items,
    placeholder,
    onAdd,
    onRemove,
}: {
    label: string
    items: string[]
    placeholder: string
    onAdd: (v: string) => void
    onRemove: (i: number) => void
}) {
    const [value, setValue] = useState("")
    const handleAdd = () => {
        const v = value.trim()
        if (v && !items.includes(v)) {
            onAdd(v)
            setValue("")
        }
    }
    return (
        <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">{label}</Label>
            <div className="flex gap-1.5">
                <Input
                    className="h-8 text-xs font-mono"
                    placeholder={placeholder}
                    value={value}
                    onChange={(e) => setValue(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), handleAdd())}
                />
                <Button variant="outline" size="sm" className="h-8 px-2 shrink-0" onClick={handleAdd} disabled={!value.trim()}>
                    <HiOutlinePlus className="w-3.5 h-3.5" />
                </Button>
            </div>
            <TagList items={items} onRemove={onRemove} />
        </div>
    )
}

// ─── Server Editor ──────────────────────────────────────────

function ServerEditor({
    server,
    onChange,
    onRemove,
    index,
}: {
    server: DNSServer
    onChange: (s: DNSServer) => void
    onRemove: () => void
    index: number
}) {
    const [expanded, setExpanded] = useState(false)
    const hasAdvanced = (server.domains?.length ?? 0) > 0 ||
        (server.expected_ips?.length ?? 0) > 0 ||
        (server.unexpected_ips?.length ?? 0) > 0 ||
        server.skip_fallback ||
        !!server.query_strategy ||
        !!server.tag ||
        !!server.client_ip ||
        (server.timeout_ms ?? 0) > 0 ||
        server.disable_cache != null ||
        server.serve_stale != null ||
        server.final_query

    const update = <K extends keyof DNSServer>(key: K, value: DNSServer[K]) => {
        onChange({ ...server, [key]: value })
    }

    const addToArray = (field: "domains" | "expected_ips" | "unexpected_ips", value: string) => {
        const v = value.trim()
        if (!v) return
        if ((field === "expected_ips" || field === "unexpected_ips") && !isValidIPRule(v)) {
            toast.error(`Invalid IP rule: "${v}" (use IP, CIDR, geoip:CC, or !-prefix to negate)`)
            return
        }
        const current = server[field] || []
        if (!current.includes(v)) {
            update(field, [...current, v])
        }
    }

    const removeFromArray = (field: "domains" | "expected_ips" | "unexpected_ips", index: number) => {
        const current = [...(server[field] || [])]
        current.splice(index, 1)
        update(field, current)
    }

    return (
        <div className="rounded-lg border border-white/[0.06] bg-white/[0.02] p-3 space-y-3">
            {/* Header row */}
            <div className="flex items-center gap-2">
                <span className="text-xs text-muted-foreground font-mono w-5 shrink-0">#{index + 1}</span>
                <Input
                    className={`h-8 text-xs font-mono flex-1 ${!server.address.trim() ? "border-red-500/50" : ""}`}
                    placeholder="8.8.8.8 or https://dns.google/dns-query"
                    value={server.address}
                    onChange={(e) => update("address", e.target.value)}
                />
                <Input
                    className="h-8 text-xs font-mono w-20"
                    placeholder="53"
                    type="number"
                    min={0}
                    max={65535}
                    value={server.port ?? ""}
                    onChange={(e) => {
                        const v = e.target.value ? parseInt(e.target.value) : undefined
                        if (v !== undefined && (isNaN(v) || v < 0 || v > 65535)) return
                        update("port", v)
                    }}
                />
                <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-8 p-0 shrink-0"
                    onClick={() => setExpanded(!expanded)}
                >
                    {expanded ? <HiOutlineChevronDown className="w-3.5 h-3.5" /> : <HiOutlineChevronRight className="w-3.5 h-3.5" />}
                </Button>
                <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-8 p-0 text-red-400 hover:text-red-500 hover:bg-red-500/10 shrink-0"
                    onClick={onRemove}
                >
                    <HiOutlineTrash className="w-3.5 h-3.5" />
                </Button>
            </div>

            {/* Advanced indicator */}
            {!expanded && hasAdvanced && (
                <div className="flex flex-wrap gap-1 ml-7">
                    {(server.domains?.length ?? 0) > 0 && <Badge variant="outline" className="text-[10px] px-1.5 py-0">{server.domains!.length} domains</Badge>}
                    {(server.expected_ips?.length ?? 0) > 0 && <Badge variant="outline" className="text-[10px] px-1.5 py-0">{server.expected_ips!.length} expected IPs</Badge>}
                    {server.skip_fallback && <Badge variant="outline" className="text-[10px] px-1.5 py-0">skip fallback</Badge>}
                    {server.query_strategy && <Badge variant="outline" className="text-[10px] px-1.5 py-0">{server.query_strategy}</Badge>}
                    {server.tag && <Badge variant="outline" className="text-[10px] px-1.5 py-0">tag: {server.tag}</Badge>}
                    {server.final_query && <Badge variant="outline" className="text-[10px] px-1.5 py-0">final query</Badge>}
                    {server.client_ip && <Badge variant="outline" className="text-[10px] px-1.5 py-0">ECS: {server.client_ip}</Badge>}
                    {(server.timeout_ms ?? 0) > 0 && <Badge variant="outline" className="text-[10px] px-1.5 py-0">{server.timeout_ms}ms</Badge>}
                    {server.disable_cache != null && <Badge variant="outline" className="text-[10px] px-1.5 py-0">cache: {server.disable_cache ? "off" : "on"}</Badge>}
                </div>
            )}

            {/* Expanded advanced settings */}
            {expanded && (
                <div className="space-y-3 ml-7 animate-in slide-in-from-top-2 duration-200">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label className="text-xs text-muted-foreground">Query Strategy</Label>
                            <Select
                                value={server.query_strategy || "_none"}
                                onValueChange={(v) => update("query_strategy", v === "_none" ? "" : v)}
                            >
                                <SelectTrigger className="h-8 text-xs">
                                    <SelectValue placeholder="Inherit global" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="_none">Inherit global</SelectItem>
                                    {QUERY_STRATEGIES.map((s) => (
                                        <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs text-muted-foreground">Tag</Label>
                            <Input
                                className="h-8 text-xs font-mono"
                                placeholder="dns-google"
                                value={server.tag || ""}
                                onChange={(e) => update("tag", e.target.value)}
                            />
                        </div>
                    </div>

                    <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                        <div>
                            <Label className="text-xs font-medium cursor-pointer">Skip Fallback</Label>
                            <p className="text-[10px] text-muted-foreground leading-tight">Skip this server during fallback</p>
                        </div>
                        <Switch
                            checked={server.skip_fallback || false}
                            onCheckedChange={(c) => update("skip_fallback", c)}
                        />
                    </div>

                    <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                        <div>
                            <Label className="text-xs font-medium cursor-pointer">Final Query</Label>
                            <p className="text-[10px] text-muted-foreground leading-tight">Stop after this server, no fallback</p>
                        </div>
                        <Switch
                            checked={server.final_query || false}
                            onCheckedChange={(c) => update("final_query", c)}
                        />
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label className="text-xs text-muted-foreground">Client IP (EDNS)</Label>
                            <Input
                                className="h-8 text-xs font-mono"
                                placeholder="Override global client IP"
                                value={server.client_ip || ""}
                                onChange={(e) => update("client_ip", e.target.value)}
                            />
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs text-muted-foreground">Timeout (ms)</Label>
                            <Input
                                className="h-8 text-xs font-mono"
                                type="number"
                                placeholder="4000"
                                value={server.timeout_ms ?? ""}
                                onChange={(e) => update("timeout_ms", e.target.value ? parseInt(e.target.value) : undefined)}
                            />
                        </div>
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                        <div className="space-y-1">
                            <Label className="text-xs text-muted-foreground">Cache Override</Label>
                            <Select
                                value={server.disable_cache === true ? "disabled" : server.disable_cache === false ? "enabled" : "_inherit"}
                                onValueChange={(v) => update("disable_cache", v === "_inherit" ? undefined : v === "disabled")}
                            >
                                <SelectTrigger className="h-8 text-xs">
                                    <SelectValue placeholder="Inherit global" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="_inherit">Inherit global</SelectItem>
                                    <SelectItem value="enabled">Cache enabled</SelectItem>
                                    <SelectItem value="disabled">Cache disabled</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs text-muted-foreground">Serve Stale Override</Label>
                            <Select
                                value={server.serve_stale === true ? "enabled" : server.serve_stale === false ? "disabled" : "_inherit"}
                                onValueChange={(v) => update("serve_stale", v === "_inherit" ? undefined : v === "enabled")}
                            >
                                <SelectTrigger className="h-8 text-xs">
                                    <SelectValue placeholder="Inherit global" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="_inherit">Inherit global</SelectItem>
                                    <SelectItem value="enabled">Serve stale</SelectItem>
                                    <SelectItem value="disabled">Don't serve stale</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs text-muted-foreground">Stale TTL Override</Label>
                            <Input
                                className="h-8 text-xs font-mono"
                                type="number"
                                placeholder="Inherit"
                                value={server.serve_expired_ttl ?? ""}
                                onChange={(e) => update("serve_expired_ttl", e.target.value ? parseInt(e.target.value) : undefined)}
                            />
                        </div>
                    </div>

                    <ArrayField
                        label="Domains"
                        items={server.domains || []}
                        placeholder="geosite:google or domain.com"
                        onAdd={(v) => addToArray("domains", v)}
                        onRemove={(i) => removeFromArray("domains", i)}
                    />

                    <ArrayField
                        label="Expected IPs"
                        items={server.expected_ips || []}
                        placeholder="geoip:cn or 1.2.3.0/24"
                        onAdd={(v) => addToArray("expected_ips", v)}
                        onRemove={(i) => removeFromArray("expected_ips", i)}
                    />

                    <ArrayField
                        label="Unexpected IPs"
                        items={server.unexpected_ips || []}
                        placeholder="geoip:private or 10.0.0.0/8"
                        onAdd={(v) => addToArray("unexpected_ips", v)}
                        onRemove={(i) => removeFromArray("unexpected_ips", i)}
                    />
                </div>
            )}
        </div>
    )
}

// ─── Static Hosts Editor ────────────────────────────────────

function HostsEditor({
    hosts,
    onChange,
}: {
    hosts: Record<string, string | string[]>
    onChange: (h: Record<string, string | string[]>) => void
}) {
    const [newDomain, setNewDomain] = useState("")
    const [newIP, setNewIP] = useState("")

    const entries = Object.entries(hosts)

    const addHost = () => {
        const domain = newDomain.trim()
        const ip = newIP.trim()
        if (!domain || !ip) return
        if (!isValidHostsValue(ip)) {
            toast.error(`Invalid hosts value: "${ip}" (expected IP, domain, or #<rcode>)`)
            return
        }
        const existing = hosts[domain]
        if (existing) {
            // Append to existing
            const arr = Array.isArray(existing) ? existing : [existing]
            if (!arr.includes(ip)) {
                onChange({ ...hosts, [domain]: [...arr, ip] })
            }
        } else {
            onChange({ ...hosts, [domain]: ip })
        }
        setNewDomain("")
        setNewIP("")
    }

    const removeHost = (domain: string) => {
        const copy = { ...hosts }
        delete copy[domain]
        onChange(copy)
    }

    const removeHostIP = (domain: string, ipIndex: number) => {
        const value = hosts[domain]
        if (!Array.isArray(value)) {
            removeHost(domain)
            return
        }
        const arr = [...value]
        arr.splice(ipIndex, 1)
        if (arr.length === 0) {
            removeHost(domain)
        } else if (arr.length === 1) {
            onChange({ ...hosts, [domain]: arr[0] })
        } else {
            onChange({ ...hosts, [domain]: arr })
        }
    }

    return (
        <div className="space-y-3">
            {/* Add host form */}
            <div className="flex gap-1.5">
                <Input
                    className="h-8 text-xs font-mono flex-1"
                    placeholder="domain.com, geosite:cn, keyword:ad, regexp:.*"
                    value={newDomain}
                    onChange={(e) => setNewDomain(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), addHost())}
                />
                <Input
                    className="h-8 text-xs font-mono flex-1"
                    placeholder="1.2.3.4, other.domain.com, or #3 (NXDOMAIN)"
                    value={newIP}
                    onChange={(e) => setNewIP(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), addHost())}
                />
                <Button variant="outline" size="sm" className="h-8 px-2 shrink-0" onClick={addHost} disabled={!newDomain.trim() || !newIP.trim()}>
                    <HiOutlinePlus className="w-3.5 h-3.5" />
                </Button>
            </div>

            {/* Entries */}
            {entries.length > 0 ? (
                <div className="space-y-1.5">
                    {entries.map(([domain, value]) => {
                        const ips = Array.isArray(value) ? value : [value]
                        return (
                            <div key={domain} className="flex items-start gap-2 py-1.5 px-2.5 rounded-md bg-white/[0.02] border border-white/[0.04]">
                                <span className="text-xs font-mono text-primary/80 shrink-0 pt-0.5">{domain}</span>
                                <span className="text-xs text-muted-foreground pt-0.5">&rarr;</span>
                                <div className="flex flex-wrap gap-1 flex-1">
                                    {ips.map((ip, i) => (
                                        <Badge key={i} variant="secondary" className="pl-2 pr-1 py-0.5 text-xs font-mono gap-1 bg-white/[0.04] border border-white/[0.06]">
                                            {ip}
                                            <button onClick={() => removeHostIP(domain, i)} className="ml-0.5 hover:text-red-400 transition-colors">
                                                <HiOutlineX className="w-3 h-3" />
                                            </button>
                                        </Badge>
                                    ))}
                                </div>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-6 w-6 p-0 text-red-400 hover:text-red-500 hover:bg-red-500/10 shrink-0"
                                    onClick={() => removeHost(domain)}
                                >
                                    <HiOutlineTrash className="w-3 h-3" />
                                </Button>
                            </div>
                        )
                    })}
                </div>
            ) : (
                <p className="text-xs text-muted-foreground text-center py-3">No static host entries</p>
            )}
        </div>
    )
}

// ─── Main Component ──────────────────────────────────────────

interface DNSSettingsCardProps {
    nodeId: number
    onSettingsSaved?: () => void
}

export function DNSSettingsCard({ nodeId, onSettingsSaved }: DNSSettingsCardProps) {
    const [settings, setSettings] = useState<DNSSettings>(EMPTY_SETTINGS)
    const [originalSettings, setOriginalSettings] = useState<DNSSettings>(EMPTY_SETTINGS)
    const [isLoading, setIsLoading] = useState(true)
    const [isSaving, setIsSaving] = useState(false)
    const hasLoadedRef = useRef(false)

    const isDirty = JSON.stringify(settings) !== JSON.stringify(originalSettings)

    const [fakeDNSPools, setFakeDNSPools] = useState<FakeDNSPool[]>([])
    const [originalFakeDNS, setOriginalFakeDNS] = useState<FakeDNSPool[]>([])
    const isFakeDNSDirty = JSON.stringify(fakeDNSPools) !== JSON.stringify(originalFakeDNS)

    // Load settings
    const loadSettings = useCallback(async () => {
        try {
            setIsLoading(true)
            const res = await getNodeDNSSettings(nodeId)
            if (res.success) {
                const data = res.data
                if (data) {
                    const normalized: DNSSettings = {
                        ...EMPTY_SETTINGS,
                        ...data,
                        servers: data.servers || [],
                        hosts: data.hosts || {},
                    }
                    setSettings(normalized)
                    setOriginalSettings(normalized)
                } else {
                    setSettings(EMPTY_SETTINGS)
                    setOriginalSettings(EMPTY_SETTINGS)
                }
            }
            // Also load FakeDNS
            const fakeRes = await getNodeFakeDNSSettings(nodeId)
            if (fakeRes.success && fakeRes.data) {
                setFakeDNSPools(fakeRes.data)
                setOriginalFakeDNS(fakeRes.data)
            }
        } catch {
            toast.error("Failed to load DNS settings")
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

    // FE-side guards mirroring BE validator. Blocks save with toast.
    const validateBeforeSave = (): string | null => {
        if (settings.servers) {
            for (let i = 0; i < settings.servers.length; i++) {
                const s = settings.servers[i]
                if (!s.address || !s.address.trim()) {
                    return `Server #${i + 1}: address is required`
                }
                if (s.port !== undefined && (s.port < 0 || s.port > 65535)) {
                    return `Server #${i + 1}: port must be 0-65535`
                }
                for (const ip of s.expected_ips || []) {
                    if (!isValidIPRule(ip)) return `Server #${i + 1}: invalid expected IP "${ip}"`
                }
                for (const ip of s.unexpected_ips || []) {
                    if (!isValidIPRule(ip)) return `Server #${i + 1}: invalid unexpected IP "${ip}"`
                }
            }
        }
        if (settings.client_ip && !isValidIP(settings.client_ip)) {
            return `Client IP "${settings.client_ip}" is not a valid IP`
        }
        if (settings.hosts) {
            for (const [k, v] of Object.entries(settings.hosts)) {
                if (!k.trim()) return "Hosts: empty domain key"
                const items = Array.isArray(v) ? v : [v]
                for (const item of items) {
                    if (!isValidHostsValue(item)) return `Hosts[${k}]: invalid value "${item}"`
                }
            }
        }
        return null
    }

    // Save
    const handleSave = async () => {
        const err = validateBeforeSave()
        if (err) {
            toast.error(err)
            return
        }
        try {
            setIsSaving(true)
            const res = await updateNodeDNSSettings(nodeId, settings)
            if (res.success && res.data) {
                const normalized: DNSSettings = {
                    ...EMPTY_SETTINGS,
                    ...res.data,
                    servers: res.data.servers || [],
                    hosts: res.data.hosts || {},
                }
                setSettings(normalized)
                setOriginalSettings(normalized)
                toast.success("DNS settings saved", {
                    description: "Config will be synced to the node automatically",
                })
                onSettingsSaved?.()
            } else {
                toast.error("Failed to save DNS settings")
            }
        } catch {
            toast.error("Failed to save DNS settings")
        } finally {
            setIsSaving(false)
        }
    }

    const handleReset = () => {
        setSettings(originalSettings)
    }

    const handleDelete = async () => {
        try {
            setIsSaving(true)
            const res = await deleteNodeDNSSettings(nodeId)
            if (res.success) {
                setSettings(EMPTY_SETTINGS)
                setOriginalSettings(EMPTY_SETTINGS)
                toast.success("DNS settings cleared")
                onSettingsSaved?.()
            } else {
                toast.error("Failed to clear DNS settings")
            }
        } catch {
            toast.error("Failed to clear DNS settings")
        } finally {
            setIsSaving(false)
        }
    }

    const handleSaveFakeDNS = async () => {
        try {
            setIsSaving(true)
            const res = await updateNodeFakeDNSSettings(nodeId, fakeDNSPools)
            if (res.success && res.data) {
                setFakeDNSPools(res.data)
                setOriginalFakeDNS(res.data)
                toast.success("FakeDNS settings saved")
                onSettingsSaved?.()
            }
        } catch {
            toast.error("Failed to save FakeDNS settings")
        } finally {
            setIsSaving(false)
        }
    }

    // Field helpers
    const updateField = <K extends keyof DNSSettings>(key: K, value: DNSSettings[K]) => {
        setSettings((prev) => ({ ...prev, [key]: value }))
    }

    // Server helpers
    const addServer = (server?: DNSServer) => {
        updateField("servers", [...(settings.servers || []), server || { ...EMPTY_SERVER }])
    }

    const updateServer = (index: number, server: DNSServer) => {
        const servers = [...(settings.servers || [])]
        servers[index] = server
        updateField("servers", servers)
    }

    const removeServer = (index: number) => {
        const servers = [...(settings.servers || [])]
        servers.splice(index, 1)
        updateField("servers", servers)
    }

    const serverCount = settings.servers?.length ?? 0
    const hostCount = Object.keys(settings.hosts || {}).length

    if (isLoading) {
        return (
            <Card className="border border-white/5 bg-card/50 backdrop-blur-sm">
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
                    <Skeleton className="h-32 w-full" />
                </CardContent>
            </Card>
        )
    }

    return (
        <Card className="border border-white/5 bg-card/50 backdrop-blur-sm">
            {/* Header */}
            <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                        <HiOutlineGlobeAlt className="w-5 h-5 text-primary" />
                        <CardTitle className="text-base font-semibold">DNS Configuration</CardTitle>
                        {(serverCount > 0 || hostCount > 0) && (
                            <div className="flex gap-1.5">
                                {serverCount > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0">{serverCount} server{serverCount !== 1 ? "s" : ""}</Badge>}
                                {hostCount > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0">{hostCount} host{hostCount !== 1 ? "s" : ""}</Badge>}
                            </div>
                        )}
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
                    {!isDirty && JSON.stringify(originalSettings) !== JSON.stringify(EMPTY_SETTINGS) && (
                        <Button
                            variant="ghost"
                            size="sm"
                            onClick={handleDelete}
                            disabled={isSaving}
                            className="h-8 text-xs text-red-400 hover:text-red-500 hover:bg-red-500/10"
                        >
                            <HiOutlineTrash className="w-3.5 h-3.5 mr-1.5" />
                            Clear All
                        </Button>
                    )}
                </div>
            </CardHeader>

            <CardContent className="space-y-5">
                {/* Global Settings */}
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                    <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">Query Strategy</Label>
                        <Select
                            value={settings.query_strategy || "_none"}
                            onValueChange={(v) => updateField("query_strategy", v === "_none" ? "" : v)}
                        >
                            <SelectTrigger className="h-9">
                                <SelectValue placeholder="Default (UseIP)" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="_none">Default (UseIP)</SelectItem>
                                {QUERY_STRATEGIES.map((s) => (
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
                    <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">Client IP</Label>
                        <Input
                            className="h-9 text-sm font-mono"
                            placeholder="e.g. 1.2.3.4"
                            value={settings.client_ip || ""}
                            onChange={(e) => updateField("client_ip", e.target.value)}
                        />
                        <p className="text-[10px] text-muted-foreground">IP to use for EDNS Client Subnet</p>
                    </div>
                    <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">Tag</Label>
                        <Input
                            className="h-9 text-sm font-mono"
                            placeholder="dns-inbound"
                            value={settings.tag || ""}
                            onChange={(e) => updateField("tag", e.target.value)}
                        />
                        <p className="text-[10px] text-muted-foreground">Inbound tag for DNS queries</p>
                    </div>
                </div>

                {/* Toggles */}
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                    <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                        <div>
                            <Label className="text-xs font-medium cursor-pointer">Disable Cache</Label>
                            <p className="text-[10px] text-muted-foreground leading-tight">Disable DNS response cache</p>
                        </div>
                        <Switch
                            checked={settings.disable_cache || false}
                            onCheckedChange={(c) => updateField("disable_cache", c)}
                        />
                    </div>
                    <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                        <div>
                            <Label className="text-xs font-medium cursor-pointer">Disable Fallback</Label>
                            <p className="text-[10px] text-muted-foreground leading-tight">Disable DNS server fallback</p>
                        </div>
                        <Switch
                            checked={settings.disable_fallback || false}
                            onCheckedChange={(c) => updateField("disable_fallback", c)}
                        />
                    </div>
                    <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                        <div>
                            <Label className="text-xs font-medium cursor-pointer">Disable Fallback If Match</Label>
                            <p className="text-[10px] text-muted-foreground leading-tight">Skip fallback when match found</p>
                        </div>
                        <Switch
                            checked={settings.disable_fallback_if_match || false}
                            onCheckedChange={(c) => updateField("disable_fallback_if_match", c)}
                        />
                    </div>
                </div>

                {/* New Global Toggles */}
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                    <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                        <div>
                            <Label className="text-xs font-medium cursor-pointer">Serve Stale</Label>
                            <p className="text-[10px] text-muted-foreground leading-tight">Serve expired cache while refreshing</p>
                        </div>
                        <Switch
                            checked={settings.serve_stale || false}
                            onCheckedChange={(c) => updateField("serve_stale", c)}
                        />
                    </div>
                    <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                        <div>
                            <Label className="text-xs font-medium cursor-pointer">Parallel Query</Label>
                            <p className="text-[10px] text-muted-foreground leading-tight">Query DNS servers concurrently</p>
                        </div>
                        <Switch
                            checked={settings.enable_parallel_query || false}
                            onCheckedChange={(c) => updateField("enable_parallel_query", c)}
                        />
                    </div>
                    <div className="flex items-center justify-between py-2 px-3 rounded-md bg-white/[0.02] border border-white/[0.04]">
                        <div>
                            <Label className="text-xs font-medium cursor-pointer">Use System Hosts</Label>
                            <p className="text-[10px] text-muted-foreground leading-tight">Read /etc/hosts into static hosts</p>
                        </div>
                        <Switch
                            checked={settings.use_system_hosts || false}
                            onCheckedChange={(c) => updateField("use_system_hosts", c)}
                        />
                    </div>
                </div>

                {/* Serve Expired TTL (conditional on serve_stale) */}
                {settings.serve_stale && (
                    <div className="space-y-1.5 animate-in slide-in-from-top-2 duration-200">
                        <Label className="text-xs text-muted-foreground">Serve Expired TTL (seconds)</Label>
                        <Input
                            className="h-9 text-sm font-mono w-48"
                            type="number"
                            placeholder="0 (unlimited)"
                            value={settings.serve_expired_ttl ?? ""}
                            onChange={(e) => updateField("serve_expired_ttl", e.target.value ? parseInt(e.target.value) : undefined)}
                        />
                        <p className="text-[10px] text-muted-foreground">Max staleness in seconds. 0 = unlimited.</p>
                    </div>
                )}

                {/* Divider */}
                <div className="border-t border-white/[0.06]" />

                {/* DNS Servers Section */}
                <div className="space-y-3">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                            <Label className="text-sm font-medium">DNS Servers</Label>
                            {serverCount > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0">{serverCount}</Badge>}
                        </div>
                        <div className="flex items-center gap-1.5">
                            {/* Quick-add presets */}
                            <div className="hidden sm:flex items-center gap-1">
                                {DNS_PRESETS.map((preset) => (
                                    <Button
                                        key={preset.label}
                                        variant="outline"
                                        size="sm"
                                        className="h-7 text-[10px] px-2"
                                        onClick={() => addServer({ ...preset.server })}
                                    >
                                        {preset.label}
                                    </Button>
                                ))}
                            </div>
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-7 text-xs px-2"
                                onClick={() => addServer()}
                            >
                                <HiOutlinePlus className="w-3 h-3 mr-1" />
                                Add
                            </Button>
                        </div>
                    </div>

                    {/* Mobile preset buttons */}
                    <div className="flex sm:hidden flex-wrap gap-1.5">
                        {DNS_PRESETS.map((preset) => (
                            <Button
                                key={preset.label}
                                variant="outline"
                                size="sm"
                                className="h-7 text-[10px] px-2"
                                onClick={() => addServer({ ...preset.server })}
                            >
                                {preset.label}
                            </Button>
                        ))}
                    </div>

                    {serverCount > 0 ? (
                        <div className="space-y-2">
                            {(settings.servers || []).map((server, i) => (
                                <ServerEditor
                                    key={i}
                                    index={i}
                                    server={server}
                                    onChange={(s) => updateServer(i, s)}
                                    onRemove={() => removeServer(i)}
                                />
                            ))}
                        </div>
                    ) : (
                        <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
                            <HiOutlineGlobeAlt className="w-10 h-10 opacity-30 mb-2" />
                            <p className="text-xs">No DNS servers configured</p>
                            <p className="text-[10px] text-muted-foreground">DNS section will be omitted from xray config</p>
                        </div>
                    )}
                </div>

                {/* Divider */}
                <div className="border-t border-white/[0.06]" />

                {/* Static Hosts Section */}
                <div className="space-y-3">
                    <div className="flex items-center gap-2">
                        <Label className="text-sm font-medium">Static Hosts</Label>
                        {hostCount > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0">{hostCount}</Badge>}
                    </div>
                    <p className="text-[10px] text-muted-foreground -mt-1">
                        Map domains to static IP addresses. Values can be IPs, other domains, or <span className="font-mono">#&lt;rcode&gt;</span> (e.g. <span className="font-mono">#3</span> for NXDOMAIN). Bare hostnames match <em>exactly</em>; use <span className="font-mono">domain:</span> for suffix match, <span className="font-mono">regexp:</span> for regex, <span className="font-mono">keyword:</span> for substring.
                    </p>
                    <HostsEditor
                        hosts={(settings.hosts || {}) as Record<string, string | string[]>}
                        onChange={(h) => updateField("hosts", h)}
                    />
                </div>

                {/* Divider */}
                <div className="border-t border-white/[0.06]" />

                {/* FakeDNS Pools Section */}
                <div className="space-y-3">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                            <Label className="text-sm font-medium">FakeDNS Pools</Label>
                            {fakeDNSPools.length > 0 && <Badge variant="secondary" className="text-[10px] px-1.5 py-0">{fakeDNSPools.length}</Badge>}
                        </div>
                        <div className="flex items-center gap-1.5">
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-7 text-[10px] px-2"
                                onClick={() => setFakeDNSPools([...fakeDNSPools, { ip_pool: "198.18.0.0/15", lru_size: 65535 }])}
                            >
                                + IPv4 Pool
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-7 text-[10px] px-2"
                                onClick={() => setFakeDNSPools([...fakeDNSPools, { ip_pool: "fc00::/18", lru_size: 65535 }])}
                            >
                                + IPv6 Pool
                            </Button>
                        </div>
                    </div>
                    {fakeDNSPools.length > 0 ? (
                        <div className="space-y-2">
                            {fakeDNSPools.map((pool, i) => {
                                const err = fakeDNSPoolError(pool)
                                return (
                                    <div key={i} className="space-y-1">
                                        <div className={`flex items-center gap-2 p-3 rounded-lg border bg-white/[0.02] ${err ? "border-red-500/40" : "border-white/[0.06]"}`}>
                                            <span className="text-xs text-muted-foreground font-mono w-5 shrink-0">#{i + 1}</span>
                                            <div className="flex-1 grid grid-cols-2 gap-2">
                                                <div className="space-y-1">
                                                    <Label className="text-[10px] text-muted-foreground">IP Pool (CIDR)</Label>
                                                    <Input
                                                        className="h-8 text-xs font-mono"
                                                        placeholder="198.18.0.0/15"
                                                        value={pool.ip_pool || ""}
                                                        onChange={(e) => {
                                                            const updated = [...fakeDNSPools]
                                                            updated[i] = { ...pool, ip_pool: e.target.value }
                                                            setFakeDNSPools(updated)
                                                        }}
                                                    />
                                                </div>
                                                <div className="space-y-1">
                                                    <Label className="text-[10px] text-muted-foreground">LRU Size</Label>
                                                    <Input
                                                        className="h-8 text-xs font-mono"
                                                        type="number"
                                                        placeholder="65535"
                                                        value={pool.lru_size ?? ""}
                                                        onChange={(e) => {
                                                            const updated = [...fakeDNSPools]
                                                            updated[i] = { ...pool, lru_size: parseInt(e.target.value) || undefined }
                                                            setFakeDNSPools(updated)
                                                        }}
                                                    />
                                                </div>
                                            </div>
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                className="h-8 w-8 p-0 text-red-400 hover:text-red-500 hover:bg-red-500/10 shrink-0"
                                                onClick={() => setFakeDNSPools(fakeDNSPools.filter((_, j) => j !== i))}
                                            >
                                                <HiOutlineTrash className="w-3.5 h-3.5" />
                                            </Button>
                                        </div>
                                        {err && (
                                            <p className="text-[10px] text-red-400 ml-7">{err}</p>
                                        )}
                                    </div>
                                )
                            })}
                            {isFakeDNSDirty && (
                                <div className="flex justify-end gap-2">
                                    <Button variant="ghost" size="sm" className="h-8 text-xs" onClick={() => setFakeDNSPools(originalFakeDNS)}>
                                        Reset
                                    </Button>
                                    <Button
                                        size="sm"
                                        className="h-8 text-xs"
                                        onClick={handleSaveFakeDNS}
                                        disabled={isSaving || fakeDNSPools.some(p => fakeDNSPoolError(p) !== null)}
                                    >
                                        {isSaving ? <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" /> : <HiOutlineSave className="w-3.5 h-3.5 mr-1.5" />}
                                        Save FakeDNS
                                    </Button>
                                </div>
                            )}
                        </div>
                    ) : (
                        <p className="text-xs text-muted-foreground text-center py-3">No FakeDNS pools configured</p>
                    )}
                </div>
            </CardContent>
        </Card>
    )
}
