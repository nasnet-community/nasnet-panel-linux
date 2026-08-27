import { ApiError, api, type ApiResponse } from "@/lib/api"
import type {
    AssignRoleRequest,
    LANConfig,
    LANDeviceList,
    LANView,
    NetworkApply,
    NetworkInterfaceView,
    NetworkPlan,
    NetworkState,
    PoolStrategy,
    PortForward,
    Verdict,
    VerdictLevel,
    VPNPoolStatus,
    VPNProfile,
    VPNProfileInput,
    WireGuardConfig,
} from "@/lib/types/network"
import type { RouterHealth } from "@/lib/types/health"

export async function getNetworkInterfaces(): Promise<ApiResponse<NetworkInterfaceView[]>> {
    return api.get<NetworkInterfaceView[]>("/api/v1/network/interfaces")
}

export async function getNetworkState(): Promise<ApiResponse<NetworkState>> {
    return api.get<NetworkState>("/api/v1/network/state")
}

export async function getRouterHealth(): Promise<ApiResponse<RouterHealth>> {
    return api.get<RouterHealth>("/api/v1/network/health")
}

export async function setUplinkForce(ifName: string, state: "" | "up" | "down"): Promise<ApiResponse<{ state: string }>> {
    return api.put<{ state: string }>(`/api/v1/network/uplinks/${encodeURIComponent(ifName)}/force`, { state })
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

export async function getLAN(): Promise<ApiResponse<LANView>> {
    return api.get<LANView>("/api/v1/network/lan")
}

export async function updateLAN(cfg: Partial<LANConfig>): Promise<ApiResponse<NetworkApply>> {
    return api.put<NetworkApply>("/api/v1/network/lan", cfg)
}

export async function getLANDevices(): Promise<ApiResponse<LANDeviceList>> {
    return api.get<LANDeviceList>("/api/v1/network/lan/devices")
}

/** An empty label removes the name. Ports are keyed the same way devices are. */
export async function setInterfaceLabel(key: string, label: string): Promise<ApiResponse<null>> {
    return api.put<null>(`/api/v1/network/interfaces/${encodeURIComponent(key)}`, { label })
}

/** An empty label removes the name. The MAC goes in the path, like an interface key. */
export async function setDeviceLabel(mac: string, label: string): Promise<ApiResponse<null>> {
    return api.put<null>(`/api/v1/network/lan/devices/${encodeURIComponent(mac)}/label`, { label })
}

export async function getPortForwards(): Promise<ApiResponse<PortForward[]>> {
    return api.get<PortForward[]>("/api/v1/network/port-forwards")
}

export type PortForwardInput = Omit<PortForward, "id"> & { id?: number; confirmed?: boolean }

export async function createPortForward(pf: PortForwardInput): Promise<ApiResponse<null>> {
    return api.post<null>("/api/v1/network/port-forwards", pf)
}

export async function updatePortForward(
    id: number,
    pf: PortForwardInput,
): Promise<ApiResponse<null>> {
    return api.put<null>(`/api/v1/network/port-forwards/${id}`, pf)
}

export async function deletePortForward(id: number): Promise<ApiResponse<null>> {
    return api.delete<null>(`/api/v1/network/port-forwards/${id}`)
}

/** Verdicts a rejected request came back with, so the UI names the rule. */
export function verdictsFromError(err: unknown): Verdict[] {
    if (!(err instanceof ApiError)) return []
    const body = err.body as { verdicts?: Verdict[] } | undefined
    return body?.verdicts ?? []
}

/** 409 means the change is permitted but needs a typed CONFIRM first. */
export function needsConfirm(err: unknown): boolean {
    return err instanceof ApiError && err.status === 409
}

/** Human summary of one forward. An empty uplink_key means every uplink. */
export function portForwardSummary(pf: PortForward, labels: Record<string, string>): string {
    const where = pf.uplink_key ? (labels[pf.uplink_key] ?? pf.uplink_key) : "any uplink"
    return `${pf.proto.toUpperCase()}/${pf.dport} on ${where} → ${pf.to_addr}:${pf.to_port}`
}

/** Pre-check only; V27 on the server is the enforcement point. An empty
 *  uplink_key on either side means "any uplink", so it collides with everything. */
export function collidesLocally(
    existing: PortForward[],
    candidate: { proto: string; dport: number; uplink_key: string; id?: number },
): boolean {
    return existing.some(
        (e) =>
            e.id !== candidate.id &&
            e.enabled &&
            e.proto === candidate.proto &&
            e.dport === candidate.dport &&
            (e.uplink_key === "" ||
                candidate.uplink_key === "" ||
                e.uplink_key === candidate.uplink_key),
    )
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

export async function getVPNProfiles(): Promise<ApiResponse<VPNProfile[]>> {
    return api.get<VPNProfile[]>("/api/v1/network/vpn/profiles")
}

export async function createVPNProfile(input: VPNProfileInput): Promise<ApiResponse<VPNProfile>> {
    return api.post<VPNProfile>("/api/v1/network/vpn/profiles", input)
}

export async function updateVPNProfile(
    id: number,
    input: VPNProfileInput,
): Promise<ApiResponse<VPNProfile>> {
    return api.put<VPNProfile>(`/api/v1/network/vpn/profiles/${id}`, input)
}

export async function deleteVPNProfile(id: number): Promise<ApiResponse<null>> {
    return api.delete<null>(`/api/v1/network/vpn/profiles/${id}`)
}

/** A dry run: shows what the paste means without storing anything. */
export async function parseVPNInput(raw: string): Promise<ApiResponse<WireGuardConfig>> {
    return api.post<WireGuardConfig>("/api/v1/network/vpn/parse", { raw })
}

export async function generateVPNKeypair(): Promise<
    ApiResponse<{ private_key: string; public_key: string }>
> {
    return api.post<{ private_key: string; public_key: string }>("/api/v1/network/vpn/keypair")
}

/** Rewrites routes and the firewall, so it arms the dead-man. */
export async function enableVPNProfile(id: number): Promise<ApiResponse<NetworkApply>> {
    return api.post<NetworkApply>(`/api/v1/network/vpn/profiles/${id}/enable`)
}

export async function disableVPNProfile(id: number): Promise<ApiResponse<NetworkApply>> {
    return api.post<NetworkApply>(`/api/v1/network/vpn/profiles/${id}/disable`)
}

/** Empty uplinkKey clears the pin and hands the tunnel back to the deal. */
export async function setVPNProfileTransport(
    id: number,
    uplinkKey: string,
): Promise<ApiResponse<null>> {
    return api.patch<null>(`/api/v1/network/vpn/profiles/${id}/transport`, {
        uplink_key: uplinkKey,
    })
}

/** spread, order or fastest. */
export async function setVPNPoolStrategy(strategy: PoolStrategy): Promise<ApiResponse<null>> {
    return api.patch<null>("/api/v1/network/vpn/pool/strategy", { strategy })
}

/** The whole order, first to last. A partial one is refused. */
export async function setVPNPoolOrder(ids: number[]): Promise<ApiResponse<null>> {
    return api.patch<null>("/api/v1/network/vpn/pool/order", { ids })
}

export async function getVPNStatus(): Promise<ApiResponse<VPNPoolStatus>> {
    return api.get<VPNPoolStatus>("/api/v1/network/vpn/status")
}
