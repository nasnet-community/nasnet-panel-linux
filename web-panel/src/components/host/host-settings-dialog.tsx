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
import { toast } from "sonner"
import { useQueryClient } from "@tanstack/react-query"
import type { Host, HostWithRelations, Inbound } from "@/lib/types"
import { addInboundHost, updateHost, createHost, createHostTemplate } from "@/lib/admin-api"
import { queryKeys } from "@/lib/queries/keys"
import { SECURITY_TYPES, TLS_FINGERPRINTS } from "@/lib/types"
import { useNodes, useNodeInbounds, useHostTemplates, useHostTags } from "@/lib/queries"
import { HiOutlineDownload, HiOutlineInformationCircle, HiOutlineSave, HiOutlineX } from "react-icons/hi"

type HostType = "server" | "info"

interface HostSettingsDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    host?: Host | HostWithRelations | null
    /** When provided, the dialog operates in embedded mode (scoped to this inbound). */
    inboundId?: number
    /** When true, shows a node → inbound cascade selector (standalone mode). */
    showInboundSelector?: boolean
    onSuccess?: () => void
}

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

export function HostSettingsDialog({
    open,
    onOpenChange,
    host,
    inboundId: propInboundId,
    showInboundSelector = false,
    onSuccess,
}: HostSettingsDialogProps) {
    const isEdit = !!host
    const [loading, setLoading] = useState(false)
    const queryClient = useQueryClient()

    // Host type: server (inbound) only — info hosts are managed via the API
    const [hostType, setHostType] = useState<HostType>("server")

    // Inbound selector state (standalone mode)
    const [selectedNodeId, setSelectedNodeId] = useState<string>("")
    const [selectedInboundId, setSelectedInboundId] = useState<string>("")

    const { data: nodes } = useNodes()
    const { data: inbounds } = useNodeInbounds(selectedNodeId ? parseInt(selectedNodeId) : 0)
    const { data: templates = [] } = useHostTemplates()
    const { data: existingTags = [] } = useHostTags()

    // Form fields
    const [remark, setRemark] = useState("")
    const [address, setAddress] = useState("")
    const [port, setPort] = useState("")
    const [sni, setSni] = useState("")
    const [hostField, setHostField] = useState("")
    const [path, setPath] = useState("")
    const [alpn, setAlpn] = useState("")
    const [fingerprint, setFingerprint] = useState("")
    const [security, setSecurity] = useState("")
    const [allowInsecure, setAllowInsecure] = useState(false)
    const [priority, setPriority] = useState("0")
    const [isDisabled, setIsDisabled] = useState(false)
    const [tags, setTags] = useState<string[]>([])
    const [tagInput, setTagInput] = useState("")

    // Save as template
    const [showSaveTemplate, setShowSaveTemplate] = useState(false)
    const [templateName, setTemplateName] = useState("")
    const [templateDesc, setTemplateDesc] = useState("")

    // Determine effective inbound ID
    const effectiveInboundId = propInboundId ?? (selectedInboundId ? parseInt(selectedInboundId) : 0)

    useEffect(() => {
        if (open) {
            setShowSaveTemplate(false)
            setTemplateName("")
            setTemplateDesc("")
            if (host) {
                setHostType("server")
                // Pre-select node/inbound from host's relation if available
                if (showInboundSelector && 'inbound' in host && host.inbound) {
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
                setPriority(String(host.priority || 0))
                setIsDisabled(host.is_disabled)
                setTags(host.tags || [])
            } else {
                setHostType("server")
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
                setPriority("0")
                setIsDisabled(false)
                setTags([])
                if (showInboundSelector) {
                    setSelectedNodeId("")
                    setSelectedInboundId("")
                }
            }
        }
    }, [open, host, showInboundSelector])

    function handleFillFromInbound() {
        if (!inbounds || !selectedInboundId) return
        const inb = inbounds.find((i) => i.id === parseInt(selectedInboundId))
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
            setSni(inb.reality_settings.serverNames?.[0] || "")
            setFingerprint(inb.reality_settings.fingerprint || "")
        }
        if (inb.transport_settings) {
            setHostField(inb.transport_settings.host || "")
            setPath(inb.transport_settings.path || "")
        }
        toast.success("Fields populated from inbound")
    }

    function handleLoadTemplate(templateId: string) {
        if (templateId === "_none") return
        const tmpl = templates.find(t => t.id === parseInt(templateId))
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
        setTags(tags.filter(t => t !== tag))
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
                address,
                port: port ? parseInt(port) : null,
                sni,
                host: hostField,
                path,
                alpn,
                fingerprint: fingerprint === "inherit" ? "" : fingerprint,
                security: security === "inherit" ? "" : security,
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
                address,
                port: port ? parseInt(port) : null,
                sni,
                host: hostField,
                path,
                alpn,
                fingerprint: fingerprint === "inherit" ? "" : fingerprint,
                security: security === "inherit" ? "" : security,
                allow_insecure: allowInsecure,
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

    const isInfoHost = hostType === "info"

    // Tag suggestions: existing tags not already selected
    const tagSuggestions = existingTags.filter(t => !tags.includes(t))

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[520px] max-h-[85vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>{isEdit ? "Edit Host" : "Add Host"}</DialogTitle>
                    <DialogDescription>
                        {isInfoHost
                            ? "Create an info-only host that displays subscription stats in the client. Not a connectable server."
                            : isEdit
                            ? "Modify host override settings. Empty fields inherit from the inbound."
                            : "Add a presentation-layer host. Each host produces a separate config link. Empty fields inherit from the inbound."
                        }
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4 py-2">
                    {/* Server Host: Inbound Selector (standalone mode) */}
                    {!isInfoHost && showInboundSelector && (
                        <div className="space-y-3 p-3 rounded-lg border border-dashed bg-muted/30">
                            <Label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Target Inbound</Label>
                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Node</Label>
                                    <Select
                                        value={selectedNodeId}
                                        onValueChange={(v) => { setSelectedNodeId(v); setSelectedInboundId("") }}
                                        disabled={isEdit}
                                    >
                                        <SelectTrigger className="h-9 text-sm"><SelectValue placeholder="Select node..." /></SelectTrigger>
                                        <SelectContent>
                                            {nodes?.map((n) => (
                                                <SelectItem key={n.id} value={String(n.id)}>{n.name} ({n.ip})</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Inbound</Label>
                                    <Select value={selectedInboundId} onValueChange={setSelectedInboundId} disabled={!selectedNodeId || isEdit}>
                                        <SelectTrigger className="h-9 text-sm"><SelectValue placeholder="Select inbound..." /></SelectTrigger>
                                        <SelectContent>
                                            {inbounds?.map((ib) => (
                                                <SelectItem key={ib.id} value={String(ib.id)}>{ib.tag} ({ib.protocol}:{ib.port})</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>
                            </div>
                            {selectedInboundId && (
                                <Button variant="outline" size="sm" className="h-7 text-xs gap-1.5" onClick={handleFillFromInbound}>
                                    <HiOutlineDownload className="h-3.5 w-3.5" />
                                    Fill from Inbound
                                </Button>
                            )}
                        </div>
                    )}

                    {/* Load from Template */}
                    {!isInfoHost && templates.length > 0 && (
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
                                            {t.name}{t.description ? ` - ${t.description}` : ""}
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
                            placeholder={isInfoHost ? "📊 {data_left} | ⏳ {time_left}" : "{flag} {node} CDN | {data_left}"}
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
                                onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); handleAddTag() } }}
                                placeholder="Add tag..."
                                className="h-8 text-sm flex-1"
                                list="tag-suggestions"
                            />
                            <datalist id="tag-suggestions">
                                {tagSuggestions.map(t => <option key={t} value={t} />)}
                            </datalist>
                            <Button variant="outline" size="sm" className="h-8 text-xs px-2" onClick={handleAddTag} disabled={!tagInput.trim()}>
                                Add
                            </Button>
                        </div>
                        {tags.length > 0 && (
                            <div className="flex flex-wrap gap-1">
                                {tags.map((tag) => (
                                    <Badge key={tag} variant="secondary" className="text-xs h-5 px-1.5 gap-1">
                                        {tag}
                                        <button onClick={() => handleRemoveTag(tag)} className="text-muted-foreground hover:text-foreground">
                                            <HiOutlineX className="h-3 w-3" />
                                        </button>
                                    </Badge>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* Connection Overrides — hidden for info hosts */}
                    {!isInfoHost && (
                        <>
                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Address</Label>
                                    <Input value={address} onChange={(e) => setAddress(e.target.value)} placeholder="CDN or IP" className="h-9 text-sm" />
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Port</Label>
                                    <Input type="number" value={port} onChange={(e) => setPort(e.target.value)} placeholder="Inherit" className="h-9 text-sm" />
                                </div>
                            </div>

                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1.5">
                                    <Label className="text-xs">SNI</Label>
                                    <Input value={sni} onChange={(e) => setSni(e.target.value)} placeholder="Inherit" className="h-9 text-sm" />
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Security</Label>
                                    <Select value={security} onValueChange={setSecurity}>
                                        <SelectTrigger className="h-9 text-sm"><SelectValue placeholder="Inherit" /></SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="inherit">Inherit</SelectItem>
                                            {SECURITY_TYPES.map((s) => (
                                                <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>
                            </div>

                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Host (Transport)</Label>
                                    <Input value={hostField} onChange={(e) => setHostField(e.target.value)} placeholder="Inherit" className="h-9 text-sm" />
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Path (Transport)</Label>
                                    <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="Inherit" className="h-9 text-sm" />
                                </div>
                            </div>

                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1.5">
                                    <Label className="text-xs">ALPN</Label>
                                    <Input value={alpn} onChange={(e) => setAlpn(e.target.value)} placeholder="h2,http/1.1" className="h-9 text-sm" />
                                </div>
                                <div className="space-y-1.5">
                                    <Label className="text-xs">Fingerprint</Label>
                                    <Select value={fingerprint} onValueChange={setFingerprint}>
                                        <SelectTrigger className="h-9 text-sm"><SelectValue placeholder="Inherit" /></SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="inherit">Inherit</SelectItem>
                                            {TLS_FINGERPRINTS.map((fp) => (
                                                <SelectItem key={fp.value} value={fp.value}>{fp.label}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>
                            </div>
                        </>
                    )}

                    {/* Priority & Toggles */}
                    <div className="grid grid-cols-2 gap-3">
                        <div className="space-y-1.5">
                            <Label className="text-xs">Priority</Label>
                            <Input type="number" value={priority} onChange={(e) => setPriority(e.target.value)} placeholder="0" className="h-9 text-sm" />
                            <p className="text-[10px] text-muted-foreground">Lower = higher priority</p>
                        </div>
                        <div className="space-y-3 pt-1">
                            {!isInfoHost && (
                                <div className="flex items-center justify-between">
                                    <Label className="text-xs">Allow Insecure</Label>
                                    <Switch checked={allowInsecure} onCheckedChange={setAllowInsecure} />
                                </div>
                            )}
                            <div className="flex items-center justify-between">
                                <Label className="text-xs">Disabled</Label>
                                <Switch checked={isDisabled} onCheckedChange={setIsDisabled} />
                            </div>
                        </div>
                    </div>

                    {/* Save as Template inline */}
                    {showSaveTemplate && (
                        <div className="space-y-2 p-3 rounded-lg border border-dashed bg-muted/30">
                            <Label className="text-xs font-medium">Save as Template</Label>
                            <Input value={templateName} onChange={(e) => setTemplateName(e.target.value)} placeholder="Template name *" className="h-8 text-sm" />
                            <Input value={templateDesc} onChange={(e) => setTemplateDesc(e.target.value)} placeholder="Description (optional)" className="h-8 text-sm" />
                            <div className="flex items-center gap-2">
                                <Button size="sm" className="h-7 text-xs" onClick={handleSaveAsTemplate} disabled={!templateName.trim()}>Save</Button>
                                <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setShowSaveTemplate(false)}>Cancel</Button>
                            </div>
                        </div>
                    )}
                </div>

                <DialogFooter className="gap-2 sm:gap-0">
                    {!isInfoHost && !showSaveTemplate && (
                        <Button variant="ghost" size="sm" className="gap-1 mr-auto text-xs" onClick={() => setShowSaveTemplate(true)}>
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
