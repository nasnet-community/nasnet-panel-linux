import type { Node, NodeTransitionReason } from "@/lib/types/nodes"

/** The built-in node runs inside this panel; it has no agent. */
export function isLocalNode(n: Pick<Node, "connect_mode"> | null | undefined): boolean {
    return n?.connect_mode === "local"
}

const LOOPBACK = /^(127\.|::1$|localhost$)/

export function nodeAddress(n: Pick<Node, "ip" | "connect_mode">): string {
    return isLocalNode(n) && LOOPBACK.test(n.ip) ? "this router" : n.ip
}

const REASON_LABELS: Partial<Record<NodeTransitionReason, string>> = {
    heartbeat_timeout: "Heartbeat timed out",
    agent_unreachable: "Agent unreachable",
    health_check_failed: "Health check failed",
    hub_shutdown: "Panel stopped",
    admin_disabled: "Disabled by operator",
    maintenance: "Maintenance",
    panel_down: "Panel down",
}

// The built-in node has no agent (D19): name what failed without one.
const LOCAL_REASON_LABELS: Partial<Record<NodeTransitionReason, string>> = {
    agent_unreachable: "Not responding",
    heartbeat_timeout: "Not responding",
}

/** Why a node went offline, in the words its kind uses. */
export function outageReasonLabel(reason: NodeTransitionReason | string, local: boolean): string {
    const r = reason as NodeTransitionReason
    return (local ? LOCAL_REASON_LABELS[r] : undefined) ?? REASON_LABELS[r] ?? reason.replace(/_/g, " ")
}
