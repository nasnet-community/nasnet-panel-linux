import { api, type ApiResponse } from "@/lib/api"
import { buildQueryString } from "./utils"
import type {
    AccessHistoryParams,
    AccessHistoryResponse,
    AccessHistorySearchParams,
    AccessHistorySearchResponse,
    AccessHistoryGlobalSearchParams,
    AccessHistoryGlobalSearchResponse,
} from "@/lib/types"

export async function getSubscriptionAccessHistory(
    subId: number,
    params: AccessHistoryParams,
): Promise<ApiResponse<AccessHistoryResponse>> {
    return api.get<AccessHistoryResponse>(
        `/api/v1/subscriptions/${subId}/access-history${buildQueryString({
            from: params.from,
            to: params.to,
            granularity: params.granularity,
            node_ids: params.node_ids,
            top_n: params.top_n,
            include_ips: params.include_ips,
        })}`,
    )
}

export async function searchSubscriptionAccessHistory(
    subId: number,
    params: AccessHistorySearchParams,
): Promise<ApiResponse<AccessHistorySearchResponse>> {
    return api.get<AccessHistorySearchResponse>(
        `/api/v1/subscriptions/${subId}/access-history/search${buildQueryString({
            from: params.from,
            to: params.to,
            q: params.q,
            kinds: params.kinds,
            node_ids: params.node_ids,
            limit: params.limit,
            include_ips: params.include_ips,
        })}`,
    )
}

export async function searchGlobalAccessHistory(
    params: AccessHistoryGlobalSearchParams,
): Promise<ApiResponse<AccessHistoryGlobalSearchResponse>> {
    return api.get<AccessHistoryGlobalSearchResponse>(
        `/api/v1/access-history/search${buildQueryString({
            from: params.from,
            to: params.to,
            q: params.q,
            kinds: params.kinds,
            node_ids: params.node_ids,
            subscription_ids: params.subscription_ids,
            emails: params.emails,
            limit: params.limit,
            include_ips: params.include_ips,
        })}`,
    )
}
