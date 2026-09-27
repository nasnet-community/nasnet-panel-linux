// Changes this panel made itself, so the matching SSE event stays quiet: the
// admin already saw their own confirmation.

const CREATED_WINDOW_MS = 15_000

let manualSubscriptionAt = 0

export function markSubscriptionCreatedHere() {
    manualSubscriptionAt = Date.now()
}

export function subscriptionCreatedHereRecently(): boolean {
    return Date.now() - manualSubscriptionAt < CREATED_WINDOW_MS
}
