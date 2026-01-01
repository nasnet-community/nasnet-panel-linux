/** Usage bar color thresholds — single source of truth across all components. */
export const USAGE_THRESHOLDS = {
    /** Above this percentage → red/danger */
    danger: 90,
    /** Above this percentage → amber/warning */
    warning: 70,
} as const

/** Returns a Tailwind bg class for the usage bar fill. */
export function getUsageBarColor(percent: number): string {
    if (percent > USAGE_THRESHOLDS.danger) return "bg-red-500"
    if (percent > USAGE_THRESHOLDS.warning) return "bg-amber-500"
    return "bg-emerald-500"
}

/** Returns a Tailwind text class for usage text. */
export function getUsageTextColor(percent: number): string {
    if (percent > USAGE_THRESHOLDS.danger) return "text-red-500"
    if (percent > USAGE_THRESHOLDS.warning) return "text-amber-500"
    return "text-emerald-500"
}

/** Accent-bar bg class for mobile sub cards. Worst-of (usage OR expiry) wins.
 * usagePercent: 0-100 (0 = unlimited). */
export function getAccentSeverity(
    usagePercent: number,
    status: string,
    expiresAt?: string | null,
): string {
    if (status === "expired" || status === "cancelled") return "bg-muted-foreground"

    // Paused contributes amber to worst-of — usage/expiry can still escalate to red
    let severity: "green" | "amber" | "red" = status === "paused" ? "amber" : "green"

    // Usage severity
    if (usagePercent > USAGE_THRESHOLDS.danger) severity = "red"
    else if (usagePercent > USAGE_THRESHOLDS.warning) severity = "amber"

    // Expiry severity (worst-of with usage)
    if (expiresAt) {
        const days = Math.ceil((new Date(expiresAt).getTime() - Date.now()) / (1000 * 60 * 60 * 24))
        if (days <= 3) severity = "red"
        else if (days <= 7 && severity !== "red") severity = "amber"
    }

    const colorMap = { green: "bg-emerald-500", amber: "bg-amber-500", red: "bg-red-500" }
    return colorMap[severity]
}
