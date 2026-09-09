import { useState } from "react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import { TransportSettings, RangeConfig, XmuxConfig, XHTTP_SESSION_ID_TABLES } from "@/lib/types"
import { Disclosure, SectionTitle, SwitchRow } from "@/components/connection-dialog/section"

interface TransportFormProps {
    network: string
    settings?: TransportSettings
    onChange: (settings: TransportSettings) => void
}

// Transports whose xray settings object carries its own acceptProxyProtocol.
// (sockopt has a separate, global one — both are real xray keys.)
const PROXY_PROTOCOL_NETWORKS = ["tcp", "raw", "ws", "httpupgrade"]

export function TransportForm({
    network,
    settings,
    onChange,
}: TransportFormProps) {
    const data = settings || {}

    return (
        <div className="space-y-4">
            {network === "tcp" && (
                <div className="space-y-2">
                    <Label>Header Type</Label>
                    <Select
                        value={data.headerType || "none"}
                        onValueChange={(v) => onChange({ ...data, headerType: v })}
                    >
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value="none">None</SelectItem>
                            <SelectItem value="http">HTTP</SelectItem>
                        </SelectContent>
                    </Select>
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

            {PROXY_PROTOCOL_NETWORKS.includes(network) && (
                <SwitchRow
                    label="Accept PROXY protocol"
                    help="Trust the PROXY protocol v1/v2 header sent by a fronting load balancer."
                    checked={data.acceptProxyProtocol ?? false}
                    onCheckedChange={(checked) => onChange({ ...data, acceptProxyProtocol: checked })}
                />
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
    const [showXmux, setShowXmux] = useState(!!data.xmux)
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
                    <Select
                        value={data.mode || "auto"}
                        onValueChange={(v) => onChange({ ...data, mode: v })}
                    >
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value="auto">Auto</SelectItem>
                            <SelectItem value="packet-up">Packet Up</SelectItem>
                            <SelectItem value="stream-up">Stream Up</SelectItem>
                            <SelectItem value="stream-one">Stream One</SelectItem>
                        </SelectContent>
                    </Select>
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
                <SectionTitle>Custom headers</SectionTitle>
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

            <Disclosure title="Advanced XHTTP" summary="Padding, post sizing, Xmux, placement">
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

                    <Disclosure title="Placement &amp; padding" summary="Session, seq and uplink placement">
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
                                        <Label>Session ID Table</Label>
                                        <Select
                                            value={data.sessionIDTable || "__uuid__"}
                                            onValueChange={(v) => onChange({ ...data, sessionIDTable: v === "__uuid__" ? "" : v })}
                                        >
                                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                {XHTTP_SESSION_ID_TABLES.map((t) => (
                                                    <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                        <p className="text-xs text-muted-foreground">
                                            Alphabet the session ID is drawn from. Default generates a UUID.
                                        </p>
                                    </div>
                                    <RangeInput
                                        label="Session ID Length"
                                        value={data.sessionIDLength}
                                        onChange={(v) => onChange({ ...data, sessionIDLength: v })}
                                        fromPlaceholder="8"
                                        toPlaceholder="16"
                                    />
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
                    </Disclosure>
            </Disclosure>
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
