import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { TLSSettings, TLS_FINGERPRINTS, AgentCertificate, TLSCertificate, SNI } from "@/lib/types"
import { useCertificates, useSNIs } from "@/lib/queries"
import * as React from "react"

interface TLSFormProps {
    settings?: TLSSettings
    onChange: (settings: TLSSettings) => void
    isOutbound?: boolean
}

export function TLSForm({
    settings,
    onChange,
    isOutbound = false,
}: TLSFormProps) {
    const data = settings || {}
    const { data: certificates = [] } = useCertificates()
    const { data: domains = [] } = useSNIs()

    // Filter for public/valid certs if needed, or just show all valid ones
    const availableCerts = certificates.filter((c: AgentCertificate) => !c.is_revoked && c.is_valid && c.type === "public")
    // All SNI domains are valid certificate sources
    const availableDomains = domains as SNI[]

    const hasAnyManaged = availableCerts.length > 0 || availableDomains.length > 0

    const handleCertChange = (updates: Partial<TLSCertificate>) => {
        const currentCert = data.certificates?.[0] || {}
        onChange({
            ...data,
            certificates: [{ ...currentCert, ...updates }],
        })
    }

    const currentCert = data.certificates?.[0] || {}

    // Determine the current managed selection value from cert data
    const getManagedValue = (): string => {
        if ((currentCert.id || 0) > 0) return `cert-${currentCert.id}`
        if ((currentCert.sniId || 0) > 0) return `sni-${currentCert.sniId}`
        return "0"
    }

    // Track managed mode locally, initializing based on whether an ID is present
    const [isManagedMode, setIsManagedMode] = React.useState(() => {
        if ((currentCert.id || 0) > 0) return true
        if ((currentCert.sniId || 0) > 0) return true
        return false
    })

    // Sync local state if external prop changes (e.g. initial load)
    React.useEffect(() => {
        if ((currentCert.id || 0) > 0 || (currentCert.sniId || 0) > 0) {
            setIsManagedMode(true)
        }
    }, [currentCert.id, currentCert.sniId])

    // Local text state for ALPN to prevent comma from being eaten during typing
    const [alpnText, setAlpnText] = React.useState(() => (data.alpn || []).join(", "))
    const alpnFocused = React.useRef(false)
    const alpnKey = JSON.stringify(data.alpn || [])
    React.useEffect(() => {
        if (!alpnFocused.current) {
            setAlpnText((data.alpn || []).join(", "))
        }
    }, [alpnKey])

    const handleManagedSelect = (value: string) => {
        if (value === "0") {
            handleCertChange({ id: 0, sniId: 0, certificateFile: "", keyFile: "" })
            return
        }
        if (value.startsWith("cert-")) {
            const certId = Number(value.replace("cert-", ""))
            handleCertChange({ id: certId, sniId: 0, certificateFile: "", keyFile: "" })
        } else if (value.startsWith("sni-")) {
            const sniId = Number(value.replace("sni-", ""))
            handleCertChange({ id: 0, sniId, certificateFile: "", keyFile: "" })
        }
    }

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Server Name (SNI)</Label>
                    <Input
                        placeholder="example.com"
                        value={data.serverName || ""}
                        onChange={(e) => onChange({ ...data, serverName: e.target.value })}
                    />
                </div>
                <div className="space-y-2">
                    <Label>Fingerprint</Label>
                    <select
                        className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                        value={data.fingerprint || "chrome"}
                        onChange={(e) => onChange({ ...data, fingerprint: e.target.value })}
                    >
                        {TLS_FINGERPRINTS.map((f) => (
                            <option key={f.value} value={f.value}>
                                {f.label}
                            </option>
                        ))}
                    </select>
                    <p className="text-xs text-muted-foreground">
                        Client-side hint for generated links; ignored by the server.
                    </p>
                </div>
            </div>

            <div className="space-y-2">
                <Label>ALPN</Label>
                <Input
                    placeholder="h2, http/1.1"
                    value={alpnText}
                    onFocus={() => { alpnFocused.current = true }}
                    onChange={(e) => {
                        setAlpnText(e.target.value)
                        onChange({
                            ...data,
                            alpn: e.target.value.split(",").map((s) => s.trim()).filter(Boolean),
                        })
                    }}
                    onBlur={() => {
                        alpnFocused.current = false
                        const parsed = alpnText.split(",").map((s) => s.trim()).filter(Boolean)
                        setAlpnText(parsed.join(", "))
                    }}
                />
            </div>

            {!isOutbound && (
                <div className="space-y-4 rounded-md border p-4">
                    <div className="flex items-center justify-between">
                        <Label>Certificate Source</Label>
                        <div className="flex items-center space-x-2">
                            <span className={!isManagedMode ? "font-bold" : "text-muted-foreground"}>File Path</span>
                            <Switch
                                checked={isManagedMode}
                                onCheckedChange={(checked) => {
                                    setIsManagedMode(checked)
                                    if (checked) {
                                        // Switch to managed: clear paths, keeping IDs 0 initially until selected
                                        handleCertChange({ id: 0, sniId: 0, certificateFile: "", keyFile: "" })
                                    } else {
                                        // Switch to manual: clear IDs
                                        handleCertChange({ id: 0, sniId: 0 })
                                    }
                                }}
                            />
                            <span className={isManagedMode ? "font-bold" : "text-muted-foreground"}>Managed</span>
                        </div>
                    </div>

                    {isManagedMode ? (
                        <div className="space-y-2">
                            <Label>Select Certificate</Label>
                            {!hasAnyManaged ? (
                                <div className="text-sm text-yellow-500 border border-yellow-200 bg-yellow-50 p-2 rounded">
                                    No certificates found. Issue one in Certificates page or add a domain.
                                </div>
                            ) : (
                                <select
                                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                                    value={getManagedValue()}
                                    onChange={(e) => handleManagedSelect(e.target.value)}
                                >
                                    <option value="0">Select a certificate...</option>
                                    {availableDomains.length > 0 && (
                                        <optgroup label="Domains">
                                            {availableDomains.map((d: SNI) => (
                                                <option key={`sni-${d.id}`} value={`sni-${d.id}`}>
                                                    {d.domain}{d.expires_at && d.expires_at !== "0001-01-01T00:00:00Z" ? ` (Expires: ${d.expires_at.split('T')[0]})` : ""}
                                                </option>
                                            ))}
                                        </optgroup>
                                    )}
                                    {availableCerts.length > 0 && (
                                        <optgroup label="Certificates">
                                            {availableCerts.map((c: AgentCertificate) => (
                                                <option key={`cert-${c.id}`} value={`cert-${c.id}`}>
                                                    {c.common_name} (Expires: {c.not_after.split('T')[0]})
                                                </option>
                                            ))}
                                        </optgroup>
                                    )}
                                </select>
                            )}
                        </div>
                    ) : (
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="space-y-2">
                                <Label>Certificate File</Label>
                                <Input
                                    placeholder="/etc/ssl/certs/server.crt"
                                    value={currentCert.certificateFile || ""}
                                    onChange={(e) => handleCertChange({ certificateFile: e.target.value })}
                                />
                            </div>
                            <div className="space-y-2">
                                <Label>Key File</Label>
                                <Input
                                    placeholder="/etc/ssl/private/server.key"
                                    value={currentCert.keyFile || ""}
                                    onChange={(e) => handleCertChange({ keyFile: e.target.value })}
                                />
                            </div>
                        </div>
                    )}

                    {/* Per-certificate options; apply regardless of source (managed or file path) */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2 border-t">
                        <div className="flex items-center space-x-2 pt-2">
                            <Switch
                                id="cert-buildChain"
                                checked={currentCert.buildChain ?? false}
                                onCheckedChange={(checked) => handleCertChange({ buildChain: checked })}
                            />
                            <Label htmlFor="cert-buildChain">Build Chain</Label>
                        </div>
                        <div className="flex items-center space-x-2 pt-2">
                            <Switch
                                id="cert-oneTimeLoading"
                                checked={currentCert.oneTimeLoading ?? false}
                                onCheckedChange={(checked) => handleCertChange({ oneTimeLoading: checked })}
                            />
                            <Label htmlFor="cert-oneTimeLoading">One-Time Loading</Label>
                        </div>
                    </div>
                    <div className="space-y-2">
                        <Label>OCSP Stapling (s)</Label>
                        <Input
                            type="number"
                            placeholder="0"
                            value={currentCert.ocspStapling ?? ""}
                            onChange={(e) => handleCertChange({ ocspStapling: parseInt(e.target.value) || 0 })}
                        />
                        <p className="text-xs text-muted-foreground">Refresh interval in seconds; 0 disables OCSP stapling.</p>
                    </div>
                </div>
            )}

            {isOutbound && (
                <div className="flex items-center justify-between">
                    <div>
                        <Label>Allow Insecure</Label>
                        <p className="text-xs text-muted-foreground">Skip TLS certificate verification (self-signed certs)</p>
                        <p className="text-xs text-amber-500">Deprecated: will be removed from xray-core after June 2026</p>
                    </div>
                    <Switch
                        checked={data.allowInsecure ?? false}
                        onCheckedChange={(checked) => onChange({ ...data, allowInsecure: checked })}
                    />
                </div>
            )}

            <div className="flex items-center justify-between">
                <div>
                    <Label>Reject Unknown SNI</Label>
                    <p className="text-xs text-muted-foreground">Block connections with unknown SNI</p>
                </div>
                <Switch
                    checked={data.rejectUnknownSni ?? false}
                    onCheckedChange={(checked) => onChange({ ...data, rejectUnknownSni: checked })}
                />
            </div>

            <AdvancedTLSSection data={data} onChange={onChange} isOutbound={isOutbound} />
            <ECHSection data={data} onChange={onChange} />
        </div>
    )
}

