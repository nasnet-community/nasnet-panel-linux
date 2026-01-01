import { Link } from "react-router"
import { useLocation } from "react-router"
import { HiOutlineChevronRight, HiOutlineArrowLeft } from "react-icons/hi"

// Map of paths to labels — covers child pages that need breadcrumbs
const PATH_LABELS: Record<string, string> = {
    "/dashboard": "Dashboard",
    "/users": "Users",
    "/subscriptions": "Subscriptions",
    "/accounts": "Accounts",
    "/plans": "Plans",
    "/payments": "Payments",
    "/nodes": "Nodes",
    "/hosts": "Hosts",
    "/certificates": "Certificates",
    "/backup": "Backup",
    "/audit": "Audit Log",
    "/settings": "Settings",
}

// Parent relationships for child pages
const PARENT_MAP: Record<string, string> = {
    "/accounts": "/subscriptions",
}

// Section labels for intermediate breadcrumb context
const SECTION_MAP: Record<string, string> = {
    "/users": "Customers",
    "/subscriptions": "Customers",
    "/accounts": "Customers",
    "/plans": "Customers",
    "/payments": "Customers",
    "/nodes": "Infrastructure",
    "/hosts": "Infrastructure",
    "/certificates": "Infrastructure",
    "/backup": "System",
    "/audit": "System",
    "/settings": "System",
}

export function Breadcrumbs() {
    const { pathname } = useLocation()

    // Only show breadcrumbs for child pages (pages with parents)
    const basePath = "/" + pathname.split("/").filter(Boolean)[0]
    const parentPath = PARENT_MAP[basePath]

    if (!parentPath) return null

    const parentLabel = PATH_LABELS[parentPath]
    const currentLabel = PATH_LABELS[basePath]

    if (!parentLabel || !currentLabel) return null

    return (
        <>
            {/* Desktop breadcrumb */}
            <nav className="hidden sm:flex items-center gap-1.5 text-sm text-muted-foreground mb-4">
                <Link
                    to={parentPath}
                    className="hover:text-foreground transition-colors"
                >
                    {parentLabel}
                </Link>
                <HiOutlineChevronRight className="w-3.5 h-3.5" />
                <span className="text-foreground font-medium">{currentLabel}</span>
            </nav>

            {/* Mobile back link */}
            <nav className="sm:hidden mb-3">
                <Link
                    to={parentPath}
                    className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
                >
                    <HiOutlineArrowLeft className="w-3.5 h-3.5" />
                    {parentLabel}
                </Link>
            </nav>
        </>
    )
}
