import { api, type ApiResponse } from "@/lib/api"
import type {
    AssignRoleRequest,
    NetworkApply,
    NetworkInterfaceView,
    NetworkPlan,
    NetworkState,
    Verdict,
    VerdictLevel,
} from "@/lib/types/network"

export async function getNetworkInterfaces(): Promise<ApiResponse<NetworkInterfaceView[]>> {
    return api.get<NetworkInterfaceView[]>("/api/v1/network/interfaces")
}

export async function getNetworkState(): Promise<ApiResponse<NetworkState>> {
    return api.get<NetworkState>("/api/v1/network/state")
}

export async function planNetworkChange(req: AssignRoleRequest): Promise<ApiResponse<NetworkPlan>> {
    return api.post<NetworkPlan>("/api/v1/network/plan", req)
}

export async function applyNetworkChange(req: AssignRoleRequest): Promise<ApiResponse<NetworkApply>> {
    return api.post<NetworkApply>("/api/v1/network/apply", req)
}

export async function confirmNetworkApply(planId: number): Promise<ApiResponse<null>> {
    return api.post<null>("/api/v1/network/confirm", { plan_id: planId })
}

export async function rollbackNetworkApply(): Promise<ApiResponse<null>> {
    return api.post<null>("/api/v1/network/rollback")
}

export async function identifyInterface(key: string): Promise<ApiResponse<null>> {
    return api.post<null>(`/api/v1/network/interfaces/${encodeURIComponent(key)}/identify`)
}

const SEVERITY: Record<VerdictLevel, number> = { warn: 1, confirm: 2, reject: 3 }

export function verdictSeverity(level: VerdictLevel): number {
    return SEVERITY[level] ?? 0
}

export function isRejected(verdicts: Verdict[]): boolean {
    return verdicts.some((v) => v.level === "reject")
}

/** Seconds left in the confirm window, floored at zero. */
export function remainingSeconds(deadlineUnix: number): number {
    return Math.max(0, deadlineUnix - Math.floor(Date.now() / 1000))
}

/**
 * The box may re-address itself mid-apply, so confirm goes to both the address
 * the operator is on and the one the plan moves it to. Deduplicated.
 */
export function confirmUrls(currentOrigin: string, altOrigin: string): string[] {
    const path = "/api/v1/network/confirm"
    const origins =
        altOrigin && altOrigin !== currentOrigin ? [currentOrigin, altOrigin] : [currentOrigin]
    return origins.map((o) => `${o.replace(/\/$/, "")}${path}`)
}

/**
 * Retries confirm against every candidate origin until one succeeds or the
 * window closes. Per-origin errors are swallowed: an unreachable old address is
 * the expected case after a re-address.
 */
export async function confirmWithFallback(
    planId: number,
    deadlineUnix: number,
    altOrigin: string,
): Promise<boolean> {
    const urls = confirmUrls(window.location.origin, altOrigin)
    while (remainingSeconds(deadlineUnix) > 0) {
        for (const url of urls) {
            try {
                const res = await fetch(url, {
                    method: "POST",
                    credentials: "include",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ plan_id: planId }),
                })
                if (res.ok) return true
            } catch {
                // This origin is unreachable right now — try the other one.
            }
        }
        await new Promise((r) => setTimeout(r, 2000))
    }
    return false
}
