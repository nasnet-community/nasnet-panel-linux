import { create } from "zustand"
import { persist } from "zustand/middleware"

export type CertTab = "internal" | "public"
export type CertFilter = "all" | "valid" | "expiring" | "expired" | "revoked" | null
export type CertSortField = "name" | "expiry" | "created" | "type"
export type SortDir = "asc" | "desc"
export type CertViewMode = "list" | "timeline"

interface CertificatesState {
    // Tab
    activeTab: CertTab

    // Filter
    activeFilter: CertFilter
    searchQuery: string

    // Sort
    sortBy: CertSortField
    sortDir: SortDir

    // Selection
    selectedIds: Set<number>

    // View
    viewMode: CertViewMode
    caBannerExpanded: boolean

    // Actions
    setActiveTab: (tab: CertTab) => void
    setActiveFilter: (filter: CertFilter) => void
    setSearchQuery: (query: string) => void
    toggleSort: (field: CertSortField) => void
    setSortBy: (field: CertSortField, dir: SortDir) => void
    toggleSelection: (id: number) => void
    selectAll: (ids: number[]) => void
    clearSelection: () => void
    setViewMode: (mode: CertViewMode) => void
    toggleViewMode: () => void
    setCaBannerExpanded: (expanded: boolean) => void
    toggleCaBanner: () => void
}

export const useCertificatesStore = create<CertificatesState>()(
    persist(
    (set, get) => ({
    activeTab: "internal",
    activeFilter: "all",
    searchQuery: "",
    sortBy: "expiry",
    sortDir: "asc",
    selectedIds: new Set<number>(),
    viewMode: "list",
    caBannerExpanded: false,

    setActiveTab: (activeTab) => set({ activeTab, activeFilter: "all", searchQuery: "", selectedIds: new Set() }),
    setActiveFilter: (filter) => {
        const current = get().activeFilter
        set({ activeFilter: current === filter ? "all" : filter, selectedIds: new Set() })
    },
    setSearchQuery: (searchQuery) => set({ searchQuery }),
    toggleSort: (field) => {
        const state = get()
        if (state.sortBy === field) {
            set({ sortDir: state.sortDir === "asc" ? "desc" : "asc" })
        } else {
            set({ sortBy: field, sortDir: "asc" })
        }
    },
    setSortBy: (sortBy, sortDir) => set({ sortBy, sortDir }),
    toggleSelection: (id) => set((s) => {
        const next = new Set(s.selectedIds)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        return { selectedIds: next }
    }),
    selectAll: (ids) => set({ selectedIds: new Set(ids) }),
    clearSelection: () => set({ selectedIds: new Set() }),
    setViewMode: (viewMode) => set({ viewMode }),
    toggleViewMode: () => set((s) => ({ viewMode: s.viewMode === "list" ? "timeline" : "list" })),
    setCaBannerExpanded: (caBannerExpanded) => set({ caBannerExpanded }),
    toggleCaBanner: () => set((s) => ({ caBannerExpanded: !s.caBannerExpanded })),
}),
    {
        name: "certificates-sort",
        partialize: (state) => ({
            sortBy: state.sortBy,
            sortDir: state.sortDir,
            activeFilter: state.activeFilter,
            activeTab: state.activeTab,
            viewMode: state.viewMode,
        }),
    },
))
