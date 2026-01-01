import { Badge } from "@/components/ui/badge"

// Subscription status badge
const subscriptionVariants: Record<string, "success" | "warning" | "danger" | "outline"> = {
    active: "success",
    paused: "warning",
    expired: "danger",
    cancelled: "outline",
    traffic_exhausted: "danger",
}

const subscriptionLabels: Record<string, string> = {
    active: "Active",
    paused: "Paused",
    expired: "Expired",
    cancelled: "Cancelled",
    traffic_exhausted: "Exhausted",
}

export function SubscriptionStatusBadge({ status, className }: { status: string; className?: string }) {
    return (
        <Badge variant={subscriptionVariants[status] || "outline"} className={className}>
            {subscriptionLabels[status] || status}
        </Badge>
    )
}

// Payment status badge
const paymentVariants: Record<string, "success" | "warning" | "danger" | "outline"> = {
    completed: "success",
    pending: "warning",
    failed: "danger",
    refunded: "outline",
}

export function PaymentStatusBadge({ status, className }: { status: string; className?: string }) {
    return (
        <Badge variant={paymentVariants[status] || "outline"} className={className}>
            {status.charAt(0).toUpperCase() + status.slice(1)}
        </Badge>
    )
}

// User status badge
export function UserStatusBadge({ isBanned, isAdmin, className }: { isBanned: boolean; isAdmin: boolean; className?: string }) {
    if (isBanned) return <Badge variant="danger" className={className}>Banned</Badge>
    if (isAdmin) return <Badge variant="default" className={className}>Admin</Badge>
    return <Badge variant="success" className={`font-semibold bg-emerald-500/20 border-emerald-500/30 ${className || ""}`}>Active</Badge>
}
