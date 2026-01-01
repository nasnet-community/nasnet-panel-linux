import { useState, useEffect, useCallback } from "react"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { RefreshCw, AlertCircle, CheckCircle2 } from "lucide-react"
import { useUpdateSNI, useValidateSNICert } from "@/lib/queries"
import { cn } from "@/lib/utils"
import type { SNI } from "@/lib/types"

function isValidDomain(domain: string): boolean {
    if (!domain) return false
    return /^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/.test(domain)
}

interface EditDomainDialogProps {
    domain: SNI | null
    open: boolean
    onOpenChange: (open: boolean) => void
}

export function EditDomainDialog({ domain, open, onOpenChange }: EditDomainDialogProps) {
    const [name, setName] = useState("")
    const [domainValue, setDomainValue] = useState("")
    const [alpn, setAlpn] = useState("")
    const [certificate, setCertificate] = useState("")
    const [privateKey, setPrivateKey] = useState("")
    const [certValidated, setCertValidated] = useState(false)
    const [certExpiry, setCertExpiry] = useState<string | null>(null)

    const updateMutation = useUpdateSNI()
    const validateMutation = useValidateSNICert()

    useEffect(() => {
        if (domain) {
            setName(domain.name)
            setDomainValue(domain.domain)
            setAlpn(domain.alpn)
            setCertificate("")
            setPrivateKey("")
            setCertValidated(false)
            setCertExpiry(null)
        }
    }, [domain])

    const domainValid = isValidDomain(domainValue)
    const showDomainError = domainValue.length > 0 && !domainValid

    const handleValidateCert = useCallback((certPem: string) => {
        if (!certPem.trim()) return
        setCertValidated(false)
        setCertExpiry(null)
        validateMutation.mutate(
            { certificate: certPem, private_key: privateKey || undefined, domain: domainValue || undefined },
            {
                onSuccess: (data) => {
                    setCertValidated(true)
                    setCertExpiry(data.expires_at)
                },
            }
        )
    }, [validateMutation, privateKey, domainValue])

    const handleSubmit = () => {
        if (!domain) return
        const data: Record<string, string> = {}

        if (name.trim() !== domain.name) data.name = name.trim()
        if (domainValue.trim() !== domain.domain) data.domain = domainValue.trim()
        if (alpn.trim() !== domain.alpn) data.alpn = alpn.trim()
        if (certificate.trim()) data.certificate = certificate
        if (privateKey.trim()) data.private_key = privateKey

        if (Object.keys(data).length === 0) {
            onOpenChange(false)
            return
        }

        updateMutation.mutate(
            { id: domain.id, data },
            { onSuccess: () => onOpenChange(false) }
        )
    }

    if (!domain) return null

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[550px] max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>Edit Domain</DialogTitle>
                    <DialogDescription>
                        Update domain settings. Leave certificate fields empty to keep current values.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label>Display Name</Label>
                            <Input
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Domain</Label>
                            <Input
                                value={domainValue}
                                onChange={(e) => setDomainValue(e.target.value)}
                                className={cn(showDomainError && "border-red-500 focus-visible:ring-red-500")}
                            />
                            {showDomainError && (
                                <p className="text-xs text-red-500 flex items-center gap-1">
                                    <AlertCircle className="h-3 w-3" />
                                    Invalid domain format
                                </p>
                            )}
                        </div>
                    </div>

                    <div className="space-y-2">
                        <Label>ALPN</Label>
                        <Input
                            value={alpn}
                            onChange={(e) => setAlpn(e.target.value)}
                            placeholder="h2,http/1.1"
                        />
                    </div>

                    {!domain.use_path_mode && (
                        <>
                            <div className="space-y-2">
                                <div className="flex items-center justify-between">
                                    <Label>New Certificate (PEM)</Label>
                                    {certificate.trim() && (
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            className="h-6 text-xs"
                                            onClick={() => handleValidateCert(certificate)}
                                            disabled={validateMutation.isPending}
                                        >
                                            {validateMutation.isPending ? <RefreshCw className="h-3 w-3 animate-spin mr-1" /> : null}
                                            Validate
                                        </Button>
                                    )}
                                </div>
                                <Textarea
                                    placeholder="Leave empty to keep current certificate"
                                    value={certificate}
                                    onChange={(e) => {
                                        setCertificate(e.target.value)
                                        setCertValidated(false)
                                        setCertExpiry(null)
                                    }}
                                    className="font-mono text-xs min-h-[100px]"
                                />
                                {certValidated && certExpiry && (
                                    <p className="text-xs text-emerald-500 flex items-center gap-1">
                                        <CheckCircle2 className="h-3 w-3" />
                                        Valid certificate, expires {new Date(certExpiry).toLocaleDateString()}
                                    </p>
                                )}
                            </div>

                            <div className="space-y-2">
                                <Label>New Private Key (PEM)</Label>
                                <Textarea
                                    placeholder="Leave empty to keep current key"
                                    value={privateKey}
                                    onChange={(e) => setPrivateKey(e.target.value)}
                                    className="font-mono text-xs min-h-[100px]"
                                />
                            </div>
                        </>
                    )}
                </div>

                <DialogFooter className="gap-2 sm:gap-0">
                    <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
                    <Button
                        onClick={handleSubmit}
                        disabled={!domainValid || !name.trim() || updateMutation.isPending}
                    >
                        {updateMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                        Save Changes
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
