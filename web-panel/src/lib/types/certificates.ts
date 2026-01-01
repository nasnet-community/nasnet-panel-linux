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

export interface CertBundle {
    node_id: number
    node_name: string
    ca_cert: string
    agent_cert: string
    agent_key: string
}

export interface DeployCommand {
    command: string
    node_id: number
    node_name: string
    node_ip: string
    port: number
    is_stealth?: boolean
}
