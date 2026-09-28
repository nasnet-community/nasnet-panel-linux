import { create } from "zustand"
import { persist } from "zustand/middleware"

export type LedgerFilter = "all" | "valid" | "expiring" | "revoked" | "orphan" | "legacy"

interface CertificatesState {
    ledgerOpen: boolean
    filter: LedgerFilter
    search: string
    selected: Set<number>
    setLedgerOpen: (open: boolean) => void
    setFilter: (filter: LedgerFilter) => void
    setSearch: (search: string) => void
    toggleSelected: (id: number) => void
    setSelected: (ids: number[]) => void
    clearSelected: () => void
}

export const useCertificatesStore = create<CertificatesState>()(
    persist(
        (set) => ({
            ledgerOpen: false,
            filter: "all",
            search: "",
            selected: new Set<number>(),
            setLedgerOpen: (ledgerOpen) => set({ ledgerOpen }),
            setFilter: (filter) => set({ filter, selected: new Set() }),
            setSearch: (search) => set({ search }),
            toggleSelected: (id) => set((s) => {
                const next = new Set(s.selected)
                if (next.has(id)) next.delete(id)
                else next.add(id)
                return { selected: next }
            }),
            setSelected: (ids) => set({ selected: new Set(ids) }),
            clearSelected: () => set({ selected: new Set() }),
        }),
        {
            name: "certificates-page",
            partialize: (s) => ({ ledgerOpen: s.ledgerOpen, filter: s.filter }),
        },
    ),
)
