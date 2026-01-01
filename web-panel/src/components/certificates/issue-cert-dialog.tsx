import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { CopyableText } from "@/components/ui/copyable-text"
import { Plus, RefreshCw, Info, Globe, ShieldCheck, AlertCircle, CheckCircle2 } from "lucide-react"
import { useIssuePublicCertificate, useStartDNSChallenge, useCompleteDNSChallenge } from "@/lib/queries/use-certificates"
import { DNSChallengeResponse } from "@/lib/types"
import { cn } from "@/lib/utils"

function isValidDomain(domain: string): boolean {
    if (!domain) return false
    return /^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/.test(domain)
}

export function IssueCertDialog() {
    const [open, setOpen] = useState(false)
    const [domain, setDomain] = useState("")
    const [mode, setMode] = useState<"http" | "dns">("http")
    const [dnsStep, setDnsStep] = useState<1 | 2>(1)
    const [dnsChallenge, setDnsChallenge] = useState<DNSChallengeResponse | null>(null)

    const issueMutation = useIssuePublicCertificate()
    const startDNSMutation = useStartDNSChallenge()
    const completeDNSMutation = useCompleteDNSChallenge()

    const domainValid = isValidDomain(domain)
    const showDomainError = domain.length > 0 && !domainValid

    const resetState = () => {
        setDomain("")
        setMode("http")
        setDnsStep(1)
        setDnsChallenge(null)
    }

    const handleOpenChange = (newOpen: boolean) => {
        setOpen(newOpen)
        if (!newOpen) {
            setTimeout(resetState, 300)
        }
    }

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogTrigger asChild>
                <Button size="sm">
                    <Plus className="h-4 w-4 mr-1.5" />
                    Issue Certificate
                </Button>
            </DialogTrigger>
            <DialogContent className="sm:max-w-[550px]">
                <DialogHeader>
                    <DialogTitle>Issue Public Certificate</DialogTitle>
                    <DialogDescription>
                        Generate a valid TLS certificate from Let's Encrypt for your domain.
                    </DialogDescription>
                </DialogHeader>

                <Tabs value={mode} onValueChange={(v) => setMode(v as "http" | "dns")} className="w-full mt-4">
                    <TabsList className="grid w-full grid-cols-2">
                        <TabsTrigger value="http">
                            <Globe className="h-4 w-4 mr-2" />
                            HTTP-01 (Auto)
                        </TabsTrigger>
                        <TabsTrigger value="dns">
                            <ShieldCheck className="h-4 w-4 mr-2" />
                            DNS-01 (Manual)
                        </TabsTrigger>
                    </TabsList>

                    <div className="py-6 space-y-6">
                        {mode === "http" && (
                            <div className="space-y-4 animate-in fade-in zoom-in-95 duration-200">
                                <div className="bg-blue-50 dark:bg-blue-900/20 p-4 rounded-lg flex gap-3 text-sm text-blue-700 dark:text-blue-300">
                                    <Info className="h-5 w-5 shrink-0 mt-0.5" />
                                    <div>
                                        <p className="font-medium mb-1">Requirements</p>
                                        <ul className="list-disc pl-4 space-y-1 opacity-90">
                                            <li>Domain must point to this server's public IP</li>
                                            <li>Port 80 must be open and accessible</li>
                                        </ul>
                                    </div>
                                </div>
                                <div className="space-y-3">
                                    <div className="space-y-2">
                                        <Label>Domain Name</Label>
                                        <Input
                                            placeholder="example.com"
                                            value={domain}
                                            onChange={(e) => setDomain(e.target.value)}
                                            className={cn(showDomainError && "border-red-500 focus-visible:ring-red-500")}
                                        />
                                        {showDomainError && (
                                            <p className="text-xs text-red-500 flex items-center gap-1">
                                                <AlertCircle className="h-3 w-3" />
                                                Please enter a valid domain name
                                            </p>
                                        )}
                                        {domainValid && (
                                            <p className="text-xs text-emerald-500 flex items-center gap-1">
                                                <CheckCircle2 className="h-3 w-3" />
                                                Domain format is valid
                                            </p>
                                        )}
                                    </div>
                                </div>
                            </div>
                        )}

                        {mode === "dns" && (
                            <div className="space-y-4 animate-in fade-in zoom-in-95 duration-200">
                                {/* Progress dots */}
                                <div className="flex items-center justify-center gap-2">
                                    <div className={cn(
                                        "w-2 h-2 rounded-full transition-colors",
                                        dnsStep === 1 ? "bg-primary" : "bg-primary/30"
                                    )} />
                                    <div className="w-8 h-0.5 bg-border" />
                                    <div className={cn(
                                        "w-2 h-2 rounded-full transition-colors",
                                        dnsStep === 2 ? "bg-primary" : "bg-primary/30"
                                    )} />
                                </div>

                                {dnsStep === 1 && (
                                    <>
                                        <div className="bg-amber-50 dark:bg-amber-900/20 p-4 rounded-lg flex gap-3 text-sm text-amber-700 dark:text-amber-300">
                                            <Info className="h-5 w-5 shrink-0 mt-0.5" />
                                            <div>
                                                <p className="font-medium mb-1">How it works</p>
                                                <p className="opacity-90">
                                                    You'll need to add a specific TXT record to your domain's DNS settings. This method works even if your server is behind a firewall or Cloudflare Proxy.
                                                </p>
                                            </div>
                                        </div>
                                        <div className="space-y-2">
                                            <Label>Domain Name</Label>
                                            <Input
                                                placeholder="example.com"
                                                value={domain}
                                                onChange={(e) => setDomain(e.target.value)}
                                                className={cn(showDomainError && "border-red-500 focus-visible:ring-red-500")}
                                            />
                                            {showDomainError && (
                                                <p className="text-xs text-red-500 flex items-center gap-1">
                                                    <AlertCircle className="h-3 w-3" />
                                                    Please enter a valid domain name
                                                </p>
                                            )}
                                            {domainValid && (
                                                <p className="text-xs text-emerald-500 flex items-center gap-1">
                                                    <CheckCircle2 className="h-3 w-3" />
                                                    Domain format is valid
                                                </p>
                                            )}
                                        </div>
                                    </>
                                )}

                                {dnsStep === 2 && dnsChallenge && (
                                    <div className="space-y-6">
                                        <div className="space-y-4">
                                            <Label className="text-primary font-semibold flex items-center gap-2">
                                                <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary text-xs text-primary-foreground">1</span>
                                                Add TXT Record
                                            </Label>
                                            <div className="grid gap-4 p-4 border rounded-lg bg-muted/30">
                                                <div className="grid gap-2">
                                                    <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Record Name</span>
                                                    <CopyableText text={dnsChallenge.txt_record} className="font-mono text-sm bg-background border shadow-sm" />
                                                </div>
                                                <div className="grid gap-2">
                                                    <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Record Value</span>
                                                    <CopyableText text={dnsChallenge.txt_value} className="font-mono text-sm bg-background border shadow-sm" />
                                                </div>
                                            </div>
                                        </div>

                                        <div className="space-y-2">
                                            <Label className="text-primary font-semibold flex items-center gap-2">
                                                <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary text-xs text-primary-foreground">2</span>
                                                Wait for Propagation
                                            </Label>
                                            <div className="bg-blue-50 dark:bg-blue-900/20 p-3 rounded-md text-sm text-blue-700 dark:text-blue-300 ml-8">
                                                <p>
                                                    Wait <strong>60-120 seconds</strong> after adding the record before verifying.
                                                </p>
                                            </div>
                                        </div>
                                    </div>
                                )}
                            </div>
                        )}
                    </div>
                </Tabs>

                <DialogFooter className="gap-2 sm:gap-0">
                    {mode === "http" ? (
                        <Button
                            onClick={() => issueMutation.mutate({ domain }, {
                                onSuccess: () => setOpen(false)
                            })}
                            disabled={!domainValid || issueMutation.isPending}
                            className="w-full sm:w-auto"
                        >
                            {issueMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                            Issue Certificate
                        </Button>
                    ) : (
                        <>
                            {dnsStep === 1 ? (
                                <Button
                                    onClick={() => {
                                        startDNSMutation.mutate({ domain }, {
                                            onSuccess: (data) => {
                                                setDnsChallenge(data)
                                                setDnsStep(2)
                                            }
                                        })
                                    }}
                                    disabled={!domainValid || startDNSMutation.isPending}
                                    className="w-full sm:w-auto"
                                >
                                    {startDNSMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                                    Start Challenge
                                </Button>
                            ) : (
                                <div className="flex gap-2 w-full justify-end">
                                    <Button variant="ghost" onClick={() => setDnsStep(1)} disabled={completeDNSMutation.isPending}>
                                        Back
                                    </Button>
                                    <Button
                                        onClick={() => completeDNSMutation.mutate({ domain }, {
                                            onSuccess: () => setOpen(false)
                                        })}
                                        disabled={completeDNSMutation.isPending}
                                    >
                                        {completeDNSMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                                        Verify & Issue
                                    </Button>
                                </div>
                            )}
                        </>
                    )}
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
