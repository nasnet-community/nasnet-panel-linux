import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { AgentCertificate } from "@/lib/types"
import { Copy, Download, Shield, ShieldAlert, ShieldCheck, Clock, Calendar, Key, FileText, AlertTriangle } from "lucide-react"
import { cn, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"
import { useCertificateDetails } from "@/lib/queries"

interface CertDetailsDialogProps {
    certificate: AgentCertificate | null
    open: boolean
    onOpenChange: (open: boolean) => void
}

export function CertDetailsDialog({ certificate, open, onOpenChange }: CertDetailsDialogProps) {
    const [activeTab, setActiveTab] = useState("info")

    // Fetch certificate details including PEM content and private key
    const { data: certDetails, isLoading: isLoadingDetails } = useCertificateDetails(certificate?.id || 0)

    if (!certificate) return null

    // Get the certificate PEM and private key from the details response
    const certPEM = certDetails?.certificate
    const privateKey = certDetails?.private_key
    const isLoadingKeys = isLoadingDetails

    const handleCopy = async (text: string, label: string) => {
        await copyToClipboard(text)
        toast.success(`${label} copied to clipboard`)
    }

    const handleDownload = (content: string, filename: string) => {
        const blob = new Blob([content], { type: "application/x-pem-file" })
        const url = URL.createObjectURL(blob)
        const a = document.createElement("a")
        a.href = url
        a.download = filename
        a.click()
        URL.revokeObjectURL(url)
        toast.success(`Downloaded ${filename}`)
    }

    const getStatusInfo = () => {
        if (certificate.is_revoked) return { label: "Revoked", variant: "danger" as const, icon: ShieldAlert }
        if (certificate.days_until_expiry < 0) return { label: "Expired", variant: "danger" as const, icon: ShieldAlert }
        if (certificate.days_until_expiry < 30) return { label: "Expiring Soon", variant: "warning" as const, icon: AlertTriangle }
        return { label: "Valid", variant: "success" as const, icon: ShieldCheck }
    }

    const status = getStatusInfo()
    const StatusIcon = status.icon

    const getTypeColor = () => {
        switch (certificate.type) {
            case "ca": return "border-purple-200 bg-purple-50 text-purple-700 dark:border-purple-900 dark:bg-purple-950/30 dark:text-purple-400"
            case "master": return "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-400"
            case "public": return "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-900 dark:bg-orange-950/30 dark:text-orange-400"
            default: return ""
        }
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[600px]">
                <DialogHeader>
                    <div className="flex items-center gap-3">
                        <Shield className="h-5 w-5 text-primary" />
                        <div>
                            <DialogTitle className="flex items-center gap-2">
                                Certificate Details
                                <Badge variant="outline" className={cn("capitalize font-normal", getTypeColor())}>
                                    {certificate.type}
                                </Badge>
                            </DialogTitle>
                            <DialogDescription className="mt-1 font-mono text-xs">
                                {certificate.common_name}
                            </DialogDescription>
                        </div>
                    </div>
                </DialogHeader>

                <Tabs value={activeTab} onValueChange={setActiveTab} className="mt-4">
                    <TabsList className="grid w-full grid-cols-3">
                        <TabsTrigger value="info">
                            <FileText className="h-4 w-4 mr-2" />
                            Info
                        </TabsTrigger>
                        <TabsTrigger value="certificate" disabled={!certPEM && !isLoadingKeys}>
                            <Shield className="h-4 w-4 mr-2" />
                            Certificate
                        </TabsTrigger>
                        <TabsTrigger value="key" disabled={!privateKey && !isLoadingKeys}>
                            <Key className="h-4 w-4 mr-2" />
                            Private Key
                        </TabsTrigger>
                    </TabsList>

                    <TabsContent value="info" className="space-y-4 mt-4">
                        {/* Status Banner */}
                        <div className={cn(
                            "flex items-center gap-3 p-3 rounded-lg",
                            status.variant === "success" && "bg-green-50 dark:bg-green-950/20",
                            status.variant === "warning" && "bg-yellow-50 dark:bg-yellow-950/20",
                            status.variant === "danger" && "bg-red-50 dark:bg-red-950/20"
                        )}>
                            <StatusIcon className={cn(
                                "h-5 w-5",
                                status.variant === "success" && "text-green-600",
                                status.variant === "warning" && "text-yellow-600",
                                status.variant === "danger" && "text-red-600"
                            )} />
                            <div>
                                <p className={cn(
                                    "font-medium",
                                    status.variant === "success" && "text-green-700 dark:text-green-400",
                                    status.variant === "warning" && "text-yellow-700 dark:text-yellow-400",
                                    status.variant === "danger" && "text-red-700 dark:text-red-400"
                                )}>{status.label}</p>
                                {certificate.days_until_expiry >= 0 && (
                                    <p className="text-sm text-muted-foreground">
                                        {certificate.days_until_expiry} days remaining
                                    </p>
                                )}
                            </div>
                        </div>

                        {/* Details Grid */}
                        <div className="grid gap-3">
                            <InfoRow label="Common Name" value={certificate.common_name} mono copyable />
                            <InfoRow label="Serial Number" value={certificate.serial_number} mono copyable />
                            <InfoRow
                                label="Valid From"
                                value={new Date(certificate.not_before).toLocaleString()}
                                icon={<Calendar className="h-4 w-4 text-muted-foreground" />}
                            />
                            <InfoRow
                                label="Valid To"
                                value={new Date(certificate.not_after).toLocaleString()}
                                icon={<Clock className="h-4 w-4 text-muted-foreground" />}
                            />
                            {certificate.node_id && (
                                <InfoRow label="Node ID" value={certificate.node_id.toString()} />
                            )}
                        </div>
                    </TabsContent>

                    <TabsContent value="certificate" className="space-y-3 mt-4">
                        {isLoadingKeys ? (
                            <div className="flex items-center justify-center h-32 text-muted-foreground">
                                Loading certificate...
                            </div>
                        ) : certPEM ? (
                            <>
                                <div className="flex justify-end gap-2">
                                    <Button variant="outline" size="sm" onClick={() => handleCopy(certPEM, "Certificate")}>
                                        <Copy className="h-4 w-4 mr-2" />
                                        Copy
                                    </Button>
                                    <Button variant="outline" size="sm" onClick={() => handleDownload(certPEM, `${certificate.common_name}.crt`)}>
                                        <Download className="h-4 w-4 mr-2" />
                                        Download
                                    </Button>
                                </div>
                                <pre className="p-3 rounded-lg bg-muted font-mono text-xs overflow-auto max-h-64 whitespace-pre-wrap break-all">
                                    {certPEM}
                                </pre>
                            </>
                        ) : (
                            <div className="flex items-center justify-center h-32 text-muted-foreground">
                                Certificate PEM not available for this type
                            </div>
                        )}
                    </TabsContent>

                    <TabsContent value="key" className="space-y-3 mt-4">
                        {isLoadingKeys ? (
                            <div className="flex items-center justify-center h-32 text-muted-foreground">
                                Loading private key...
                            </div>
                        ) : privateKey ? (
                            <>
                                <div className="bg-amber-50 dark:bg-amber-950/20 p-3 rounded-lg flex gap-2 text-sm text-amber-700 dark:text-amber-300">
                                    <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
                                    <span>Keep this private key secure. Never share it publicly.</span>
                                </div>
                                <div className="flex justify-end gap-2">
                                    <Button variant="outline" size="sm" onClick={() => handleCopy(privateKey, "Private key")}>
                                        <Copy className="h-4 w-4 mr-2" />
                                        Copy
                                    </Button>
                                    <Button variant="outline" size="sm" onClick={() => handleDownload(privateKey, `${certificate.common_name}.key`)}>
                                        <Download className="h-4 w-4 mr-2" />
                                        Download
                                    </Button>
                                </div>
                                <pre className="p-3 rounded-lg bg-muted font-mono text-xs overflow-auto max-h-64 whitespace-pre-wrap break-all">
                                    {privateKey}
                                </pre>
                            </>
                        ) : (
                            <div className="flex items-center justify-center h-32 text-muted-foreground">
                                Private key not available for this type
                            </div>
                        )}
                    </TabsContent>
                </Tabs>
            </DialogContent>
        </Dialog>
    )
}

function InfoRow({
    label,
    value,
    mono = false,
    copyable = false,
    icon
}: {
    label: string
    value: string
    mono?: boolean
    copyable?: boolean
    icon?: React.ReactNode
}) {
    const handleCopy = async () => {
        await copyToClipboard(value)
        toast.success(`${label} copied`)
    }

    return (
        <div className="flex items-center justify-between py-2 border-b border-border/50 last:border-0">
            <span className="text-sm text-muted-foreground flex items-center gap-2">
                {icon}
                {label}
            </span>
            <div className="flex items-center gap-2">
                <span className={cn("text-sm", mono && "font-mono")}>
                    {value.length > 30 ? `${value.slice(0, 30)}...` : value}
                </span>
                {copyable && (
                    <Button variant="ghost" size="icon" className="h-6 w-6" onClick={handleCopy} aria-label="Copy value">
                        <Copy className="h-3 w-3" />
                    </Button>
                )}
            </div>
        </div>
    )
}
