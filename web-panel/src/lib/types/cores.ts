// Types + catalog for the SSH and MTProto tunnels (core_inbounds / core_users).
// Mirrors internal/core/domain/core.go and the agent core config structs.
// The `settings` blob is a flat, per-protocol object (keys are the agent JSON
// tags); the per-protocol interfaces below type each variant for the forms.

import type { LucideIcon } from "lucide-react"
import { TerminalSquare, Send } from "lucide-react"

export type CoreProtocol = "ssh" | "mtproto"

/** Tunnels dial out themselves (the foreign uplinks on a router). */
export type EgressMode = "direct"

/** Per-node enable state of one core protocol (GET /cores/nodes/:id/protocols). */
export interface NodeProtocolState {
    protocol: CoreProtocol
    enabled: boolean
    inbound_count: number
}

/** Result of toggling a node protocol (POST .../protocols/:protocol/toggle). */
export interface NodeProtocolToggleResult {
    protocol: CoreProtocol
    enabled: boolean
    reconcile_warning: string
}
export type DeviceLimitStrategy = "accept" | "reject"

// ---------------------------------------------------------------------------
// Wire models
// ---------------------------------------------------------------------------

/** The flat per-protocol settings blob. Cast to a protocol interface below. */
export type CoreSettings = Record<string, unknown>

export interface CoreInbound {
    id: number
    node_id: number
    protocol: CoreProtocol
    listen: string
    port: number
    egress_mode: EgressMode
    settings: CoreSettings | null
    remark: string
    is_disabled: boolean
    created_at: string
    updated_at: string
    users?: CoreUser[]
}

export interface CoreUser {
    id: number
    core_inbound_id: number
    subscription_id?: number | null
    username: string
    email: string
    k_limit: number
    enabled: boolean
    created_at: string
    updated_at: string
    // secret has json:"-" server-side — never present here; only via the config endpoint.
}

/** Live runtime status of a protocol core (GET /inbounds/:id/status). */
export interface CoreStatus {
    running: boolean
    version: string
    live_sessions: number
    distinct_devices: number
    detail: string
}

/** Rendered connection links for one core_user (GET /users/:id/config). */
export interface ClientArtifact {
    protocol: string
    kind: "link"
    /** Link URIs, one per line. */
    content: string
    node_name?: string
    data_used?: number
    /** The node has no public address; the link carries the one the panel was reached on. */
    address_missing?: boolean
}

// Request bodies -------------------------------------------------------------

export interface CoreInboundInput {
    node_id: number
    protocol: CoreProtocol
    listen?: string
    port: number
    egress_mode?: EgressMode
    remark?: string
    settings?: CoreSettings | null
    is_disabled?: boolean
}

export interface CoreUserInput {
    email: string
    username?: string
    secret?: string
    k_limit?: number
}

export interface CoreUserUpdateInput {
    email?: string
    username?: string
    secret?: string
    k_limit?: number
}

// Mutation response shapes (inside the {success,data} envelope) --------------

export interface InboundMutationResult {
    inbound: CoreInbound
    reconcile_warning: string
}
export interface UserMutationResult {
    user: CoreUser
    reconcile_warning: string
}
export interface DeleteResult {
    deleted: number
    reconcile_warning: string
}

// ---------------------------------------------------------------------------
// Per-protocol settings interfaces (agent JSON tags — verified against
// internal/agent/core/<proto>/config.go)
// ---------------------------------------------------------------------------

export interface SshSettings {
    host_key?: string // PEM, generated hub-side
    host_key_fingerprint?: string // display only
    strategy?: DeviceLimitStrategy
}

export interface MtprotoModes {
    classic?: boolean
    secure?: boolean
    tls?: boolean
}
/** Which camouflage mode client links are issued for. */
export type MtprotoLinkMode = "auto" | "classic" | "secure" | "tls"

export interface MtprotoSettings {
    modes?: MtprotoModes

    /** Primary FakeTLS SNI. Empty disables FakeTLS (no secret can be built). */
    tls_domain?: string
    /** Extra SNI domains; each yields its own ee-link. */
    tls_domains_extra?: string[]
    /** Where failed probes are proxied. Empty => the primary TLS domain. */
    mask_host?: string
    unknown_sni_action?: "accept" | "drop"
    mask?: boolean
    tls_emulation?: boolean

    /** "auto" issues the strongest enabled mode. */
    link_mode?: MtprotoLinkMode
    /** Issue one link per enabled mode instead of a single link. */
    link_all_modes?: boolean

    middle_proxy?: boolean
    adtag_default?: string

    unique_ip_window_secs?: number
    rate_limit_up_bps?: number
    rate_limit_down_bps?: number

    log_level?: "debug" | "verbose" | "normal" | "silent"
    listen_backlog?: number

    metrics_port?: number
    socks_port?: number
}

// ---------------------------------------------------------------------------
// Protocol catalog — single source for picker, badge, forms
// ---------------------------------------------------------------------------

/** Tailwind accent classes per color (literal strings for JIT scanning). */
export type AccentColor = "teal" | "blue"

export interface AccentClasses {
    badge: string
    dot: string
    stripe: string
    text: string
    softBg: string
    ring: string
}

export const ACCENTS: Record<AccentColor, AccentClasses> = {
    teal: {
        badge: "bg-teal-500/10 text-teal-600 border-teal-500/20 dark:text-teal-400",
        dot: "bg-teal-500",
        stripe: "bg-teal-500",
        text: "text-teal-600 dark:text-teal-400",
        softBg: "bg-teal-500/10",
        ring: "ring-teal-500/30",
    },
    blue: {
        badge: "bg-blue-500/10 text-blue-600 border-blue-500/20 dark:text-blue-400",
        dot: "bg-blue-500",
        stripe: "bg-blue-500",
        text: "text-blue-600 dark:text-blue-400",
        softBg: "bg-blue-500/10",
        ring: "ring-blue-500/30",
    },
}

export interface CoreProtocolMeta {
    value: CoreProtocol
    label: string
    icon: LucideIcon
    accent: AccentColor
    family: string
    oneLiner: string
    /** Needs a binary installed on the node (see the Dependencies section). */
    needsBinary: boolean
    /** RHF settings-slot key. */
    settingsKey: string
}

export const CORE_PROTOCOLS: CoreProtocolMeta[] = [
    {
        value: "ssh",
        label: "SSH",
        icon: TerminalSquare,
        accent: "teal",
        family: "Userspace relay",
        oneLiner: "Built-in SSH tunnel — no daemon, no certificate.",
        needsBinary: false,
        settingsKey: "ssh_settings",
    },
    {
        value: "mtproto",
        label: "MTProto",
        icon: Send,
        accent: "blue",
        family: "Userspace relay",
        oneLiner: "Telegram proxy with FakeTLS camouflage.",
        needsBinary: true,
        settingsKey: "mtproto_settings",
    },
]

export const CORE_PROTOCOL_MAP: Record<CoreProtocol, CoreProtocolMeta> =
    Object.fromEntries(CORE_PROTOCOLS.map((p) => [p.value, p])) as Record<
        CoreProtocol,
        CoreProtocolMeta
    >

export function coreProtocolMeta(p: CoreProtocol): CoreProtocolMeta {
    return CORE_PROTOCOL_MAP[p]
}
