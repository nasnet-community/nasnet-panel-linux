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
    fragment_settings?: HostFragmentSettings | null
    priority?: number | null
    created_at: string
    updated_at: string
}

export interface HostWithRelations extends Host {
    inbound?: Inbound & { node?: { id: number; name: string; ip: string; country_code: string } }
}
