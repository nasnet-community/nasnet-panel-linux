import { create } from "zustand"

interface NodesState {
    // Dialog states
    addDialogOpen: boolean
    deployDialogOpen: boolean
    selectedDeployNodeId: number | null
    checkingHealthId: number | null

    // Bulk selection. We keep the Set as the source of truth; the setter
    // clones it so React picks up the change (Zustand compares by reference).
    selectedIds: Set<number>
    lastSelectedId: number | null

    // New node form
    newNode: {
        name: string
        ip: string
        country: string
        datacenter: string
        api_port: string
        agent_port: string
        connect_mode: "direct" | "reverse"
        is_stealth: boolean
        is_persistent_stealth: boolean
    }

    // Actions
    openAddDialog: () => void
    closeAddDialog: () => void
    openDeployDialog: (nodeId: number) => void
    closeDeployDialog: () => void
    setCheckingHealth: (nodeId: number | null) => void
    updateNewNode: (updates: Partial<NodesState["newNode"]>) => void
    resetNewNode: () => void

    // Bulk selection actions
    toggleSelected: (nodeId: number) => void
    selectRange: (fromId: number, toId: number, orderedIds: number[]) => void
    setSelected: (ids: number[]) => void
    clearSelection: () => void
    isSelected: (nodeId: number) => boolean
}

const defaultNewNode = {
    name: "",
    ip: "",
    country: "",
    datacenter: "",
    api_port: "10085",
    agent_port: "9090",
    connect_mode: "direct" as const,
    is_stealth: false,
    is_persistent_stealth: false,
}

export const useNodesStore = create<NodesState>((set, get) => ({
    addDialogOpen: false,
    deployDialogOpen: false,
    selectedDeployNodeId: null,
    checkingHealthId: null,
    selectedIds: new Set<number>(),
    lastSelectedId: null,
    newNode: defaultNewNode,

    openAddDialog: () => set({ addDialogOpen: true }),
    closeAddDialog: () => set({ addDialogOpen: false, newNode: defaultNewNode }),
    openDeployDialog: (nodeId) => set({ deployDialogOpen: true, selectedDeployNodeId: nodeId }),
    closeDeployDialog: () => set({ deployDialogOpen: false, selectedDeployNodeId: null }),
    setCheckingHealth: (nodeId) => set({ checkingHealthId: nodeId }),
    updateNewNode: (updates) => set((state) => ({
        newNode: { ...state.newNode, ...updates },
    })),
    resetNewNode: () => set({ newNode: defaultNewNode }),

    toggleSelected: (nodeId) => set((state) => {
        const next = new Set(state.selectedIds)
        if (next.has(nodeId)) {
            next.delete(nodeId)
        } else {
            next.add(nodeId)
        }
        return { selectedIds: next, lastSelectedId: nodeId }
    }),
    // selectRange: adds every id between fromId and toId (inclusive) as they
    // appear in orderedIds. Used for shift-click; does not clear prior sel.
    selectRange: (fromId, toId, orderedIds) => set((state) => {
        const from = orderedIds.indexOf(fromId)
        const to = orderedIds.indexOf(toId)
        if (from === -1 || to === -1) return {}
        const [lo, hi] = from <= to ? [from, to] : [to, from]
        const next = new Set(state.selectedIds)
        for (let i = lo; i <= hi; i++) next.add(orderedIds[i])
        return { selectedIds: next, lastSelectedId: toId }
    }),
    setSelected: (ids) => set({ selectedIds: new Set(ids), lastSelectedId: ids.length ? ids[ids.length - 1] : null }),
    clearSelection: () => set({ selectedIds: new Set<number>(), lastSelectedId: null }),
    isSelected: (nodeId) => get().selectedIds.has(nodeId),
}))
