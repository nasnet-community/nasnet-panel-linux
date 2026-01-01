import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface SettingsState {
    // Refresh interval in seconds (0 = off)
    refreshInterval: number
    setRefreshInterval: (seconds: number) => void
}

export const useSettingsStore = create<SettingsState>()(
    persist(
        (set) => ({
            refreshInterval: 10, // Default 10s
            setRefreshInterval: (seconds) => set({ refreshInterval: seconds }),
        }),
        {
            name: 'settings-storage',
        }
    )
)
