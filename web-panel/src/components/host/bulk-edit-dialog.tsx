import { useState } from "react"
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
import { Checkbox } from "@/components/ui/checkbox"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { SECURITY_TYPES, TLS_FINGERPRINTS } from "@/lib/types"
import { useBulkUpdateHostsMutation } from "@/lib/queries/use-hosts"
import type { Host } from "@/lib/types"

interface BulkEditDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    selectedIds: number[]
    onSuccess?: () => void
}

interface FieldState {
    enabled: boolean
    value: string
}

export function BulkEditDialog({ open, onOpenChange, selectedIds, onSuccess }: BulkEditDialogProps) {
    const bulkUpdateMutation = useBulkUpdateHostsMutation()

    const [address, setAddress] = useState<FieldState>({ enabled: false, value: "" })
    const [port, setPort] = useState<FieldState>({ enabled: false, value: "" })
    const [sni, setSni] = useState<FieldState>({ enabled: false, value: "" })
    const [security, setSecurity] = useState<FieldState>({ enabled: false, value: "" })
    const [hostField, setHostField] = useState<FieldState>({ enabled: false, value: "" })
    const [path, setPath] = useState<FieldState>({ enabled: false, value: "" })
    const [alpn, setAlpn] = useState<FieldState>({ enabled: false, value: "" })
    const [fingerprint, setFingerprint] = useState<FieldState>({ enabled: false, value: "" })
    const [priority, setPriority] = useState<FieldState>({ enabled: false, value: "0" })

    function resetFields() {
        setAddress({ enabled: false, value: "" })
        setPort({ enabled: false, value: "" })
        setSni({ enabled: false, value: "" })
        setSecurity({ enabled: false, value: "" })
        setHostField({ enabled: false, value: "" })
        setPath({ enabled: false, value: "" })
        setAlpn({ enabled: false, value: "" })
        setFingerprint({ enabled: false, value: "" })
        setPriority({ enabled: false, value: "0" })
    }

    async function handleApply() {
        const fields: Partial<Host> = {}
        if (address.enabled) fields.address = address.value
        if (port.enabled) fields.port = port.value ? parseInt(port.value) : null
        if (sni.enabled) fields.sni = sni.value
        if (security.enabled) fields.security = security.value === "inherit" ? "" : security.value
        if (hostField.enabled) fields.host = hostField.value
        if (path.enabled) fields.path = path.value
        if (alpn.enabled) fields.alpn = alpn.value
        if (fingerprint.enabled) fields.fingerprint = fingerprint.value === "inherit" ? "" : fingerprint.value
        if (priority.enabled) fields.priority = parseInt(priority.value) || 0

        if (Object.keys(fields).length === 0) return

        bulkUpdateMutation.mutate(
            { ids: selectedIds, fields },
            {
                onSuccess: () => {
                    resetFields()
                    onOpenChange(false)
                    onSuccess?.()
                },
            }
        )
    }

    const hasEnabledField = address.enabled || port.enabled || sni.enabled ||
        security.enabled || hostField.enabled || path.enabled ||
        alpn.enabled || fingerprint.enabled || priority.enabled

    return (
        <Dialog open={open} onOpenChange={(v) => { if (!v) resetFields(); onOpenChange(v) }}>
            <DialogContent className="sm:max-w-[480px] max-h-[85vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>Edit {selectedIds.length} Hosts</DialogTitle>
                    <DialogDescription>
                        Check the fields you want to update. Unchecked fields will be skipped.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-3 py-2">
                    <FieldRow label="Address" state={address} onChange={setAddress}>
                        <Input
                            value={address.value}
                            onChange={(e) => setAddress({ ...address, value: e.target.value })}
                            placeholder="CDN or IP"
                            className="h-9 text-sm"
                            disabled={!address.enabled}
                        />
                    </FieldRow>

                    <FieldRow label="Port" state={port} onChange={setPort}>
                        <Input
                            type="number"
                            value={port.value}
                            onChange={(e) => setPort({ ...port, value: e.target.value })}
                            placeholder="Port"
                            className="h-9 text-sm"
                            disabled={!port.enabled}
                        />
                    </FieldRow>

                    <FieldRow label="SNI" state={sni} onChange={setSni}>
                        <Input
                            value={sni.value}
                            onChange={(e) => setSni({ ...sni, value: e.target.value })}
                            placeholder="Server name"
                            className="h-9 text-sm"
                            disabled={!sni.enabled}
                        />
                    </FieldRow>

                    <FieldRow label="Security" state={security} onChange={setSecurity}>
                        <Select
                            value={security.value}
                            onValueChange={(v) => setSecurity({ ...security, value: v })}
                            disabled={!security.enabled}
                        >
                            <SelectTrigger className="h-9 text-sm">
                                <SelectValue placeholder="Select..." />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="inherit">Inherit</SelectItem>
                                {SECURITY_TYPES.map((s) => (
                                    <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </FieldRow>

                    <FieldRow label="Host" state={hostField} onChange={setHostField}>
                        <Input
                            value={hostField.value}
                            onChange={(e) => setHostField({ ...hostField, value: e.target.value })}
                            placeholder="Transport host"
                            className="h-9 text-sm"
                            disabled={!hostField.enabled}
                        />
                    </FieldRow>

                    <FieldRow label="Path" state={path} onChange={setPath}>
                        <Input
                            value={path.value}
                            onChange={(e) => setPath({ ...path, value: e.target.value })}
                            placeholder="Transport path"
                            className="h-9 text-sm"
                            disabled={!path.enabled}
                        />
                    </FieldRow>

                    <FieldRow label="ALPN" state={alpn} onChange={setAlpn}>
                        <Input
                            value={alpn.value}
                            onChange={(e) => setAlpn({ ...alpn, value: e.target.value })}
                            placeholder="h2,http/1.1"
                            className="h-9 text-sm"
                            disabled={!alpn.enabled}
                        />
                    </FieldRow>

                    <FieldRow label="Fingerprint" state={fingerprint} onChange={setFingerprint}>
                        <Select
                            value={fingerprint.value}
                            onValueChange={(v) => setFingerprint({ ...fingerprint, value: v })}
                            disabled={!fingerprint.enabled}
                        >
                            <SelectTrigger className="h-9 text-sm">
                                <SelectValue placeholder="Select..." />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="inherit">Inherit</SelectItem>
                                {TLS_FINGERPRINTS.map((fp) => (
                                    <SelectItem key={fp.value} value={fp.value}>{fp.label}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </FieldRow>

                    <FieldRow label="Priority" state={priority} onChange={setPriority}>
                        <Input
                            type="number"
                            value={priority.value}
                            onChange={(e) => setPriority({ ...priority, value: e.target.value })}
                            placeholder="0"
                            className="h-9 text-sm"
                            disabled={!priority.enabled}
                        />
                    </FieldRow>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => { resetFields(); onOpenChange(false) }}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleApply}
                        disabled={!hasEnabledField || bulkUpdateMutation.isPending}
                    >
                        {bulkUpdateMutation.isPending ? "Applying..." : "Apply"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}

function FieldRow({
    label,
    state,
    onChange,
    children,
}: {
    label: string
    state: FieldState
    onChange: (s: FieldState) => void
    children: React.ReactNode
}) {
    return (
        <div className="flex items-center gap-3">
            <Checkbox
                checked={state.enabled}
                onCheckedChange={(checked) => onChange({ ...state, enabled: !!checked })}
                className="shrink-0"
            />
            <div className="flex-1 space-y-1">
                <Label className={`text-xs ${!state.enabled ? "text-muted-foreground" : ""}`}>{label}</Label>
                {children}
            </div>
        </div>
    )
}
