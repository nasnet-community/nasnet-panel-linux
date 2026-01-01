import { useQuery } from "@tanstack/react-query"
import { auditApi, type AuditListParams } from "@/lib/api/audit"
import { queryKeys } from "./keys"

export function useAuditLogs(params: AuditListParams = {}) {
    return useQuery({
        queryKey: queryKeys.auditLogs(params),
        queryFn: async () => {
            return await auditApi.list(params)
        },
        placeholderData: (prev) => prev,
    })
}
