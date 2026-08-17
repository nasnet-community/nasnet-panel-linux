import { createBrowserRouter, Navigate, useLocation } from "react-router"
import { lazy } from "react"
import { getApiBaseUrl } from "@/lib/config"
import { AuthGuard } from "@/components/guards/auth-guard"
import { GuestGuard } from "@/components/guards/guest-guard"
import { DashboardLayout } from "@/components/layouts/dashboard-layout"
import { RouteErrorBoundary } from "@/components/ui/route-error-boundary"

const Login = lazy(() => import("@/pages/login"))
const Dashboard = lazy(() => import("@/pages/dashboard"))
const Network = lazy(() => import("@/pages/network"))
const NetworkFlow = lazy(() => import("@/pages/network/flow"))
const Users = lazy(() => import("@/pages/users"))
const UserDetail = lazy(() => import("@/pages/users/[id]"))
const Subscriptions = lazy(() => import("@/pages/subscriptions"))
const Accounts = lazy(() => import("@/pages/accounts"))
const Server = lazy(() => import("@/pages/nodes/[id]"))
const XrayBinaries = lazy(() => import("@/pages/xray-binaries"))
const Settings = lazy(() => import("@/pages/settings"))
const Certificates = lazy(() => import("@/pages/certificates"))
const Domains = lazy(() => import("@/pages/domains"))
const Hosts = lazy(() => import("@/pages/hosts"))
const AccessLogs = lazy(() => import("@/pages/access-logs"))
const AccessHistory = lazy(() => import("@/pages/access-history"))
const Audit = lazy(() => import("@/pages/audit"))
const Backup = lazy(() => import("@/pages/backup"))
const Alerts = lazy(() => import("@/pages/alerts"))
const SubPanel = lazy(() => import("@/pages/sub/[uuid]"))
// const Chats = lazy(() => import("@/pages/chats"))
const NotFound = lazy(() => import("@/pages/not-found"))

function r(element: React.ReactNode) {
  return <RouteErrorBoundary>{element}</RouteErrorBoundary>
}

function RedirectWithSearch({ to }: { to: string }) {
  const { search } = useLocation()
  return <Navigate to={{ pathname: to, search }} replace />
}

export const router = createBrowserRouter([
  // Public: subscription page
  { path: "/sub/:uuid", element: r(<SubPanel />) },

  // Guest-only
  {
    element: <GuestGuard />,
    children: [
      { path: "/login", element: r(<Login />) },
    ],
  },

  // Protected
  {
    element: <AuthGuard />,
    children: [{
      element: <DashboardLayout />,
      children: [
        { path: "/dashboard", element: r(<Dashboard />) },
        { path: "/users", element: r(<Users />) },
        { path: "/users/:id", element: r(<UserDetail />) },
        { path: "/server", element: r(<Server />) },
        { path: "/nodes", element: <Navigate to="/server" replace /> },
        { path: "/network", element: r(<Network />) },
        { path: "/network/flow", element: r(<NetworkFlow />) },
        { path: "/nodes/:id", element: <Navigate to="/server" replace /> },
        { path: "/xray-binaries", element: r(<XrayBinaries />) },
        { path: "/subscriptions", element: r(<Subscriptions />) },
        { path: "/accounts", element: r(<Accounts />) },
        // { path: "/chats", element: r(<Chats />) },
        { path: "/settings", element: r(<Settings />) },
        { path: "/certificates", element: r(<Certificates />) },
        { path: "/domains", element: r(<Domains />) },
        { path: "/hosts", element: r(<Hosts />) },
        { path: "/access-logs", element: r(<AccessLogs />) },
        { path: "/access-history", element: r(<AccessHistory />) },
        { path: "/access-history-search", element: <RedirectWithSearch to="/access-history" /> },
        { path: "/audit", element: r(<Audit />) },
        { path: "/backup", element: r(<Backup />) },
        { path: "/alerts", element: r(<Alerts />) },
      ],
    }],
  },

  { path: "/", element: <Navigate to="/dashboard" replace /> },
  { path: "*", element: r(<NotFound />) },
], {
  basename: getApiBaseUrl() || "/",
})
