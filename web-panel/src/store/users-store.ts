import { create } from "zustand"

interface UsersState {
    /** Selected user ids, each with whether that user is banned (for the bulk bar). */
    selected: Map<number, boolean>
    toggle: (id: number, banned: boolean) => void
    setMany: (entries: [number, boolean][], select: boolean) => void
    clear: () => void
}

export const useUsersStore = create<UsersState>()((set) => ({
    selected: new Map(),
    toggle: (id, banned) => set((s) => {
        const next = new Map(s.selected)
        if (next.has(id)) next.delete(id)
        else next.set(id, banned)
        return { selected: next }
    }),
    setMany: (entries, select) => set((s) => {
        const next = new Map(s.selected)
        for (const [id, banned] of entries) {
            if (select) next.set(id, banned)
            else next.delete(id)
        }
        return { selected: next }
    }),
    clear: () => set((s) => (s.selected.size === 0 ? s : { selected: new Map() })),
}))

// The old persisted sort and filter lived under this key; the URL owns the view now.
try {
    localStorage.removeItem("users-sort")
} catch {
    // storage blocked; nothing to clean
}
