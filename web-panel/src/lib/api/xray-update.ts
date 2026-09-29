import { api, type ApiResponse } from "@/lib/api"
import { getApiBaseUrl } from "@/lib/config"
import type { XrayRelease } from "./nodes"

export interface XrayCatalog extends ApiResponse<XrayRelease[]> {
    checked_at: string
    cached: boolean
}
export interface XrayRoutes {
    release_checks: "proxy" | "direct" | "environment_proxy"
    binary_download: "proxy" | "direct" | "environment_proxy"
    /** Router mode: the uplink group the request leaves through ("" otherwise). */
    release_checks_egress?: string
    binary_download_egress?: string
}
export interface XrayUpdateProgress {
    stage: "preparing" | "downloading" | "installing" | "verifying" | "succeeded" | "failed"
    detail: string
}
export const getXrayCatalog = (refresh = false) => api.getRaw<XrayCatalog>(`/api/v1/nodes/xray/releases${refresh ? "?refresh=true" : ""}`)
export const getXrayRoutes = () => api.get<XrayRoutes>("/api/v1/nodes/xray/routes")

// Never retry this POST automatically: it changes the installed binary.
export async function streamXrayUpdate(nodeId: number, version: string, onProgress: (progress: XrayUpdateProgress) => void, signal?: AbortSignal) {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/nodes/${nodeId}/agent/xray-version?stream=true`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ version }), signal,
    })
    if (!response.ok) {
        const body = await response.json().catch(() => null)
        throw new Error(body?.error || `Update request failed (${response.status})`)
    }
    if (!response.body) throw new Error("Update progress is unavailable. Check node status before retrying.")
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ""
    let completed = false
    const consume = (line: string) => {
        if (!line.trim()) return
        const progress = JSON.parse(line) as XrayUpdateProgress
        onProgress(progress)
        if (progress.stage === "failed" || progress.stage === "succeeded") completed = true
    }
    try {
        while (true) {
            const { done, value } = await reader.read()
            buffer += decoder.decode(value, { stream: !done })
            let newline: number
            while ((newline = buffer.indexOf("\n")) >= 0) {
                consume(buffer.slice(0, newline))
                buffer = buffer.slice(newline + 1)
            }
            if (done) break
        }
        consume(buffer)
        if (!completed) throw new Error("Progress connection ended before verification. Check node status before retrying.")
    } finally { reader.releaseLock() }
}
