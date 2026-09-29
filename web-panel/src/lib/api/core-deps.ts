import { api, type ApiResponse } from "@/lib/api"
import { getApiBaseUrl } from "@/lib/config"

const BASE = "/api/v1/cores"

// ---- Types (mirror the hub JSON shaping in install_handler.go) ----

export type DepStatus = {
    name: string
    kind: "binary" | string
    present: boolean
    version?: string
    path?: string
    optional: boolean
}

export type CoreDepReport = {
    core_type: string
    ready: boolean
    deps: DepStatus[]
}

export type InstallStepState = "pending" | "running" | "ok" | "skipped" | "failed"

export type InstallStep = {
    name: string
    core_type: string
    kind: string
    state: InstallStepState
    detail?: string
}

export type InstallStatus = "pending" | "running" | "succeeded" | "partial" | "failed"

export type InstallProgress = {
    status: InstallStatus
    steps: InstallStep[]
    log_lines: string[]
    done: boolean
}

export type StartInstallBody = {
    cores: string[]
    uninstall?: boolean
}

// ---- Unary API ----

export async function probeNodeDeps(
    nodeId: number,
    cores?: string[],
): Promise<ApiResponse<{ reports: CoreDepReport[] | null }>> {
    const q = cores && cores.length ? `?cores=${encodeURIComponent(cores.join(","))}` : ""
    return api.get<{ reports: CoreDepReport[] | null }>(`${BASE}/nodes/${nodeId}/deps${q}`)
}

export async function startDepInstall(
    nodeId: number,
    body: StartInstallBody,
): Promise<ApiResponse<{ job_id: string }>> {
    return api.post<{ job_id: string }>(`${BASE}/nodes/${nodeId}/deps/install`, body)
}

// ---- SSE progress stream (mirrors nuke.ts frame parsing) ----

export type DepInstallCallbacks = {
    onProgress: (p: InstallProgress) => void
    onDone: (p: InstallProgress) => void
    onError: (e: Error) => void
}

export async function streamDepInstall(
    nodeId: number,
    jobId: string,
    cbs: DepInstallCallbacks,
    signal?: AbortSignal,
): Promise<void> {
    const res = await fetch(
        `${getApiBaseUrl()}${BASE}/nodes/${nodeId}/deps/install/${encodeURIComponent(jobId)}/stream`,
        { credentials: "include", signal },
    )
    if (!res.ok || !res.body) {
        throw new Error(`install stream failed: ${res.status} ${res.statusText}`)
    }
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
    let buffer = ""
    while (true) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += value
        let idx: number
        while ((idx = buffer.indexOf("\n\n")) !== -1) {
            const frame = buffer.slice(0, idx)
            buffer = buffer.slice(idx + 2)
            parseFrame(frame, cbs)
        }
    }
}

function parseFrame(frame: string, cbs: DepInstallCallbacks): void {
    let eventName = "message"
    let dataStr = ""
    // The progress frames carry "event: "; gin's own error frames omit the space.
    for (const line of frame.split("\n")) {
        if (line.startsWith("event:")) eventName = line.slice(6).trim()
        else if (line.startsWith("data:")) dataStr += (dataStr ? "\n" : "") + line.slice(5).replace(/^ /, "")
    }
    if (!dataStr) return
    if (eventName === "error") {
        cbs.onError(new Error(dataStr))
        return
    }
    try {
        const payload = JSON.parse(dataStr) as InstallProgress
        cbs.onProgress(payload)
        if (payload.done) cbs.onDone(payload)
    } catch (e) {
        cbs.onError(e instanceof Error ? e : new Error(String(e)))
    }
}
