import { useState, useEffect } from "react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { toast } from "sonner"
import { useQueryClient } from "@tanstack/react-query"
import type { Host, HostWithRelations, Inbound } from "@/lib/types"
import { addInboundHost, updateHost, createHost, createHostTemplate } from "@/lib/admin-api"
import { queryKeys } from "@/lib/queries/keys"
import {
    SECURITY_TYPES,
    TCP_HEADER_TYPES,
    TLS_FINGERPRINTS,
    VLESS_FLOWS,
    VMESS_SECURITIES,
    XHTTP_MODES,
} from "@/lib/types"
import { useNodes, useNodeInbounds, useHostTemplates, useHostTags } from "@/lib/queries"
import { HiOutlineDownload, HiOutlineInformationCircle, HiOutlineSave, HiOutlineX } from "react-icons/hi"

/** Sentinel for "no override" in selects — shadcn Select can't hold "". */
const INHERIT = "inherit"

const REMARK_VARIABLES = [
    { var: "{flag}", desc: "Country flag emoji" },
    { var: "{country}", desc: "Country code" },
    { var: "{country_code}", desc: "ISO country code" },
    { var: "{node}", desc: "Node name" },
    { var: "{port}", desc: "Port number" },
    { var: "{protocol}", desc: "Protocol (vmess, vless)" },
    { var: "{network}", desc: "Network (ws, grpc)" },
    { var: "{security}", desc: "Security (tls, reality)" },
    { var: "{data_used}", desc: "Data used" },
    { var: "{data_left}", desc: "Data remaining (or ∞)" },
    { var: "{days_left}", desc: "Days remaining (or ∞)" },
    { var: "{time_left}", desc: "Time left with unit (30d or ∞)" },
    { var: "{data_limit}", desc: "Total data limit" },
    { var: "{usage_percent}", desc: "Usage percentage" },
    { var: "{status_emoji}", desc: "Status indicator (🟢/⏸️/🔴)" },
]

/** Collapse transport aliases to the names the link generator switches on. */
function normalizeNetwork(network?: string): string {
    const n = (network || "").toLowerCase()
    if (n === "websocket") return "ws"
    if (n === "splithttp") return "xhttp"
    if (n === "h2") return "http"
    return n
}

/** "Inherit (value)" so admins see what the inbound already sends. */
function inheritPlaceholder(current?: string | number | null): string {
    if (current === undefined || current === null || current === "") return "Inherit"
    return `Inherit (${current})`
}

const stripInherit = (v: string) => (v === INHERIT ? "" : v)

interface HostSettingsDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    host?: Host | HostWithRelations | null
    /** When provided, the dialog operates in embedded mode (scoped to this inbound). */
    inboundId?: number
    /** The target inbound, when the caller already has it. Drives which override
     *  fields are shown — without it the dialog falls back to showing all. */
    inbound?: Inbound
    /** When true, shows a node → inbound cascade selector (standalone mode). */
    showInboundSelector?: boolean
    onSuccess?: () => void
}

