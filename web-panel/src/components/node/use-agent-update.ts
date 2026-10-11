import { useNodeOverview } from "@/lib/queries"
import { isLiteNode, isLocalNode } from "@/lib/node-kind"
import type { Node } from "@/lib/types"

export const AGENT_CURRENT = "Already up to date"

/** Whether the panel ships an agent this node doesn't run. The built-in and MTProto nodes never ask. */
export function useAgentUpdate(node: Node | null | undefined) {
    const remote = !!node && !isLocalNode(node) && !isLiteNode(node)
    const { data } = useNodeOverview(node?.id ?? 0, remote)
    const due = remote && !!data?.agent.update_available
    // a disabled button shows no tooltip, so callers hang this on its wrapper
    const reason = due ? undefined : data ? AGENT_CURRENT : "Checking for an update…"
    return { due, reason }
}
