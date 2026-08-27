import type { Inbound } from "./inbounds"

export interface HostFragmentSettings {
    packets: string
    length: string
    interval: string
}

export interface Host {
    id: number
    inbound_id?: number | null
    plan_id?: number | null
    remark: string
    address: string
    port?: number | null
    sni: string
    host: string
    path: string
    alpn: string
    fingerprint: string
    security: string
    allow_insecure?: boolean | null
    reality_public_key?: string
    reality_short_id?: string
    reality_spider_x?: string
    mode?: string
    header_type?: string
    service_name?: string
    flow?: string
    encryption?: string
    vmess_security?: string
    obfs_password?: string
    port_range?: string
    fragment_settings?: HostFragmentSettings | null
    tags: string[]
    priority: number
    is_disabled: boolean
    created_at: string
    updated_at: string
}

export interface HostTemplate {
    id: number
    name: string
    description: string
    remark: string
    address: string
    port?: number | null
    sni: string
    host: string
    path: string
    alpn: string
    fingerprint: string
    security: string
    allow_insecure?: boolean | null
    reality_public_key?: string
    reality_short_id?: string
    reality_spider_x?: string
    mode?: string
    header_type?: string
    service_name?: string
    flow?: string
    encryption?: string
    vmess_security?: string
    obfs_password?: string
    port_range?: string
    fragment_settings?: HostFragmentSettings | null
    priority?: number | null
    created_at: string
    updated_at: string
}

export interface HostWithRelations extends Host {
    inbound?: Inbound & { node?: { id: number; name: string; ip: string; country_code: string } }
}

// Option lists for host override selects. Values mirror what the link
// generator emits, so anything here is understood by the clients.
export const XHTTP_MODES = [
    { value: 'auto', label: 'Auto' },
    { value: 'packet-up', label: 'Packet Up' },
    { value: 'stream-up', label: 'Stream Up' },
    { value: 'stream-one', label: 'Stream One' },
] as const

export const TCP_HEADER_TYPES = [
    { value: 'none', label: 'None (raw)' },
    { value: 'http', label: 'HTTP (masquerade)' },
] as const

export const VLESS_FLOWS = [
    { value: 'none', label: 'None' },
    { value: 'xtls-rprx-vision', label: 'xtls-rprx-vision' },
    { value: 'xtls-rprx-vision-udp443', label: 'xtls-rprx-vision-udp443' },
] as const

export const VMESS_SECURITIES = [
    { value: 'auto', label: 'auto' },
    { value: 'aes-128-gcm', label: 'aes-128-gcm' },
    { value: 'chacha20-poly1305', label: 'chacha20-poly1305' },
    // xray-core removed none/zero/plain; a client on a recent core falls back
    // to auto. Kept selectable so existing host overrides still render.
    { value: 'none', label: 'none (ignored — falls back to auto)' },
    { value: 'zero', label: 'zero (ignored — falls back to auto)' },
] as const

// Alphabets xray-core accepts for the XHTTP session ID (splithttp.PredefinedTable).
// An empty value means "no table" — the core then generates a UUID instead.
export const XHTTP_SESSION_ID_TABLES = [
    { value: '__uuid__', label: 'Default (UUID)' },
    { value: 'number', label: 'number (0-9)' },
    { value: 'hex', label: 'hex (lowercase)' },
    { value: 'HEX', label: 'HEX (uppercase)' },
    { value: 'alphabet', label: 'alphabet (a-z)' },
    { value: 'ALPHABET', label: 'ALPHABET (A-Z)' },
    { value: 'Alphabet', label: 'Alphabet (a-zA-Z)' },
    { value: 'base36', label: 'base36 (0-9a-z)' },
    { value: 'BASE36', label: 'BASE36 (0-9A-Z)' },
    { value: 'Base62', label: 'Base62 (0-9a-zA-Z)' },
] as const
