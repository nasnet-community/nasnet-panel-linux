import { useState } from "react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import { TransportSettings, RangeConfig, XmuxConfig } from "@/lib/types"

interface TransportFormProps {
    network: string
    settings?: TransportSettings
    onChange: (settings: TransportSettings) => void
}

export function TransportForm({
    network,
    settings,
    onChange,
}: TransportFormProps) {
    const data = settings || {}

    return (
        <div className="space-y-4">
            {/* Meaningful for tcp/ws/httpupgrade; shown for all networks since
                xray accepts it regardless and hiding it per-network adds
                little value here. */}
            <div className="flex items-center justify-between rounded-md border p-3">
                <div>
                    <Label>Accept PROXY Protocol</Label>
                    <p className="text-xs text-muted-foreground">Trust PROXY protocol v1/v2 header from the connecting peer</p>
                </div>
                <Switch
                    checked={data.acceptProxyProtocol ?? false}
                    onCheckedChange={(checked) => onChange({ ...data, acceptProxyProtocol: checked })}
                />
            </div>

            {network === "tcp" && (
                <div className="space-y-2">
                    <Label>Header Type</Label>
                    <select
                        className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                        value={data.headerType || "none"}
                        onChange={(e) => onChange({ ...data, headerType: e.target.value })}
                    >
                        <option value="none">None</option>
                        <option value="http">HTTP</option>
                    </select>
                    {data.headerType === "http" && (
                        <div className="space-y-2 mt-2">
                            <Label>HTTP Path</Label>
                            <Input
                                placeholder="/"
                                value={data.path || ""}
                                onChange={(e) => onChange({ ...data, path: e.target.value })}
                            />
                        </div>
                    )}
                </div>
            )}

            {network === "ws" && (
                <>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label>Host</Label>
                            <Input
                                placeholder="example.com"
                                value={data.host || ""}
                                onChange={(e) => onChange({ ...data, host: e.target.value })}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Path</Label>
                            <Input
                                placeholder="/"
                                value={data.path ?? ""}
                                onChange={(e) => onChange({ ...data, path: e.target.value })}
                            />
                        </div>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                        <div className="space-y-2">
                            <Label>Max Early Data</Label>
                            <Input
                                type="number"
                                placeholder="0"
                                value={data.maxEarlyData ?? ""}
                                onChange={(e) => onChange({ ...data, maxEarlyData: parseInt(e.target.value) || 0 })}
                            />
                            <p className="text-xs text-muted-foreground">0-day handshake bytes</p>
                        </div>
                        <div className="space-y-2">
                            <Label>Early Data Header</Label>
                            <Input
                                placeholder="Sec-WebSocket-Protocol"
                                value={data.earlyDataHeaderName || ""}
                                onChange={(e) => onChange({ ...data, earlyDataHeaderName: e.target.value })}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Heartbeat (s)</Label>
                            <Input
                                type="number"
                                placeholder="0"
                                value={data.heartbeatPeriod ?? ""}
                                onChange={(e) => onChange({ ...data, heartbeatPeriod: parseInt(e.target.value) || 0 })}
                            />
                        </div>
                    </div>
                </>
            )}

            {network === "grpc" && (
                <>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label>Service Name</Label>
                            <Input
                                placeholder="grpc"
                                value={data.serviceName || ""}
                                onChange={(e) => onChange({ ...data, serviceName: e.target.value })}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Authority</Label>
                            <Input
                                placeholder="example.com"
                                value={data.authority || ""}
                                onChange={(e) => onChange({ ...data, authority: e.target.value })}
                            />
                        </div>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label>Idle Timeout (s)</Label>
                            <Input
                                type="number"
                                placeholder="60"
                                value={data.idle_timeout ?? ""}
                                onChange={(e) => onChange({ ...data, idle_timeout: parseInt(e.target.value) || 0 })}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Health Check Timeout (s)</Label>
                            <Input
                                type="number"
                                placeholder="20"
                                value={data.health_check_timeout ?? ""}
                                onChange={(e) => onChange({ ...data, health_check_timeout: parseInt(e.target.value) || 0 })}
                            />
                        </div>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label>Initial Window Size</Label>
                            <Input
                                type="number"
                                placeholder="0"
                                value={data.initial_windows_size ?? ""}
                                onChange={(e) => onChange({ ...data, initial_windows_size: parseInt(e.target.value) || 0 })}
                            />
                        </div>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2">
                        <div className="flex items-center space-x-2">
                            <Switch
                                id="multiMode"
                                checked={data.multiMode || false}
                                onCheckedChange={(c) => onChange({ ...data, multiMode: c })}
                            />
                            <Label htmlFor="multiMode">Multi Mode</Label>
                        </div>
                        <div className="flex items-center space-x-2">
                            <Switch
                                id="permitWithoutStream"
                                checked={data.permit_without_stream || false}
                                onCheckedChange={(c) => onChange({ ...data, permit_without_stream: c })}
                            />
                            <Label htmlFor="permitWithoutStream">Permit Without Stream</Label>
                        </div>
                    </div>
                    <div className="space-y-2">
                        <Label>User Agent</Label>
                        <Input
                            placeholder="Custom user agent string"
                            value={data.userAgent || ""}
                            onChange={(e) => onChange({ ...data, userAgent: e.target.value })}
                        />
                    </div>
                </>
            )}

            {(network === "xhttp" || network === "splithttp") && (
                <XHTTPSection data={data} onChange={onChange} />
            )}

            {network === "httpupgrade" && (
                <>
                    <div className="space-y-2">
                        <Label>Host</Label>
                        <Input
                            placeholder="example.com"
                            value={data.host || ""}
                            onChange={(e) => onChange({ ...data, host: e.target.value })}
                        />
                    </div>
                    <div className="space-y-2">
                        <Label>Path</Label>
                        <Input
                            placeholder="/"
                            value={data.path ?? ""}
                            onChange={(e) => onChange({ ...data, path: e.target.value })}
                        />
                    </div>
                </>
            )}

            {network === "kcp" && (
                <KCPSection data={data} onChange={onChange} />
            )}
        </div>
    )
}

