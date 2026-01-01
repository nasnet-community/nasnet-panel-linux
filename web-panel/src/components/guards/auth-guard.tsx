import { Navigate, Outlet, useLocation } from "react-router"
import { useAuthStore } from "@/store/auth-store"

export function AuthGuard() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const isLoading = useAuthStore((s) => s.isLoading)
  const location = useLocation()

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <div className="w-8 h-8 border-4 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to={`/login?callbackUrl=${encodeURIComponent(location.pathname)}`} replace />
  }

  return <Outlet />
}
