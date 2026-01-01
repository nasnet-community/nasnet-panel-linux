import { useState } from "react"
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetFooter,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { SECURITY_TYPES, TLS_FINGERPRINTS } from "@/lib/types"
import type { HostTemplate } from "@/lib/types"
import {
    useHostTemplates,
    useCreateHostTemplateMutation,
    useUpdateHostTemplateMutation,
    useDeleteHostTemplateMutation,
} from "@/lib/queries/use-hosts"
import { HiOutlinePlus, HiOutlinePencil, HiOutlineTrash } from "react-icons/hi"

interface HostTemplateDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
}

export function HostTemplateDialog({ open, onOpenChange }: HostTemplateDialogProps) {
    const { data: templates = [] } = useHostTemplates()
    const createMutation = useCreateHostTemplateMutation()
    const updateMutation = useUpdateHostTemplateMutation()
    const deleteMutation = useDeleteHostTemplateMutation()

    const [editingTemplate, setEditingTemplate] = useState<HostTemplate | null>(null)
    const [showForm, setShowForm] = useState(false)

    // Form state
    const [name, setName] = useState("")
    const [description, setDescription] = useState("")
    const [remark, setRemark] = useState("")
    const [address, setAddress] = useState("")
    const [port, setPort] = useState("")
    const [sni, setSni] = useState("")
    const [hostField, setHostField] = useState("")
    const [path, setPath] = useState("")
    const [alpn, setAlpn] = useState("")
    const [fingerprint, setFingerprint] = useState("")
    const [security, setSecurity] = useState("")
    const [priority, setPriority] = useState("")

    // Include checkboxes
    const [includeRemark, setIncludeRemark] = useState(false)
    const [includeAddress, setIncludeAddress] = useState(false)
    const [includePort, setIncludePort] = useState(false)
    const [includeSni, setIncludeSni] = useState(false)
    const [includeHost, setIncludeHost] = useState(false)
    const [includePath, setIncludePath] = useState(false)
    const [includeAlpn, setIncludeAlpn] = useState(false)
    const [includeFingerprint, setIncludeFingerprint] = useState(false)
    const [includeSecurity, setIncludeSecurity] = useState(false)
    const [includePriority, setIncludePriority] = useState(false)

    function resetForm() {
        setName("")
        setDescription("")
        setRemark("")
        setAddress("")
        setPort("")
        setSni("")
        setHostField("")
        setPath("")
        setAlpn("")
        setFingerprint("")
        setSecurity("")
        setPriority("")
        setIncludeRemark(false)
        setIncludeAddress(false)
        setIncludePort(false)
        setIncludeSni(false)
        setIncludeHost(false)
        setIncludePath(false)
        setIncludeAlpn(false)
        setIncludeFingerprint(false)
        setIncludeSecurity(false)
        setIncludePriority(false)
        setEditingTemplate(null)
    }

    function openCreateForm() {
        resetForm()
        setShowForm(true)
    }

    function openEditForm(template: HostTemplate) {
        setEditingTemplate(template)
        setName(template.name)
        setDescription(template.description || "")
        setRemark(template.remark || "")
        setAddress(template.address || "")
        setPort(template.port != null ? String(template.port) : "")
        setSni(template.sni || "")
        setHostField(template.host || "")
        setPath(template.path || "")
        setAlpn(template.alpn || "")
        setFingerprint(template.fingerprint || "")
        setSecurity(template.security || "")
        setPriority(template.priority != null ? String(template.priority) : "")
        // Enable checkboxes for non-empty fields
        setIncludeRemark(!!template.remark)
        setIncludeAddress(!!template.address)
        setIncludePort(template.port != null)
        setIncludeSni(!!template.sni)
        setIncludeHost(!!template.host)
        setIncludePath(!!template.path)
        setIncludeAlpn(!!template.alpn)
        setIncludeFingerprint(!!template.fingerprint)
        setIncludeSecurity(!!template.security)
        setIncludePriority(template.priority != null)
        setShowForm(true)
    }

    async function handleSave() {
        if (!name.trim()) return

        const data: Partial<HostTemplate> = {
            name: name.trim(),
            description: description.trim(),
            remark: includeRemark ? remark : "",
            address: includeAddress ? address : "",
            port: includePort && port ? parseInt(port) : null,
            sni: includeSni ? sni : "",
            host: includeHost ? hostField : "",
            path: includePath ? path : "",
            alpn: includeAlpn ? alpn : "",
            fingerprint: includeFingerprint ? (fingerprint === "inherit" ? "" : fingerprint) : "",
            security: includeSecurity ? (security === "inherit" ? "" : security) : "",
            priority: includePriority ? (parseInt(priority) || 0) : null,
        }

        if (editingTemplate) {
            updateMutation.mutate({ id: editingTemplate.id, data }, {
                onSuccess: () => { setShowForm(false); resetForm() },
            })
        } else {
            createMutation.mutate(data, {
                onSuccess: () => { setShowForm(false); resetForm() },
            })
        }
    }

    const isPending = createMutation.isPending || updateMutation.isPending

    return (
        <Sheet open={open} onOpenChange={(v) => { if (!v) { setShowForm(false); resetForm() } onOpenChange(v) }}>
            <SheetContent side="right" className="sm:max-w-[520px] overflow-y-auto">
                <SheetHeader>
                    <SheetTitle>{showForm ? (editingTemplate ? "Edit Template" : "New Template") : "Host Templates"}</SheetTitle>
                    <SheetDescription>
                        {showForm
                            ? "Configure template fields. Only checked fields will be included."
                            : "Manage reusable host presets for quick host creation."
                        }
                    </SheetDescription>
                </SheetHeader>

                {!showForm ? (
                    <div className="space-y-3 py-2 px-4">
                        {templates.length === 0 && (
                            <p className="text-sm text-muted-foreground text-center py-4">No templates yet.</p>
                        )}
                        {templates.map((t) => (
                            <div key={t.id} className="flex items-center justify-between p-3 rounded-lg border">
                                <div className="min-w-0">
                                    <div className="font-medium text-sm">{t.name}</div>
                                    {t.description && (
                                        <div className="text-xs text-muted-foreground mt-0.5">{t.description}</div>
                                    )}
                                    <div className="flex flex-wrap gap-1 mt-1">
                                        {t.address && <Badge variant="secondary" className="text-[10px] h-4 px-1">{t.address}</Badge>}
                                        {t.security && <Badge variant="outline" className="text-[10px] h-4 px-1">{t.security}</Badge>}
                                        {t.sni && <Badge variant="outline" className="text-[10px] h-4 px-1">SNI: {t.sni}</Badge>}
                                    </div>
                                </div>
                                <div className="flex items-center gap-1 shrink-0">
                                    <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => openEditForm(t)}>
                                        <HiOutlinePencil className="h-3.5 w-3.5" />
                                    </Button>
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="h-7 w-7 text-destructive hover:text-destructive"
                                        onClick={() => deleteMutation.mutate(t.id)}
                                    >
                                        <HiOutlineTrash className="h-3.5 w-3.5" />
                                    </Button>
                                </div>
                            </div>
                        ))}
                        <Button variant="outline" className="w-full gap-1.5" onClick={openCreateForm}>
                            <HiOutlinePlus className="h-4 w-4" />
                            New Template
                        </Button>
                    </div>
                ) : (
                    <div className="space-y-3 py-2 px-4">
                        <div className="grid grid-cols-2 gap-3">
                            <div className="space-y-1.5">
                                <Label className="text-xs">Name *</Label>
                                <Input
                                    value={name}
                                    onChange={(e) => setName(e.target.value)}
                                    placeholder="Template name"
                                    className="h-9 text-sm"
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label className="text-xs">Description</Label>
                                <Input
                                    value={description}
                                    onChange={(e) => setDescription(e.target.value)}
                                    placeholder="Optional description"
                                    className="h-9 text-sm"
                                />
                            </div>
                        </div>

                        <div className="border-t pt-3 space-y-2.5">
                            <p className="text-xs text-muted-foreground">Check fields to include in this template:</p>

                            <TemplateFieldRow label="Remark" included={includeRemark} onToggle={setIncludeRemark}>
                                <Input value={remark} onChange={(e) => setRemark(e.target.value)} placeholder="Remark template" className="h-9 text-sm" disabled={!includeRemark} />
                            </TemplateFieldRow>

                            <TemplateFieldRow label="Address" included={includeAddress} onToggle={setIncludeAddress}>
                                <Input value={address} onChange={(e) => setAddress(e.target.value)} placeholder="CDN or IP" className="h-9 text-sm" disabled={!includeAddress} />
                            </TemplateFieldRow>

                            <TemplateFieldRow label="Port" included={includePort} onToggle={setIncludePort}>
                                <Input type="number" value={port} onChange={(e) => setPort(e.target.value)} placeholder="Port" className="h-9 text-sm" disabled={!includePort} />
                            </TemplateFieldRow>

                            <TemplateFieldRow label="SNI" included={includeSni} onToggle={setIncludeSni}>
                                <Input value={sni} onChange={(e) => setSni(e.target.value)} placeholder="Server name" className="h-9 text-sm" disabled={!includeSni} />
                            </TemplateFieldRow>

                            <TemplateFieldRow label="Security" included={includeSecurity} onToggle={setIncludeSecurity}>
                                <Select value={security} onValueChange={setSecurity} disabled={!includeSecurity}>
                                    <SelectTrigger className="h-9 text-sm"><SelectValue placeholder="Select..." /></SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="inherit">Inherit</SelectItem>
                                        {SECURITY_TYPES.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
                                    </SelectContent>
                                </Select>
                            </TemplateFieldRow>

                            <TemplateFieldRow label="Host" included={includeHost} onToggle={setIncludeHost}>
                                <Input value={hostField} onChange={(e) => setHostField(e.target.value)} placeholder="Transport host" className="h-9 text-sm" disabled={!includeHost} />
                            </TemplateFieldRow>

                            <TemplateFieldRow label="Path" included={includePath} onToggle={setIncludePath}>
                                <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="Transport path" className="h-9 text-sm" disabled={!includePath} />
                            </TemplateFieldRow>

                            <TemplateFieldRow label="ALPN" included={includeAlpn} onToggle={setIncludeAlpn}>
                                <Input value={alpn} onChange={(e) => setAlpn(e.target.value)} placeholder="h2,http/1.1" className="h-9 text-sm" disabled={!includeAlpn} />
                            </TemplateFieldRow>

                            <TemplateFieldRow label="Fingerprint" included={includeFingerprint} onToggle={setIncludeFingerprint}>
                                <Select value={fingerprint} onValueChange={setFingerprint} disabled={!includeFingerprint}>
                                    <SelectTrigger className="h-9 text-sm"><SelectValue placeholder="Select..." /></SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="inherit">Inherit</SelectItem>
                                        {TLS_FINGERPRINTS.map((fp) => <SelectItem key={fp.value} value={fp.value}>{fp.label}</SelectItem>)}
                                    </SelectContent>
                                </Select>
                            </TemplateFieldRow>

                            <TemplateFieldRow label="Priority" included={includePriority} onToggle={setIncludePriority}>
                                <Input type="number" value={priority} onChange={(e) => setPriority(e.target.value)} placeholder="0" className="h-9 text-sm" disabled={!includePriority} />
                            </TemplateFieldRow>
                        </div>
                    </div>
                )}

                <SheetFooter>
                    {showForm ? (
                        <>
                            <Button variant="outline" onClick={() => { setShowForm(false); resetForm() }}>
                                Back
                            </Button>
                            <Button onClick={handleSave} disabled={!name.trim() || isPending}>
                                {isPending ? "Saving..." : editingTemplate ? "Update" : "Create"}
                            </Button>
                        </>
                    ) : (
                        <Button variant="outline" onClick={() => onOpenChange(false)}>
                            Close
                        </Button>
                    )}
                </SheetFooter>
            </SheetContent>
        </Sheet>
    )
}

function TemplateFieldRow({
    label,
    included,
    onToggle,
    children,
}: {
    label: string
    included: boolean
    onToggle: (v: boolean) => void
    children: React.ReactNode
}) {
    return (
        <div className="flex items-center gap-3">
            <Checkbox checked={included} onCheckedChange={(v) => onToggle(!!v)} className="shrink-0" />
            <div className="flex-1 space-y-1">
                <Label className={`text-xs ${!included ? "text-muted-foreground" : ""}`}>{label}</Label>
                {children}
            </div>
        </div>
    )
}
