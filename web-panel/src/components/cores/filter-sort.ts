import type { CoreInbound } from "@/lib/types/cores"
import type { CoreProtocolFilter, CoreSortField, SortDir } from "@/store/cores-store"

export interface CoreFilterSort {
    protocolFilter: CoreProtocolFilter
    search: string
    sortBy: CoreSortField
    sortDir: SortDir
}

/**
 * Filter by protocol + free-text search, then sort. Shared by the per-node
 * Tunnels tab and the cross-node "All Tunnels" overview so both behave
 * identically. Pure: returns a new array, never mutates the input.
 */
export function filterAndSortInbounds(
    inbounds: CoreInbound[],
    { protocolFilter, search, sortBy, sortDir }: CoreFilterSort,
): CoreInbound[] {
    let list = inbounds
    if (protocolFilter !== "all") {
        list = list.filter((i) => i.protocol === protocolFilter)
    }
    const q = search.trim().toLowerCase()
    if (q) {
        list = list.filter(
            (i) =>
                i.protocol.toLowerCase().includes(q) ||
                String(i.port).includes(q) ||
                i.remark.toLowerCase().includes(q) ||
                (i.users ?? []).some((u) => u.email.toLowerCase().includes(q)),
        )
    }
    const dir = sortDir === "asc" ? 1 : -1
    return [...list].sort((a, b) => {
        switch (sortBy) {
            case "port":
                return (a.port - b.port) * dir
            case "users":
                return ((a.users?.length ?? 0) - (b.users?.length ?? 0)) * dir
            case "status":
                return (Number(a.is_disabled) - Number(b.is_disabled)) * dir
            default:
                return a.protocol.localeCompare(b.protocol) * dir
        }
    })
}
