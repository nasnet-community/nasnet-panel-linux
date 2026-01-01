import { create } from "zustand"
import { persist } from "zustand/middleware"
import { api, UserInfo } from "@/lib/api"

interface AuthState {
    user: UserInfo | null
    isAuthenticated: boolean
    isLoading: boolean
    isCheckingAuth: boolean

    // Actions
    login: (username: string, password: string, rememberMe?: boolean) => Promise<boolean>
    logout: () => Promise<void>
    checkAuth: () => Promise<void>
    setUser: (user: UserInfo | null) => void
}

export const useAuthStore = create<AuthState>()(
    persist(
        (set, get) => ({
            user: null,
            isAuthenticated: false,
            isLoading: true,
            isCheckingAuth: false,

            login: async (username: string, password: string, rememberMe?: boolean) => {
                try {
                    const response = await api.login(username, password, rememberMe)
                    if (response.success && response.data) {
                        set({
                            user: response.data.user,
                            isAuthenticated: true,
                            isLoading: false,
                        })
                        return true
                    }
                    return false
                } catch (error) {
                    console.error("Login failed:", error)
                    throw error
                }
            },

            logout: async () => {
                await api.logout()
                set({
                    user: null,
                    isAuthenticated: false,
                    isLoading: false,
                })
            },

            checkAuth: async () => {
                // Prevent concurrent auth checks
                const state = get()
                if (state.isCheckingAuth) {
                    return
                }

                try {
                    set({ isLoading: true, isCheckingAuth: true })
                    const response = await api.getCurrentUser()
                    if (response.success && response.data) {
                        set({
                            user: response.data,
                            isAuthenticated: true,
                            isLoading: false,
                            isCheckingAuth: false,
                        })
                    } else {
                        set({
                            user: null,
                            isAuthenticated: false,
                            isLoading: false,
                            isCheckingAuth: false,
                        })
                    }
                } catch {
                    set({
                        user: null,
                        isAuthenticated: false,
                        isLoading: false,
                        isCheckingAuth: false,
                    })
                }
            },

            setUser: (user) => {
                set({
                    user,
                    isAuthenticated: !!user,
                })
            },
        }),
        {
            name: "auth-storage",
            partialize: (state) => ({
                // Only persist user info, not loading state
                user: state.user,
                isAuthenticated: state.isAuthenticated,
            }),
        }
    )
)
