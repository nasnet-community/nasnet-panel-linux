import { Suspense, type ReactNode } from "react"
import { ErrorBoundary } from "@/components/ui/error-boundary"

interface RouteErrorBoundaryProps {
  children: ReactNode
}

export function RouteErrorBoundary({ children }: RouteErrorBoundaryProps) {
  return (
    <ErrorBoundary>
      <Suspense
        fallback={
          <div className="flex items-center justify-center py-24">
            <div className="w-8 h-8 border-4 border-primary border-t-transparent rounded-full animate-spin" />
          </div>
        }
      >
        {children}
      </Suspense>
    </ErrorBoundary>
  )
}
