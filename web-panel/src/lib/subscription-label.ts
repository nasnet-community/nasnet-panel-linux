import type { User } from "./types/users"

// userDisplayName renders a user as "@username", falling back to their
// first/last name, then "". Mirrors the inline idiom used across account rows.
export function userDisplayName(user?: User | null): string {
    if (!user) return ""
    if (user.username) return `@${user.username}`
    return `${user.first_name ?? ""} ${user.last_name ?? ""}`.trim()
}

// Structural shape so both the full Subscription type and lighter list items
// (picker results, aggregates) satisfy it.
interface LabelableSubscription {
    label?: string
    user?: User | null
    plan?: { name?: string } | null
}

// subscriptionLabel resolves a human label for a subscription, preferring an
// operator-set custom label, then the owning user's @username (or name), then
// the plan name. Returns "" when nothing is known — callers append "Sub #<id>".
// Mirrors Subscription.DisplayLabel() on the backend.
export function subscriptionLabel(sub?: LabelableSubscription | null): string {
    if (!sub) return ""
    if (sub.label) return sub.label
    const name = userDisplayName(sub.user)
    if (name) return name
    return sub.plan?.name ?? ""
}
