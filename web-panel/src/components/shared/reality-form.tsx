import React, { useState } from "react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Button } from "@/components/ui/button"
import { RealitySettings, TLS_FINGERPRINTS } from "@/lib/types"
import { generateX25519Keys } from "@/lib/api/nodes"
import { toast } from "sonner"

interface RealityFormProps {
    settings?: RealitySettings
    onChange: (settings: RealitySettings) => void
    isInbound?: boolean
}

export function RealityForm({
    settings,
    onChange,
    isInbound = false,
}: RealityFormProps) {
    const data = settings || {}
    const [generating, setGenerating] = useState(false)

    // Local text state for ALPN to prevent comma from being eaten during typing
    const [alpnText, setAlpnText] = React.useState(() => (data.alpn || []).join(", "))
    const alpnFocused = React.useRef(false)
    const alpnKey = JSON.stringify(data.alpn || [])
    React.useEffect(() => {
        if (!alpnFocused.current) {
            setAlpnText((data.alpn || []).join(", "))
        }
    }, [alpnKey])

    // Local text state for additional short IDs, same comma-typing trick.
    const [shortIdsText, setShortIdsText] = React.useState(() => (data.shortIds || []).join(", "))
    const shortIdsFocused = React.useRef(false)
    const shortIdsKey = JSON.stringify(data.shortIds || [])
    React.useEffect(() => {
        if (!shortIdsFocused.current) {
            setShortIdsText((data.shortIds || []).join(", "))
        }
    }, [shortIdsKey])

    async function handleGenerateKeys() {
        setGenerating(true)
        try {
            const res = await generateX25519Keys()
            if (res.success && res.data) {
                onChange({
                    ...data,
                    privateKey: res.data.privateKey,
                    publicKey: res.data.publicKey,
                })
                toast.success("Key pair generated")
            } else {
                toast.error("Failed to generate keys")
            }
        } catch {
            toast.error("Failed to generate keys")
        } finally {
            setGenerating(false)
        }
    }

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Destination (Dest)</Label>
                    <Input
                        placeholder="www.google.com:443"
                        value={data.dest || ""}
                        onChange={(e) => onChange({ ...data, dest: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">Fallback destination</p>
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
                <Label>Server Names</Label>
                <Textarea
                    placeholder="www.google.com&#10;www.microsoft.com"
                    value={(data.serverNames || []).join("\n")}
                    onChange={(e) =>
                        onChange({
                            ...data,
                            serverNames: e.target.value.split("\n").filter(Boolean),
                        })
                    }
                    rows={2}
                />
                <p className="text-xs text-muted-foreground">One per line</p>
            </div>

            <div className="space-y-2">
                <div className="flex items-center justify-between">
                    <Label>Private Key / Public Key</Label>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={handleGenerateKeys}
                        disabled={generating}
                    >
                        {generating ? "Generating..." : "Generate Key Pair"}
                    </Button>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <Input
                        placeholder="Server private key"
                        value={data.privateKey || ""}
                        onChange={(e) => onChange({ ...data, privateKey: e.target.value })}
                    />
                    <Input
                        placeholder="For client config"
                        value={data.publicKey || ""}
                        onChange={(e) => onChange({ ...data, publicKey: e.target.value })}
                    />
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Short ID</Label>
                    <Input
                        placeholder="8 hex chars"
                        value={data.shortId || ""}
                        onChange={(e) => onChange({ ...data, shortId: e.target.value })}
                    />
                </div>
                <div className="space-y-2">
                    <Label>SpiderX</Label>
                    <Input
                        placeholder="/"
                        value={data.spiderX || ""}
                        onChange={(e) => onChange({ ...data, spiderX: e.target.value })}
                    />
                </div>
            </div>

            <div className="space-y-2">
                <Label>Additional Short IDs (optional)</Label>
                <Input
                    placeholder="8 hex chars, comma-separated"
                    value={shortIdsText}
                    onFocus={() => { shortIdsFocused.current = true }}
                    onChange={(e) => {
                        setShortIdsText(e.target.value)
                        onChange({
                            ...data,
                            shortIds: e.target.value.split(",").map((s) => s.trim()).filter(Boolean),
                        })
                    }}
                    onBlur={() => {
                        shortIdsFocused.current = false
                        const parsed = shortIdsText.split(",").map((s) => s.trim()).filter(Boolean)
                        setShortIdsText(parsed.join(", "))
                    }}
                />
                <p className="text-xs text-muted-foreground">Extra short IDs accepted alongside Short ID above</p>
            </div>

            <div className="space-y-2">
                <Label>ML-DSA-65 Seed (server post-quantum)</Label>
                <Input
                    placeholder="base64url-encoded seed"
                    value={data.mldsa65Seed || ""}
                    onChange={(e) => onChange({ ...data, mldsa65Seed: e.target.value })}
                />
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
                <p className="text-xs text-muted-foreground">Comma-separated ALPN protocols</p>
            </div>

            <div className="flex items-center justify-between">
                <div>
                    <Label>Show</Label>
                    <p className="text-xs text-muted-foreground">Show debug info</p>
                </div>
                <Switch
                    checked={data.show ?? false}
                    onCheckedChange={(checked) => onChange({ ...data, show: checked })}
                />
            </div>

            {isInbound && <AdvancedRealitySection data={data} onChange={onChange} />}
        </div>
    )
}

function AdvancedRealitySection({ data, onChange }: { data: RealitySettings; onChange: (s: RealitySettings) => void }) {
    const [show, setShow] = useState(false)

    return (
        <div className="space-y-3">
            <button type="button" className="text-xs text-primary hover:underline" onClick={() => setShow(!show)}>
                {show ? "Hide" : "Show"} Advanced Reality Options
            </button>
            {show && (
                <div className="space-y-4 rounded-md border p-4 bg-muted/20">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label>Min Client Version</Label>
                            <Input
                                placeholder="x.y.z"
                                value={data.minClientVer || ""}
                                onChange={(e) => onChange({ ...data, minClientVer: e.target.value })}
                            />
                            <p className="text-xs text-muted-foreground">
                                Left empty, xray-core enforces its own floor of <strong>26.3.27</strong> and
                                refuses older clients. Set <code>0.0.0</code> to accept any client — upstream
                                warns this raises the odds of the server IP being blocked.
                            </p>
                        </div>
                        <div className="space-y-2">
                            <Label>Max Client Version</Label>
                            <Input
                                placeholder="x.y.z"
                                value={data.maxClientVer || ""}
                                onChange={(e) => onChange({ ...data, maxClientVer: e.target.value })}
                            />
                        </div>
                    </div>
                    <div className="space-y-2">
                        <Label>Max Time Diff</Label>
                        <Input
                            type="number"
                            placeholder="0"
                            value={data.maxTimeDiff ?? ""}
                            onChange={(e) => onChange({ ...data, maxTimeDiff: parseInt(e.target.value) || 0 })}
                        />
                        <p className="text-xs text-muted-foreground">Maximum allowed time difference (milliseconds)</p>
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
