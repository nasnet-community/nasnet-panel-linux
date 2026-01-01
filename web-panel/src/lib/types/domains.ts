// SNI (Server Name Indication) Types
export interface SNI {
    id: number
    name: string
    domain: string
    certificate: string
    private_key?: string // never returned by the API; only set when submitting
    cert_path: string
    key_path: string
    use_path_mode: boolean
    alpn: string
    is_auto_issued: boolean
    challenge_type: string
    expires_at: string
    auto_renew: boolean
    issue_error: string
    created_at: string
    updated_at: string
}

export interface ValidateCertResponse {
    valid: boolean
    expires_at: string
    san_warning?: string
}

export interface DNSChallengeResponse {
    txt_record: string
    txt_value: string
}
