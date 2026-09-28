import { create } from "zustand"

interface CommandPaletteState {
    open: boolean
    shortcutsOpen: boolean
    setOpen: (open: boolean) => void
    toggle: () => void
    setShortcutsOpen: (open: boolean) => void
}

export const useCommandPalette = create<CommandPaletteState>()((set) => ({
    open: false,
    shortcutsOpen: false,
    setOpen: (open) => set({ open }),
    toggle: () => set((state) => ({ open: !state.open })),
    setShortcutsOpen: (shortcutsOpen) => set({ shortcutsOpen }),
}))
