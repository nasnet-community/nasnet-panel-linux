import { keepPreviousData, useQuery } from "@tanstack/react-query"
import { useDebounce } from "use-debounce"
import { listSubscriptions } from "@/lib/api/subscriptions"
import { listUsers } from "@/lib/api/users"
import { isLocalNode } from "@/lib/node-kind"
import { useNodes } from "@/lib/queries/use-nodes"
import { readSearchInput } from "@/lib/uuid-search"

const LIMIT = 5

export function useCommandSearch(query: string, open: boolean) {
    const [debounced] = useDebounce(query, 200)
    // A pasted subscription link searches by the key it carries.
    const term = readSearchInput(debounced).value.trim()
    const enabled = open && term.length >= 2

    const users = useQuery({
        queryKey: ["command-search", "users", term],
        enabled,
        staleTime: 30_000,
        placeholderData: keepPreviousData,
        queryFn: async () => {
            const res = await listUsers({ search: term, per_page: LIMIT })
            return res.success ? res.data?.users ?? [] : []
        },
    })
    const subscriptions = useQuery({
        queryKey: ["command-search", "subscriptions", term],
        enabled,
        staleTime: 30_000,
        placeholderData: keepPreviousData,
        queryFn: async () => {
            const res = await listSubscriptions({ search: term, per_page: LIMIT })
            return res.success ? (res.data ?? []).slice(0, LIMIT) : []
        },
    })
    const nodeList = useNodes(open)
    const q = term.toLowerCase()
    const nodes = q
        ? (nodeList.data ?? []).filter((n) =>
            n.name.toLowerCase().includes(q) || n.ip.includes(q) || (isLocalNode(n) && "this router".includes(q)),
        ).slice(0, LIMIT)
        : []

    return {
        term,
        users: enabled ? users.data ?? [] : [],
        subscriptions: enabled ? subscriptions.data ?? [] : [],
        nodes,
        loading: enabled && (users.isFetching || subscriptions.isFetching),
    }
}