// ============ Range Input Helper ============
function RangeInput({
    label,
    value,
    onChange,
    fromPlaceholder = "0",
    toPlaceholder = "0",
}: {
    label: string
    value?: RangeConfig
    onChange: (v: RangeConfig | undefined) => void
    fromPlaceholder?: string
    toPlaceholder?: string
}) {
    return (
        <div className="space-y-2">
            <Label>{label}</Label>
            <div className="flex items-center gap-2">
                <Input
                    type="number"
                    placeholder={fromPlaceholder}
                    value={value?.from ?? ""}
                    onChange={(e) => {
                        const from = parseInt(e.target.value) || 0
                        const to = value?.to ?? from
                        onChange({ from, to })
                    }}
                    className="flex-1"
                />
                <span className="text-xs text-muted-foreground">to</span>
                <Input
                    type="number"
                    placeholder={toPlaceholder}
                    value={value?.to ?? ""}
                    onChange={(e) => {
                        const to = parseInt(e.target.value) || 0
                        const from = value?.from ?? to
                        onChange({ from, to })
                    }}
                    className="flex-1"
                />
            </div>
        </div>
    )
}

// ============ XHTTP Section ============
function XHTTPSection({ data, onChange }: { data: TransportSettings; onChange: (s: TransportSettings) => void }) {
    const [showAdvanced, setShowAdvanced] = useState(false)
    const [showXmux, setShowXmux] = useState(!!data.xmux)
    const [showPlacement, setShowPlacement] = useState(false)
    const [headerKey, setHeaderKey] = useState("")
    const [headerVal, setHeaderVal] = useState("")

    const addHeader = () => {
        if (!headerKey.trim()) return
        const headers = { ...(data.headers || {}), [headerKey]: headerVal }
        onChange({ ...data, headers })
        setHeaderKey("")
        setHeaderVal("")
    }

    const removeHeader = (key: string) => {
        const headers = { ...(data.headers || {}) }
        delete headers[key]
        onChange({ ...data, headers: Object.keys(headers).length > 0 ? headers : undefined })
    }

    const updateXmux = (updates: Partial<XmuxConfig>) => {
        onChange({ ...data, xmux: { ...(data.xmux || {}), ...updates } })
    }

    return (
        <>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Host</Label>
                    <Input
                        placeholder="example.com"
                        value={data.host || ""}
                        onChange={(e) => onChange({ ...data, host: e.target.value })}
                    />
                </div>
                <div className="space-y-2">
                    <Label>Path</Label>
                    <Input
                        placeholder="/"
                        value={data.path ?? ""}
                        onChange={(e) => onChange({ ...data, path: e.target.value })}
                    />
                </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Mode</Label>
                    <select
                        className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                        value={data.mode || "auto"}
                        onChange={(e) => onChange({ ...data, mode: e.target.value })}
                    >
                        <option value="auto">Auto</option>
                        <option value="packet-up">Packet Up</option>
                        <option value="stream-up">Stream Up</option>
                        <option value="stream-one">Stream One</option>
                    </select>
                </div>
                <div className="space-y-2">
                    <Label>Extra JSON</Label>
                    <Input
                        placeholder='{"max": 100}'
                        value={data.extra || ""}
                        onChange={(e) => onChange({ ...data, extra: e.target.value })}
                    />
                </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2">
                <div className="flex items-center space-x-2">
                    <Switch
                        id="noGRPCHeader"
                        checked={data.noGRPCHeader || false}
                        onCheckedChange={(c) => onChange({ ...data, noGRPCHeader: c })}
                    />
                    <Label htmlFor="noGRPCHeader">No gRPC Header</Label>
                </div>
                <div className="flex items-center space-x-2">
                    <Switch
                        id="noSSEHeader"
                        checked={data.noSSEHeader || false}
                        onCheckedChange={(c) => onChange({ ...data, noSSEHeader: c })}
                    />
                    <Label htmlFor="noSSEHeader">No SSE Header</Label>
                </div>
            </div>

            {/* Custom Headers */}
            <div className="space-y-3 border rounded-md p-4 bg-muted/20">
                <Label className="text-sm font-medium">Custom Headers</Label>
                {Object.entries(data.headers || {}).map(([key, val]) => (
                    <div key={key} className="flex gap-2 items-center">
                        <Input value={key} disabled className="flex-1 font-mono text-xs" />
                        <Input value={val} disabled className="flex-1 font-mono text-xs" />
                        <Button type="button" size="icon" variant="ghost" className="text-red-500 h-8 w-8" onClick={() => removeHeader(key)}>
                            <HiOutlineTrash className="w-4 h-4" />
                        </Button>
                    </div>
                ))}
                <div className="flex gap-2 items-end">
                    <div className="flex-1 space-y-1">
                        <Label className="text-xs">Key</Label>
                        <Input value={headerKey} onChange={(e) => setHeaderKey(e.target.value)} placeholder="Header-Name" className="text-xs" />
                    </div>
                    <div className="flex-1 space-y-1">
                        <Label className="text-xs">Value</Label>
                        <Input value={headerVal} onChange={(e) => setHeaderVal(e.target.value)} placeholder="value" className="text-xs" />
                    </div>
                    <Button type="button" size="sm" variant="outline" onClick={addHeader} disabled={!headerKey.trim()}>
                        <HiOutlinePlus className="w-4 h-4" />
                    </Button>
                </div>
            </div>

            {/* Advanced XHTTP Settings */}
            <button
                type="button"
                className="text-xs text-primary hover:underline"
                onClick={() => setShowAdvanced(!showAdvanced)}
            >
                {showAdvanced ? "Hide" : "Show"} Advanced XHTTP Settings
            </button>
            {showAdvanced && (
                <div className="space-y-4 border rounded-md p-4 bg-muted/20">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <RangeInput
                            label="X-Padding Bytes"
                            value={data.xPaddingBytes}
                            onChange={(v) => onChange({ ...data, xPaddingBytes: v })}
                            fromPlaceholder="100"
                            toPlaceholder="1000"
                        />
                        <RangeInput
                            label="SC Max Each Post Bytes"
                            value={data.scMaxEachPostBytes}
                            onChange={(v) => onChange({ ...data, scMaxEachPostBytes: v })}
                            fromPlaceholder="1000000"
                            toPlaceholder="1000000"
                        />
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <RangeInput
                            label="SC Min Posts Interval (ms)"
                            value={data.scMinPostsIntervalMs}
                            onChange={(v) => onChange({ ...data, scMinPostsIntervalMs: v })}
                            fromPlaceholder="30"
                            toPlaceholder="30"
                        />
                        <RangeInput
                            label="SC Stream Up Server Secs"
                            value={data.scStreamUpServerSecs}
                            onChange={(v) => onChange({ ...data, scStreamUpServerSecs: v })}
                            fromPlaceholder="20"
                            toPlaceholder="80"
                        />
                    </div>
                    <div className="space-y-2">
                        <Label>SC Max Buffered Posts</Label>
                        <Input
                            type="number"
                            placeholder="30"
                            value={data.scMaxBufferedPosts ?? ""}
                            onChange={(e) => onChange({ ...data, scMaxBufferedPosts: parseInt(e.target.value) || 0 })}
                        />
                    </div>

                    {/* Xmux */}
                    <div className="space-y-3 border rounded-md p-3 bg-background">
                        <div className="flex items-center justify-between">
                            <Label className="text-sm font-medium">Xmux (Multiplexing)</Label>
                            <Switch
                                checked={showXmux}
                                onCheckedChange={(checked) => {
                                    setShowXmux(checked)
                                    if (!checked) onChange({ ...data, xmux: undefined })
                                }}
                            />
                        </div>
                        {showXmux && (
                            <div className="space-y-4">
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <RangeInput
                                        label="Max Concurrency"
                                        value={data.xmux?.maxConcurrency}
                                        onChange={(v) => updateXmux({ maxConcurrency: v })}
                                    />
                                    <RangeInput
                                        label="Max Connections"
                                        value={data.xmux?.maxConnections}
                                        onChange={(v) => updateXmux({ maxConnections: v })}
                                    />
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <RangeInput
                                        label="C Max Reuse Times"
                                        value={data.xmux?.cMaxReuseTimes}
                                        onChange={(v) => updateXmux({ cMaxReuseTimes: v })}
                                    />
                                    <RangeInput
                                        label="H Max Request Times"
                                        value={data.xmux?.hMaxRequestTimes}
                                        onChange={(v) => updateXmux({ hMaxRequestTimes: v })}
                                        fromPlaceholder="600"
                                        toPlaceholder="900"
                                    />
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <RangeInput
                                        label="H Max Reusable Secs"
                                        value={data.xmux?.hMaxReusableSecs}
                                        onChange={(v) => updateXmux({ hMaxReusableSecs: v })}
                                        fromPlaceholder="1800"
                                        toPlaceholder="3000"
                                    />
                                    <div className="space-y-2">
                                        <Label>H Keep Alive Period (s)</Label>
                                        <Input
                                            type="number"
                                            placeholder="0"
                                            value={data.xmux?.hKeepAlivePeriod ?? ""}
                                            onChange={(e) => updateXmux({ hKeepAlivePeriod: parseInt(e.target.value) || 0 })}
                                        />
                                    </div>
                                </div>
                            </div>
                        )}
                    </div>

                    {/* Placement & Padding */}
                    <div className="space-y-3 border rounded-md p-3 bg-background">
                        <button
                            type="button"
                            className="text-xs text-primary hover:underline"
                            onClick={() => setShowPlacement(!showPlacement)}
                        >
                            {showPlacement ? "Hide" : "Show"} Placement &amp; Padding
                        </button>
                        {showPlacement && (
                            <div className="space-y-4">
                                <div className="flex items-center space-x-2">
                                    <Switch
                                        id="xPaddingObfsMode"
                                        checked={data.xPaddingObfsMode || false}
                                        onCheckedChange={(c) => onChange({ ...data, xPaddingObfsMode: c })}
                                    />
                                    <Label htmlFor="xPaddingObfsMode">X-Padding Obfs Mode</Label>
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <div className="space-y-2">
                                        <Label>X-Padding Key</Label>
                                        <Input
                                            placeholder="Padding key"
                                            value={data.xPaddingKey || ""}
                                            onChange={(e) => onChange({ ...data, xPaddingKey: e.target.value })}
                                        />
                                    </div>
                                    <div className="space-y-2">
                                        <Label>X-Padding Header</Label>
                                        <Input
                                            placeholder="Padding header name"
                                            value={data.xPaddingHeader || ""}
                                            onChange={(e) => onChange({ ...data, xPaddingHeader: e.target.value })}
                                        />
                                    </div>
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <div className="space-y-2">
                                        <Label>X-Padding Method</Label>
                                        <Input
                                            placeholder="Padding method"
                                            value={data.xPaddingMethod || ""}
                                            onChange={(e) => onChange({ ...data, xPaddingMethod: e.target.value })}
                                        />
                                    </div>
                                    <div className="space-y-2">
                                        <Label>X-Padding Placement</Label>
                                        <Select
                                            value={data.xPaddingPlacement || "queryInHeader"}
                                            onValueChange={(v) => onChange({ ...data, xPaddingPlacement: v })}
                                        >
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="queryInHeader">queryInHeader</SelectItem>
                                                <SelectItem value="cookie">cookie</SelectItem>
                                                <SelectItem value="header">header</SelectItem>
                                                <SelectItem value="query">query</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <div className="space-y-2">
                                        <Label>Uplink HTTP Method</Label>
                                        <Select
                                            value={data.uplinkHTTPMethod || "POST"}
                                            onValueChange={(v) => onChange({ ...data, uplinkHTTPMethod: v })}
                                        >
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="POST">POST</SelectItem>
                                                <SelectItem value="GET">GET</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <div className="space-y-2">
                                        <Label>Session Placement</Label>
                                        <Select
                                            value={data.sessionPlacement || "path"}
                                            onValueChange={(v) => onChange({ ...data, sessionPlacement: v })}
                                        >
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="path">path</SelectItem>
                                                <SelectItem value="cookie">cookie</SelectItem>
                                                <SelectItem value="header">header</SelectItem>
                                                <SelectItem value="query">query</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="space-y-2">
                                        <Label>Session Key</Label>
                                        <Input
                                            placeholder="Session key"
                                            value={data.sessionKey || ""}
                                            onChange={(e) => onChange({ ...data, sessionKey: e.target.value })}
                                        />
                                    </div>
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <div className="space-y-2">
                                        <Label>Seq Placement</Label>
                                        <Select
                                            value={data.seqPlacement || "path"}
                                            onValueChange={(v) => onChange({ ...data, seqPlacement: v })}
                                        >
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="path">path</SelectItem>
                                                <SelectItem value="cookie">cookie</SelectItem>
                                                <SelectItem value="header">header</SelectItem>
                                                <SelectItem value="query">query</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="space-y-2">
                                        <Label>Seq Key</Label>
                                        <Input
                                            placeholder="Seq key"
                                            value={data.seqKey || ""}
                                            onChange={(e) => onChange({ ...data, seqKey: e.target.value })}
                                        />
                                    </div>
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <div className="space-y-2">
                                        <Label>Uplink Data Placement</Label>
                                        <Select
                                            value={data.uplinkDataPlacement || "body"}
                                            onValueChange={(v) => onChange({ ...data, uplinkDataPlacement: v })}
                                        >
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="body">body</SelectItem>
                                                <SelectItem value="cookie">cookie</SelectItem>
                                                <SelectItem value="header">header</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="space-y-2">
                                        <Label>Uplink Data Key</Label>
                                        <Input
                                            placeholder="Uplink data key"
                                            value={data.uplinkDataKey || ""}
                                            onChange={(e) => onChange({ ...data, uplinkDataKey: e.target.value })}
                                        />
                                    </div>
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                    <RangeInput
                                        label="Uplink Chunk Size"
                                        value={data.uplinkChunkSize}
                                        onChange={(v) => onChange({ ...data, uplinkChunkSize: v })}
                                        fromPlaceholder="100"
                                        toPlaceholder="65536"
                                    />
                                    <div className="space-y-2">
                                        <Label>Server Max Header Bytes</Label>
                                        <Input
                                            type="number"
                                            placeholder="0"
                                            value={data.serverMaxHeaderBytes ?? ""}
                                            onChange={(e) => onChange({ ...data, serverMaxHeaderBytes: parseInt(e.target.value) || 0 })}
                                        />
                                    </div>
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </>
    )
}

// ============ KCP Section ============
function KCPSection({ data, onChange }: { data: TransportSettings; onChange: (s: TransportSettings) => void }) {
    return (
        <div className="rounded-md border border-border p-4 bg-muted/20">
            <p className="text-sm text-muted-foreground">
                mKCP transport has limited configuration options in current xray-core versions.
                Header type and seed settings have been removed.
            </p>
        </div>
    )
}
