import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Badge } from "@/components/ui/badge"
import { Label } from "@/components/ui/label"
import type { SNI } from "@/lib/types"

interface DomainDetailsDialogProps {
    domain: SNI | null
    open: boolean
    onOpenChange: (open: boolean) => void
}

export function DomainDetailsDialog({ domain, open, onOpenChange }: DomainDetailsDialogProps) {
    if (!domain) return null

    const isExpired = domain.expires_at && domain.expires_at !== "0001-01-01T00:00:00Z"
        ? new Date(domain.expires_at) < new Date()
        : false

    const hasExpiry = domain.expires_at && domain.expires_at !== "0001-01-01T00:00:00Z"

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[500px]">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        {domain.name}
                        {domain.is_auto_issued && (
                            <Badge variant="default" className="text-xs">ACME</Badge>
                        )}
                        {domain.use_path_mode && !domain.is_auto_issued && (
                            <Badge variant="outline" className="text-xs">Path Mode</Badge>
                        )}
                    </DialogTitle>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="grid grid-cols-2 gap-4">
                        <div>
                            <Label className="text-xs text-muted-foreground">Domain</Label>
                            <p className="text-sm font-mono mt-1">{domain.domain}</p>
                        </div>
                        <div>
                            <Label className="text-xs text-muted-foreground">ALPN</Label>
                            <p className="text-sm font-mono mt-1">{domain.alpn || "h2,http/1.1"}</p>
                        </div>
                    </div>

                    {hasExpiry && (
                        <div>
                            <Label className="text-xs text-muted-foreground">Certificate Expiry</Label>
                            <p className={`text-sm mt-1 ${isExpired ? "text-red-500 font-medium" : ""}`}>
                                {new Date(domain.expires_at).toLocaleString()}
                                {isExpired && " (Expired)"}
                            </p>
                        </div>
                    )}

                    {domain.is_auto_issued && (
                        <div className="grid grid-cols-2 gap-4">
                            <div>
                                <Label className="text-xs text-muted-foreground">Challenge Type</Label>
                                <p className="text-sm mt-1">{domain.challenge_type || "N/A"}</p>
                            </div>
                            <div>
                                <Label className="text-xs text-muted-foreground">Auto-Renew</Label>
                                <p className="text-sm mt-1">{domain.auto_renew ? "Enabled" : "Disabled"}</p>
                            </div>
                        </div>
                    )}

                    {domain.issue_error && (
                        <div className="bg-red-50 dark:bg-red-900/20 p-3 rounded-lg">
                            <Label className="text-xs text-red-600 dark:text-red-400">Last Issue Error</Label>
                            <p className="text-sm text-red-700 dark:text-red-300 mt-1 font-mono">{domain.issue_error}</p>
                        </div>
                    )}

                    {domain.use_path_mode ? (
                        <div className="space-y-3">
                            <div>
                                <Label className="text-xs text-muted-foreground">Certificate Path</Label>
                                <p className="text-sm font-mono mt-1 bg-muted/30 px-2 py-1 rounded">{domain.cert_path}</p>
                            </div>
                            <div>
                                <Label className="text-xs text-muted-foreground">Key Path</Label>
                                <p className="text-sm font-mono mt-1 bg-muted/30 px-2 py-1 rounded">{domain.key_path}</p>
                            </div>
                        </div>
                    ) : (
                        <div className="space-y-3">
                            {domain.certificate && (
                                <div>
                                    <Label className="text-xs text-muted-foreground">Certificate</Label>
                                    <pre className="text-xs font-mono mt-1 bg-muted/30 px-2 py-1.5 rounded max-h-[100px] overflow-auto whitespace-pre-wrap break-all">
                                        {domain.certificate.substring(0, 200)}...
                                    </pre>
                                </div>
                            )}
                            {domain.private_key && (
                                <div>
                                    <Label className="text-xs text-muted-foreground">Private Key</Label>
                                    <p className="text-xs font-mono mt-1 bg-muted/30 px-2 py-1.5 rounded text-muted-foreground">
                                        [Key present - hidden for security]
                                    </p>
                                </div>
                            )}
                        </div>
                    )}

                    <div className="grid grid-cols-2 gap-4 pt-2 border-t">
                        <div>
                            <Label className="text-xs text-muted-foreground">Created</Label>
                            <p className="text-xs mt-1">{new Date(domain.created_at).toLocaleString()}</p>
                        </div>
                        <div>
                            <Label className="text-xs text-muted-foreground">Updated</Label>
                            <p className="text-xs mt-1">{new Date(domain.updated_at).toLocaleString()}</p>
                        </div>
                    </div>
                </div>
            </DialogContent>
        </Dialog>
    )
}
