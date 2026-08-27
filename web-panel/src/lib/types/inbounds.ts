import type { Host } from "./hosts"

export interface Inbound {
    id: number
    node_id: number
    tag: string
    protocol: string
    port: number
    port_range?: string
    listen: string
    address: string
    remark: string
    network: string
    security: string
    tls_settings?: TLSSettings
    reality_settings?: RealitySettings
    transport_settings?: TransportSettings
    sniffing_settings?: SniffingSettings
    shadowsocks_settings?: ShadowsocksSettings
    wireguard_settings?: WireGuardSettings
    http_settings?: HTTPSettings
    socks_settings?: SOCKSSettings
    sockopt_settings?: SockoptSettings
    vless_settings?: VLESSSettings
    vmess_settings?: VMessSettings
    trojan_settings?: TrojanSettings
    dokodemo_settings?: DokodemoSettings
    hysteria_settings?: HysteriaSettings
    link_format: string
    is_disabled: boolean
    hosts?: Host[]
    finalmask?: FinalMask
    created_at: string
    updated_at: string
}

// TLS Settings
export interface TLSSettings {
    serverName?: string
    alpn?: string[]
    fingerprint?: string
    certificates?: TLSCertificate[]
    cipherSuites?: string
    minVersion?: string
    maxVersion?: string
    allowInsecure?: boolean
    disableSystemRoot?: boolean
    rejectUnknownSni?: boolean
    enableSessionResumption?: boolean
    pinnedPeerCertSha256?: string
    masterKeyLog?: string
    ech?: string
    curvePreferences?: string[]
    verifyPeerCertByName?: string
}

export interface TLSCertificate {
    certificateFile?: string
    keyFile?: string
    usage?: string
    buildChain?: boolean
    oneTimeLoading?: boolean
    ocspStapling?: number
    id?: number
    sniId?: number
}

// Reality Settings
export interface RealitySettings {
    show?: boolean
    dest?: string
    xver?: number
    serverNames?: string[]
    serverName?: string
    privateKey?: string
    publicKey?: string
    shortId?: string
    spiderX?: string
    fingerprint?: string
    alpn?: string[]
    minClientVer?: string
    maxClientVer?: string
    maxTimeDiff?: number
    mldsa65Verify?: string
    shortIds?: string[]
    mldsa65Seed?: string
    masterKeyLog?: string
}

// Range Config for XHTTP/SplitHTTP
export interface RangeConfig {
    from: number
    to: number
}

// Xmux Config for XHTTP multiplexing
export interface XmuxConfig {
    maxConcurrency?: RangeConfig
    maxConnections?: RangeConfig
    cMaxReuseTimes?: RangeConfig
    hMaxRequestTimes?: RangeConfig
    hMaxReusableSecs?: RangeConfig
    hKeepAlivePeriod?: number
}

// Transport Settings
export interface TransportSettings {
    // Common
    path?: string
    host?: string
    acceptProxyProtocol?: boolean
    // gRPC
    serviceName?: string
    authority?: string
    multiMode?: boolean
    idle_timeout?: number
    health_check_timeout?: number
    permit_without_stream?: boolean
    initial_windows_size?: number
    // TCP header
    headerType?: string
    // WebSocket
    maxEarlyData?: number
    earlyDataHeaderName?: string
    heartbeatPeriod?: number
    // XHTTP/SplitHTTP
    mode?: string
    headers?: Record<string, string>
    noSSEHeader?: boolean
    scMaxBufferedPosts?: number
    scMaxEachPostBytes?: RangeConfig
    scMinPostsIntervalMs?: RangeConfig
    scStreamUpServerSecs?: RangeConfig
    xPaddingBytes?: RangeConfig
    xmux?: XmuxConfig
    noGRPCHeader?: boolean
    extra?: string
    userAgent?: string
    xPaddingObfsMode?: boolean
    xPaddingKey?: string
    xPaddingHeader?: string
    xPaddingPlacement?: string
    xPaddingMethod?: string
    uplinkHTTPMethod?: string
    sessionPlacement?: string
    sessionKey?: string
    sessionIDTable?: string
    sessionIDLength?: RangeConfig
    seqPlacement?: string
    seqKey?: string
    uplinkDataPlacement?: string
    uplinkDataKey?: string
    uplinkChunkSize?: RangeConfig
    serverMaxHeaderBytes?: number
    // mKCP
    mtu?: number
    tti?: number
    uplinkCapacity?: number
    downlinkCapacity?: number
    cwndMultiplier?: number
    maxSendingWindow?: number
}

// Sniffing Settings
export interface SniffingSettings {
    enabled: boolean
    destOverride?: string[]
    metadataOnly?: boolean
    routeOnly?: boolean
    domainsExcluded?: string[]
    ipsExcluded?: string[]
}

// Shadowsocks Settings
export interface ShadowsocksSettings {
    method: string
    password?: string
    network?: string
    ivCheck?: boolean
    level?: number
    email?: string
    uot?: boolean
    uotVersion?: number
}

// WireGuard Settings
export interface WireGuardPeer {
    publicKey: string
    preSharedKey?: string
    endpoint?: string
    keepAlive?: number
    allowedIps: string[]
}

export interface WireGuardSettings {
    secretKey: string
    mtu?: number
    endpoint: string[]
    numWorkers?: number
    reserved?: number[]
    domainStrategy?: 'forceip' | 'forceipv4' | 'forceipv6' | 'forceipv4v6' | 'forceipv6v4'
    noKernelTun?: boolean
    peerPoolCidr?: string
    clientDns?: string
    peers: WireGuardPeer[]
}

// HTTP Settings
export interface HTTPAccount {
    user: string
    pass: string
}

