import { Navigate, useLocation } from "react-router"
import { Skeleton } from "@/components/ui/skeleton"
import { useNodes } from "@/lib/queries"
import { isLocalNode } from "@/lib/node-kind"

/** `/server` is the built-in node's address; it keeps `?tab=` and bookmarks. */
export function LocalNodeRedirect() {
    const { search } = useLocation()
    const { data: nodes, isLoading } = useNodes()
    if (isLoading) return <Skeleton className="h-[180px] w-full" />
    const local = nodes?.find(isLocalNode)
    return <Navigate to={{ pathname: local ? `/nodes/${local.id}` : "/nodes", search }} replace />
}
