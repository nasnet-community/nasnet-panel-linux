import { useState, useCallback } from "react"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Plus, RefreshCw, AlertCircle, CheckCircle2, Upload, FileText, FolderOpen, ShieldCheck } from "lucide-react"
import { useCreateSNI, useCreateSNIWithPaths, useValidateSNICert, useIssueSNIHTTP01, useStartSNIDNS01, useCompleteSNIDNS01 } from "@/lib/queries"
import type { DNSChallengeResponse } from "@/lib/types"
import { cn } from "@/lib/utils"

function isValidDomain(domain: string): boolean {
    if (!domain) return false
    return /^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/.test(domain)
}

export function AddDomainDialog() {
    const [open, setOpen] = useState(false)
    const [mode, setMode] = useState<"paste" | "upload" | "paths" | "acme">("paste")

    // Common fields
    const [name, setName] = useState("")
    const [domain, setDomain] = useState("")
    const [alpn, setAlpn] = useState("h2,http/1.1")

    // Paste mode
    const [certificate, setCertificate] = useState("")
    const [privateKey, setPrivateKey] = useState("")

    // Path mode
    const [certPath, setCertPath] = useState("")
    const [keyPath, setKeyPath] = useState("")

    // ACME mode
    const [acmeMethod, setAcmeMethod] = useState<"http01" | "dns01">("http01")
    const [dnsChallenge, setDnsChallenge] = useState<DNSChallengeResponse | null>(null)

    // Validation
    const [certValidated, setCertValidated] = useState(false)
    const [certExpiry, setCertExpiry] = useState<string | null>(null)
    const [sanWarning, setSanWarning] = useState<string | null>(null)

    const createMutation = useCreateSNI()
    const createPathsMutation = useCreateSNIWithPaths()
    const validateMutation = useValidateSNICert()
    const issueHTTP01 = useIssueSNIHTTP01()
    const startDNS01 = useStartSNIDNS01()
    const completeDNS01 = useCompleteSNIDNS01()

    const domainValid = isValidDomain(domain)
    const showDomainError = domain.length > 0 && !domainValid

    const resetState = () => {
        setMode("paste")
        setName("")
        setDomain("")
        setAlpn("h2,http/1.1")
        setCertificate("")
        setPrivateKey("")
        setCertPath("")
        setKeyPath("")
        setAcmeMethod("http01")
        setDnsChallenge(null)
        setCertValidated(false)
        setCertExpiry(null)
        setSanWarning(null)
    }

    const handleOpenChange = (newOpen: boolean) => {
        setOpen(newOpen)
        if (!newOpen) {
            setTimeout(resetState, 300)
        }
    }

    const handleValidateCert = useCallback((certPem: string) => {
        if (!certPem.trim()) return
        setCertValidated(false)
        setCertExpiry(null)
        setSanWarning(null)
        // Send the key + domain too so the backend verifies the pair and SAN coverage.
        validateMutation.mutate(
            { certificate: certPem, private_key: privateKey || undefined, domain: domain || undefined },
            {
                onSuccess: (data) => {
                    setCertValidated(true)
                    setCertExpiry(data.expires_at)
                    setSanWarning(data.san_warning || null)
                },
            }
        )
    }, [validateMutation, privateKey, domain])

    const handleFileUpload = useCallback((file: File, setter: (v: string) => void) => {
        const reader = new FileReader()
        reader.onload = (e) => {
            const content = e.target?.result as string
            setter(content)
            // Auto-validate if it's a certificate
            if (setter === setCertificate && content.includes("BEGIN CERTIFICATE")) {
                handleValidateCert(content)
            }
        }
        reader.readAsText(file)
    }, [handleValidateCert])

    const canSubmitPaste = domainValid && name.trim() && certificate.trim() && privateKey.trim()
    const canSubmitPaths = domainValid && name.trim() && certPath.trim() && keyPath.trim()

    const handleSubmit = () => {
        if (mode === "paths") {
            createPathsMutation.mutate(
                { name: name.trim(), domain: domain.trim(), cert_path: certPath.trim(), key_path: keyPath.trim(), alpn: alpn.trim() || undefined },
                { onSuccess: () => setOpen(false) }
            )
        } else {
            createMutation.mutate(
                { name: name.trim(), domain: domain.trim(), certificate, private_key: privateKey, alpn: alpn.trim() || undefined },
                { onSuccess: () => setOpen(false) }
            )
        }
    }

    const isPending = createMutation.isPending || createPathsMutation.isPending

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogTrigger asChild>
                <Button size="sm">
                    <Plus className="h-4 w-4 mr-1.5" />
                    Add Domain
                </Button>
            </DialogTrigger>
            <DialogContent className="sm:max-w-[600px] max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>Add Domain</DialogTitle>
                    <DialogDescription>
                        Add a domain with its TLS certificate for SNI-based routing.
                    </DialogDescription>
                </DialogHeader>

                {/* Common fields */}
                <div className="space-y-4">
                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label>Display Name</Label>
                            <Input
                                placeholder="My Domain"
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Domain</Label>
                            <Input
                                placeholder="example.com"
                                value={domain}
                                onChange={(e) => setDomain(e.target.value)}
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
                            placeholder="h2,http/1.1"
                            value={alpn}
                            onChange={(e) => setAlpn(e.target.value)}
                        />
                        <p className="text-xs text-muted-foreground">Comma-separated ALPN protocols. Default: h2,http/1.1</p>
                    </div>
                </div>

                {/* Certificate input tabs */}
                <Tabs value={mode} onValueChange={(v) => setMode(v as "paste" | "upload" | "paths" | "acme")} className="w-full mt-2">
                    <TabsList className="grid w-full grid-cols-4">
                        <TabsTrigger value="paste" className="gap-1.5 text-xs sm:text-sm">
                            <FileText className="h-3.5 w-3.5" />
                            Paste PEM
                        </TabsTrigger>
                        <TabsTrigger value="upload" className="gap-1.5 text-xs sm:text-sm">
                            <Upload className="h-3.5 w-3.5" />
                            Upload Files
                        </TabsTrigger>
                        <TabsTrigger value="paths" className="gap-1.5 text-xs sm:text-sm">
                            <FolderOpen className="h-3.5 w-3.5" />
                            File Paths
                        </TabsTrigger>
                        <TabsTrigger value="acme" className="gap-1.5 text-xs sm:text-sm">
                            <ShieldCheck className="h-3.5 w-3.5" />
                            Let's Encrypt
                        </TabsTrigger>
                    </TabsList>

                    <TabsContent value="paste" className="space-y-4 mt-4">
                        <div className="space-y-2">
                            <div className="flex items-center justify-between">
                                <Label>Certificate (PEM)</Label>
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
                                placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                                value={certificate}
                                onChange={(e) => {
                                    setCertificate(e.target.value)
                                    setCertValidated(false)
                                    setCertExpiry(null)
                                }}
                                className="font-mono text-xs min-h-[120px]"
                            />
                            {certValidated && certExpiry && (
                                <p className="text-xs text-emerald-500 flex items-center gap-1">
                                    <CheckCircle2 className="h-3 w-3" />
                                    Valid certificate, expires {new Date(certExpiry).toLocaleDateString()}
                                </p>
                            )}
                            {sanWarning && (
                                <p className="text-xs text-amber-500 flex items-center gap-1">
                                    <AlertCircle className="h-3 w-3" />
                                    {sanWarning}
                                </p>
                            )}
                        </div>
                        <div className="space-y-2">
                            <Label>Private Key (PEM)</Label>
                            <Textarea
                                placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                                value={privateKey}
                                onChange={(e) => setPrivateKey(e.target.value)}
                                className="font-mono text-xs min-h-[120px]"
                            />
                        </div>
                    </TabsContent>

                    <TabsContent value="upload" className="space-y-4 mt-4">
                        <div className="space-y-2">
                            <Label>Certificate File (.crt, .pem)</Label>
                            <Input
                                type="file"
                                accept=".crt,.pem,.cer"
                                onChange={(e) => {
                                    const file = e.target.files?.[0]
                                    if (file) handleFileUpload(file, setCertificate)
                                }}
                                className="cursor-pointer"
                            />
                            {certValidated && certExpiry && (
                                <p className="text-xs text-emerald-500 flex items-center gap-1">
                                    <CheckCircle2 className="h-3 w-3" />
                                    Valid certificate, expires {new Date(certExpiry).toLocaleDateString()}
                                </p>
                            )}
                            {certificate && !certValidated && !validateMutation.isPending && (
                                <p className="text-xs text-muted-foreground">File loaded ({certificate.length} bytes)</p>
                            )}
                        </div>
                        <div className="space-y-2">
                            <Label>Private Key File (.key, .pem)</Label>
                            <Input
                                type="file"
                                accept=".key,.pem"
                                onChange={(e) => {
                                    const file = e.target.files?.[0]
                                    if (file) handleFileUpload(file, setPrivateKey)
                                }}
                                className="cursor-pointer"
                            />
                            {privateKey && (
                                <p className="text-xs text-muted-foreground">File loaded ({privateKey.length} bytes)</p>
                            )}
                        </div>
                    </TabsContent>

                    <TabsContent value="paths" className="space-y-4 mt-4">
                        <div className="bg-amber-50 dark:bg-amber-900/20 p-3 rounded-lg text-sm text-amber-700 dark:text-amber-300">
                            Provide paths to certificate files on the server. The server will read them directly.
                        </div>
                        <div className="space-y-2">
                            <Label>Certificate Path</Label>
                            <Input
                                placeholder="/etc/ssl/certs/example.com.crt"
                                value={certPath}
                                onChange={(e) => setCertPath(e.target.value)}
                                className="font-mono text-sm"
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Private Key Path</Label>
                            <Input
                                placeholder="/etc/ssl/private/example.com.key"
                                value={keyPath}
                                onChange={(e) => setKeyPath(e.target.value)}
                                className="font-mono text-sm"
                            />
                        </div>
                    </TabsContent>

                    <TabsContent value="acme" className="space-y-4 mt-4">
                        <div className="flex gap-2">
                            <Button
                                variant={acmeMethod === "http01" ? "default" : "outline"}
                                size="sm"
                                onClick={() => { setAcmeMethod("http01"); setDnsChallenge(null) }}
                            >
                                HTTP-01
                            </Button>
                            <Button
                                variant={acmeMethod === "dns01" ? "default" : "outline"}
                                size="sm"
                                onClick={() => setAcmeMethod("dns01")}
                            >
                                DNS-01
                            </Button>
                        </div>

                        {acmeMethod === "http01" ? (
                            <div className="space-y-3">
                                <p className="text-xs text-muted-foreground">
                                    Issues a certificate via Let's Encrypt HTTP-01. The server must be reachable on port 80 for <code>{domain || "your domain"}</code>.
                                </p>
                                <Button
                                    onClick={() => issueHTTP01.mutate({ name: name.trim(), domain: domain.trim() }, { onSuccess: () => setOpen(false) })}
                                    disabled={!domainValid || !name.trim() || issueHTTP01.isPending}
                                    className="w-full"
                                >
                                    {issueHTTP01.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                                    Issue Certificate (HTTP-01)
                                </Button>
                            </div>
                        ) : (
                            <div className="space-y-3">
                                {!dnsChallenge ? (
                                    <>
                                        <p className="text-xs text-muted-foreground">
                                            Starts a DNS-01 challenge. You'll add a TXT record to your DNS, then complete issuance.
                                        </p>
                                        <Button
                                            onClick={() => startDNS01.mutate(domain.trim(), { onSuccess: (data) => setDnsChallenge(data) })}
                                            disabled={!domainValid || !name.trim() || startDNS01.isPending}
                                            className="w-full"
                                        >
                                            {startDNS01.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                                            Start DNS-01 Challenge
                                        </Button>
                                    </>
                                ) : (
                                    <>
                                        <div className="rounded-md border p-3 space-y-1">
                                            <p className="text-xs text-muted-foreground">Add this TXT record, then complete:</p>
                                            <div><span className="text-muted-foreground text-xs">Name: </span><code className="text-xs break-all">{dnsChallenge.txt_record}</code></div>
                                            <div><span className="text-muted-foreground text-xs">Value: </span><code className="text-xs break-all">{dnsChallenge.txt_value}</code></div>
                                        </div>
                                        <Button
                                            onClick={() => completeDNS01.mutate({ name: name.trim(), domain: domain.trim() }, { onSuccess: () => setOpen(false) })}
                                            disabled={completeDNS01.isPending}
                                            className="w-full"
                                        >
                                            {completeDNS01.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                                            I've added the record — Complete
                                        </Button>
                                    </>
                                )}
                            </div>
                        )}
                    </TabsContent>
                </Tabs>

                {mode !== "acme" && (
                    <DialogFooter className="gap-2 sm:gap-0">
                        <Button
                            onClick={handleSubmit}
                            disabled={isPending || (mode === "paths" ? !canSubmitPaths : !canSubmitPaste)}
                            className="w-full sm:w-auto"
                        >
                            {isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                            Add Domain
                        </Button>
                    </DialogFooter>
                )}
            </DialogContent>
        </Dialog>
    )
}
