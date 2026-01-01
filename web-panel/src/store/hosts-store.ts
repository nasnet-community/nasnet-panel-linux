import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { HostWithRelations } from "@/lib/types"

type StatusFilter = "all" | "enabled" | "disabled"
type HostTypeFilter = "all" | "server" | "info"

interface HostsState {
    // Filters & Pagination
    statusFilter: StatusFilter
    hostType: HostTypeFilter
    page: number
    search: string
    nodeFilter: string
    inboundFilter: string
    planFilter: string
    tagFilter: string

    // Sorting
    sortBy: string
    sortOrder: "asc" | "desc"

    // Pagination
    perPage: number

    // Groups
    collapsedGroups: string[]

    // Mobile
    mobileFiltersOpen: boolean

    // Selection
    selectedHosts: Set<number>
    isMultiSelectMode: boolean

    // Dialog state
    editDialog: { open: boolean; host: HostWithRelations | null }

    // Actions
    setStatusFilter: (status: StatusFilter) => void
    setHostType: (type: HostTypeFilter) => void
    setPage: (page: number) => void
    setSearch: (search: string) => void
    setNodeFilter: (value: string) => void
    setInboundFilter: (value: string) => void
    setPlanFilter: (value: string) => void
    setTagFilter: (value: string) => void
    setSortBy: (sortBy: string) => void
    setSortOrder: (sortOrder: "asc" | "desc") => void
    toggleSort: (column: string) => void
    setPerPage: (perPage: number) => void
    toggleGroupCollapsed: (groupKey: string) => void
    collapseAllGroups: (keys: string[]) => void
    expandAllGroups: () => void
    setMobileFiltersOpen: (open: boolean) => void
    toggleSelectHost: (id: number) => void
    selectAll: (ids: number[]) => void
    clearSelection: () => void
    enterMultiSelectMode: () => void
    exitMultiSelectMode: () => void
    openEditDialog: (host?: HostWithRelations | null) => void
    closeEditDialog: () => void
}

export const useHostsStore = create<HostsState>()(
    persist(
    (set) => ({
    statusFilter: "all",
    hostType: "all",
    page: 1,
    search: "",
    nodeFilter: "all",
    inboundFilter: "all",
    planFilter: "all",
    tagFilter: "all",
    sortBy: "priority",
    sortOrder: "asc" as const,
    perPage: 20,
    collapsedGroups: [],
    mobileFiltersOpen: false,
    selectedHosts: new Set<number>(),
    isMultiSelectMode: false,
    editDialog: { open: false, host: null },

    setStatusFilter: (statusFilter) => set({ statusFilter, page: 1, selectedHosts: new Set() }),
    setHostType: (hostType) => set({ hostType, page: 1, selectedHosts: new Set() }),
    setPage: (page) => set({ page, selectedHosts: new Set() }),
    setSearch: (search) => set({ search, page: 1 }),
    setNodeFilter: (nodeFilter) => set({ nodeFilter, inboundFilter: "all", page: 1 }),
    setInboundFilter: (inboundFilter) => set({ inboundFilter, page: 1 }),
    setPlanFilter: (planFilter) => set({ planFilter, page: 1 }),
    setTagFilter: (tagFilter) => set({ tagFilter, page: 1, selectedHosts: new Set() }),
    setSortBy: (sortBy) => set({ sortBy, page: 1 }),
    setSortOrder: (sortOrder) => set({ sortOrder, page: 1 }),
    toggleSort: (column) => set((state) => {
        if (state.sortBy === column) {
            return { sortOrder: state.sortOrder === "asc" ? "desc" : "asc", page: 1 }
        }
        return { sortBy: column, sortOrder: "asc", page: 1 }
    }),
    setPerPage: (perPage) => set({ perPage, page: 1 }),

    toggleGroupCollapsed: (groupKey) => set((state) => {
        const collapsed = state.collapsedGroups.includes(groupKey)
            ? state.collapsedGroups.filter(k => k !== groupKey)
            : [...state.collapsedGroups, groupKey]
        return { collapsedGroups: collapsed }
    }),
    collapseAllGroups: (keys) => set({ collapsedGroups: keys }),
    expandAllGroups: () => set({ collapsedGroups: [] }),
    setMobileFiltersOpen: (mobileFiltersOpen) => set({ mobileFiltersOpen }),

    toggleSelectHost: (id) => set((state) => {
        const newSelected = new Set(state.selectedHosts)
        if (newSelected.has(id)) newSelected.delete(id)
        else newSelected.add(id)
        return { selectedHosts: newSelected }
    }),
    selectAll: (ids) => set({ selectedHosts: new Set(ids) }),
    clearSelection: () => set({ selectedHosts: new Set(), isMultiSelectMode: false }),
    enterMultiSelectMode: () => set({ isMultiSelectMode: true }),
    exitMultiSelectMode: () => set({ isMultiSelectMode: false, selectedHosts: new Set() }),

    openEditDialog: (host = null) => set({ editDialog: { open: true, host: host ?? null } }),
    closeEditDialog: () => set({ editDialog: { open: false, host: null } }),
}),
    {
        name: "hosts-filters",
        partialize: (state) => ({
            statusFilter: state.statusFilter,
            hostType: state.hostType,
            sortBy: state.sortBy,
            sortOrder: state.sortOrder,
            perPage: state.perPage,
            collapsedGroups: state.collapsedGroups,
            tagFilter: state.tagFilter,
        }),
    },
))
