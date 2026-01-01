import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { Subscription } from "@/lib/types"

type StatusFilter = "all" | "active" | "paused" | "expired" | "cancelled"
export type SubscriptionSortField = "created_at" | "data_used" | "lifetime_data_used" | "end_date" | "last_active_at" | "id"
export type SortDir = "asc" | "desc"

interface SubscriptionsState {
    // Filters & Pagination
    status: StatusFilter
    page: number
    search: string

    // Sorting
    sortField: SubscriptionSortField
    sortDir: SortDir

    // Advanced filters
    planFilter: string
    sourceFilter: string
    exhaustedFilter: string
    filtersExpanded: boolean

    // Selection
    selectedSubscriptions: Set<number>
    isMultiSelectMode: boolean

    // Mobile UI
    mobileSearchExpanded: boolean
    mobileFilterSheetOpen: boolean

    // Dialog states
    extendDialog: { open: boolean; subscription: Subscription | null; days: string }
    dataLimitDialog: { open: boolean; subscription: Subscription | null; limit: string }
    expiryDialog: { open: boolean; subscription: Subscription | null; date: string }

    // Detail Sheet
    detailsSheet: { open: boolean; subscription: Subscription | null }

    // Create Manual Dialog
    createManualDialog: { open: boolean; userId?: number }

    // Actions
    setStatus: (status: StatusFilter) => void
    setPage: (page: number) => void
    setSearch: (search: string) => void
    toggleSort: (field: SubscriptionSortField) => void
    setPlanFilter: (plan: string) => void
    setSourceFilter: (source: string) => void
    setExhaustedFilter: (exhausted: string) => void
    clearAdvancedFilters: () => void
    toggleFiltersExpanded: () => void
    toggleSelectSubscription: (id: number) => void
    selectAll: (ids: number[]) => void
    clearSelection: () => void
    enterMultiSelectMode: () => void
    exitMultiSelectMode: () => void
    setMobileSearchExpanded: (expanded: boolean) => void
    setMobileFilterSheetOpen: (open: boolean) => void
    openDetailsSheet: (subscription: Subscription) => void
    closeDetailsSheet: () => void
    openExtendDialog: (subscription: Subscription) => void
    closeExtendDialog: () => void
    setExtendDays: (days: string) => void
    openDataLimitDialog: (subscription: Subscription) => void
    closeDataLimitDialog: () => void
    setDataLimit: (limit: string) => void
    openExpiryDialog: (subscription: Subscription) => void
    closeExpiryDialog: () => void
    setExpiryDate: (date: string) => void
    openCreateManualDialog: (userId?: number) => void
    closeCreateManualDialog: () => void
}

export const useSubscriptionsStore = create<SubscriptionsState>()(
    persist(
    (set) => ({
    status: "all",
    page: 1,
    search: "",
    sortField: "last_active_at",
    sortDir: "desc",
    planFilter: "all",
    sourceFilter: "all",
    exhaustedFilter: "all",
    filtersExpanded: false,
    selectedSubscriptions: new Set<number>(),
    isMultiSelectMode: false,
    mobileSearchExpanded: false,
    mobileFilterSheetOpen: false,
    extendDialog: { open: false, subscription: null, days: "30" },
    dataLimitDialog: { open: false, subscription: null, limit: "" },
    expiryDialog: { open: false, subscription: null, date: "" },
    detailsSheet: { open: false, subscription: null },
    createManualDialog: { open: false, userId: undefined as number | undefined },

    setStatus: (status) => set({ status, page: 1, selectedSubscriptions: new Set() }),
    setPage: (page) => set({ page, selectedSubscriptions: new Set() }),
    setSearch: (search) => set({ search, page: 1, selectedSubscriptions: new Set() }),
    toggleSort: (field) => set((state) => ({
        sortField: field,
        sortDir: state.sortField === field ? (state.sortDir === "asc" ? "desc" : "asc") : "desc",
        page: 1,
    })),
    setPlanFilter: (planFilter) => set({ planFilter, page: 1 }),
    setSourceFilter: (sourceFilter) => set({ sourceFilter, page: 1 }),
    setExhaustedFilter: (exhaustedFilter) => set({ exhaustedFilter, page: 1 }),
    clearAdvancedFilters: () => set({ planFilter: "all", sourceFilter: "all", exhaustedFilter: "all", page: 1 }),
    toggleFiltersExpanded: () => set((state) => ({ filtersExpanded: !state.filtersExpanded })),
    toggleSelectSubscription: (id) => set((state) => {
        const newSelected = new Set(state.selectedSubscriptions)
        if (newSelected.has(id)) newSelected.delete(id)
        else newSelected.add(id)
        return { selectedSubscriptions: newSelected }
    }),
    selectAll: (ids) => set({ selectedSubscriptions: new Set(ids) }),
    clearSelection: () => set({ selectedSubscriptions: new Set(), isMultiSelectMode: false }),
    enterMultiSelectMode: () => set({ isMultiSelectMode: true }),
    exitMultiSelectMode: () => set({ isMultiSelectMode: false, selectedSubscriptions: new Set() }),
    setMobileSearchExpanded: (expanded) => set({ mobileSearchExpanded: expanded }),
    setMobileFilterSheetOpen: (open) => set({ mobileFilterSheetOpen: open }),

    openDetailsSheet: (subscription) => set({ detailsSheet: { open: true, subscription } }),
    closeDetailsSheet: () => set({ detailsSheet: { open: false, subscription: null } }),

    openExtendDialog: (subscription) => set({
        extendDialog: { open: true, subscription, days: "30" },
    }),
    closeExtendDialog: () => set({
        extendDialog: { open: false, subscription: null, days: "30" },
    }),
    setExtendDays: (days) => set((state) => ({
        extendDialog: { ...state.extendDialog, days },
    })),

    openDataLimitDialog: (subscription) => set({
        dataLimitDialog: {
            open: true,
            subscription,
            limit: (subscription.custom_data_limit ?? subscription.data_limit) !== 0
                ? ((subscription.custom_data_limit ?? subscription.data_limit) / (1024 * 1024 * 1024)).toFixed(2)
                : "",
        },
    }),
    closeDataLimitDialog: () => set({
        dataLimitDialog: { open: false, subscription: null, limit: "" },
    }),
    setDataLimit: (limit) => set((state) => ({
        dataLimitDialog: { ...state.dataLimitDialog, limit },
    })),

    openExpiryDialog: (subscription) => set({
        expiryDialog: {
            open: true,
            subscription,
            date: subscription.end_date
                ? new Date(subscription.end_date).toISOString().split("T")[0]
                : "",
        },
    }),
    closeExpiryDialog: () => set({
        expiryDialog: { open: false, subscription: null, date: "" },
    }),
    setExpiryDate: (date) => set((state) => ({
        expiryDialog: { ...state.expiryDialog, date },
    })),

    openCreateManualDialog: (userId) => set({ createManualDialog: { open: true, userId } }),
    closeCreateManualDialog: () => set({ createManualDialog: { open: false, userId: undefined } }),
}),
    {
        name: "subscriptions-sort",
        partialize: (state) => ({
            sortField: state.sortField,
            sortDir: state.sortDir,
            status: state.status,
            planFilter: state.planFilter,
            sourceFilter: state.sourceFilter,
            exhaustedFilter: state.exhaustedFilter,
        }),
    },
))
