import type {
    TLSSettings,
    RealitySettings,
    TransportSettings,
    VMessSettings,
    VLESSSettings,
    TrojanSettings,
    ShadowsocksSettings,
    WireGuardSettings,
    HTTPSettings,
    SOCKSSettings,
    SockoptSettings,
    FinalMask,
    HysteriaSettings,
    MuxSettings,
    ProxySettingsConfig,
} from "./inbounds"

// Freedom Fragment for anti-censorship
export interface FreedomFragment {
    packets?: string
    length?: string
    interval?: string
    maxSplit?: string
}

// Freedom Noise for anti-censorship
export interface FreedomNoise {
    type?: string
    packet?: string
    delay?: string
    applyTo?: string
}

// Freedom Settings (for Outbound)
export interface FreedomSettings {
    domainStrategy?: string
    redirect?: string
    userLevel?: number
    fragment?: FreedomFragment
    noise?: FreedomNoise[]
    proxyProtocol?: number
}

// Blackhole Settings (for Outbound)
export interface BlackholeSettings {
    responseType?: string
}

// DNS Outbound Settings
export interface DNSOutboundSettings {
    network?: string
    address?: string
    port?: number
    userLevel?: number
    nonIPQuery?: string
    blockTypes?: number[]
}

// Loopback Settings
export interface LoopbackSettings {
    inboundTag?: string
}

export const OUTBOUND_PROTOCOLS = [
    { value: 'freedom', label: 'Freedom (Direct)' },
    { value: 'blackhole', label: 'Blackhole (Block)' },
    { value: 'vless', label: 'VLESS' },
    { value: 'vmess', label: 'VMess' },
    { value: 'trojan', label: 'Trojan' },
    { value: 'shadowsocks', label: 'Shadowsocks' },
    { value: 'wireguard', label: 'WireGuard' },
    { value: 'http', label: 'HTTP' },
    { value: 'socks', label: 'SOCKS' },
    { value: 'dns', label: 'DNS' },
    { value: 'loopback', label: 'Loopback' },
    { value: 'hysteria2', label: 'Hysteria2' },
] as const

export interface Outbound {
    id: number
    node_id: number
    tag: string
    protocol: string
    address: string
    port: number
    network: string
    security: string
    tls_settings?: TLSSettings
    reality_settings?: RealitySettings
    transport_settings?: TransportSettings

    // Protocol Specific Settings
    freedom_settings?: FreedomSettings
    blackhole_settings?: BlackholeSettings
    vmess_settings?: VMessSettings
    vless_settings?: VLESSSettings
    trojan_settings?: TrojanSettings
    shadowsocks_settings?: ShadowsocksSettings
    wireguard_settings?: WireGuardSettings
    http_settings?: HTTPSettings
    socks_settings?: SOCKSSettings
    dns_settings?: DNSOutboundSettings
    loopback_settings?: LoopbackSettings
    hysteria_settings?: HysteriaSettings
    mux_settings?: MuxSettings
    proxy_settings?: ProxySettingsConfig
    send_through?: string

    sockopt_settings?: SockoptSettings
    finalmask?: FinalMask

    remark: string
    uplink?: number
    downlink?: number
    is_disabled: boolean
    created_at: string
    updated_at: string
}
