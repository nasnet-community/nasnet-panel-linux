import { api } from "@/lib/api"
import { getApiBaseUrl } from "@/lib/config"

export type NukePhaseResult = {
    phase: string
    ok: boolean
    skipped: boolean
    error?: string
    duration_ms: number
    bytes_removed: number
    files_removed: number
}

export type NukeReport = {
    mode: string
    dry_run: boolean
    phases: NukePhaseResult[]
    result: "NUKE_RESULT_SUCCESS" | "NUKE_RESULT_PARTIAL" | "NUKE_RESULT_FAILED"
    total_duration_ms: number
}

// ==================== Wipe (unary) ====================

export type WipeOptions = {
    dryRun?: boolean
    alsoRemoveHubRecord?: boolean
}

export interface WipeResponse {
    success: boolean
    report: NukeReport
    error?: string
}

export type WipeResult = {
    report: NukeReport
    error?: string  // populated when all phases failed
}

export async function wipeNode(nodeId: number, opts: WipeOptions = {}): Promise<WipeResult> {
    const res = await api.post<WipeResponse>(`/api/v1/nodes/${nodeId}/wipe`, {
        dry_run: opts.dryRun ?? false,
        also_remove_hub_record: opts.alsoRemoveHubRecord ?? false,
    })
    // Both success=true (full/partial) and success=false (ErrNukeFailed) carry a report.
    if (res.data?.report) {
        return { report: res.data.report, error: res.data.error }
    }
    throw new Error(res.data?.error ?? res.error ?? "wipe failed")
}

// ==================== Nuke (SSE stream) ====================

export type NukeOptions = {
    dryRun?: boolean
    shredRoot?: boolean
    keepHubRecord?: boolean
}

export type NukeStreamCallbacks = {
    onPhase: (p: NukePhaseResult) => void
    onDone: (r: NukeReport) => void
    onError: (e: Error) => void
}

export async function nukeNode(
    nodeId: number,
    opts: NukeOptions,
    cbs: NukeStreamCallbacks,
    signal?: AbortSignal,
): Promise<void> {
    const baseUrl = getApiBaseUrl()

    const res = await fetch(`${baseUrl}/api/v1/nodes/${nodeId}/nuke`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
            dry_run: opts.dryRun ?? false,
            shred_root: opts.shredRoot ?? false,
            keep_hub_record: opts.keepHubRecord ?? false,
        }),
        signal,
    })

    if (!res.ok || !res.body) {
        throw new Error(`nuke request failed: ${res.status} ${res.statusText}`)
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

function parseFrame(frame: string, cbs: NukeStreamCallbacks): void {
    let eventName = "message"
    let dataStr = ""
    for (const line of frame.split("\n")) {
        if (line.startsWith("event: ")) eventName = line.slice(7).trim()
        else if (line.startsWith("data: ")) dataStr += (dataStr ? '\n' : '') + line.slice(6)
    }
    if (!dataStr) return
    try {
        const payload = JSON.parse(dataStr)
        if (eventName === "phase") cbs.onPhase(payload as NukePhaseResult)
        else if (eventName === "done") cbs.onDone(payload as NukeReport)
        else if (eventName === "error") cbs.onError(new Error(payload.error ?? "stream error"))
    } catch (e) {
        cbs.onError(e instanceof Error ? e : new Error(String(e)))
    }
}
