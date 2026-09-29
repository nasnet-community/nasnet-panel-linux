import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { CoreProtocol } from "@/lib/types/cores"

export type CoreProtocolFilter = "all" | CoreProtocol
export type CoreSortField = "protocol" | "port" | "users" | "status"
export type SortDir = "asc" | "desc"

interface CoresState {
    search: string
    protocolFilter: CoreProtocolFilter
    sortBy: CoreSortField
    sortDir: SortDir
    selectedIds: Set<number>

    setSearch: (search: string) => void
    setProtocolFilter: (filter: CoreProtocolFilter) => void
    toggleSort: (field: CoreSortField) => void
    setSort: (field: CoreSortField, dir: SortDir) => void
    toggleSelection: (id: number) => void
    selectAll: (ids: number[]) => void
    clearSelection: () => void
}

export const useCoresStore = create<CoresState>()(
    persist(
        (set, get) => ({
            search: "",
            protocolFilter: "all",
            sortBy: "protocol",
            sortDir: "asc",
            selectedIds: new Set<number>(),

            setSearch: (search) => set({ search }),
            setProtocolFilter: (protocolFilter) => set({ protocolFilter, selectedIds: new Set() }),
            toggleSort: (field) => {
                const s = get()
                if (s.sortBy === field) {
                    set({ sortDir: s.sortDir === "asc" ? "desc" : "asc" })
                } else {
                    set({ sortBy: field, sortDir: "asc" })
                }
            },
            setSort: (sortBy, sortDir) => set({ sortBy, sortDir }),
            toggleSelection: (id) =>
                set((s) => {
                    const next = new Set(s.selectedIds)
                    if (next.has(id)) next.delete(id)
                    else next.add(id)
                    return { selectedIds: next }
                }),
            selectAll: (ids) => set({ selectedIds: new Set(ids) }),
            clearSelection: () => set({ selectedIds: new Set() }),
        }),
        {
            name: "tunnels-ui",
            partialize: (state) => ({
                sortBy: state.sortBy,
                sortDir: state.sortDir,
                protocolFilter: state.protocolFilter,
            }),
        },
    ),
)