export interface HTTPSettings {
    allowTransparent?: boolean
    timeout?: number
    userLevel?: number
    accounts?: HTTPAccount[]
    headers?: Record<string, string>
}

// SOCKS Settings
export interface SOCKSAccount {
    user: string
    pass: string
}

export interface SOCKSSettings {
    auth?: 'noauth' | 'password'
    udp?: boolean
    ip?: string
    userLevel?: number
    accounts?: SOCKSAccount[]
}

// Protocol Options Constants
export const INBOUND_PROTOCOLS = [
    { value: 'vless', label: 'VLESS' },
    { value: 'vmess', label: 'VMess' },
    { value: 'trojan', label: 'Trojan' },
    { value: 'shadowsocks', label: 'Shadowsocks' },
    { value: 'wireguard', label: 'WireGuard' },
    { value: 'http', label: 'HTTP' },
    { value: 'socks', label: 'SOCKS' },
    { value: 'mixed', label: 'Mixed (SOCKS+HTTP)' },
    { value: 'dokodemo-door', label: 'Dokodemo-door' },
    { value: 'hysteria2', label: 'Hysteria2' },
] as const

export const NETWORK_TYPES = [
    { value: 'tcp', label: 'TCP' },
    { value: 'ws', label: 'WebSocket' },
    { value: 'grpc', label: 'gRPC' },
    { value: 'httpupgrade', label: 'HTTP Upgrade' },
    { value: 'xhttp', label: 'XHTTP (SplitHTTP)' },
    { value: 'splithttp', label: 'SplitHTTP' },
    { value: 'kcp', label: 'mKCP' },
] as const

export const SECURITY_TYPES = [
    { value: 'none', label: 'None' },
    { value: 'tls', label: 'TLS' },
    { value: 'reality', label: 'Reality' },
] as const

export const SHADOWSOCKS_METHODS = [
    { value: '2022-blake3-aes-128-gcm', label: '2022-blake3-aes-128-gcm (Recommended)' },
    { value: '2022-blake3-aes-256-gcm', label: '2022-blake3-aes-256-gcm' },
    { value: '2022-blake3-chacha20-poly1305', label: '2022-blake3-chacha20-poly1305' },
    { value: 'aes-256-gcm', label: 'aes-256-gcm' },
    { value: 'aes-128-gcm', label: 'aes-128-gcm' },
    { value: 'chacha20-poly1305', label: 'chacha20-poly1305' },
    { value: 'xchacha20-poly1305', label: 'xchacha20-poly1305' },
    { value: 'none', label: 'none (No Encryption)' },
    { value: 'plain', label: 'plain (No Encryption)' },
] as const

export const TLS_FINGERPRINTS = [
    { value: 'chrome', label: 'Chrome' },
    { value: 'firefox', label: 'Firefox' },
    { value: 'safari', label: 'Safari' },
    { value: 'ios', label: 'iOS' },
    { value: 'android', label: 'Android' },
    { value: 'edge', label: 'Edge' },
    { value: '360', label: '360 Browser' },
    { value: 'qq', label: 'QQ Browser' },
    { value: 'random', label: 'Random' },
    { value: 'randomized', label: 'Randomized' },
] as const

// VMess Settings
export interface VMessSettings {
    uuid?: string
    alterId?: number
    security?: string
    experiments?: string
}

// Fallback for VLESS/Trojan
export interface Fallback {
    name?: string
    alpn?: string
    path?: string
    dest?: string | number
    xver?: number
    type?: string
}

// VLESS Settings
export interface VLESSSettings {
    uuid?: string
    flow?: string
    encryption?: string
    decryption?: string
    fallbacks?: Fallback[]
}

// Trojan Settings
export interface TrojanSettings {
    password?: string
    fallbacks?: Fallback[]
}

// Dokodemo-door Settings
export interface DokodemoSettings {
    address?: string
    port?: number
    networks?: string
    userLevel?: number
    followRedirect?: boolean
    portMap?: Record<string, unknown>
}

// Sockopt Settings
export interface SockoptSettings {
    mark?: number
    tcpFastOpen?: boolean
    tproxy?: string
    domainStrategy?: string
    dialerProxy?: string
    tcpKeepAliveInterval?: number
    tcpKeepAliveIdle?: number
    tcpCongestion?: string
    tcpWindowClamp?: number
    tcpUserTimeout?: number
    tcpMaxSeg?: number
    tcpMptcp?: boolean
    acceptProxyProtocol?: boolean
    interface?: string
    v6Only?: boolean
    penetrate?: boolean
    addressPortStrategy?: string
    trustedXForwardedFor?: string[]
    happyEyeballs?: { tryDelay?: number; maxConcurrency?: number } | null
    customSockopt?: { level?: number; optName?: number; optValue?: unknown }[]
}

// FinalMask for packet masking. Each section is a JSON object (or unset).
// Values match xray-core's finalmask shape; the FE accepts free-form objects
// because xray-core keeps adding fields under quicParams (udpHop, congestion,
// bbrProfile, …) and we don't want to ship a typed schema that lags upstream.
// tcp/udp are arrays of Mask objects in xray-core; quicParams is an object.
export type FinalMaskSection = Record<string, unknown> | unknown[]
export interface FinalMask {
    tcp?: FinalMaskSection
    udp?: FinalMaskSection
    quicParams?: FinalMaskSection
}

// Mux Settings (outbound only)
export interface MuxSettings {
    enabled?: boolean
    concurrency?: number
    xudpConcurrency?: number
    xudpProxyUDP443?: string
}

// Proxy Settings (outbound proxy chaining)
export interface ProxySettingsConfig {
    tag?: string
    transportLayer?: boolean
}

// Hysteria2 Settings
export interface HysteriaSettings {
    auth?: string
    congestion?: string
    up?: string
    down?: string
    udpIdleTimeout?: number
}
