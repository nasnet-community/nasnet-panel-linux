import type { Node } from "@/lib/types/nodes"

/** The built-in node runs inside this panel; it has no agent. */
export function isLocalNode(n: Pick<Node, "connect_mode"> | null | undefined): boolean {
    return n?.connect_mode === "local"
}

const LOOPBACK = /^(127\.|::1$|localhost$)/

export function nodeAddress(n: Pick<Node, "ip" | "connect_mode">): string {
    return isLocalNode(n) && LOOPBACK.test(n.ip) ? "this router" : n.ip
}
