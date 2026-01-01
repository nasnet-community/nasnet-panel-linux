import { useEffect, useRef } from "react"
import { useAuthStore } from "@/store/auth-store"

interface AuthProviderProps {
    children: React.ReactNode
}

export function AuthProvider({ children }: AuthProviderProps) {
    const checkAuth = useAuthStore((state) => state.checkAuth)
    const hasChecked = useRef(false)

    useEffect(() => {
        // Only check auth once on mount
        if (!hasChecked.current) {
            hasChecked.current = true
            checkAuth()
        }
    }, [checkAuth])

    return <>{children}</>
}
