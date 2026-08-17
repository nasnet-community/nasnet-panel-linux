export type FlowNodeStatus = "ok" | "warn" | "down" | "ghost" | "off"
export type FlowEdgeStatus = "ok" | "down" | "ghost"
export type FlowEdgeKind = "data" | "transport" | "drop" | "dns"

export interface FlowDetailSection {
    title: string
    lines: string[]
}

export interface FlowNode {
    id: string
    kind: string
    label: string
    sublabel?: string
    status: FlowNodeStatus
    /** Why this piece is missing or unhappy, in the operator's language. */
    hint?: string
    detail?: FlowDetailSection[]
}

export interface FlowEdge {
    id: string
    from: string
    to: string
    kind: FlowEdgeKind
    status: FlowEdgeStatus
    label?: string
    counter_key?: string
}

/** Cumulative, not a rate: the page diffs consecutive polls itself. */
export interface FlowCounter {
    rx_bytes: number
    tx_bytes: number
    packets?: number
}

export interface FlowMismatch {
    node_id: string
    rule: string
    severity: "error" | "warn"
    message: string
    expected?: string
    actual?: string
}

export interface FlowView {
    generated_unix: number
    nodes: FlowNode[]
    edges: FlowEdge[]
    mismatches: FlowMismatch[]
    counters: Record<string, FlowCounter>
}

export type TraceSource = "lan" | "xray-foreign" | "xray-domestic" | "router"

export interface TraceRequest {
    dest: string
    source: TraceSource
}

export interface TraceStep {
    title: string
    evidence: string[]
    verdict: "ok" | "warn" | "drop" | "info"
}

export interface TraceView {
    dest: string
    resolved_ip: string
    source: string
    steps: TraceStep[]
    path_nodes: string[]
    path_edges: string[]
    final_verdict: "delivered-domestic" | "delivered-vpn" | "dropped" | "unprotected"
}

export interface FlowConn {
    proto: string
    src: string
    dst: string
    mark: number
    group: string
    pin: number
    device?: string
    rx_bytes: number
    tx_bytes: number
}

export interface FlowConnsView {
    flows: FlowConn[]
    total: number
    acct_enabled: boolean
}

export interface FlowEvent {
    type: string
    timestamp: string
    payload: unknown
}