export function HostSettingsDialog({
    open,
    onOpenChange,
    host,
    inboundId: propInboundId,
    inbound: propInbound,
    showInboundSelector = false,
    onSuccess,
}: HostSettingsDialogProps) {
    const isEdit = !!host
    const [loading, setLoading] = useState(false)
    const queryClient = useQueryClient()

    const [tab, setTab] = useState("general")

    // Inbound selector state (standalone mode)
    const [selectedNodeId, setSelectedNodeId] = useState<string>("")
    const [selectedInboundId, setSelectedInboundId] = useState<string>("")

    const { data: nodes } = useNodes()
    const { data: inbounds } = useNodeInbounds(selectedNodeId ? parseInt(selectedNodeId) : 0)
    const { data: templates = [] } = useHostTemplates()
    const { data: existingTags = [] } = useHostTags()

    // Presentation
    const [remark, setRemark] = useState("")
    const [priority, setPriority] = useState("0")
    const [isDisabled, setIsDisabled] = useState(false)
    const [tags, setTags] = useState<string[]>([])
    const [tagInput, setTagInput] = useState("")

    // Connection
    const [address, setAddress] = useState("")
    const [port, setPort] = useState("")
    const [security, setSecurity] = useState("")
    const [sni, setSni] = useState("")
    const [alpn, setAlpn] = useState("")
    const [fingerprint, setFingerprint] = useState("")
    const [allowInsecure, setAllowInsecure] = useState(false)
    const [realityPbk, setRealityPbk] = useState("")
    const [realitySid, setRealitySid] = useState("")
    const [realitySpx, setRealitySpx] = useState("")

    // Transport
    const [hostField, setHostField] = useState("")
    const [path, setPath] = useState("")
    const [serviceName, setServiceName] = useState("")
    const [mode, setMode] = useState("")
    const [headerType, setHeaderType] = useState("")

    // Protocol-specific
    const [flow, setFlow] = useState("")
    const [encryption, setEncryption] = useState("")
    const [vmessSecurity, setVmessSecurity] = useState("")
    const [obfsPassword, setObfsPassword] = useState("")
    const [portRange, setPortRange] = useState("")

    // Advanced — fragment
    const [fragPackets, setFragPackets] = useState("")
    const [fragLength, setFragLength] = useState("")
    const [fragInterval, setFragInterval] = useState("")

    // Save as template
    const [showSaveTemplate, setShowSaveTemplate] = useState(false)
    const [templateName, setTemplateName] = useState("")
    const [templateDesc, setTemplateDesc] = useState("")

    // Determine effective inbound ID
    const effectiveInboundId = propInboundId ?? (selectedInboundId ? parseInt(selectedInboundId) : 0)

    useEffect(() => {
        if (!open) return
        setShowSaveTemplate(false)
        setTemplateName("")
        setTemplateDesc("")
        setTab("general")
        if (host) {
            // Pre-select node/inbound from host's relation if available
            if (showInboundSelector && "inbound" in host && host.inbound) {
                const inb = host.inbound as Inbound
                setSelectedNodeId(String(inb.node_id))
                setSelectedInboundId(String(host.inbound_id))
            }
            setRemark(host.remark || "")
            setAddress(host.address || "")
            setPort(host.port != null ? String(host.port) : "")
            setSni(host.sni || "")
            setHostField(host.host || "")
            setPath(host.path || "")
            setAlpn(host.alpn || "")
            setFingerprint(host.fingerprint || "")
            setSecurity(host.security || "")
            setAllowInsecure(host.allow_insecure ?? false)
            setRealityPbk(host.reality_public_key || "")
            setRealitySid(host.reality_short_id || "")
            setRealitySpx(host.reality_spider_x || "")
            setServiceName(host.service_name || "")
            setMode(host.mode || "")
            setHeaderType(host.header_type || "")
            setFlow(host.flow || "")
            setEncryption(host.encryption || "")
            setVmessSecurity(host.vmess_security || "")
            setObfsPassword(host.obfs_password || "")
            setPortRange(host.port_range || "")
            setFragPackets(host.fragment_settings?.packets || "")
            setFragLength(host.fragment_settings?.length || "")
            setFragInterval(host.fragment_settings?.interval || "")
            setPriority(String(host.priority || 0))
            setIsDisabled(host.is_disabled)
            setTags(host.tags || [])
        } else {
            setRemark("")
            setAddress("")
            setPort("")
            setSni("")
            setHostField("")
            setPath("")
            setAlpn("")
            setFingerprint("")
            setSecurity("")
            setAllowInsecure(false)
            setRealityPbk("")
            setRealitySid("")
            setRealitySpx("")
            setServiceName("")
            setMode("")
            setHeaderType("")
            setFlow("")
            setEncryption("")
            setVmessSecurity("")
            setObfsPassword("")
            setPortRange("")
            setFragPackets("")
            setFragLength("")
            setFragInterval("")
            setPriority("0")
            setIsDisabled(false)
            setTags([])
            if (showInboundSelector) {
                setSelectedNodeId("")
                setSelectedInboundId("")
            }
        }
    }, [open, host, showInboundSelector])

    // The inbound this host decorates: passed in, carried on the host relation,
    // or picked in the cascade selector. Unknown → every field is shown.
    const targetInbound: Inbound | undefined =
        propInbound ??
        (host && "inbound" in host ? (host.inbound as Inbound | undefined) : undefined) ??
        inbounds?.find((i) => i.id === effectiveInboundId)

    const protocol = (targetInbound?.protocol || "").toLowerCase()
    const network = normalizeNetwork(targetInbound?.network)
    const knownProtocol = protocol !== ""
    const isWireGuard = protocol === "wireguard"
    const isHysteria = protocol === "hysteria2" || protocol === "hysteria"
    const isVless = protocol === "vless"
    const isVmess = protocol === "vmess"

    // Effective security decides whether SNI/fingerprint land on TLS or Reality
    // (same rule ApplyHostOverrides uses server-side).
    const effSecurity = (stripInherit(security) || targetInbound?.security || "").toLowerCase()
    const isReality = effSecurity === "reality"
    const hasSecurity = effSecurity === "tls" || isReality || !knownProtocol

    // WireGuard links carry no SNI/transport; Hysteria2 is QUIC — no transport.
    const showTransportTab = !knownProtocol || (!isWireGuard && !isHysteria)
    const showProtocolTab = !knownProtocol || isVless || isVmess || isHysteria || isWireGuard
    const showAdvancedTab = !knownProtocol || !isWireGuard

    // Transport fields by network (unknown network → show all).
    const anyNetwork = network === ""
    const showHostPath = anyNetwork || ["ws", "httpupgrade", "xhttp", "http", "tcp"].includes(network)
    const showServiceName = anyNetwork || network === "grpc"
    const showMode = anyNetwork || network === "xhttp"
    const showHeaderType = anyNetwork || network === "tcp"

    const tabDefs = [
        { id: "general", label: "General" },
        { id: "connection", label: "Connection" },
        ...(showTransportTab ? [{ id: "transport", label: "Transport" }] : []),
        ...(showProtocolTab ? [{ id: "protocol", label: "Protocol" }] : []),
        ...(showAdvancedTab ? [{ id: "advanced", label: "Advanced" }] : []),
    ]

    // Keep the active tab valid when protocol/host-type changes hide it.
    useEffect(() => {
        if (!tabDefs.some((t) => t.id === tab)) setTab("general")
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [protocol, network])

    function handleFillFromInbound() {
        const inb = targetInbound
        if (!inb) return

        setAddress(inb.address || "")
        setPort(inb.port ? String(inb.port) : "")
        setSecurity(inb.security || "")

        if (inb.tls_settings) {
            setSni(inb.tls_settings.serverName || "")
            setAlpn(inb.tls_settings.alpn?.join(",") || "")
            setFingerprint(inb.tls_settings.fingerprint || "")
        }
        if (inb.reality_settings) {
            setSni(inb.reality_settings.serverNames?.[0] || inb.reality_settings.serverName || "")
            setFingerprint(inb.reality_settings.fingerprint || "")
            setRealityPbk(inb.reality_settings.publicKey || "")
            setRealitySid(inb.reality_settings.shortId || "")
            setRealitySpx(inb.reality_settings.spiderX || "")
        }
        if (inb.transport_settings) {
            setHostField(inb.transport_settings.host || "")
            setPath(inb.transport_settings.path || "")
            setServiceName(inb.transport_settings.serviceName || "")
            setMode(inb.transport_settings.mode || "")
            setHeaderType(inb.transport_settings.headerType || "")
        }
        if (inb.vless_settings) {
            setFlow(inb.vless_settings.flow || "")
            setEncryption(inb.vless_settings.encryption || "")
        }
        if (inb.vmess_settings?.security) {
            setVmessSecurity(inb.vmess_settings.security)
        }
        if (inb.port_range) {
            setPortRange(inb.port_range)
        }
        toast.success("Fields populated from inbound")
    }

    function handleLoadTemplate(templateId: string) {
        if (templateId === "_none") return
        const tmpl = templates.find((t) => t.id === parseInt(templateId))
        if (!tmpl) return
        if (tmpl.remark) setRemark(tmpl.remark)
        if (tmpl.address) setAddress(tmpl.address)
        if (tmpl.port != null) setPort(String(tmpl.port))
        if (tmpl.sni) setSni(tmpl.sni)
        if (tmpl.host) setHostField(tmpl.host)
        if (tmpl.path) setPath(tmpl.path)
        if (tmpl.alpn) setAlpn(tmpl.alpn)
        if (tmpl.fingerprint) setFingerprint(tmpl.fingerprint)
        if (tmpl.security) setSecurity(tmpl.security)
        if (tmpl.allow_insecure != null) setAllowInsecure(tmpl.allow_insecure)
        if (tmpl.reality_public_key) setRealityPbk(tmpl.reality_public_key)
        if (tmpl.reality_short_id) setRealitySid(tmpl.reality_short_id)
        if (tmpl.reality_spider_x) setRealitySpx(tmpl.reality_spider_x)
        if (tmpl.service_name) setServiceName(tmpl.service_name)
        if (tmpl.mode) setMode(tmpl.mode)
        if (tmpl.header_type) setHeaderType(tmpl.header_type)
        if (tmpl.flow) setFlow(tmpl.flow)
        if (tmpl.encryption) setEncryption(tmpl.encryption)
        if (tmpl.vmess_security) setVmessSecurity(tmpl.vmess_security)
        if (tmpl.obfs_password) setObfsPassword(tmpl.obfs_password)
        if (tmpl.port_range) setPortRange(tmpl.port_range)
        if (tmpl.fragment_settings) {
            setFragPackets(tmpl.fragment_settings.packets || "")
            setFragLength(tmpl.fragment_settings.length || "")
            setFragInterval(tmpl.fragment_settings.interval || "")
        }
        if (tmpl.priority != null) setPriority(String(tmpl.priority))
        toast.success(`Loaded template "${tmpl.name}"`)
    }

    function handleAddTag() {
        const tag = tagInput.trim().toLowerCase()
        if (tag && !tags.includes(tag)) {
            setTags([...tags, tag])
        }
        setTagInput("")
    }

    function handleRemoveTag(tag: string) {
        setTags(tags.filter((t) => t !== tag))
    }

    /** Fragment is stored as one JSON blob — all-empty means "no fragment". */
    function fragmentPayload() {
        if (!fragPackets && !fragLength && !fragInterval) return null
        return { packets: fragPackets, length: fragLength, interval: fragInterval }
    }

    /** Every override field, sentinel-stripped. Shared by save + save-as-template. */
    function overridePayload() {
        return {
            address,
            port: port ? parseInt(port) : null,
            sni,
            host: hostField,
            path,
            alpn,
            fingerprint: stripInherit(fingerprint),
            security: stripInherit(security),
            allow_insecure: allowInsecure,
            reality_public_key: realityPbk,
            reality_short_id: realitySid,
            reality_spider_x: realitySpx,
            service_name: serviceName,
            mode: stripInherit(mode),
            header_type: stripInherit(headerType),
            flow: stripInherit(flow),
            encryption: encryption,
            vmess_security: stripInherit(vmessSecurity),
            obfs_password: obfsPassword,
            port_range: portRange,
            fragment_settings: fragmentPayload(),
        }
    }

    async function handleSaveAsTemplate() {
        if (!templateName.trim()) {
            toast.error("Template name is required")
            return
        }
        try {
            const res = await createHostTemplate({
                name: templateName.trim(),
                description: templateDesc.trim(),
                remark,
                ...overridePayload(),
                priority: parseInt(priority) || 0,
            })
            if (!res.success) throw new Error(res.error || "Failed to save template")
            toast.success("Template saved")
            queryClient.invalidateQueries({ queryKey: queryKeys.hostTemplates })
            setShowSaveTemplate(false)
            setTemplateName("")
            setTemplateDesc("")
        } catch (err: any) {
            toast.error(err.message)
        }
    }

    async function handleSave() {
        if (showInboundSelector && !isEdit && !effectiveInboundId) {
            toast.error("Please select a node and inbound")
            return
        }

        setLoading(true)
        try {
            const data: Partial<Host> = {
                remark,
                ...overridePayload(),
                priority: parseInt(priority) || 0,
                is_disabled: isDisabled,
                tags,
            }

            if (isEdit && host) {
                const result = await updateHost(host.id, data)
                if (!result.success) throw new Error(result.error || "Failed to update host")
                toast.success("Host updated")
            } else if (showInboundSelector) {
                const result = await createHost({ ...data, inbound_id: effectiveInboundId })
                if (!result.success) throw new Error(result.error || "Failed to create host")
                toast.success("Host created")
            } else {
                const result = await addInboundHost(propInboundId!, data)
                if (!result.success) throw new Error(result.error || "Failed to create host")
                toast.success("Host created")
            }

            queryClient.invalidateQueries({ queryKey: queryKeys.hosts })
            queryClient.invalidateQueries({ queryKey: queryKeys.hostTags() })
            onOpenChange(false)
            onSuccess?.()
        } catch (err: any) {
            toast.error(err.message || "Failed to save host")
        } finally {
            setLoading(false)
        }
    }

    // Tag suggestions: existing tags not already selected
    const tagSuggestions = existingTags.filter((t) => !tags.includes(t))

    const note = (text: string) => (
        <p className="rounded-md border border-dashed bg-muted/30 px-3 py-2 text-[11px] text-muted-foreground">{text}</p>
    )

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[600px] max-h-[88vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>{isEdit ? "Edit Host" : "Add Host"}</DialogTitle>
                    <DialogDescription>
                        Presentation-layer override. Each host produces its own config link; empty fields inherit from
                        the inbound.
                    </DialogDescription>
                    {targetInbound && (
                        <div className="flex flex-wrap items-center gap-1.5 pt-1">
                            <Badge variant="secondary" className="h-5 px-1.5 text-[10px] uppercase">
                                {protocol || "unknown"}
                            </Badge>
                            {network && (
                                <Badge variant="outline" className="h-5 px-1.5 text-[10px] uppercase">
                                    {network}
                                </Badge>
                            )}
                            <Badge variant="outline" className="h-5 px-1.5 text-[10px] uppercase">
                                {effSecurity || "none"}
                            </Badge>
                            <span className="text-[10px] text-muted-foreground">
                                {targetInbound.tag} — only fields this inbound uses are shown
                            </span>
                        </div>
                    )}
                </DialogHeader>

                <Tabs value={tab} onValueChange={setTab} className="w-full">
                    {tabDefs.length > 1 && (
                        <TabsList className="w-full">
                            {tabDefs.map((t) => (
                                <TabsTrigger key={t.id} value={t.id} className="flex-1 text-xs">
                                    {t.label}
                                </TabsTrigger>
                            ))}
                        </TabsList>
                    )}

                    {/* ---------------- General ---------------- */}
                    <TabsContent value="general" className="space-y-4 py-2">
                        {/* Server Host: Inbound Selector (standalone mode) */}
                        {showInboundSelector && (
                            <div className="space-y-3 p-3 rounded-lg border border-dashed bg-muted/30">
                                <Label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                                    Target Inbound
                                </Label>
                                <div className="grid grid-cols-2 gap-3">
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Node</Label>
                                        <Select
                                            value={selectedNodeId}
                                            onValueChange={(v) => {
                                                setSelectedNodeId(v)
                                                setSelectedInboundId("")
                                            }}
                                            disabled={isEdit}
                                        >
                                            <SelectTrigger className="h-9 text-sm">
                                                <SelectValue placeholder="Select node..." />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {nodes?.map((n) => (
                                                    <SelectItem key={n.id} value={String(n.id)}>
                                                        {n.name} ({n.ip})
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Inbound</Label>
                                        <Select
                                            value={selectedInboundId}
                                            onValueChange={setSelectedInboundId}
                                            disabled={!selectedNodeId || isEdit}
                                        >
                                            <SelectTrigger className="h-9 text-sm">
                                                <SelectValue placeholder="Select inbound..." />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {inbounds?.map((ib) => (
                                                    <SelectItem key={ib.id} value={String(ib.id)}>
                                                        {ib.tag} ({ib.protocol}:{ib.port})
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                </div>
                                {targetInbound && (
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        className="h-7 text-xs gap-1.5"
                                        onClick={handleFillFromInbound}
                                    >
                                        <HiOutlineDownload className="h-3.5 w-3.5" />
                                        Fill from Inbound
                                    </Button>
                                )}
                            </div>
                        )}

                        {/* Load from Template */}
                        {templates.length > 0 && (
                            <div className="space-y-1.5">
                                <Label className="text-xs">Load from Template</Label>
                                <Select onValueChange={handleLoadTemplate}>
                                    <SelectTrigger className="h-9 text-sm">
                                        <SelectValue placeholder="Select a template..." />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="_none">None</SelectItem>
                                        {templates.map((t) => (
                                            <SelectItem key={t.id} value={String(t.id)}>
                                                {t.name}
                                                {t.description ? ` - ${t.description}` : ""}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                        )}

                        {/* Remark Template */}
                        <div className="space-y-2">
                            <Label>Remark Template</Label>
                            <Input
                                value={remark}
                                onChange={(e) => setRemark(e.target.value)}
                                placeholder="{flag} {node} CDN | {data_left}"
                            />
                            <div className="flex flex-wrap gap-1">
                                {REMARK_VARIABLES.map((v) => (
                                    <button
                                        key={v.var}
                                        type="button"
                                        className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground hover:bg-primary/10 hover:text-primary transition-colors"
                                        title={v.desc}
                                        onClick={() => setRemark((prev) => prev + v.var)}
                                    >
                                        {v.var}
                                    </button>
                                ))}
                            </div>
                        </div>

                        {/* Tags */}
                        <div className="space-y-2">
                            <Label className="text-xs">Tags</Label>
                            <div className="flex items-center gap-2">
                                <Input
                                    value={tagInput}
                                    onChange={(e) => setTagInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === "Enter") {
                                            e.preventDefault()
                                            handleAddTag()
                                        }
                                    }}
                                    placeholder="Add tag..."
                                    className="h-8 text-sm flex-1"
                                    list="tag-suggestions"
                                />
                                <datalist id="tag-suggestions">
                                    {tagSuggestions.map((t) => (
                                        <option key={t} value={t} />
                                    ))}
                                </datalist>
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="h-8 text-xs px-2"
                                    onClick={handleAddTag}
                                    disabled={!tagInput.trim()}
                                >
                                    Add
                                </Button>
                            </div>
                            {tags.length > 0 && (
                                <div className="flex flex-wrap gap-1">
                                    {tags.map((tag) => (
                                        <Badge key={tag} variant="secondary" className="text-xs h-5 px-1.5 gap-1">
                                            {tag}
                                            <button
                                                onClick={() => handleRemoveTag(tag)}
                                                className="text-muted-foreground hover:text-foreground"
                                            >
                                                <HiOutlineX className="h-3 w-3" />
                                            </button>
                                        </Badge>
                                    ))}
                                </div>
                            )}
                        </div>

                        {/* Priority & Disabled */}
                        <div className="grid grid-cols-2 gap-3">
                            <div className="space-y-1.5">
                                <Label className="text-xs">Priority</Label>
                                <Input
                                    type="number"
                                    value={priority}
                                    onChange={(e) => setPriority(e.target.value)}
                                    placeholder="0"
                                    className="h-9 text-sm"
                                />
                                <p className="text-[10px] text-muted-foreground">Lower = higher priority</p>
                            </div>
                            <div className="flex items-center justify-between pt-6">
                                <Label className="text-xs">Disabled</Label>
                                <Switch checked={isDisabled} onCheckedChange={setIsDisabled} />
                            </div>
                        </div>
                    </TabsContent>

                    {/* ---------------- Connection ---------------- */}
                    <TabsContent value="connection" className="space-y-4 py-2">
                        <div className="grid grid-cols-2 gap-3">
                            <div className="space-y-1.5">
                                <Label className="text-xs">Address</Label>
                                <Input
                                    value={address}
                                    onChange={(e) => setAddress(e.target.value)}
                                    placeholder={inheritPlaceholder(targetInbound?.address)}
                                    className="h-9 text-sm"
                                />
                                <p className="text-[10px] text-muted-foreground">CDN hostname or IP clients dial</p>
                            </div>
                            <div className="space-y-1.5">
                                <Label className="text-xs">Port</Label>
                                <Input
                                    type="number"
                                    value={port}
                                    onChange={(e) => setPort(e.target.value)}
                                    placeholder={inheritPlaceholder(targetInbound?.port)}
                                    className="h-9 text-sm"
                                />
                            </div>
                        </div>

                        {isWireGuard ? (
                            note(
                                "WireGuard: Address and Port become the peer's Endpoint, and Remark labels this host in the customer's server picker. Keys, DNS and MTU come from the inbound."
                            )
                        ) : (
                            <>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Security</Label>
                                    <Select value={security} onValueChange={setSecurity}>
                                        <SelectTrigger className="h-9 text-sm">
                                            <SelectValue placeholder={inheritPlaceholder(targetInbound?.security)} />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value={INHERIT}>Inherit</SelectItem>
                                            {SECURITY_TYPES.map((s) => (
                                                <SelectItem key={s.value} value={s.value}>
                                                    {s.label}
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>

                                {!hasSecurity && note("This inbound runs plain TCP — TLS/Reality fields are ignored by clients.")}

                                {hasSecurity && (
                                    <>
                                        <div className="grid grid-cols-2 gap-3">
                                            <div className="space-y-1.5">
                                                <Label className="text-xs">SNI</Label>
                                                <Input
                                                    value={sni}
                                                    onChange={(e) => setSni(e.target.value)}
                                                    placeholder={inheritPlaceholder(
                                                        isReality
                                                            ? targetInbound?.reality_settings?.serverNames?.[0]
                                                            : targetInbound?.tls_settings?.serverName
                                                    )}
                                                    className="h-9 text-sm"
                                                />
                                            </div>
                                            <div className="space-y-1.5">
                                                <Label className="text-xs">Fingerprint</Label>
                                                <Select value={fingerprint} onValueChange={setFingerprint}>
                                                    <SelectTrigger className="h-9 text-sm">
                                                        <SelectValue placeholder="Inherit" />
                                                    </SelectTrigger>
                                                    <SelectContent>
                                                        <SelectItem value={INHERIT}>Inherit</SelectItem>
                                                        {TLS_FINGERPRINTS.map((fp) => (
                                                            <SelectItem key={fp.value} value={fp.value}>
                                                                {fp.label}
                                                            </SelectItem>
                                                        ))}
                                                    </SelectContent>
                                                </Select>
                                            </div>
                                        </div>

                                        {isReality ? (
                                            <div className="space-y-3 rounded-lg border border-dashed p-3">
                                                <Label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                                                    Reality
                                                </Label>
                                                <div className="space-y-1.5">
                                                    <Label className="text-xs">Public Key (pbk)</Label>
                                                    <Input
                                                        value={realityPbk}
                                                        onChange={(e) => setRealityPbk(e.target.value)}
                                                        placeholder={inheritPlaceholder(
                                                            targetInbound?.reality_settings?.publicKey
                                                        )}
                                                        className="h-9 text-sm font-mono"
                                                    />
                                                </div>
                                                <div className="grid grid-cols-2 gap-3">
                                                    <div className="space-y-1.5">
                                                        <Label className="text-xs">Short ID (sid)</Label>
                                                        <Input
                                                            value={realitySid}
                                                            onChange={(e) => setRealitySid(e.target.value)}
                                                            placeholder={inheritPlaceholder(
                                                                targetInbound?.reality_settings?.shortId
                                                            )}
                                                            className="h-9 text-sm font-mono"
                                                        />
                                                    </div>
                                                    <div className="space-y-1.5">
                                                        <Label className="text-xs">SpiderX (spx)</Label>
                                                        <Input
                                                            value={realitySpx}
                                                            onChange={(e) => setRealitySpx(e.target.value)}
                                                            placeholder={inheritPlaceholder(
                                                                targetInbound?.reality_settings?.spiderX
                                                            )}
                                                            className="h-9 text-sm"
                                                        />
                                                    </div>
                                                </div>
                                                <p className="text-[10px] text-muted-foreground">
                                                    Override only when this host fronts a different Reality server than the
                                                    inbound advertises.
                                                </p>
                                            </div>
                                        ) : (
                                            <div className="grid grid-cols-2 gap-3">
                                                <div className="space-y-1.5">
                                                    <Label className="text-xs">ALPN</Label>
                                                    <Input
                                                        value={alpn}
                                                        onChange={(e) => setAlpn(e.target.value)}
                                                        placeholder={inheritPlaceholder(
                                                            targetInbound?.tls_settings?.alpn?.join(",")
                                                        )}
                                                        className="h-9 text-sm"
                                                    />
                                                </div>
                                                <div className="flex items-center justify-between pt-6">
                                                    <Label className="text-xs">Allow Insecure</Label>
                                                    <Switch checked={allowInsecure} onCheckedChange={setAllowInsecure} />
                                                </div>
                                            </div>
                                        )}
                                    </>
                                )}
                            </>
                        )}
                    </TabsContent>

                    {/* ---------------- Transport ---------------- */}
                    <TabsContent value="transport" className="space-y-4 py-2">
                        {!anyNetwork && !showHostPath && !showServiceName && !showMode && !showHeaderType &&
                            note(`The ${network} transport carries no host-overridable fields.`)}

                        {showHostPath && (
                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Host header</Label>
                                    <Input
                                        value={hostField}
                                        onChange={(e) => setHostField(e.target.value)}
                                        placeholder={inheritPlaceholder(targetInbound?.transport_settings?.host)}
                                        className="h-9 text-sm"
                                    />
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Path</Label>
                                    <Input
                                        value={path}
                                        onChange={(e) => setPath(e.target.value)}
                                        placeholder={inheritPlaceholder(targetInbound?.transport_settings?.path)}
                                        className="h-9 text-sm"
                                    />
                                </div>
                            </div>
                        )}

                        {showServiceName && (
                            <div className="space-y-1.5">
                                <Label className="text-xs">gRPC serviceName</Label>
                                <Input
                                    value={serviceName}
                                    onChange={(e) => setServiceName(e.target.value)}
                                    placeholder={inheritPlaceholder(targetInbound?.transport_settings?.serviceName)}
                                    className="h-9 text-sm"
                                />
                                <p className="text-[10px] text-muted-foreground">
                                    Left empty, Path is mirrored into serviceName.
                                </p>
                            </div>
                        )}

                        {(showMode || showHeaderType) && (
                            <div className="grid grid-cols-2 gap-3">
                                {showMode && (
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">XHTTP mode</Label>
                                        <Select value={mode} onValueChange={setMode}>
                                            <SelectTrigger className="h-9 text-sm">
                                                <SelectValue
                                                    placeholder={inheritPlaceholder(targetInbound?.transport_settings?.mode)}
                                                />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value={INHERIT}>Inherit</SelectItem>
                                                {XHTTP_MODES.map((m) => (
                                                    <SelectItem key={m.value} value={m.value}>
                                                        {m.label}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                )}
                                {showHeaderType && (
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">TCP header type</Label>
                                        <Select value={headerType} onValueChange={setHeaderType}>
                                            <SelectTrigger className="h-9 text-sm">
                                                <SelectValue
                                                    placeholder={inheritPlaceholder(
                                                        targetInbound?.transport_settings?.headerType
                                                    )}
                                                />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value={INHERIT}>Inherit</SelectItem>
                                                {TCP_HEADER_TYPES.map((h) => (
                                                    <SelectItem key={h.value} value={h.value}>
                                                        {h.label}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                )}
                            </div>
                        )}
                    </TabsContent>

                    {/* ---------------- Protocol ---------------- */}
                    <TabsContent value="protocol" className="space-y-4 py-2">
                        {(isVless || !knownProtocol) && (
                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1.5">
                                    <Label className="text-xs">VLESS flow</Label>
                                    <Select value={flow} onValueChange={setFlow}>
                                        <SelectTrigger className="h-9 text-sm">
                                            <SelectValue
                                                placeholder={inheritPlaceholder(targetInbound?.vless_settings?.flow)}
                                            />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value={INHERIT}>Inherit</SelectItem>
                                            {VLESS_FLOWS.map((f) => (
                                                <SelectItem key={f.value} value={f.value}>
                                                    {f.label}
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                    <p className="text-[10px] text-muted-foreground">
                                        Vision needs raw TCP + TLS/Reality — clear it for CDN hosts.
                                    </p>
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">VLESS encryption</Label>
                                    <Input
                                        value={encryption}
                                        onChange={(e) => setEncryption(e.target.value)}
                                        placeholder={inheritPlaceholder(targetInbound?.vless_settings?.encryption || "none")}
                                        className="h-9 text-sm font-mono"
                                    />
                                    <p className="text-[10px] text-muted-foreground">
                                        Post-quantum (mlkem768…) client string; "none" for classic VLESS.
                                    </p>
                                </div>
                            </div>
                        )}

                        {(isVmess || !knownProtocol) && (
                            <div className="space-y-1.5">
                                <Label className="text-xs">VMess security (scy)</Label>
                                <Select value={vmessSecurity} onValueChange={setVmessSecurity}>
                                    <SelectTrigger className="h-9 text-sm">
                                        <SelectValue
                                            placeholder={inheritPlaceholder(targetInbound?.vmess_settings?.security || "auto")}
                                        />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value={INHERIT}>Inherit</SelectItem>
                                        {VMESS_SECURITIES.map((s) => (
                                            <SelectItem key={s.value} value={s.value}>
                                                {s.label}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                        )}

                        {(isHysteria || !knownProtocol) && (
                            <div className="space-y-3 rounded-lg border border-dashed p-3">
                                <Label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                                    Hysteria2
                                </Label>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Obfs password (salamander)</Label>
                                    <Input
                                        value={obfsPassword}
                                        onChange={(e) => setObfsPassword(e.target.value)}
                                        placeholder="Inherit from inbound finalmask"
                                        className="h-9 text-sm font-mono"
                                    />
                                    <p className="text-[10px] text-muted-foreground">
                                        Must match the server's mask password, or the QUIC packets never decode.
                                    </p>
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Port hopping (mport)</Label>
                                    <Input
                                        value={portRange}
                                        onChange={(e) => setPortRange(e.target.value)}
                                        placeholder={inheritPlaceholder(targetInbound?.port_range)}
                                        className="h-9 text-sm"
                                    />
                                    <p className="text-[10px] text-muted-foreground">e.g. 20000-25000 — the listener must bind that range.</p>
                                </div>
                            </div>
                        )}

                        {isWireGuard && note("WireGuard has no link-level protocol overrides — see the Connection tab.")}
                    </TabsContent>

                    {/* ---------------- Advanced ---------------- */}
                    <TabsContent value="advanced" className="space-y-4 py-2">
                        <div className="space-y-3 rounded-lg border border-dashed p-3">
                            <Label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                                Fragment (anti-censorship)
                            </Label>
                            <div className="grid grid-cols-3 gap-2">
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Packets</Label>
                                    <Input
                                        value={fragPackets}
                                        onChange={(e) => setFragPackets(e.target.value)}
                                        placeholder="tlshello"
                                        className="h-9 text-sm"
                                    />
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Length</Label>
                                    <Input
                                        value={fragLength}
                                        onChange={(e) => setFragLength(e.target.value)}
                                        placeholder="100-200"
                                        className="h-9 text-sm"
                                    />
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Interval</Label>
                                    <Input
                                        value={fragInterval}
                                        onChange={(e) => setFragInterval(e.target.value)}
                                        placeholder="10-20"
                                        className="h-9 text-sm"
                                    />
                                </div>
                            </div>
                            <p className="text-[10px] text-muted-foreground">
                                Emitted as fragment=packets,length,interval. Clients that don't support it ignore the value.
                            </p>
                        </div>

                        {!showInboundSelector && targetInbound && (
                            <Button variant="outline" size="sm" className="h-8 text-xs gap-1.5" onClick={handleFillFromInbound}>
                                <HiOutlineDownload className="h-3.5 w-3.5" />
                                Fill from Inbound
                            </Button>
                        )}
                    </TabsContent>
                </Tabs>

                {/* Save as Template inline */}
                {showSaveTemplate && (
                    <div className="space-y-2 p-3 rounded-lg border border-dashed bg-muted/30">
                        <Label className="text-xs font-medium">Save as Template</Label>
                        <Input
                            value={templateName}
                            onChange={(e) => setTemplateName(e.target.value)}
                            placeholder="Template name *"
                            className="h-8 text-sm"
                        />
                        <Input
                            value={templateDesc}
                            onChange={(e) => setTemplateDesc(e.target.value)}
                            placeholder="Description (optional)"
                            className="h-8 text-sm"
                        />
                        <div className="flex items-center gap-2">
                            <Button size="sm" className="h-7 text-xs" onClick={handleSaveAsTemplate} disabled={!templateName.trim()}>
                                Save
                            </Button>
                            <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setShowSaveTemplate(false)}>
                                Cancel
                            </Button>
                        </div>
                    </div>
                )}

                <DialogFooter className="gap-2 sm:gap-0">
                    {!showSaveTemplate && (
                        <Button
                            variant="ghost"
                            size="sm"
                            className="gap-1 mr-auto text-xs"
                            onClick={() => setShowSaveTemplate(true)}
                        >
                            <HiOutlineSave className="h-3.5 w-3.5" />
                            Save as Template
                        </Button>
                    )}
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button onClick={handleSave} disabled={loading}>
                        {loading ? "Saving..." : isEdit ? "Update" : "Create"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
