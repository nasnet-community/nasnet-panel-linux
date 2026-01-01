import { ThemeProvider } from "./theme-provider"
import { QueryProvider } from "./query-provider"
import { ToastProvider } from "./toast-provider"
import { AuthProvider } from "./auth-provider"

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider>
      <QueryProvider>
        <AuthProvider>
          {children}
        </AuthProvider>
        <ToastProvider />
      </QueryProvider>
    </ThemeProvider>
  )
}
