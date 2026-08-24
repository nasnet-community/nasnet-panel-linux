import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
    applyNetworkChange,
    confirmNetworkApply,
    createPortForward,
    createVPNProfile,
    deletePortForward,
    deleteVPNProfile,
    disableVPNProfile,
    enableVPNProfile,
    generateVPNKeypair,
    getLAN,
    getLANDevices,
    getNetworkInterfaces,
    getNetworkState,
    getPortForwards,
    getVPNProfiles,
    getVPNStatus,
    parseVPNInput,
    planNetworkChange,
    rollbackNetworkApply,
    setDeviceLabel,
    setInterfaceLabel,
    setVPNProfileRole,
    setVPNProfileTransport,
    updateLAN,
    updateVPNProfile,
    updatePortForward,
    type PortForwardInput,
} from "@/lib/api/network"
import { queryKeys } from "@/lib/queries/keys"
import type { AssignRoleRequest, LANConfig, VPNProfileInput } from "@/lib/types/network"

/** Router mode off 404s every route; not worth retrying, so the page can hide. */
export function useNetworkInterfaces(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkInterfaces(),
        queryFn: async () => {
            const res = await getNetworkInterfaces()
            if (!res.success) throw new Error(res.error || "Failed to fetch interfaces")
            return res.data!
        },
        enabled,
        staleTime: 10 * 1000,
        retry: false,
    })
}

export function useNetworkState(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkState(),
        queryFn: async () => {
            const res = await getNetworkState()
            if (!res.success) throw new Error(res.error || "Failed to fetch network state")
            return res.data!
        },
        enabled,
        staleTime: 5 * 1000,
        refetchInterval: 15 * 1000,
        retry: false,
    })
}

export function usePlanNetworkChange() {
    return useMutation({
        mutationFn: async (req: AssignRoleRequest) => {
            const res = await planNetworkChange(req)
            if (!res.success) throw new Error(res.error || "Failed to plan the change")
            return res.data!
        },
    })
}

export function useApplyNetworkChange() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (req: AssignRoleRequest) => {
            const res = await applyNetworkChange(req)
            if (!res.success) throw new Error(res.error || "Failed to apply the change")
            return res.data!
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

export function useConfirmNetworkApply() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (planId: number) => {
            const res = await confirmNetworkApply(planId)
            if (!res.success) throw new Error(res.error || "Failed to confirm")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

export function useRollbackNetworkApply() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async () => {
            const res = await rollbackNetworkApply()
            if (!res.success) throw new Error(res.error || "Failed to roll back")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

export function useLAN(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkLAN(),
        queryFn: async () => {
            const res = await getLAN()
            if (!res.success) throw new Error(res.error || "Failed to fetch the LAN config")
            return res.data!
        },
        enabled,
        staleTime: 10 * 1000,
        retry: false,
    })
}

/** Enabling the LAN goes through the two-phase apply, so this returns a plan id
 *  and a confirm deadline rather than taking effect. */
export function useUpdateLAN() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (cfg: Partial<LANConfig>) => {
            const res = await updateLAN(cfg)
            if (!res.success) throw new Error(res.error || "Failed to update the LAN")
            return res.data!
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

/** Polls while the tab is visible. Arrivals land on the device's first frame;
 *  departures wait out the bridge ageing time, so a faster poll buys nothing. */
export function useLANDevices(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkLANDevices(),
        queryFn: async () => {
            const res = await getLANDevices()
            if (!res.success) throw new Error(res.error || "Failed to read the connected devices")
            return res.data!
        },
        enabled,
        refetchInterval: enabled ? 10 * 1000 : false,
        refetchIntervalInBackground: false,
        staleTime: 5 * 1000,
        retry: false,
    })
}

export function useSetInterfaceLabel() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ key, label }: { key: string; label: string }) => {
            const res = await setInterfaceLabel(key, label)
            if (!res.success) throw new Error(res.error || "Failed to save the name")
            return res.data
        },
        onSuccess: () => {
            // The name shows on the ports table, the health cards and the pool's via column.
            void qc.invalidateQueries({ queryKey: queryKeys.networkInterfaces() })
            void qc.invalidateQueries({ queryKey: queryKeys.networkState() })
            void qc.invalidateQueries({ queryKey: queryKeys.networkVPNStatus() })
        },
    })
}

export function useSetDeviceLabel() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ mac, label }: { mac: string; label: string }) => {
            const res = await setDeviceLabel(mac, label)
            if (!res.success) throw new Error(res.error || "Failed to save the name")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkLANDevices() })
        },
    })
}

