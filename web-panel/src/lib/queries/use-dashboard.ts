import { useQuery } from "@tanstack/react-query"
import { getDashboardStats, getOnlineUsers, getOnlineUsersWithIPs, getOnlineUsersHistory, listNodes } from "@/lib/admin-api"
import { queryKeys } from "./keys"

// Dashboard stats query
export function useDashboardStats() {
    return useQuery({
        queryKey: queryKeys.dashboardStats(),
        queryFn: async () => {
            const res = await getDashboardStats()
            if (!res.success) throw new Error(res.error || "Failed to fetch dashboard stats")
            return res.data!
        },
    })
}

// Online users count
export function useOnlineUsers() {
    return useQuery({
        queryKey: queryKeys.onlineUsers(),
        queryFn: async () => {
            const res = await getOnlineUsers()
            if (!res.success) throw new Error(res.error || "Failed to fetch online users")
            return res.data || []
        },
        staleTime: 10 * 1000, // Refresh more frequently for real-time data
    })
}

// Online users with their connected IPs
export function useOnlineUsersWithIPs() {
    return useQuery({
        queryKey: queryKeys.onlineUsersWithIPs(),
        queryFn: async () => {
            const res = await getOnlineUsersWithIPs()
            if (!res.success) throw new Error(res.error || "Failed to fetch online users with IPs")
            return res.data || {}
        },
        staleTime: 10 * 1000,
    })
}

// Nodes summary for dashboard widget
export function useNodesSummary(limit = 4) {
    return useQuery({
        queryKey: queryKeys.nodeList(),
        queryFn: async () => {
            const res = await listNodes()
            if (!res.success) throw new Error(res.error || "Failed to fetch nodes")
            return res.data || []
        },
        select: (data) => data.slice(0, limit), // Only take first N nodes for summary
    })
}

// Dedup'd global online-user history for sidebar sparkline.
// Backend writes one snapshot per scheduler tick (default 5s);
// we poll at 15s so we don't burn requests faster than new data arrives.
export function useOnlineUsersHistory(minutes: number = 15) {
    return useQuery({
        queryKey: queryKeys.onlineUsersHistory(minutes),
        queryFn: async () => {
            const res = await getOnlineUsersHistory(minutes)
            if (!res.success) throw new Error(res.error || "Failed to fetch online users history")
            return res.data?.points ?? []
        },
        refetchInterval: 15_000,
        staleTime: 10_000,
    })
}
