import { Suspense } from "react"
import { RouterProvider } from "react-router"
import { Providers } from "@/components/providers"
import { router } from "@/routes"

export function App() {
  return (
    <Providers>
      <Suspense
        fallback={
          <div className="min-h-screen flex items-center justify-center bg-background">
            <div className="w-8 h-8 border-4 border-primary border-t-transparent rounded-full animate-spin" />
          </div>
        }
      >
        <RouterProvider router={router} />
      </Suspense>
    </Providers>
  )
}
