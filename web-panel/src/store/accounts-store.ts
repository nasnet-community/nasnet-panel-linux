import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { Account } from "@/lib/admin-api"

type StatusFilter = "all" | "active" | "disabled" | "expired"

interface AccountsState {
    // Filters & Pagination
    status: StatusFilter
    page: number
    search: string
    exhaustedFilter: string
    nodeFilter: string
    inboundFilter: string
    sourceFilter: string
    filtersExpanded: boolean

    // Selection
    selectedAccounts: Set<number>
    isMultiSelectMode: boolean

    // Mobile UI
    mobileSearchExpanded: boolean
    mobileFilterSheetOpen: boolean

    // Detail Sheet
    detailsSheet: { open: boolean; account: Account | null }

    // Delete Dialog
    deleteDialog: { open: boolean; account: Account | null }

    // Create Dialog
    createDialog: { open: boolean }

    // Actions
    setStatus: (status: StatusFilter) => void
    setPage: (page: number) => void
    setSearch: (search: string) => void
    setExhaustedFilter: (value: string) => void
    setNodeFilter: (value: string) => void
    setInboundFilter: (value: string) => void
    setSourceFilter: (value: string) => void
    toggleFiltersExpanded: () => void
    clearAdvancedFilters: () => void
    toggleSelectAccount: (id: number) => void
    selectAll: (ids: number[]) => void
    clearSelection: () => void
    enterMultiSelectMode: () => void
    exitMultiSelectMode: () => void
    setMobileSearchExpanded: (expanded: boolean) => void
    setMobileFilterSheetOpen: (open: boolean) => void
    openDetailsSheet: (account: Account) => void
    closeDetailsSheet: () => void
    openDeleteDialog: (account: Account) => void
    closeDeleteDialog: () => void
    openCreateDialog: () => void
    closeCreateDialog: () => void
}

export const useAccountsStore = create<AccountsState>()(
    persist(
    (set) => ({
    status: "all",
    page: 1,
    search: "",
    exhaustedFilter: "all",
    nodeFilter: "all",
    inboundFilter: "all",
    sourceFilter: "all",
    filtersExpanded: false,
    selectedAccounts: new Set<number>(),
    isMultiSelectMode: false,
    mobileSearchExpanded: false,
    mobileFilterSheetOpen: false,
    detailsSheet: { open: false, account: null },
    deleteDialog: { open: false, account: null },
    createDialog: { open: false },

    setStatus: (status) => set({ status, page: 1, selectedAccounts: new Set() }),
    setPage: (page) => set({ page, selectedAccounts: new Set() }),
    setSearch: (search) => set({ search, page: 1 }),
    setExhaustedFilter: (exhaustedFilter) => set({ exhaustedFilter, page: 1 }),
    setNodeFilter: (nodeFilter) => set({ nodeFilter, inboundFilter: "all", page: 1 }),
    setInboundFilter: (inboundFilter) => set({ inboundFilter, page: 1 }),
    setSourceFilter: (sourceFilter) => set({ sourceFilter, page: 1 }),
    toggleFiltersExpanded: () => set((state) => ({ filtersExpanded: !state.filtersExpanded })),
    clearAdvancedFilters: () => set({
        exhaustedFilter: "all",
        nodeFilter: "all",
        inboundFilter: "all",
        sourceFilter: "all",
        page: 1,
    }),

    toggleSelectAccount: (id) => set((state) => {
        const newSelected = new Set(state.selectedAccounts)
        if (newSelected.has(id)) newSelected.delete(id)
        else newSelected.add(id)
        return { selectedAccounts: newSelected }
    }),
    selectAll: (ids) => set({ selectedAccounts: new Set(ids) }),
    clearSelection: () => set({ selectedAccounts: new Set(), isMultiSelectMode: false }),
    enterMultiSelectMode: () => set({ isMultiSelectMode: true }),
    exitMultiSelectMode: () => set({ isMultiSelectMode: false, selectedAccounts: new Set() }),
    setMobileSearchExpanded: (expanded) => set({ mobileSearchExpanded: expanded }),
    setMobileFilterSheetOpen: (open) => set({ mobileFilterSheetOpen: open }),

    openDetailsSheet: (account) => set({ detailsSheet: { open: true, account } }),
    closeDetailsSheet: () => set({ detailsSheet: { open: false, account: null } }),

    openDeleteDialog: (account) => set({ deleteDialog: { open: true, account } }),
    closeDeleteDialog: () => set({ deleteDialog: { open: false, account: null } }),

    openCreateDialog: () => set({ createDialog: { open: true } }),
    closeCreateDialog: () => set({ createDialog: { open: false } }),
}),
    {
        name: "accounts-filters",
        partialize: (state) => ({
            status: state.status,
        }),
    },
))
