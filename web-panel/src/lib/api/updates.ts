import { api, ApiError } from "@/lib/api"
import { getApiBaseUrl } from "@/lib/config"

const BASE = "/api/v1/updates"

export type UpdateStepStatus = "waiting" | "running" | "done" | "failed" | "skipped"

export interface UpdateStep {
    id: string
    label: string
    status: UpdateStepStatus
    detail?: string
}

export interface UpdateJob {
    id: string
    kind: "update" | "rollback"
    from: string
    to: string
    source: "download" | "upload" | "prev"
    upload_id?: string
    route?: string
    phase: "panel" | "runner" | "done"
    result?: "ok" | "failed" | "rolled_back"
    error?: string
    steps: UpdateStep[]
    started_at: string
    finished_at?: string
}

/** A release as the section shows it, from its signed release.json. */
export interface UpdateOffer {
    version: string
    prerelease: boolean
    notes: string
    size: number
    sha256: string
    min_from?: string
    xray: string
    built_at: string
}

export interface UpdateCheck {
    checked_at: string
    channel: "stable" | "beta"
    route?: "direct" | "proxy" | "mirror"
    latest?: UpdateOffer
    available: boolean
    /** newer, but not installable from here */
    blocked?: string
    error?: string
    feed_error?: string
}

export interface UpdateKind {
    name: "systemd" | "docker" | "source" | "not_root"
    why?: string
    command?: string
}

export interface UpdateSchedule {
    version: string
    upload_id?: string
    size: number
    at: string
}

export interface RollbackWindow {
    version: string
    until: string
}

export interface PanelUpdate {
    id: number
    job_id: string
    kind: "update" | "rollback"
    from: string
    to: string
    result: "ok" | "failed" | "rolled_back"
    reason?: string
    started_at: string
    finished_at: string
}

export interface NodeXray {
    id: number
    name: string
    version: string
    local?: boolean
}

export interface UpdateParts {
    agents?: { total: number; due: number[] }
    xray?: { tested: string; current: number; older: NodeXray[]; newer: NodeXray[] }
    geofiles: { built_at?: string }
}

export interface UpdateStatus {
    current: string
    arch: string
    kind: UpdateKind
    channel: "stable" | "beta"
    check_enabled: boolean
    mirror_url: string
    tested_xray: string
    built_at?: string
    net_pending?: boolean
    check?: UpdateCheck
    available?: UpdateOffer
    job?: UpdateJob
    schedule?: UpdateSchedule
    rollback?: RollbackWindow
    history: PanelUpdate[]
    parts: UpdateParts
}

/** A bundle the server took, as its signed bundle.json describes it. */
export interface UpdateUpload {
    upload_id: string
    version: string
    prerelease: boolean
    arch: string
    min_from?: string
    notes: string
    xray: string
    size: number
    built_at: string
    uploaded_at: string
}

export interface ApplyRequest {
    version?: string
    upload_id?: string
    /** ISO time: schedule instead of starting now */
    at?: string
}

function need<T>(data: T | undefined, error?: string): T {
    if (data === undefined) throw new Error(error || "The panel didn't answer")
    return data
}

export const updatesApi = {
    status: async (): Promise<UpdateStatus> => {
        const resp = await api.get<UpdateStatus>(BASE)
        return need(resp.data, resp.error)
    },

    check: async (): Promise<UpdateCheck> => {
        // a slow route can take a while: direct, then the proxy, then the mirror
        const resp = await api.post<UpdateCheck>(`${BASE}/check`, undefined, 120_000)
        return need(resp.data, resp.error)
    },

    apply: async (req: ApplyRequest): Promise<{ job_id?: string; schedule?: UpdateSchedule }> => {
        const resp = await api.post<{ job_id?: string; schedule?: UpdateSchedule }>(`${BASE}/apply`, req)
        return need(resp.data, resp.error)
    },

    cancelSchedule: async (): Promise<UpdateSchedule> => {
        const resp = await api.delete<{ schedule: UpdateSchedule }>(`${BASE}/schedule`)
        return need(resp.data, resp.error).schedule
    },

    rollback: async (): Promise<string> => {
        const resp = await api.post<{ job_id: string }>(`${BASE}/rollback`)
        return need(resp.data, resp.error).job_id
    },

    /** Streams the bundle up with progress; no timeout, since 300 MB can take a while. */
    upload: (file: File, onProgress?: (sent: number, total: number) => void, signal?: AbortSignal): Promise<UpdateUpload> =>
        new Promise((resolve, reject) => {
            const xhr = new XMLHttpRequest()
            xhr.open("POST", `${getApiBaseUrl()}${BASE}/upload`)
            xhr.withCredentials = true
            xhr.upload.onprogress = e => { if (e.lengthComputable) onProgress?.(e.loaded, e.total) }
            xhr.onload = () => {
                let body: { success?: boolean; data?: UpdateUpload; error?: string } = {}
                try { body = JSON.parse(xhr.responseText) } catch { /* not JSON */ }
                if (xhr.status >= 200 && xhr.status < 300 && body.data) resolve(body.data)
                else reject(new ApiError(body.error || `Upload failed (HTTP ${xhr.status})`, undefined, xhr.status, body))
            }
            xhr.onerror = () => reject(new Error("The upload was cut off"))
            xhr.onabort = () => reject(new DOMException("Upload cancelled", "AbortError"))
            signal?.addEventListener("abort", () => xhr.abort())
            const form = new FormData()
            form.append("file", file)
            xhr.send(form)
        }),
}

export interface UpdateJobCallbacks {
    onProgress: (job: UpdateJob) => void
    onDone: (job: UpdateJob) => void
}

/**
 * Follows a job until it ends or the stream breaks, which it does when the
 * panel restarts: the caller then polls until the new version answers.
 */
export async function streamUpdateJob(jobId: string, cbs: UpdateJobCallbacks, signal?: AbortSignal): Promise<void> {
    const res = await fetch(`${getApiBaseUrl()}${BASE}/jobs/${encodeURIComponent(jobId)}/stream`, { credentials: "include", signal })
    if (!res.ok || !res.body) throw new Error(`progress stream failed: ${res.status}`)
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
    let buffer = ""
    while (true) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += value
        let idx: number
        while ((idx = buffer.indexOf("\n\n")) !== -1) {
            const job = parseUpdateFrame(buffer.slice(0, idx))
            buffer = buffer.slice(idx + 2)
            if (!job) continue
            cbs.onProgress(job)
            if (job.phase === "done") cbs.onDone(job)
        }
    }
}

export function parseUpdateFrame(frame: string): UpdateJob | null {
    let data = ""
    for (const line of frame.split("\n")) {
        if (line.startsWith("data:")) data += (data ? "\n" : "") + line.slice(5).replace(/^ /, "")
    }
    if (!data) return null
    try {
        return JSON.parse(data) as UpdateJob
    } catch {
        return null
    }
}