function AdvancedTLSSection({ data, onChange, isOutbound }: { data: TLSSettings; onChange: (s: TLSSettings) => void; isOutbound: boolean }) {
    const [show, setShow] = React.useState(false)

    // Local text state for curve preferences to prevent comma from being
    // eaten during typing (same trick as ALPN above).
    const [curveText, setCurveText] = React.useState(() => (data.curvePreferences || []).join(", "))
    const curveFocused = React.useRef(false)
    const curveKey = JSON.stringify(data.curvePreferences || [])
    React.useEffect(() => {
        if (!curveFocused.current) {
            setCurveText((data.curvePreferences || []).join(", "))
        }
    }, [curveKey])

    return (
        <div className="space-y-3">
            <button type="button" className="text-xs text-primary hover:underline" onClick={() => setShow(!show)}>
                {show ? "Hide" : "Show"} Advanced TLS Options
            </button>
            {show && (
                <div className="space-y-4 rounded-md border p-4 bg-muted/20">
                    <div className="flex items-center justify-between">
                        <Label>Enable Session Resumption</Label>
                        <Switch
                            checked={data.enableSessionResumption ?? false}
                            onCheckedChange={(checked) => onChange({ ...data, enableSessionResumption: checked })}
                        />
                    </div>
                    {/* Disable System Root CAs is most meaningful on the
                        outbound (client trust store) but xray accepts it
                        on inbound configs too — show on both. */}
                    <div className="flex items-center justify-between">
                        <Label>Disable System Root CAs</Label>
                        <Switch
                            checked={data.disableSystemRoot ?? false}
                            onCheckedChange={(checked) => onChange({ ...data, disableSystemRoot: checked })}
                        />
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label>Min TLS Version</Label>
                            <select
                                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                                value={data.minVersion || ""}
                                onChange={(e) => onChange({ ...data, minVersion: e.target.value })}
                            >
                                <option value="">Default</option>
                                <option value="1.0">TLS 1.0</option>
                                <option value="1.1">TLS 1.1</option>
                                <option value="1.2">TLS 1.2</option>
                                <option value="1.3">TLS 1.3</option>
                            </select>
                        </div>
                        <div className="space-y-2">
                            <Label>Max TLS Version</Label>
                            <select
                                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                                value={data.maxVersion || ""}
                                onChange={(e) => onChange({ ...data, maxVersion: e.target.value })}
                            >
                                <option value="">Default</option>
                                <option value="1.0">TLS 1.0</option>
                                <option value="1.1">TLS 1.1</option>
                                <option value="1.2">TLS 1.2</option>
                                <option value="1.3">TLS 1.3</option>
                            </select>
                        </div>
                    </div>
                    <div className="space-y-2">
                        <Label>Cipher Suites</Label>
                        <Input
                            placeholder="TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384"
                            value={data.cipherSuites || ""}
                            onChange={(e) => onChange({ ...data, cipherSuites: e.target.value })}
                        />
                    </div>
                    {isOutbound && (
                        <div className="space-y-2">
                            <Label>Pinned Peer Cert SHA256</Label>
                            <Input
                                placeholder="Comma-separated hex hashes"
                                value={data.pinnedPeerCertSha256 || ""}
                                onChange={(e) => onChange({ ...data, pinnedPeerCertSha256: e.target.value })}
                            />
                        </div>
                    )}
                    <div className="space-y-2">
                        <Label>Verify Peer Cert By Name</Label>
                        <Input
                            placeholder="example.com"
                            value={data.verifyPeerCertByName || ""}
                            onChange={(e) => onChange({ ...data, verifyPeerCertByName: e.target.value })}
                        />
                        <p className="text-xs text-muted-foreground">Sanctioned replacement for allowInsecure — verify cert against this name</p>
                    </div>
                    <div className="space-y-2">
                        <Label>Curve Preferences</Label>
                        <Input
                            placeholder="X25519, CurveP256"
                            value={curveText}
                            onFocus={() => { curveFocused.current = true }}
                            onChange={(e) => {
                                setCurveText(e.target.value)
                                onChange({
                                    ...data,
                                    curvePreferences: e.target.value.split(",").map((s) => s.trim()).filter(Boolean),
                                })
                            }}
                            onBlur={() => {
                                curveFocused.current = false
                                const parsed = curveText.split(",").map((s) => s.trim()).filter(Boolean)
                                setCurveText(parsed.join(", "))
                            }}
                        />
                        <p className="text-xs text-muted-foreground">Comma-separated elliptic curve preference order</p>
                    </div>
                    <div className="space-y-2">
                        <Label>Master Key Log</Label>
                        <Input
                            placeholder="/path/to/keylog.txt"
                            value={data.masterKeyLog || ""}
                            onChange={(e) => onChange({ ...data, masterKeyLog: e.target.value })}
                        />
                        <p className="text-xs text-muted-foreground">TLS key log file for debugging</p>
                    </div>
                </div>
            )}
        </div>
    )
}

function ECHSection({ data, onChange }: { data: TLSSettings; onChange: (s: TLSSettings) => void }) {
    const [show, setShow] = React.useState(!!data.ech)

    return (
        <div className="space-y-3">
            <button type="button" className="text-xs text-primary hover:underline" onClick={() => setShow(!show)}>
                {show ? "Hide" : "Show"} Encrypted Client Hello (ECH)
            </button>
            {show && (
                <div className="space-y-4 rounded-md border p-4 bg-muted/20">
                    <div className="space-y-2">
                        <Label>ECH Configuration (JSON)</Label>
                        <Textarea
                            placeholder='{"selfKey": [{"privateKey": "...", "config": "..."}]}'
                            value={data.ech || ""}
                            onChange={(e) => onChange({ ...data, ech: e.target.value })}
                            rows={4}
                            className="font-mono text-xs"
                        />
                        <p className="text-xs text-muted-foreground">ECH settings as JSON object. See xray-core docs for supported fields.</p>
                    </div>
                </div>
            )}
        </div>
    )
}
