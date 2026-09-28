// Agent Certificate Types
export interface AgentCertificate {
    id: number
    type: "ca" | "master" | "agent" | "public"
    node_id?: number
    common_name: string
    serial_number: string
    not_before: string
    not_after: string
    is_revoked: boolean
    is_valid: boolean
    days_until_expiry: number
    auto_renew?: boolean
    created_at: string
}

export interface DeployCommand {
    command: string
    node_id: number
    node_name: string
    node_ip: string
    port: number
    is_stealth?: boolean
}

export type CertType = "ca" | "master" | "agent" | "public"

export interface CertSummary {
    id: number
    type: CertType
    common_name: string
    serial: string
    not_before: string
    not_after: string
    days_left: number
    is_revoked: boolean
    revoked_at?: string
    node_id?: number
    node_name?: string
    orphan?: boolean
}

export interface CertCA extends CertSummary {
    fingerprint_sha256: string
}

export type PresentedMatch = "current" | "older" | "unknown"

export interface PresentedCert {
    serial: string
    common_name: string
    not_after: string
    seen_at: string
    mode: "direct" | "reverse"
    match: PresentedMatch
    revoked: boolean
}

export interface NodeCertRow {
    node_id: number
    name: string
    connect_mode: "local" | "direct" | "reverse"
    is_stealth: boolean
    online: boolean
    rotate_supported: boolean | null
    current: CertSummary | null
    presented: PresentedCert | null
    history: CertSummary[]
}

export type DomainSource = "acme-http" | "acme-dns" | "file" | "uploaded"

export interface DomainCertRow {
    id: number
    name: string
    domain: string
    source: DomainSource
    node_names: string[]
    expires_at: string | null
    days_left: number | null
    auto_renew: boolean
    is_auto_issued: boolean
    issue_error: string
    usage_count: number
    checked_at: string | null
    checked_on?: string
    note?: string
}

export type StrictBlockerReason = "not_seen" | "presents_revoked" | "presents_unknown" | "id_mismatch"

export interface StrictBlocker {
    node_id: number
    node_name: string
    reason: StrictBlockerReason
}

export interface StrictModeState {
    enabled: boolean
    ready: boolean
    blockers: StrictBlocker[]
}

export type AttentionKind = "strict_blocked" | "mismatch" | "cannot_rotate" | "expiring" | "orphans" | "legacy"

export interface AttentionItem {
    kind: AttentionKind
    count: number
    cert_ids?: number[]
    node_ids?: number[]
    message: string
}

export interface CertCounts {
    active: number
    expiring_30d: number
    revoked: number
    orphans: number
}

export interface CertOverview {
    ca: CertCA | null
    strict_mode: StrictModeState
    auto_rotate: { enabled: boolean; days: number }
    nodes: NodeCertRow[]
    domains: DomainCertRow[]
    legacy: CertSummary[]
    attention: AttentionItem[]
    counts: CertCounts
    certificates: CertSummary[]
}

export interface RotatedCert {
    id: number
    serial: string
    common_name: string
    not_after: string
}

export interface RotateResult {
    node_id: number
    node_name: string
    ok: boolean
    serial?: string
    error?: string
    code?: string
}

export interface CertDetails extends CertSummary {
    certificate: string
    fingerprint_sha256?: string
    dns_names?: string[]
    ip_addresses?: string[]
    issuer?: string
    has_private_key: boolean
}
