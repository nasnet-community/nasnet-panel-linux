import { create } from "zustand"
import { persist } from "zustand/middleware"

export type SortField = "username" | "balance" | "created_at" | "active_subscriptions" | "total_subscriptions" | "last_active_at"
export type SortDir = "asc" | "desc"
export type FilterType = "all" | "active" | "banned" | "admin" | "has_subscription" | "no_subscription"

interface UsersState {
    // Filters & Pagination
    page: number
    search: string
    filter: FilterType
    sortField: SortField
    sortDir: SortDir

    // Selection
    selectedUsers: Set<number>

    // Dialog states
    balanceDialog: { open: boolean; userId: number | null; amount: string }
    autoRefreshInterval: number // in milliseconds, 0 = off

    // Actions
    setPage: (page: number) => void
    setSearch: (search: string) => void
    setFilter: (filter: FilterType) => void
    toggleSort: (field: SortField) => void
    setSortDir: (dir: SortDir) => void
    toggleSelectUser: (userId: number) => void
    selectAll: (userIds: number[]) => void
    clearSelection: () => void
    openBalanceDialog: (userId: number) => void
    closeBalanceDialog: () => void
    setBalanceAmount: (amount: string) => void
    setAutoRefreshInterval: (interval: number) => void
    mobileSearchExpanded: boolean
    mobileFilterSheetOpen: boolean
    setMobileSearchExpanded: (expanded: boolean) => void
    setMobileFilterSheetOpen: (open: boolean) => void
    reset: () => void
}

const initialState = {
    page: 1,
    search: "",
    filter: "all" as FilterType,
    sortField: "created_at" as SortField,
    sortDir: "desc" as SortDir,
    selectedUsers: new Set<number>(),
    balanceDialog: { open: false, userId: null as number | null, amount: "" },
    autoRefreshInterval: 0,
    mobileSearchExpanded: false,
    mobileFilterSheetOpen: false,
}

export const useUsersStore = create<UsersState>()(
    persist(
    (set) => ({
    ...initialState,

    setPage: (page) => set({ page }),
    setSearch: (search) => set({ search, page: 1 }),
    setFilter: (filter) => set({ filter, page: 1 }),
    toggleSort: (field) => set((state) => ({
        sortField: field,
        sortDir: state.sortField === field && state.sortDir === "asc" ? "desc" : "asc",
        page: 1,
    })),
    setSortDir: (dir) => set({ sortDir: dir, page: 1 }),
    toggleSelectUser: (userId) => set((state) => {
        const newSelected = new Set(state.selectedUsers)
        if (newSelected.has(userId)) newSelected.delete(userId)
        else newSelected.add(userId)
        return { selectedUsers: newSelected }
    }),
    selectAll: (userIds) => set({ selectedUsers: new Set(userIds) }),
    clearSelection: () => set({ selectedUsers: new Set() }),
    openBalanceDialog: (userId) => set({ balanceDialog: { open: true, userId, amount: "" } }),
    closeBalanceDialog: () => set({ balanceDialog: { open: false, userId: null, amount: "" } }),
    setBalanceAmount: (amount) => set((state) => ({
        balanceDialog: { ...state.balanceDialog, amount },
    })),
    setAutoRefreshInterval: (interval) => set({ autoRefreshInterval: interval }),
    setMobileSearchExpanded: (expanded) => set({ mobileSearchExpanded: expanded }),
    setMobileFilterSheetOpen: (open) => set({ mobileFilterSheetOpen: open }),
    reset: () => set(initialState),
}),
    {
        name: "users-sort",
        partialize: (state) => ({
            sortField: state.sortField,
            sortDir: state.sortDir,
            filter: state.filter,
        }),
    },
))
