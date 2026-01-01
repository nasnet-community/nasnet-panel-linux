import { useState } from "react"
import { Button } from "@/components/ui/button"
import { HiOutlineShieldCheck, HiOutlineExclamation } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { settingsApi, type TLSTestResult } from "@/lib/api/settings"
import type { Setting } from "@/lib/domain/setting"

interface TLSTestButtonProps {
    settings: Setting[]
    onSettingChange: (category: string, key: string, value: string | boolean) => void
}

function extractDomain(url: string): string {
    try {
        return new URL(url).hostname
    } catch {
        return ""
    }
}

export function TLSTestButton({ settings }: TLSTestButtonProps) {
    const [testing, setTesting] = useState(false)
    const [result, setResult] = useState<TLSTestResult | null>(null)

    const certFile = settings.find(s => s.key === "tls_cert_file")?.value || ""
    const keyFile = settings.find(s => s.key === "tls_key_file")?.value || ""

    const canTest = certFile.length > 0 && keyFile.length > 0

    // Domain mismatch detection
    const urlKeys = ["app_base_url", "sub_panel_url"]
    const configuredDomains = [...new Set(
        urlKeys
            .map(k => extractDomain(settings.find(s => s.key === k)?.value || ""))
            .filter(Boolean)
    )]

    const domainMismatch = result?.success && result.domains && result.domains.length > 0 && configuredDomains.length > 0
        ? configuredDomains.filter(d => !result.domains!.some(cd => cd === d || d.endsWith("." + cd) || cd.endsWith("." + d)))
        : []

    const handleTest = async () => {
        setTesting(true)
        setResult(null)
        try {
            const res = await settingsApi.testTLS(certFile, keyFile)
            setResult(res)
        } catch (err) {
            setResult({ success: false, error: err instanceof Error ? err.message : "Test failed" })
        } finally {
            setTesting(false)
        }
    }

    return (
        <div className="space-y-3">
            <div className="flex items-center gap-3">
                <Button
                    variant="outline"
                    size="sm"
                    onClick={handleTest}
                    disabled={!canTest || testing}
                >
                    {testing ? (
                        <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    ) : (
                        <HiOutlineShieldCheck className="h-4 w-4 mr-2" />
                    )}
                    Test Certificate
                </Button>
                {!canTest && (
                    <span className="text-xs text-muted-foreground">
                        Fill in both file paths to test
                    </span>
                )}
            </div>

            {result && (
                <div className={`rounded-lg border px-4 py-3 text-sm ${
                    result.success && !result.expired && !result.not_yet_valid
                        ? "border-green-500/30 bg-green-500/5 text-green-700 dark:text-green-400"
                        : result.success && (result.warning)
                        ? "border-yellow-500/30 bg-yellow-500/5 text-yellow-700 dark:text-yellow-400"
                        : "border-red-500/30 bg-red-500/5 text-red-700 dark:text-red-400"
                }`}>
                    {!result.success ? (
                        <div className="flex items-start gap-2">
                            <HiOutlineExclamation className="h-5 w-5 mt-0.5 shrink-0" />
                            <div>
                                <p className="font-medium">Certificate test failed</p>
                                <p className="text-xs mt-1 opacity-80">{result.error}</p>
                            </div>
                        </div>
                    ) : (
                        <div className="space-y-1.5">
                            <p className="font-medium flex items-center gap-2">
                                <HiOutlineShieldCheck className="h-5 w-5 shrink-0" />
                                Certificate is valid
                                {result.warning && (
                                    <span className="text-xs font-normal opacity-80">({result.warning})</span>
                                )}
                            </p>
                            <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs opacity-80 pl-7">
                                <span>Subject:</span>
                                <span className="font-mono">{result.subject}</span>
                                <span>Issuer:</span>
                                <span className="font-mono">{result.issuer}</span>
                                {result.domains && result.domains.length > 0 && (
                                    <>
                                        <span>Domains:</span>
                                        <span className="font-mono">{result.domains.join(", ")}</span>
                                    </>
                                )}
                                <span>Expires:</span>
                                <span className="font-mono">
                                    {result.not_after ? new Date(result.not_after).toLocaleDateString() : "—"}
                                    {result.days_until_expiry !== undefined && ` (${result.days_until_expiry} days)`}
                                </span>
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* Domain mismatch warning */}
            {domainMismatch.length > 0 && (
                <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 px-4 py-3 text-sm">
                    <div className="flex items-start gap-2 text-yellow-700 dark:text-yellow-400">
                        <HiOutlineExclamation className="h-5 w-5 mt-0.5 shrink-0" />
                        <div>
                            <p className="font-medium">Domain mismatch</p>
                            <p className="text-xs mt-1 opacity-80">
                                Certificate covers {result!.domains!.join(", ")} but your URLs use{" "}
                                {domainMismatch.join(", ")}. Browsers will show a security warning.
                            </p>
                        </div>
                    </div>
                </div>
            )}
        </div>
    )
}
