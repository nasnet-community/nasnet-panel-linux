import { DangerZone } from "@/components/node/danger-zone"
import { useNodes } from "@/lib/queries"
import { isLocalNode } from "@/lib/node-kind"

/** This router's Wipe and Nuke. Both take the panel with them, so they live here, not on a node page. */
export function RouterDangerZone() {
    const nodes = useNodes()
    const local = nodes.data?.find(isLocalNode)
    if (!local) return <p className="text-sm text-muted-foreground">{nodes.isError ? "Couldn't load this router's node." : "Loading…"}</p>
    return (
        <DangerZone
            nodeId={local.id} nodeName={local.name} canRemoveRecord={false} online={local.is_online}
            note="These erase this router. The panel is deleted too, so this page stops working."
        />
    )
}