export function usePortForwards(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkPortForwards(),
        queryFn: async () => {
            const res = await getPortForwards()
            if (!res.success) throw new Error(res.error || "Failed to fetch port forwards")
            return res.data!
        },
        enabled,
        staleTime: 10 * 1000,
        retry: false,
    })
}

/** Forwards touch only the nft table, not addressing, so they apply at once —
 *  no dead-man countdown, unlike the LAN. */
export function useSavePortForward() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (pf: PortForwardInput) => {
            const res = pf.id ? await updatePortForward(pf.id, pf) : await createPortForward(pf)
            if (!res.success) throw new Error(res.error || "Failed to save the forward")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkPortForwards() })
        },
    })
}

export function useDeletePortForward() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deletePortForward(id)
            if (!res.success) throw new Error(res.error || "Failed to delete the forward")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkPortForwards() })
        },
    })
}

export function useVPNProfiles(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkVPNProfiles(),
        queryFn: async () => {
            const res = await getVPNProfiles()
            if (!res.success) throw new Error(res.error || "Failed to read the VPN profiles")
            return res.data!
        },
        enabled,
        staleTime: 10 * 1000,
        retry: false,
    })
}

/** Polls while the tab is visible. The handshake age is the only liveness
 *  signal WireGuard gives, and it moves slowly. */
export function useVPNStatus(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkVPNStatus(),
        queryFn: async () => {
            const res = await getVPNStatus()
            if (!res.success) throw new Error(res.error || "Failed to read the VPN status")
            return res.data!
        },
        enabled,
        refetchInterval: enabled ? 5 * 1000 : false,
        refetchIntervalInBackground: false,
        staleTime: 2 * 1000,
        retry: false,
    })
}

export function useSaveVPNProfile() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, ...input }: VPNProfileInput & { id?: number }) => {
            const res = id ? await updateVPNProfile(id, input) : await createVPNProfile(input)
            if (!res.success) throw new Error(res.error || "Failed to save the VPN")
            return res.data!
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkVPNProfiles() })
        },
    })
}

export function useDeleteVPNProfile() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteVPNProfile(id)
            if (!res.success) throw new Error(res.error || "Failed to delete the VPN")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkVPNProfiles() })
        },
    })
}

/** The server's own parser, so the preview can't disagree with what's stored. */
export function useParseVPNInput() {
    return useMutation({
        mutationFn: async (raw: string) => {
            const res = await parseVPNInput(raw)
            if (!res.success) throw new Error(res.error || "This is not a WireGuard config")
            return { config: res.data!, verdicts: res.verdicts ?? [] }
        },
    })
}

export function useGenerateVPNKeypair() {
    return useMutation({
        mutationFn: async () => {
            const res = await generateVPNKeypair()
            if (!res.success) throw new Error(res.error || "Failed to generate a key")
            return res.data!
        },
    })
}

/** These rewrite routes and the firewall, so invalidate the whole subtree. */
export function useEnableVPNProfile() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await enableVPNProfile(id)
            if (!res.success) throw new Error(res.error || "Failed to turn the VPN on")
            // V33 rides along with a 200, so keep it.
            return { ...res.data!, verdicts: res.verdicts ?? [] }
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

export function useDisableVPNProfile() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await disableVPNProfile(id)
            if (!res.success) throw new Error(res.error || "Failed to turn the VPN off")
            return { ...res.data!, verdicts: res.verdicts ?? [] }
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

/** Same blast radius as a role edit: one tunnel re-handshakes, nothing else. */
export function useSetVPNTransport() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, uplinkKey }: { id: number; uplinkKey: string }) => {
            const res = await setVPNProfileTransport(id, uplinkKey)
            if (!res.success) throw new Error(res.error || "Failed to change the tunnel's uplink")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkVPNProfiles() })
            void qc.invalidateQueries({ queryKey: queryKeys.networkVPNStatus() })
            void qc.invalidateQueries({ queryKey: queryKeys.networkHealth() })
        },
    })
}

/** Redistribution only; refresh the pool views, not the whole subtree. */
export function useSetVPNRole() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, priority, weight }: { id: number; priority: number; weight: number }) => {
            const res = await setVPNProfileRole(id, { priority, weight })
            if (!res.success) throw new Error(res.error || "Failed to change the tunnel's role")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkVPNProfiles() })
            void qc.invalidateQueries({ queryKey: queryKeys.networkVPNStatus() })
            void qc.invalidateQueries({ queryKey: queryKeys.networkHealth() })
        },
    })
}
